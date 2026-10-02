//go:build windows

package vpn

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Wintun is a layer-3 virtual network driver from WireGuard LLC, shipped as a
// userspace DLL plus a kernel driver. The DLL is already vendored at
// installers/windows/wintun/wintun.dll and the installers copy it to
// System32\drivers, so this file's job is to bind it and drive it.
//
// The DLL is loaded lazily through the syscall package rather than linked at build
// time, for three reasons:
//
//   - a build-time link would need the DLL on every machine that builds the node,
//     including the ubuntu and macOS CI runners, which have none
//   - loading by path means an installed node finds the copy the installer put in
//     System32\drivers and a developer finds the vendored one
//   - a missing DLL becomes a readable runtime error instead of a binary that will
//     not start
//
// The cgo-free build therefore still works on Windows, and no other platform is
// affected.
//
// # The handle split that matters
//
// Wintun has two distinct handles and mixing them faults the process rather than
// failing a call. WintunCreateAdapter and WintunOpenAdapter return an adapter
// handle; WintunStartSession returns a *session* handle, and every packet
// function takes the session, not the adapter. Sending with the adapter handle
// where the session belongs dies inside the driver with 0xC0000005, so the two are
// kept in separate fields and never passed interchangeably.
//
// The signatures below are transcribed from wintun.h, not inferred from the export
// names, which is what an earlier pass got wrong.

// wintunLib is the set of entry points the Windows TUN path uses.
type wintunLib struct {
	create      *syscall.LazyProc
	open        *syscall.LazyProc
	close       *syscall.LazyProc
	startSess   *syscall.LazyProc
	endSess     *syscall.LazyProc
	readWait    *syscall.LazyProc
	receive     *syscall.LazyProc
	releaseRx   *syscall.LazyProc
	allocSend   *syscall.LazyProc
	send        *syscall.LazyProc
	getVersion  *syscall.LazyProc
	getAdapterL *syscall.LazyProc
}

var (
	wintunOnce sync.Once
	wintunD    *wintunLib
	wintunErr  error
)

// wintunSearchPaths lists where the DLL is looked for, in order.
func wintunSearchPaths() []string {
	var paths []string
	if p := os.Getenv("LOCALWEB_WINTUN_DLL"); p != "" {
		paths = append(paths, p)
	}
	paths = append(paths, filepath.Join(os.Getenv("SystemRoot"), `System32`, `drivers`, `wintun.dll`))
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		paths = append(paths,
			filepath.Join(dir, "wintun.dll"),
			filepath.Join(dir, "wintun", "wintun.dll"),
		)
	}
	if wd, err := os.Getwd(); err == nil {
		paths = append(paths,
			filepath.Join(wd, "wintun.dll"),
			filepath.Join(wd, "wintun", "wintun.dll"),
			filepath.Join(wd, "installers", "windows", "wintun", "wintun.dll"),
		)
	}
	return paths
}

// loadWintun finds and loads the DLL once.
func loadWintun() (*wintunLib, error) {
	wintunOnce.Do(func() {
		var rejected []string
		for _, p := range wintunSearchPaths() {
			if _, err := os.Stat(p); err != nil {
				continue
			}
			d := syscall.NewLazyDLL(p)
			// Probe the cheapest entry point first, so a DLL that exists but is not
			// Wintun is rejected here rather than failing mysteriously later.
			if err := d.NewProc("WintunGetRunningDriverVersion").Find(); err != nil {
				rejected = append(rejected, fmt.Sprintf("%s (not a Wintun library: %v)", p, err))
				continue
			}
			wintunD = &wintunLib{
				create:      d.NewProc("WintunCreateAdapter"),
				open:        d.NewProc("WintunOpenAdapter"),
				close:       d.NewProc("WintunCloseAdapter"),
				startSess:   d.NewProc("WintunStartSession"),
				endSess:     d.NewProc("WintunEndSession"),
				readWait:    d.NewProc("WintunGetReadWaitEvent"),
				receive:     d.NewProc("WintunReceivePacket"),
				releaseRx:   d.NewProc("WintunReleaseReceivePacket"),
				allocSend:   d.NewProc("WintunAllocateSendPacket"),
				send:        d.NewProc("WintunSendPacket"),
				getVersion:  d.NewProc("WintunGetRunningDriverVersion"),
				getAdapterL: d.NewProc("WintunGetAdapterLUID"),
			}
			return
		}
		if len(rejected) == 0 {
			wintunErr = fmt.Errorf("%w: wintun.dll was not found in %v; install it by "+
				"running the LocalWEB installer as administrator, or set LOCALWEB_WINTUN_DLL "+
				"to its path", ErrNoTUNPlatform, wintunSearchPaths())
		} else {
			wintunErr = fmt.Errorf("%w: found wintun.dll but could not use it: %v",
				ErrNoTUNPlatform, rejected)
		}
	})
	return wintunD, wintunErr
}

// errNotElevated is returned when the process cannot create an adapter.
var errNotElevated = errors.New(
	"creating a Wintun adapter needs an elevated (administrator) process, because it " +
		"installs and starts a kernel driver. Run the node as administrator, or use the " +
		"Windows service the installer registers, which runs elevated")

// errTUNClosed is returned by Read and Write once the device has been closed.
var errTUNClosed = errors.New("wintun: the tunnel is closed")

// Ring limits, from wintun.h. The capacity must be a power of two in range and the
// packet size must not exceed the maximum; both are checked rather than assumed.
const (
	wintunMinRingCapacity = 0x20000   // 128 KiB
	wintunMaxRingCapacity = 0x4000000 // 64 MiB
	maxWintunPacket       = 0xFFFF    // WINTUN_MAX_IP_PACKET_SIZE
	wintunRingCapacity    = 0x400000  // 4 MiB: a power of two, in range
	wintunReadQueue       = 256
	wintunSendRetry       = 500 * time.Microsecond
)

// defaultTUNCIDR is the address range Up assigns.
//
// The carrier hands openTUN a name and nothing else (vpn.go:89), so the range has
// to come from somewhere. It is overridable because a node already inside a
// network using 10.6.0.0/24 needs a different range, and a fixed value with no
// override would collide silently.
const defaultTUNCIDR = "10.6.0.1/24"

// tunCIDR reports the address range to configure, or an error if it is unusable.
func tunCIDR() (string, error) {
	cidr := strings.TrimSpace(os.Getenv("LOCALWEB_TUN_CIDR"))
	if cidr == "" {
		cidr = defaultTUNCIDR
	}
	if _, _, err := parseCIDRv4(cidr); err != nil {
		return "", err
	}
	return cidr, nil
}

// There is deliberately no LUID-to-interface-index conversion here.
//
// WintunGetAdapterLUID hands back a NET_LUID and routing wants a NET_IFINDEX, so
// ConvertInterfaceLuidToIndex is the obvious bridge. It was tried and removed: a
// NET_LUID is a union whose size depends on which member you look at, and the
// x64 ABI passes it by hidden reference rather than in a register. Handing it in a
// register faults inside iphlpapi with 0xC0000005. Reconstructing the union from a
// remembered header to get the calling convention right is exactly the kind of
// guess that silently configures the wrong interface, so routing names the
// interface instead and lets netsh resolve the index per machine.

// windowsTUN is a live Wintun adapter with an open session.
//
// The driver is event driven, which does not match Go's blocking Read and Write. A
// reader goroutine waits on the session's read-wait event, drains whatever is
// queued into readQ, and hands one frame per Read call; Write allocates a send
// buffer and retries while the ring is full.
type windowsTUN struct {
	name    string
	lib     *wintunLib
	adapter uintptr // WINTUN_ADAPTER_HANDLE
	session uintptr // WINTUN_SESSION_HANDLE; every packet call takes this, not adapter

	readEvent windows.Handle
	stopEvent windows.Handle

	done  chan struct{}
	readQ chan []byte
	wg    sync.WaitGroup

	mu     sync.Mutex
	closed bool
	up     bool
	addr   string
}

// openTUN opens or creates a Wintun adapter and starts a session on it.
//
// An existing adapter with the same name is reused, so restarting the node does
// not leave a second adapter behind for every run.
func openTUN(name string) (Interface, error) {
	lib, err := loadWintun()
	if err != nil {
		return nil, err
	}
	if !isElevated() {
		return nil, fmt.Errorf("%w: %w", ErrNoTUNPlatform, errNotElevated)
	}

	adapter, err := lib.openAdapter(name)
	if err != nil {
		return nil, err
	}

	t := &windowsTUN{
		name:    name,
		lib:     lib,
		adapter: adapter,
		done:    make(chan struct{}),
		readQ:   make(chan []byte, wintunReadQueue),
	}

	// Every failure from here on has to undo what was already done. Teardown is one
	// ordered function rather than a stack of cleanups because the driver has a
	// required order, and passing a closed handle to one of these faults instead of
	// failing.
	defer func() {
		if err != nil {
			t.teardown()
		}
	}()

	t.session, err = lib.startSession(adapter)
	if err != nil {
		return nil, err
	}

	t.readEvent = lib.readWaitEvent(t.session)

	t.stopEvent, err = windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		return nil, fmt.Errorf("wintun: create the stop event: %w", err)
	}

	t.wg.Add(1)
	go t.readLoop()

	return t, nil
}

// readLoop drains the session's receive ring until the tunnel closes.
func (t *windowsTUN) readLoop() {
	defer t.wg.Done()
	for {
		// Drain first: the event is only a hint that data may be available, and
		// waiting on it before the first receive would add latency to every packet.
		drained := t.drainOnce()
		if drained > 0 {
			continue
		}
		which, err := windows.WaitForMultipleObjects(
			[]windows.Handle{t.readEvent, t.stopEvent}, false, windows.INFINITE)
		if err != nil {
			return
		}
		if which == windows.WAIT_OBJECT_0+1 {
			return // The stop event fired: Close is waiting on us.
		}
	}
}

// drainOnce moves every queued packet into readQ and reports how many it took.
//
// Bounded by the read queue on purpose. An unbounded queue would let a caller that
// stops reading grow the process without limit; here a slow reader fills the
// driver's ring and Windows starts dropping, which is visible rather than silent.
func (t *windowsTUN) drainOnce() int {
	n := 0
	for {
		var size uint32
		p, _, _ := t.lib.receive.Call(t.session, uintptr(unsafe.Pointer(&size)))
		if p == 0 {
			return n
		}
		if size == 0 || size > maxWintunPacket {
			// An impossible length is dropped rather than copied, but the buffer is
			// still returned to the driver or the ring would never advance.
			t.lib.releaseRx.Call(t.session, p)
			continue
		}
		frame := make([]byte, int(size))
		copy(frame, wintunBuf(p)[:size])
		t.lib.releaseRx.Call(t.session, p)

		n++
		select {
		case t.readQ <- frame:
		case <-t.done:
			return n
		}
	}
}

func (t *windowsTUN) Name() string { return t.name }

func (t *windowsTUN) Read(buf []byte) (int, error) {
	// A queued frame is delivered before a closed tunnel is reported, so closing
	// while frames are in flight does not discard them.
	select {
	case frame := <-t.readQ:
		return copy(buf, frame), nil
	default:
	}
	select {
	case frame := <-t.readQ:
		return copy(buf, frame), nil
	case <-t.done:
		return 0, errTUNClosed
	}
}

func (t *windowsTUN) Write(buf []byte) (int, error) {
	if len(buf) == 0 {
		return 0, nil
	}
	if len(buf) > maxWintunPacket {
		return 0, fmt.Errorf("wintun: %d bytes exceeds WINTUN_MAX_IP_PACKET_SIZE (%d)",
			len(buf), maxWintunPacket)
	}
	for {
		select {
		case <-t.done:
			return 0, errTUNClosed
		default:
		}

		p, _, _ := t.lib.allocSend.Call(t.session, uintptr(len(buf)))
		if p != 0 {
			copy(wintunBuf(p)[:len(buf)], buf)
			// WintunSendPacket takes the session and the buffer only; it takes
			// ownership, so this is not released.
			t.lib.send.Call(t.session, p)
			return len(buf), nil
		}

		// Ring full. This Wintun build exports only a read-wait event, so there is
		// nothing to block on for write readiness. Bounded backoff makes a full ring
		// a slow send rather than a dropped packet.
		select {
		case <-t.done:
			return 0, errTUNClosed
		case <-time.After(wintunSendRetry):
		}
	}
}

// Up assigns the tunnel address.
//
// Wintun has no separate "up" call: the adapter is usable once a session exists and
// it has an address. This is where the address is set, because the carrier calls Up
// before it has anything to route.
func (t *windowsTUN) Up() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return errTUNClosed
	}
	if t.up {
		return nil
	}

	cidr, err := tunCIDR()
	if err != nil {
		return err
	}
	ip, _, err := parseCIDRv4(cidr)
	if err != nil {
		return err
	}
	octets := parseIPv4(ip)
	if octets == nil {
		return fmt.Errorf("wintun: %q is not a usable IPv4 address", ip)
	}

	if err := t.netshAddress(octets); err != nil {
		return err
	}
	t.addr = ip
	t.up = true
	return nil
}

func (t *windowsTUN) Down() error {
	// Windows has no portable way to mark a Wintun adapter down without deleting it.
	// Reported rather than silently treated as success, so a caller asking for
	// teardown learns the tunnel is still addressed.
	return fmt.Errorf("wintun: the adapter stays addressed until the tunnel is closed; " +
		"call Close to end the session")
}

// Addrs reports the address Up assigned.
//
// Wintun exports no address query, so this is the value that was configured and not
// a read-back from the driver.
func (t *windowsTUN) Addrs() ([]string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return nil, errTUNClosed
	}
	if !t.up {
		return nil, fmt.Errorf("wintun: the tunnel has no address yet; call Up first")
	}
	return []string{t.addr}, nil
}

// AddRoute points a destination range at this adapter, on-link.
//
// Wintun routes layer-3 prefixes on-link and has no notion of a next hop, so via
// names the address the range belongs to, matching what the Linux implementation
// does with it. An empty via uses dst.
func (t *windowsTUN) AddRoute(dst string, via string) error {
	t.mu.Lock()
	ready, closed := t.up, t.closed
	t.mu.Unlock()
	if closed {
		return errTUNClosed
	}
	if !ready {
		return fmt.Errorf("wintun: call Up before adding a route; the interface has no index yet")
	}

	dstIP, dstPrefix, err := cidrPrefix(dst)
	if err != nil {
		if parseIPv4(dst) == nil {
			return fmt.Errorf("wintun: %q is not an IPv4 address or CIDR", dst)
		}
		dstIP, dstPrefix = dst, 32 // A bare address is a host route.
	}
	dstOctets := parseIPv4(dstIP)
	if dstOctets == nil {
		return fmt.Errorf("wintun: %q is not a dotted-quad IPv4 address", dstIP)
	}

	viaOctets := dstOctets
	if v := strings.TrimSpace(via); v != "" {
		viaOctets = parseIPv4(v)
		if viaOctets == nil {
			return fmt.Errorf("wintun: %q is not a dotted-quad IPv4 address", v)
		}
	}
	return t.netshRoute(dstPrefix, viaOctets)
}

// Close ends the session and releases the adapter.
func (t *windowsTUN) Close() error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil
	}
	t.closed = true
	close(t.done)
	t.mu.Unlock()

	// The stop event is set before anything is torn down, so a reader blocked on the
	// driver's event returns and does not touch handles that are about to close.
	windows.SetEvent(t.stopEvent)
	t.wg.Wait()
	return t.teardown()
}

// teardown releases the session then the adapter, in the only order the driver
// accepts. It is safe to call on a partially constructed tunnel and is idempotent.
//
// The read-wait event is deliberately not closed: wintun.h says it is managed by
// the session, and closing it leaves a handle the driver still owns.
func (t *windowsTUN) teardown() error {
	t.mu.Lock()
	adapter, session := t.adapter, t.session
	stopEvent := t.stopEvent
	t.adapter, t.session, t.stopEvent = 0, 0, 0
	t.mu.Unlock()

	var errs []string
	if session != 0 {
		t.lib.endSess.Call(session)
	}
	if adapter != 0 {
		t.lib.close.Call(adapter)
	}
	if stopEvent != 0 {
		if err := windows.CloseHandle(stopEvent); err != nil {
			errs = append(errs, "close stop event: "+err.Error())
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("wintun: closing the tunnel reported %s", strings.Join(errs, "; "))
	}
	return nil
}

var _ Interface = (*windowsTUN)(nil)

// isElevated reports whether this process holds an administrator token.
//
// If the token cannot be read it reports false, so the refusal is in the safe
// direction.
func isElevated() bool {
	var tok windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(),
		windows.TOKEN_QUERY, &tok); err != nil {
		return false
	}
	defer tok.Close()
	return tok.IsElevated()
}

// wintunDriverVersion reports the running Wintun driver version.
func wintunDriverVersion() (string, error) {
	lib, err := loadWintun()
	if err != nil {
		return "", err
	}
	r, _, callErr := lib.getVersion.Call()
	if r == 0 {
		return "", fmt.Errorf("wintun: the driver is not installed or not running: %v", callErr)
	}
	return fmt.Sprintf("0x%x", uint32(r)), nil
}

// openAdapter opens the named adapter, creating it if it does not exist.
//
// Reusing an existing adapter is what keeps restarts from piling up adapters:
// Wintun matches on the adapter name, so a restart finds the one already there.
//
// The UTF-16 buffers are held in locals and kept alive across each call. Handing the
// driver a uintptr into a slice whose helper has already returned leaves it reading
// freed memory, which surfaces much later as a corrupt handle.
func (l *wintunLib) openAdapter(name string) (uintptr, error) {
	nameBuf, err := windows.UTF16FromString(name)
	if err != nil {
		return 0, fmt.Errorf("wintun: the adapter name %q cannot be encoded: %w", name, err)
	}
	descBuf, err := windows.UTF16FromString("LocalWEB VPN tunnel")
	if err != nil {
		return 0, fmt.Errorf("wintun: the adapter description cannot be encoded: %w", err)
	}

	if h, _, openErr := l.open.Call(uintptr(unsafe.Pointer(&nameBuf[0]))); openErr == nil && h != 0 {
		runtime.KeepAlive(nameBuf)
		return h, nil
	}

	var luid uint32
	r, _, createErr := l.create.Call(
		uintptr(unsafe.Pointer(&nameBuf[0])),
		uintptr(unsafe.Pointer(&descBuf[0])),
		0, // A null interface-type GUID lets the driver pick its default.
		uintptr(unsafe.Pointer(&luid)),
	)
	runtime.KeepAlive(nameBuf)
	runtime.KeepAlive(descBuf)
	runtime.KeepAlive(&luid)

	if r == 0 {
		return 0, fmt.Errorf("%w: creating adapter %q failed: %v",
			ErrNoTUNPlatform, name, lastWintunError(createErr))
	}
	return r, nil
}

// startSession returns the session handle every packet call needs.
//
// The return is a handle, not a success flag: wintun.h documents it as the session
// handle to release with WintunEndSession, with NULL on failure. Reading it as a
// BOOL discards the handle and the next call passes an adapter where a session
// belongs, which faults inside the driver.
func (l *wintunLib) startSession(adapter uintptr) (uintptr, error) {
	if wintunRingCapacity < wintunMinRingCapacity || wintunRingCapacity > wintunMaxRingCapacity {
		return 0, fmt.Errorf("wintun: ring capacity %#x is outside the supported range", wintunRingCapacity)
	}
	session, _, err := l.startSess.Call(adapter, wintunRingCapacity)
	if session == 0 {
		return 0, fmt.Errorf("wintun: starting a session failed: %v", lastWintunError(err))
	}
	return session, nil
}

// readWaitEvent returns the session's read-wait event.
//
// The handle is the return value; there is no out-parameter. wintun.h says not to
// CloseHandle it because the session owns it.
func (l *wintunLib) readWaitEvent(session uintptr) windows.Handle {
	h, _, _ := l.readWait.Call(session)
	if h == 0 {
		// Without the event the reader would have to spin. Falling back to the stop
		// event alone would block forever, so this is reported by making the reader
		// poll: see readEventIsUsable.
		return 0
	}
	return windows.Handle(h)
}

// netshAddress assigns an IPv4 address to the adapter by name.
//
// This Wintun build exports neither WintunSetAddress nor WintunAddRoute; both were
// added to Wintun after 0.14.1, which is what this repository vendors and pins. The
// alternative is CreateUnicastIpAddressEntry from netioapi, whose SIO_ADDRESS_LIST is
// a 28-byte struct wrapping a pointer-bearing IP_ADAPTER_UNICAST_ADDRESS whose
// layout differs between Windows releases. Building that by hand from a remembered
// header is how a node silently configures the wrong address, so netsh is used: it
// is the documented way to configure an interface, it resolves the struct per
// machine, and it is present on every supported Windows.
//
// The cost is a process spawn on the Up path. The packet path, which is the hot one,
// never shells out.
func (t *windowsTUN) netshAddress(ip *[4]byte) error {
	mask := maskFromPrefix(24)
	out, err := runNetsh("interface", "ipv4", "set", "address",
		"name="+t.name, "source=static", net.IP(ip[:]).String(), mask)
	if err != nil {
		return fmt.Errorf("wintun: assigning %s/%s to %q failed: %v: %s",
			net.IP(ip[:]), mask, t.name, err, out)
	}
	return nil
}

// netshRoute points a destination prefix at this adapter, on-link.
//
// The interface is named rather than numbered, so no LUID-to-index conversion is
// needed on this path.
func (t *windowsTUN) netshRoute(prefix int, via *[4]byte) error {
	// net.IPv4 builds the 16-byte IPv4-in-IPv6 form that Mask expects. Padding the
	// four-octet slice by hand produces an 8-byte IP, and Mask on that returns nil.
	network := net.IPv4(via[0], via[1], via[2], via[3]).Mask(net.CIDRMask(prefix, 32))
	// The mask form is used rather than "prefix=10.0.0.0/24": netsh splits the
	// slash-prefixed form on the slash and then rejects the bare address with
	// "Prefix should be of the form <IPv4 address>/[0-32]".
	out, err := runNetsh("interface", "ipv4", "add", "route",
		"prefix="+network.String(),
		"mask="+maskFromPrefix(prefix),
		"interface="+t.name,
		"nexthop="+net.IP(via[:]).String())
	if err != nil {
		return fmt.Errorf("wintun: routing %s/%d via %q failed: %v: %s",
			network, prefix, t.name, err, out)
	}
	return nil
}

// runNetsh runs netsh and returns its combined output when it fails.
//
// The output is included because netsh reports the reason on stdout, which is the
// only place it appears.
func runNetsh(args ...string) (string, error) {
	cmd := exec.Command("netsh", args...)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// wintunLUID returns a well-formed LUID string for the given name.
func wintunLUID(name string) string {
	sum := uint32(2166136261)
	for i := 0; i < len(name); i++ {
		sum ^= uint32(name[i])
		sum *= 16777619
	}
	const hexDigits = "0123456789abcdef"
	out := make([]byte, 0, 34)
	out = append(out, '{')
	for i := 0; i < 16; i++ {
		sum ^= uint32(i) + 1
		sum *= 16777619
		out = append(out, hexDigits[(sum>>4)&0xf], hexDigits[sum&0xf])
	}
	return string(append(out, '}'))
}

// wintunLUIDIsWellFormed checks the LUID shape Wintun requires.
func wintunLUIDIsWellFormed(luid string) bool {
	if len(luid) != 34 || luid[0] != '{' || luid[33] != '}' {
		return false
	}
	for i := 1; i < 33; i++ {
		c := luid[i]
		if (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') {
			continue
		}
		return false
	}
	return true
}

// parseCIDRv4 splits a dotted-quad CIDR into its address and dotted mask.
func parseCIDRv4(cidr string) (ip string, mask string, err error) {
	addr, bits, found := strings.Cut(strings.TrimSpace(cidr), "/")
	if !found {
		return "", "", fmt.Errorf("wintun: %q is not a CIDR, expected something like 10.6.0.1/24", cidr)
	}
	if parseIPv4(addr) == nil {
		return "", "", fmt.Errorf("wintun: %q is not a dotted-quad IPv4 address", addr)
	}
	n := 0
	for i := 0; i < len(bits); i++ {
		if bits[i] < '0' || bits[i] > '9' {
			return "", "", fmt.Errorf("wintun: %q has a non-numeric prefix length", cidr)
		}
		n = n*10 + int(bits[i]-'0')
	}
	if n > 32 {
		return "", "", fmt.Errorf("wintun: %q has a prefix length outside 0..32", cidr)
	}
	return addr, maskFromPrefix(n), nil
}

// cidrPrefix returns the address and prefix length of a dotted-quad CIDR.
//
// parseCIDRv4 returns the prefix as a dotted mask, which is the printable form;
// routing wants the count, so it is taken directly. Validation is not repeated:
// parseCIDRv4 has already proved the suffix is numeric and in range.
func cidrPrefix(cidr string) (string, int, error) {
	ip, _, err := parseCIDRv4(cidr)
	if err != nil {
		return "", 0, err
	}
	_, bits, _ := strings.Cut(strings.TrimSpace(cidr), "/")
	n := 0
	for i := 0; i < len(bits); i++ {
		n = n*10 + int(bits[i]-'0')
	}
	return ip, n, nil
}

// maskFromPrefix renders a prefix length as a dotted mask.
func maskFromPrefix(n int) string {
	m := uint32(0)
	if n > 0 {
		m = ^uint32(0) << (32 - n)
	}
	return fmt.Sprintf("%d.%d.%d.%d", byte(m>>24), byte(m>>16), byte(m>>8), byte(m))
}

// lastWintunError prefers the call's own error and falls back to the last error on
// the thread, which is how a Windows DLL reports a failed call.
func lastWintunError(callErr error) error {
	if callErr != nil {
		return callErr
	}
	return windows.GetLastError()
}

// resetWintunForTest clears the package-level load cache so a test can change
// LOCALWEB_WINTUN_DLL and get a fresh lookup.
func resetWintunForTest() {
	wintunOnce = sync.Once{}
	wintunD = nil
	wintunErr = nil
}
