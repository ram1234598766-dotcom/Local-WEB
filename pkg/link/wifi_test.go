package link

import (
	"context"
	"net"
	"testing"
)

func TestWiFiStationSurfaceValues(t *testing.T) {
	w := &WiFiStation{}

	if got := w.Name(); got != "wifi-station" {
		t.Errorf("Name() = %q, want wifi-station", got)
	}
	if got := w.Mode(); got != ModeWiFiStation {
		t.Errorf("Mode() = %s, want wifi-station", got)
	}
	if !w.RequiresWiFi() {
		t.Error("RequiresWiFi() = false, want true")
	}
	if !w.RequiresRouter() {
		t.Error("RequiresRouter() = false, want true for station mode")
	}
	if got := w.Bandwidth(); got != 1000 {
		t.Errorf("Bandwidth() = %d, want 1000", got)
	}
	if got := w.MaxPeers(); got != 1000 {
		t.Errorf("MaxPeers() = %d, want 1000", got)
	}
	if err := w.Advertise(PeerInfo{}); err != nil {
		t.Errorf("Advertise: %v", err)
	}
	if err := w.Stop(); err != nil {
		t.Errorf("Stop: %v", err)
	}
}

func TestWiFiStationIsAvailableWithoutInterface(t *testing.T) {
	if (&WiFiStation{}).IsAvailable(context.Background()) {
		t.Error("IsAvailable() = true with a nil interface, want false")
	}
	w := &WiFiStation{iface: &net.Interface{Name: "definitely-not-an-interface-xyz"}}
	if w.IsAvailable(context.Background()) {
		t.Error("IsAvailable() = true for a nonexistent interface, want false")
	}
}

// findWiFiInterface walks the real interface list, so only the invariants it
// promises are assertable on any host.
func TestFindWiFiInterfaceInvariants(t *testing.T) {
	iface, err := findWiFiInterface()
	if err != nil {
		return
	}

	if iface.Flags&net.FlagUp == 0 {
		t.Errorf("returned interface %q is down", iface.Name)
	}
	if iface.Flags&net.FlagLoopback != 0 {
		t.Errorf("returned interface %q is the loopback", iface.Name)
	}
	if iface.Flags&net.FlagPointToPoint != 0 {
		t.Errorf("returned interface %q is point-to-point", iface.Name)
	}
	if iface.Flags&net.FlagMulticast == 0 {
		t.Errorf("returned interface %q lacks multicast, which mDNS requires", iface.Name)
	}

	addrs, err := iface.Addrs()
	if err != nil {
		t.Fatalf("Addrs(%q): %v", iface.Name, err)
	}
	found := false
	for _, a := range addrs {
		if ipNet, ok := a.(*net.IPNet); ok && ipNet.IP.To4() != nil && !ipNet.IP.IsLoopback() {
			found = true
		}
	}
	if !found {
		t.Errorf("returned interface %q has no non-loopback IPv4 address", iface.Name)
	}
}

// detectGateway assumes the gateway is .1 in the subnet. The loopback
// interface is the one address set available on every host, so it gives a
// deterministic way to exercise the success path.
func TestDetectGatewayAssumesDotOne(t *testing.T) {
	iface := findLoopbackInterface(t)

	gw, err := detectGateway(iface)
	if err != nil {
		t.Fatalf("detectGateway(%q): %v", iface.Name, err)
	}
	v4 := gw.To4()
	if v4 == nil {
		t.Fatalf("detectGateway(%q) = %v, want an IPv4 address", iface.Name, gw)
	}
	if v4[3] != 1 {
		t.Errorf("gateway = %v, want the last octet forced to 1", v4)
	}
	if !v4.IsLoopback() {
		t.Errorf("gateway = %v, want a loopback address derived from the loopback interface", v4)
	}
}

func TestDetectGatewayWithNoAddrs(t *testing.T) {
	// Index 0 never names a real interface, so Addrs cannot resolve.
	if _, err := detectGateway(&net.Interface{Index: 0, Name: "no-such-iface-xyz"}); err == nil {
		t.Error("detectGateway succeeded for a nonexistent interface, want an error")
	}
}

func TestNewWiFiStation(t *testing.T) {
	w, err := NewWiFiStation()
	if err != nil {
		if w != nil {
			t.Error("NewWiFiStation returned a link alongside an error")
		}
		return
	}

	if w.iface == nil {
		t.Fatal("iface is nil on a successful constructor")
	}
	if w.gateway == nil || w.gateway.To4() == nil {
		t.Errorf("gateway = %v, want an IPv4 address", w.gateway)
	}
	if w.ssid != "" {
		t.Errorf("ssid = %q, want empty (mDNS is handled by the discovery layer)", w.ssid)
	}
	if w.channel != 0 {
		t.Errorf("channel = %d, want 0 (station mode is not IBSS)", w.channel)
	}
}
