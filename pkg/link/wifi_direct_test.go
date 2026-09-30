package link

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"
)

func TestWiFiDirectSurfaceValues(t *testing.T) {
	w := &WiFiDirect{}

	if got := w.Name(); got != "wifi-direct" {
		t.Errorf("Name() = %q, want wifi-direct", got)
	}
	if got := w.Mode(); got != ModeWiFiDirect {
		t.Errorf("Mode() = %s, want wifi-direct", got)
	}
	if !w.RequiresWiFi() {
		t.Error("RequiresWiFi() = false, want true")
	}
	if w.RequiresRouter() {
		t.Error("RequiresRouter() = true, want false (P2P needs no router)")
	}
	if got := w.Bandwidth(); got != 250 {
		t.Errorf("Bandwidth() = %d, want 250", got)
	}
	if got := w.MaxPeers(); got != 10 {
		t.Errorf("MaxPeers() = %d, want 10", got)
	}
}

// wpaCmd must short-circuit before exec when no control path was discovered,
// which is the case for every non-Linux host and for Linux without
// wpa_supplicant. Asserting the error keeps that guard honest.
func TestWiFiDirectWpaCmdWithoutControlPath(t *testing.T) {
	w := &WiFiDirect{}
	if w.wpaCtrlPath != "" {
		t.Fatal("zero value should have no wpa_ctrl path")
	}

	err := w.wpaCmd("P2P_FIND")
	if err == nil {
		t.Fatal("wpaCmd succeeded with no control path, want an error")
	}
	if !strings.Contains(err.Error(), "wpa_ctrl") {
		t.Errorf("wpaCmd error = %q, want it to mention wpa_ctrl", err)
	}
}

func TestWiFiDirectStartDiscoveryFailsWithoutBackend(t *testing.T) {
	// On Linux this reaches startLinuxDiscovery, which calls wpaCmd and fails
	// on the empty control path; on Windows and macOS it fails outright. No
	// platform ends up shelling out with a zero-value receiver.
	w := &WiFiDirect{}
	if err := w.startDiscovery(); err == nil {
		t.Error("startDiscovery() succeeded with no backend, want an error")
	}
}

func TestWiFiDirectStopDiscoveryIsSafeWithoutBackend(t *testing.T) {
	// stopDiscovery swallows the wpa_cmd error on Linux and is a no-op
	// elsewhere; it must never panic.
	w := &WiFiDirect{}
	w.stopDiscovery()
}

func TestWiFiDirectConnectToGroupFailsWithoutBackend(t *testing.T) {
	w := &WiFiDirect{}
	if err := w.connectToGroup("00:11:22:33:44:55"); err == nil {
		t.Error("connectToGroup succeeded with no backend, want an error")
	}
}

func TestWiFiDirectAdvertiseFailsWithoutWpaCtrl(t *testing.T) {
	w := &WiFiDirect{}
	err := w.Advertise(PeerInfo{Name: "node"})
	if err == nil {
		t.Fatal("Advertise succeeded with no wpa_cli, want an error")
	}
	if !strings.Contains(err.Error(), "wpa_ctrl") {
		t.Errorf("Advertise error = %q, want it to mention wpa_ctrl", err)
	}
}

func TestWiFiDirectScanPeersDropsStaleEntries(t *testing.T) {
	fresh := time.Now()
	recent := time.Now().Add(-29 * time.Second)
	stale := time.Now().Add(-31 * time.Second)

	w := &WiFiDirect{
		peers: map[string]*WDPeer{
			"aa": {MAC: mustMAC("aa:bb:cc:dd:ee:01"), DeviceName: "fresh", LastSeen: fresh},
			"bb": {MAC: mustMAC("aa:bb:cc:dd:ee:02"), DeviceName: "recent", LastSeen: recent},
			"cc": {MAC: mustMAC("aa:bb:cc:dd:ee:03"), DeviceName: "stale", LastSeen: stale},
		},
	}

	got := w.scanPeers()
	if len(got) != 2 {
		t.Fatalf("scanPeers() = %d peers, want 2 (the entry older than 30s is dropped)", len(got))
	}
	names := map[string]bool{}
	for _, p := range got {
		names[p.DeviceName] = true
	}
	if !names["fresh"] || !names["recent"] {
		t.Errorf("scanPeers() returned %v, want the fresh and recent peers", names)
	}
	if names["stale"] {
		t.Error("scanPeers() kept a peer last seen more than 30s ago")
	}
}

func TestWiFiDirectScanPeersOnEmptyMap(t *testing.T) {
	w := &WiFiDirect{peers: map[string]*WDPeer{}}
	if got := w.scanPeers(); len(got) != 0 {
		t.Errorf("scanPeers() = %d peers on an empty map, want 0", len(got))
	}
}

// getP2PIP only enumerates interfaces; the outcome depends on the host, so
// assert the invariants of whichever branch it takes.
func TestWiFiDirectGetP2PIPInvariants(t *testing.T) {
	w := &WiFiDirect{}
	ip, err := w.getP2PIP()
	if err != nil {
		if err.Error() != "no P2P interface found" {
			t.Errorf("unexpected error text: %v", err)
		}
		if ip != nil {
			t.Error("getP2PIP returned an IP alongside an error")
		}
		return
	}
	if ip.To4() == nil {
		t.Errorf("getP2PIP() = %v, want an IPv4 address", ip)
	}
}

func TestWiFiDirectStopCancelsTheLinkContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	w := &WiFiDirect{
		peers:       map[string]*WDPeer{},
		wpaCtrlPath: "",
		ctx:         ctx,
		cancel:      cancel,
	}

	if err := w.Stop(); err != nil {
		t.Errorf("Stop: %v", err)
	}
	if ctx.Err() == nil {
		t.Error("Stop did not cancel the link context")
	}
}

func TestWDPeerShape(t *testing.T) {
	// scanPeers hands callers the stored *WDPeer directly, so the fields the
	// discovery loop reads must be populated for it to be useful.
	mac := mustMAC("aa:bb:cc:dd:ee:ff")
	seen := time.Now()
	p := &WDPeer{
		MAC:        mac,
		DeviceName: "phone",
		Interfaces: []string{"192.168.49.2:4443"},
		Connected:  true,
		LastSeen:   seen,
	}
	if p.MAC.String() != "aa:bb:cc:dd:ee:ff" {
		t.Errorf("MAC = %s", p.MAC)
	}
	if !p.LastSeen.Equal(seen) {
		t.Errorf("LastSeen = %v, want %v", p.LastSeen, seen)
	}
	if len(p.Interfaces) != 1 {
		t.Errorf("Interfaces = %v", p.Interfaces)
	}
}

func mustMAC(s string) net.HardwareAddr {
	hw, err := net.ParseMAC(s)
	if err != nil {
		panic(err)
	}
	return hw
}
