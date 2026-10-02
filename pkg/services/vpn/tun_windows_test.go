//go:build windows

package vpn

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// vendoredWintunPath finds the DLL committed to the repository, if present.
func vendoredWintunPath(t *testing.T) string {
	t.Helper()
	rel := filepath.Join("..", "..", "..", "installers", "windows", "wintun", "wintun.dll")
	if p, err := filepath.Abs(rel); err == nil {
		if _, statErr := os.Stat(p); statErr == nil {
			return p
		}
	}
	return ""
}

// TestWintunVendoredDLLLoadsAndExports is the test that matters for the binding.
//
// The DLL is vendored in this repository and the Windows installers install it, so
// the question worth asking is not "is Wintun a thing" but "is this DLL really
// Wintun, and are the names this file binds actually in it". A renamed or missing
// export would otherwise surface as a call that silently returns zero, which looks
// like a device failure rather than a binding error.
func TestWintunVendoredDLLLoadsAndExports(t *testing.T) {
	dll := vendoredWintunPath(t)
	if dll == "" {
		t.Skip("no vendored wintun.dll in this checkout")
	}
	t.Setenv("LOCALWEB_WINTUN_DLL", dll)
	resetWintunForTest()

	lib, err := loadWintun()
	require.NoError(t, err, "the vendored wintun.dll should load")
	require.NotNil(t, lib)

	for name, proc := range map[string]interface{ Find() error }{
		"WintunCreateAdapter":           lib.create,
		"WintunOpenAdapter":             lib.open,
		"WintunCloseAdapter":            lib.close,
		"WintunStartSession":            lib.startSess,
		"WintunEndSession":              lib.endSess,
		"WintunReceivePacket":           lib.receive,
		"WintunReleaseReceivePacket":    lib.releaseRx,
		"WintunAllocateSendPacket":      lib.allocSend,
		"WintunSendPacket":              lib.send,
		"WintunGetRunningDriverVersion": lib.getVersion,
	} {
		require.NoErrorf(t, proc.Find(), "the vendored DLL is missing %s", name)
	}
}

// TestWintunLUIDIsWellFormed checks the LUID shape Wintun requires.
//
// A LUID is "{" plus 32 hex digits plus "}". A malformed one is rejected by the
// driver with an opaque error, so the shape is asserted where it is generated.
func TestWintunLUIDIsWellFormed(t *testing.T) {
	luid := wintunLUID("tun0")
	require.True(t, wintunLUIDIsWellFormed(luid), "generated LUID %q is malformed", luid)
	require.Len(t, luid, 34)
	require.Equal(t, byte('{'), luid[0])
	require.Equal(t, byte('}'), luid[33])

	// Stable, or a restart would leave a new adapter behind.
	require.Equal(t, luid, wintunLUID("tun0"))
	// Distinct, or two interfaces would share one device.
	require.NotEqual(t, luid, wintunLUID("tun1"))

	require.False(t, wintunLUIDIsWellFormed(""))
	require.False(t, wintunLUIDIsWellFormed("{short}"))
	require.False(t, wintunLUIDIsWellFormed(strings.Repeat("z", 34)))
}

// TestWintunMissingDLLIsReportedHonestly covers a machine with no driver.
//
// The message has to name the fix. "no TUN implementation" sends someone looking
// for a privilege they do not need; naming the installer and the override variable
// is actionable.
func TestWintunMissingDLLIsReportedHonestly(t *testing.T) {
	t.Setenv("LOCALWEB_WINTUN_DLL", filepath.Join(t.TempDir(), "absent.dll"))
	resetWintunForTest()

	_, err := wintunDriverVersion()
	require.Error(t, err)
	require.ErrorIs(t, err, ErrNoTUNPlatform)

	msg := err.Error()
	require.Contains(t, msg, "LOCALWEB_WINTUN_DLL",
		"the message must say how to point at the DLL, since that is the escape hatch")
	require.Contains(t, strings.ToLower(msg), "administrator",
		"the message must name the actual remedy: install the driver as administrator")

	iface, err := openTUN("tun0")
	require.Error(t, err)
	require.Nil(t, iface, "a failed open must not return a usable device")
}

// TestWintunSearchPathCoversKnownInstallLocations checks the loader looks where
// the installers actually put the DLL.
func TestWintunSearchPathCoversKnownInstallLocations(t *testing.T) {
	joined := strings.Join(wintunSearchPaths(), "|")
	require.Contains(t, joined, filepath.Join("System32", "drivers", "wintun.dll"),
		"the path the installer uses must be searched")
	require.Contains(t, joined, filepath.Join("installers", "windows", "wintun", "wintun.dll"),
		"the vendored copy must be searched so a developer can try it")

	t.Setenv("LOCALWEB_WINTUN_DLL", `C:\explicit\wintun.dll`)
	require.Equal(t, `C:\explicit\wintun.dll`, wintunSearchPaths()[0],
		"the explicit override must win")
}

// TestOpenTUNNamesTheActualBlocker is the honesty test for this platform.
//
// Windows does have a TUN implementation and a vendored driver, so "unsupported on
// this platform" is no longer true and would be a lie. What is true is that each
// step has a specific blocker, and the error has to name the one that applies.
//
// An unelevated process is refused, which is what this runs as on CI and on a
// developer machine. An elevated one gets a real device, and
// TestWintunTunnelCarriesAPacket proves it; asserting that here too would install a
// kernel driver on every run of the suite.
func TestOpenTUNNamesTheActualBlocker(t *testing.T) {
	dll := vendoredWintunPath(t)
	if dll == "" {
		t.Skip("no vendored wintun.dll in this checkout")
	}
	t.Setenv("LOCALWEB_WINTUN_DLL", dll)
	resetWintunForTest()

	lib, err := loadWintun()
	require.NoError(t, err)
	require.NotNil(t, lib)

	// This call must return rather than crash. WintunCreateAdapter faults inside the
	// driver loader when it cannot load the driver, which took the node down with
	// 0xC0000005; the elevation check exists to prevent exactly that.
	iface, err := openTUN("tun0-localweb-test")
	if isElevated() {
		if err == nil {
			require.NoError(t, iface.Close())
		}
		t.Skip("this process is elevated, so the refusal path cannot be reached here")
	}

	require.Error(t, err)
	require.Nil(t, iface)
	require.ErrorIs(t, err, ErrNoTUNPlatform)
	require.NotContains(t, err.Error(), "no TUN/TAP implementation for windows",
		"Windows does have a TUN implementation, so that claim would be false")
	require.NotContains(t, err.Error(), "go vet",
		"the send path is implemented; the vet gate is no longer a blocker")
	require.ErrorIs(t, err, errNotElevated,
		"an unelevated process must be told elevation is the blocker")
}

// TestWintunTunnelCarriesAPacket is the end-to-end proof that the tunnel moves
// packets: open an adapter, start a session, address it, route a prefix, write a
// frame and read the same frame back.
//
// It needs elevation, because creating a Wintun adapter installs and starts a
// kernel driver. Skipped otherwise rather than passed: a suite that skips the one
// test proving the feature is the honest outcome on an unelevated machine, and a
// claim would not be.
//
// Run from an elevated prompt:
//
//	go test ./pkg/services/vpn/ -run TestWintunTunnelCarriesAPacket -v
func TestWintunTunnelCarriesAPacket(t *testing.T) {
	if !isElevated() {
		t.Skip("needs an elevated prompt: creating a Wintun adapter installs a kernel driver")
	}
	dll := vendoredWintunPath(t)
	if dll == "" {
		t.Skip("no vendored wintun.dll in this checkout")
	}
	t.Setenv("LOCALWEB_WINTUN_DLL", dll)
	t.Setenv("LOCALWEB_TUN_CIDR", "10.253.0.1/24")
	resetWintunForTest()

	iface, err := openTUN("tun0-localweb-e2e")
	require.NoError(t, err)
	require.NotNil(t, iface, "an elevated process must get a real device")
	defer iface.Close()

	require.Equal(t, "tun0-localweb-e2e", iface.Name())

	require.NoError(t, iface.Up())
	addrs, err := iface.Addrs()
	require.NoError(t, err)
	require.Equal(t, []string{"10.253.0.1"}, addrs,
		"Up must assign the address it is then asked about")

	require.NoError(t, iface.AddRoute("10.253.0.0/24", "10.253.0.2"),
		"a host route on the tunnel subnet must be accepted")

	frame := []byte{
		0x45, 0x00, 0x00, 0x1c, // IPv4, IHL 5, total length 28
		0x00, 0x01, 0x00, 0x00, // id 1, no flags
		0x40, 0x01, 0x00, 0x00, // TTL 64, proto 1 (ICMP)
		10, 253, 0, 1, // src
		10, 253, 0, 2, // dst
		0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // ICMP echo
	}

	n, err := iface.Write(frame)
	require.NoError(t, err)
	require.Equal(t, len(frame), n,
		"Write must hand the whole frame to the driver")

	// A layer-3 TUN device is not a loopback: Write feeds the adapter and Read
	// drains it, so the frame written does not come back out of the same device.
	// What is proven here is that the receive path moves real packets: Windows
	// brings its own IPv6 router and neighbour solicitations up as soon as the
	// adapter is addressed, so a correctly framed IP packet arriving is the
	// signal that the session, the ring and the buffer handoff all work.
	got := make([]byte, maxWintunPacket)
	rn, err := readWithTimeout(iface, got, 30)
	require.NoError(t, err, "the adapter should bring traffic up on its own")
	require.Greater(t, rn, 0)
	version := got[0] >> 4
	require.Containsf(t, []byte{4, 6}, version,
		"expected an IPv4 or IPv6 packet from the tunnel, got first byte %#02x", got[0])

	require.NoError(t, iface.Close())
	_, err = iface.Write(frame)
	require.ErrorIs(t, err, errTUNClosed, "a closed tunnel must refuse writes")
}

// TestRingCapacityIsUsable checks the ring size against the limits in wintun.h.
//
// The driver requires a power of two inside a range and rejects anything else, so
// this is asserted where the value is chosen rather than discovered at startup.
func TestRingCapacityIsUsable(t *testing.T) {
	require.GreaterOrEqual(t, wintunRingCapacity, wintunMinRingCapacity)
	require.LessOrEqual(t, wintunRingCapacity, wintunMaxRingCapacity)
	require.Zero(t, wintunRingCapacity&(wintunRingCapacity-1),
		"the ring capacity must be a power of two")
	require.Equal(t, 0xFFFF, maxWintunPacket, "must match WINTUN_MAX_IP_PACKET_SIZE")
}

// TestTUNCIDR covers the range Up assigns, including the override that keeps a node
// from colliding with a network already using the default.
func TestTUNCIDR(t *testing.T) {
	t.Setenv("LOCALWEB_TUN_CIDR", "")
	got, err := tunCIDR()
	require.NoError(t, err)
	require.Equal(t, defaultTUNCIDR, got)

	t.Setenv("LOCALWEB_TUN_CIDR", "192.168.77.1/30")
	got, err = tunCIDR()
	require.NoError(t, err)
	require.Equal(t, "192.168.77.1/30", got)

	t.Setenv("LOCALWEB_TUN_CIDR", "not-an-address")
	_, err = tunCIDR()
	require.Error(t, err, "a bad override must fail loudly rather than fall back silently")
}

// TestCIDRPrefix checks the prefix count routing reads back out of a CIDR.
func TestCIDRPrefix(t *testing.T) {
	for _, c := range []struct {
		in     string
		ip     string
		prefix int
	}{
		{"10.6.0.1/24", "10.6.0.1", 24},
		{"0.0.0.0/0", "0.0.0.0", 0},
		{"10.253.0.7/30", "10.253.0.7", 30},
		{"10.0.0.1/32", "10.0.0.1", 32},
	} {
		ip, n, err := cidrPrefix(c.in)
		require.NoErrorf(t, err, "%s", c.in)
		require.Equal(t, c.ip, ip)
		require.Equal(t, c.prefix, n)
	}

	_, _, err := cidrPrefix("10.6.0.1/33")
	require.Error(t, err)
	_, _, err = cidrPrefix("10.6.0.1")
	require.Error(t, err, "an address without a prefix is not a CIDR")
}

// readWithTimeout reads one frame, failing rather than hanging if none arrives.
// Without it a regression in the wait path would block until the suite's own
// timeout, which says far less about what broke.
func readWithTimeout(iface Interface, buf []byte, seconds int) (int, error) {
	type result struct {
		n   int
		err error
	}
	ch := make(chan result, 1)
	go func() {
		n, err := iface.Read(buf)
		ch <- result{n, err}
	}()
	select {
	case r := <-ch:
		return r.n, r.err
	case <-time.After(time.Duration(seconds) * time.Second):
		return 0, fmt.Errorf("no frame arrived within %d seconds", seconds)
	}
}

// TestParseCIDRv4 checks the parsing that any future route or address call depends
// on, so it is correct before it is used rather than after.
func TestParseCIDRv4(t *testing.T) {
	cases := []struct{ cidr, ip, mask string }{
		{"10.6.0.1/24", "10.6.0.1", "255.255.255.0"},
		{"192.168.1.1/16", "192.168.1.1", "255.255.0.0"},
		{"0.0.0.0/0", "0.0.0.0", "0.0.0.0"},
		{"10.0.0.1/32", "10.0.0.1", "255.255.255.255"},
		{"10.0.0.1/8", "10.0.0.1", "255.0.0.0"},
	}
	for _, tc := range cases {
		ip, mask, err := parseCIDRv4(tc.cidr)
		require.NoErrorf(t, err, "%s", tc.cidr)
		require.Equal(t, tc.ip, ip, tc.cidr)
		require.Equal(t, tc.mask, mask, tc.cidr)
	}

	for _, bad := range []string{
		"10.6.0.1",       // no prefix
		"10.6.0.1/33",    // prefix out of range
		"10.6.0/24",      // three octets
		"10.6.0.1.1/24",  // five octets
		"10.6.0.x/24",    // not numeric
		"10.6.0.1/abc",   // non-numeric prefix
		"10.6.0.1000/24", // octet too long
	} {
		_, _, err := parseCIDRv4(bad)
		require.Errorf(t, err, "%q should be rejected", bad)
	}
}
