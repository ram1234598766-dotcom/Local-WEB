package link

import (
	"context"
	"net"
	"runtime"
	"strings"
	"testing"
)

func TestPlatformPredicates(t *testing.T) {
	preds := map[string]bool{
		"isLinux":   isLinux(),
		"isMacOS":   isMacOS(),
		"isWindows": isWindows(),
	}
	trues := 0
	for name, v := range preds {
		if v {
			trues++
		}
		t.Logf("%s = %v", name, v)
	}
	if trues != 1 {
		t.Errorf("%d platform predicates are true, want exactly 1", trues)
	}

	switch runtime.GOOS {
	case "linux":
		if !isLinux() || isMacOS() || isWindows() {
			t.Errorf("predicates disagree with GOOS=linux: %v", preds)
		}
	case "darwin":
		if !isMacOS() || isLinux() || isWindows() {
			t.Errorf("predicates disagree with GOOS=darwin: %v", preds)
		}
	case "windows":
		if !isWindows() || isLinux() || isMacOS() {
			t.Errorf("predicates disagree with GOOS=windows: %v", preds)
		}
	}
}

func TestUSBSurfaceValues(t *testing.T) {
	u := &USB{}

	if got := u.Name(); got != "usb-tether" {
		t.Errorf("Name() = %q, want usb-tether", got)
	}
	if got := u.Mode(); got != ModeUSBTether {
		t.Errorf("Mode() = %s, want usb-tether", got)
	}
	if u.RequiresWiFi() {
		t.Error("RequiresWiFi() = true, want false (USB needs no WiFi radio)")
	}
	if u.RequiresRouter() {
		t.Error("RequiresRouter() = true, want false (USB is point-to-point)")
	}
	if got := u.Bandwidth(); got != 480 {
		t.Errorf("Bandwidth() = %d, want 480 (USB 2.0)", got)
	}
	if got := u.MaxPeers(); got != 2 {
		t.Errorf("MaxPeers() = %d, want 2", got)
	}
	if err := u.Advertise(PeerInfo{}); err != nil {
		t.Errorf("Advertise: %v", err)
	}
	if err := u.Stop(); err != nil {
		t.Errorf("Stop: %v", err)
	}
}

func TestUSBIsAvailableIsFalseForUnknownInterface(t *testing.T) {
	if (&USB{}).IsAvailable(context.Background()) {
		t.Error("IsAvailable() = true for an empty interface name, want false")
	}
	u := &USB{iface: "no-such-link-iface-42"}
	if u.IsAvailable(context.Background()) {
		t.Error("IsAvailable() = true for a nonexistent interface, want false")
	}
}

// findUSBInterface enumerates real interfaces, so the result depends on the
// machine. Only the invariants it promises can be asserted everywhere.
func TestFindUSBInterfaceInvariants(t *testing.T) {
	u := &USB{}
	iface, err := u.findUSBInterface()
	if err != nil {
		if err.Error() != "no USB network interface found" {
			t.Errorf("unexpected error text: %v", err)
		}
		return
	}

	lowered := strings.ToLower(iface)
	matched := false
	for _, prefix := range []string{"usb", "enx", "enp"} {
		if strings.HasPrefix(lowered, prefix) {
			matched = true
		}
	}
	if !matched {
		t.Errorf("findUSBInterface() = %q, want a usb/enx/enp prefixed name", iface)
	}

	real, err := net.InterfaceByName(iface)
	if err != nil {
		t.Fatalf("findUSBInterface() returned %q which does not resolve: %v", iface, err)
	}
	if real.Flags&net.FlagUp == 0 {
		t.Errorf("returned interface %q is not up", iface)
	}
	if real.Flags&net.FlagLoopback != 0 {
		t.Errorf("returned interface %q is the loopback", iface)
	}
	if ip, err := u.getInterfaceIP(iface); err != nil || ip == nil {
		t.Errorf("returned interface %q has no IPv4 address: %v", iface, err)
	}
}

func TestGetInterfaceIP(t *testing.T) {
	u := &USB{}

	t.Run("unknown interface", func(t *testing.T) {
		ip, err := u.getInterfaceIP("definitely-not-an-interface-xyz")
		if err == nil {
			t.Fatal("getInterfaceIP succeeded for a nonexistent interface, want an error")
		}
		if ip != nil {
			t.Error("getInterfaceIP returned an IP alongside an error")
		}
	})

	// The loopback interface exists on every supported platform and always
	// carries a 127.0.0.0/8 address, which gives a deterministic success path.
	iface := findLoopbackInterface(t)
	ip, err := u.getInterfaceIP(iface.Name)
	if err != nil {
		t.Fatalf("getInterfaceIP(%q): %v", iface.Name, err)
	}
	if ip4 := ip.To4(); ip4 == nil {
		t.Fatalf("getInterfaceIP(%q) = %v, want an IPv4 address", iface.Name, ip)
	} else if !ip4.IsLoopback() {
		t.Errorf("getInterfaceIP(%q) = %v, want a loopback address", iface.Name, ip4)
	}
}

// findLoopbackInterface returns the OS loopback interface.
func findLoopbackInterface(t *testing.T) *net.Interface {
	t.Helper()
	ifaces, err := net.Interfaces()
	if err != nil {
		t.Fatalf("net.Interfaces: %v", err)
	}
	for i := range ifaces {
		if ifaces[i].Flags&net.FlagLoopback != 0 {
			return &ifaces[i]
		}
	}
	t.Fatal("no loopback interface on this host")
	return nil
}

// NewUSB needs a real USB network interface. Where one exists the derived
// gateway must be consistent with the local address; where it does not, the
// constructor must fail cleanly rather than return a half-built link.
func TestNewUSB(t *testing.T) {
	u, err := NewUSB()
	if err != nil {
		if u != nil {
			t.Error("NewUSB returned a link alongside an error")
		}
		return
	}

	if u.iface == "" {
		t.Error("iface is empty on a successful constructor")
	}
	if u.localIP == nil || u.localIP.To4() == nil {
		t.Fatalf("localIP = %v, want an IPv4 address", u.localIP)
	}
	if u.gateway == nil || u.gateway.To4() == nil {
		t.Fatalf("gateway = %v, want an IPv4 address", u.gateway)
	}
	// The gateway is derived by copying the local address and forcing the
	// last octet to .1, so the first three octets must match.
	if !sameFirstThreeOctets(u.gateway, u.localIP) {
		t.Errorf("gateway = %v, want localIP %v with the last octet set to 1", u.gateway, u.localIP)
	}
	if got := u.gateway.To4()[3]; got != 1 {
		t.Errorf("gateway last octet = %d, want 1", got)
	}
}

func sameFirstThreeOctets(a, b net.IP) bool {
	x, y := a.To4(), b.To4()
	if x == nil || y == nil {
		return false
	}
	return x[0] == y[0] && x[1] == y[1] && x[2] == y[2]
}
