package link

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

func TestAggregationModeConstantValues(t *testing.T) {
	if AggregationFailover != 0 {
		t.Errorf("AggregationFailover = %d, want 0", AggregationFailover)
	}
	if AggregationRoundRobin != 1 {
		t.Errorf("AggregationRoundRobin = %d, want 1", AggregationRoundRobin)
	}
	if AggregationBandwidth != 2 {
		t.Errorf("AggregationBandwidth = %d, want 2", AggregationBandwidth)
	}
	if AggregationLatency != 3 {
		t.Errorf("AggregationLatency = %d, want 3", AggregationLatency)
	}
}

func TestAggregationModeString(t *testing.T) {
	tests := []struct {
		name string
		mode AggregationMode
		want string
	}{
		{"AggregationFailover", AggregationFailover, "failover"},
		{"AggregationRoundRobin", AggregationRoundRobin, "round-robin"},
		{"AggregationBandwidth", AggregationBandwidth, "bandwidth"},
		{"AggregationLatency", AggregationLatency, "latency"},
		{"out of range", AggregationMode(42), "unknown"},
		{"negative", AggregationMode(-1), "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.mode.String(); got != tt.want {
				t.Errorf("AggregationMode(%d).String() = %q, want %q", int(tt.mode), got, tt.want)
			}
		})
	}
}

func TestNewMultiPathManagerDefaults(t *testing.T) {
	m := NewMultiPathManager(MultiPathConfig{})
	if m == nil {
		t.Fatal("NewMultiPathManager returned nil")
	}
	defer m.Stop()

	if len(m.preferences) != len(defaultPreferences()) {
		t.Errorf("preferences = %d entries, want %d", len(m.preferences), len(defaultPreferences()))
	}
	if len(m.configs) != len(DefaultLinkConfigs()) {
		t.Errorf("configs = %d entries, want %d", len(m.configs), len(DefaultLinkConfigs()))
	}
	if cap(m.events) != 128 {
		t.Errorf("events channel capacity = %d, want 128", cap(m.events))
	}
	if m.aggregation != AggregationBandwidth {
		t.Errorf("default aggregation = %s, want bandwidth", m.aggregation)
	}
	if got := m.PeerCount(); got != 0 {
		t.Errorf("PeerCount() = %d on a fresh manager, want 0", got)
	}
}

func TestNewMultiPathManagerHonoursExplicitAggregation(t *testing.T) {
	for _, mode := range []AggregationMode{AggregationRoundRobin, AggregationBandwidth, AggregationLatency} {
		// Aggregation is a pointer so that "unset" is distinguishable from
		// AggregationFailover, which is itself 0.
		m := NewMultiPathManager(MultiPathConfig{Aggregation: &mode})
		if m.aggregation != mode {
			t.Errorf("aggregation = %s, want %s", m.aggregation, mode)
		}
		m.Stop()
	}
}

func TestNewMultiPathManagerStoresConfig(t *testing.T) {
	rec := &peerRecorder{}
	ble := newFakeLink("ble", ModeBLE)
	prefs := []LinkMode{ModeBLE}
	cfgs := map[LinkMode]LinkConfig{ModeBLE: {Mode: ModeBLE, Enabled: true}}

	m := NewMultiPathManager(MultiPathConfig{
		Links:       []Link{ble},
		Preferences: prefs,
		Configs:     cfgs,
		OnPeer:      rec.on,
		MaxLinks:    2,
	})
	defer m.Stop()

	if len(m.preferences) != 1 || m.preferences[0] != ModeBLE {
		t.Errorf("preferences = %v, want [ble]", m.preferences)
	}
	if len(m.configs) != 1 {
		t.Errorf("configs = %d entries, want 1", len(m.configs))
	}
	if m.onPeer == nil {
		t.Error("OnPeer callback not stored")
	}
}

// newTestPeerLinkSet builds a PeerLinkSet with one connection per mode.
func newTestPeerLinkSet(peerID [32]byte) *PeerLinkSet {
	return &PeerLinkSet{
		peerID:      peerID,
		connections: make(map[LinkMode]*LinkConnection),
	}
}

func addTestConn(pls *PeerLinkSet, mode LinkMode, latency time.Duration, active bool) *LinkConnection {
	conn := &LinkConnection{
		Link:     newFakeLink(mode.String(), mode),
		Conn:     newPipeConn().local,
		PeerAddr: "10.0.0.1:4443",
		Latency:  latency,
		Active:   active,
	}
	pls.connections[mode] = conn
	return conn
}

func TestPeerLinkSetActiveConnections(t *testing.T) {
	pls := newTestPeerLinkSet(testPeerID(30))
	if got := len(pls.activeConnections()); got != 0 {
		t.Errorf("activeConnections() = %d on an empty set, want 0", got)
	}

	addTestConn(pls, ModeBLE, time.Millisecond, true)
	addTestConn(pls, ModeWiFiDirect, time.Millisecond, false)

	active := pls.activeConnections()
	if len(active) != 1 {
		t.Fatalf("activeConnections() = %d, want 1", len(active))
	}
	if active[0].Link.Mode() != ModeBLE {
		t.Errorf("active link = %s, want ble", active[0].Link.Mode())
	}
}

func TestPeerLinkSetUpdatePrimary(t *testing.T) {
	t.Run("no active connections leaves primary untouched", func(t *testing.T) {
		pls := newTestPeerLinkSet(testPeerID(31))
		pls.primary = ModeWiFiDirect

		for _, mode := range []AggregationMode{
			AggregationFailover, AggregationRoundRobin, AggregationBandwidth, AggregationLatency,
		} {
			pls.updatePrimary(mode)
			if pls.primary != ModeWiFiDirect {
				t.Errorf("%s: primary = %s, want the untouched wifi-direct", mode, pls.primary)
			}
		}
	})

	t.Run("single active connection becomes primary", func(t *testing.T) {
		pls := newTestPeerLinkSet(testPeerID(32))
		addTestConn(pls, ModeUSBTether, 30*time.Millisecond, true)

		for _, mode := range []AggregationMode{
			AggregationFailover, AggregationRoundRobin, AggregationBandwidth, AggregationLatency,
		} {
			pls.primary = ModeNone
			pls.updatePrimary(mode)
			if pls.primary != ModeUSBTether {
				t.Errorf("%s: primary = %s, want usb-tether", mode, pls.primary)
			}
		}
	})

	t.Run("bandwidth policy picks the lowest measured latency", func(t *testing.T) {
		// AggregationBandwidth has no bandwidth signal, so the implementation
		// proxies it with latency; assert that proxy is at least deterministic.
		pls := newTestPeerLinkSet(testPeerID(33))
		addTestConn(pls, ModeWiFiStation, 30*time.Millisecond, true)
		addTestConn(pls, ModeBLE, 5*time.Millisecond, true)
		addTestConn(pls, ModeUSBTether, 60*time.Millisecond, true)

		pls.updatePrimary(AggregationBandwidth)
		if pls.primary != ModeBLE {
			t.Errorf("primary = %s, want ble (5ms)", pls.primary)
		}
	})

	t.Run("latency policy picks the lowest measured latency", func(t *testing.T) {
		pls := newTestPeerLinkSet(testPeerID(34))
		addTestConn(pls, ModeWiFiStation, 40*time.Millisecond, true)
		addTestConn(pls, ModeAdHocWiFi, 7*time.Millisecond, true)

		pls.updatePrimary(AggregationLatency)
		if pls.primary != ModeAdHocWiFi {
			t.Errorf("primary = %s, want ad-hoc-wifi (7ms)", pls.primary)
		}
	})

	t.Run("bandwidth and latency ignore inactive links", func(t *testing.T) {
		pls := newTestPeerLinkSet(testPeerID(35))
		addTestConn(pls, ModeBLE, time.Millisecond, false)
		addTestConn(pls, ModeWiFiStation, 40*time.Millisecond, true)

		pls.updatePrimary(AggregationLatency)
		if pls.primary != ModeWiFiStation {
			t.Errorf("primary = %s, want wifi-station (only active link)", pls.primary)
		}
	})

	t.Run("failover keeps a still-active primary", func(t *testing.T) {
		pls := newTestPeerLinkSet(testPeerID(36))
		addTestConn(pls, ModeBLE, 100*time.Millisecond, true)
		addTestConn(pls, ModeWiFiDirect, 2*time.Millisecond, true)
		pls.primary = ModeBLE

		// Failover must not migrate to a "better" link; that is the whole
		// point of a failover policy.
		pls.updatePrimary(AggregationFailover)
		if pls.primary != ModeBLE {
			t.Errorf("primary = %s, want the retained ble", pls.primary)
		}
	})

	t.Run("failover replaces a dead primary with an active link", func(t *testing.T) {
		pls := newTestPeerLinkSet(testPeerID(37))
		addTestConn(pls, ModeBLE, time.Millisecond, false)
		addTestConn(pls, ModeWiFiDirect, time.Millisecond, true)
		pls.primary = ModeBLE

		pls.updatePrimary(AggregationFailover)
		if pls.primary != ModeWiFiDirect {
			t.Errorf("primary = %s, want wifi-direct after BLE died", pls.primary)
		}
	})

	t.Run("round robin with one link keeps that link", func(t *testing.T) {
		pls := newTestPeerLinkSet(testPeerID(38))
		addTestConn(pls, ModeBLE, 100*time.Millisecond, true)

		pls.updatePrimary(AggregationRoundRobin)
		if pls.primary != ModeBLE {
			t.Errorf("primary = %s, want ble", pls.primary)
		}
	})

	t.Run("round robin always lands on an active link", func(t *testing.T) {
		// The rotation walks a Go map, so the successor itself is not
		// deterministic; only the invariant is assertable.
		pls := newTestPeerLinkSet(testPeerID(39))
		addTestConn(pls, ModeBLE, time.Millisecond, true)
		addTestConn(pls, ModeWiFiDirect, time.Millisecond, true)
		addTestConn(pls, ModeUSBTether, time.Millisecond, true)
		pls.primary = ModeBLE

		pls.updatePrimary(AggregationRoundRobin)
		switch pls.primary {
		case ModeBLE, ModeWiFiDirect, ModeUSBTether:
		default:
			t.Errorf("primary = %s, want one of the three active links", pls.primary)
		}
	})
}

func TestPeerLinkSetUpdateStats(t *testing.T) {
	pls := newTestPeerLinkSet(testPeerID(40))
	ble := addTestConn(pls, ModeBLE, 10*time.Millisecond, true)
	ble.BytesSent = 100
	ble.BytesRecv = 40
	station := addTestConn(pls, ModeWiFiStation, 20*time.Millisecond, true)
	station.BytesSent = 20
	station.BytesRecv = 10
	dead := addTestConn(pls, ModeUSBTether, 999*time.Millisecond, false)
	dead.BytesSent = 7

	pls.primary = ModeBLE
	pls.updateStats()

	agg := pls.aggregated
	if agg.TotalLinks != 2 {
		t.Errorf("TotalLinks = %d, want 2 (inactive link excluded)", agg.TotalLinks)
	}
	if agg.PrimaryLink != ModeBLE {
		t.Errorf("PrimaryLink = %s, want ble", agg.PrimaryLink)
	}
	if agg.AvgLatency != 15*time.Millisecond {
		t.Errorf("AvgLatency = %v, want 15ms (mean of the two active links)", agg.AvgLatency)
	}
	if agg.LastUpdate.IsZero() {
		t.Error("LastUpdate not set")
	}
}

func TestPeerLinkSetUpdateStatsWithNoActiveLinks(t *testing.T) {
	pls := newTestPeerLinkSet(testPeerID(41))
	addTestConn(pls, ModeBLE, time.Second, false)

	pls.updateStats()

	if pls.aggregated.TotalLinks != 0 {
		t.Errorf("TotalLinks = %d, want 0", pls.aggregated.TotalLinks)
	}
	if pls.aggregated.AvgLatency != 0 {
		t.Errorf("AvgLatency = %v, want 0", pls.aggregated.AvgLatency)
	}
}

func TestMultiPathHandleEventAddsConnections(t *testing.T) {
	ble := newFakeLink("ble", ModeBLE)
	wd := newFakeLink("wifi-direct", ModeWiFiDirect)
	m := NewMultiPathManager(MultiPathConfig{Links: []Link{ble, wd}})
	defer m.Stop()

	id := testPeerID(42)
	m.handleEvent(PeerEvent{
		Type: PeerDiscovered,
		Peer: PeerInfo{ID: id, Addrs: []string{"10.0.0.1:4443"}, LinkMode: ModeBLE, Latency: 5 * time.Millisecond},
	})
	m.handleEvent(PeerEvent{
		Type: PeerDiscovered,
		Peer: PeerInfo{ID: id, Addrs: []string{"10.0.0.2:4443"}, LinkMode: ModeWiFiDirect, Latency: 30 * time.Millisecond},
	})

	links, ok := m.GetPeerLinks(id)
	if !ok {
		t.Fatal("GetPeerLinks reported the peer as unknown")
	}
	if len(links) != 2 {
		t.Fatalf("active links = %d, want 2", len(links))
	}
	if links[ModeBLE] == nil || links[ModeWiFiDirect] == nil {
		t.Fatalf("missing an expected link mode: %v", links)
	}
	if got := links[ModeBLE].PeerAddr; got != "10.0.0.1:4443" {
		t.Errorf("BLE PeerAddr = %q, want 10.0.0.1:4443", got)
	}
	if got := links[ModeWiFiDirect].PeerAddr; got != "10.0.0.2:4443" {
		t.Errorf("wifi-direct PeerAddr = %q, want 10.0.0.2:4443", got)
	}

	stats, ok := m.GetAggregatedStats(id)
	if !ok {
		t.Fatal("GetAggregatedStats reported the peer as unknown")
	}
	if stats.TotalLinks != 2 {
		t.Errorf("TotalLinks = %d, want 2", stats.TotalLinks)
	}
	if stats.PrimaryLink != ModeBLE {
		t.Errorf("PrimaryLink = %s, want ble (5ms beat 30ms)", stats.PrimaryLink)
	}
	if got := m.PeerCount(); got != 1 {
		t.Errorf("PeerCount() = %d, want 1", got)
	}
}

func TestMultiPathHandleEventCapsConcurrentLinks(t *testing.T) {
	modes := []LinkMode{ModeBLE, ModeWiFiDirect, ModeWiFiStation, ModeUSBTether}
	links := make([]Link, 0, len(modes))
	for _, mode := range modes {
		links = append(links, newFakeLink(mode.String(), mode))
	}
	m := NewMultiPathManager(MultiPathConfig{Links: links, MaxLinks: 1})
	defer m.Stop()

	id := testPeerID(43)
	for _, mode := range modes {
		m.handleEvent(PeerEvent{
			Type: PeerDiscovered,
			Peer: PeerInfo{ID: id, Addrs: []string{"10.0.0.1:4443"}, LinkMode: mode},
		})
	}

	active, ok := m.GetPeerLinks(id)
	if !ok {
		t.Fatal("GetPeerLinks reported the peer as unknown")
	}
	// handleEvent hardcodes a cap of 3, ignoring the configured MaxLinks.
	if len(active) != 3 {
		t.Errorf("active links = %d, want 3 (the hardcoded cap in handleEvent)", len(active))
	}
	if active[ModeBLE] == nil {
		t.Error("the first discovered link was dropped")
	}
	if active[ModeUSBTether] != nil {
		t.Error("a link beyond the cap was admitted")
	}
}

func TestMultiPathHandleEventUpdateRefreshesExistingLink(t *testing.T) {
	ble := newFakeLink("ble", ModeBLE)
	m := NewMultiPathManager(MultiPathConfig{Links: []Link{ble}})
	defer m.Stop()

	id := testPeerID(44)
	m.handleEvent(PeerEvent{
		Type: PeerDiscovered,
		Peer: PeerInfo{ID: id, Addrs: []string{"10.0.0.1:4443"}, LinkMode: ModeBLE, Latency: 50 * time.Millisecond},
	})
	m.handleEvent(PeerEvent{
		Type: PeerUpdated,
		Peer: PeerInfo{ID: id, Addrs: []string{"10.0.0.1:4443"}, LinkMode: ModeBLE, Latency: 3 * time.Millisecond},
	})

	active, ok := m.GetPeerLinks(id)
	if !ok {
		t.Fatal("GetPeerLinks reported the peer as unknown")
	}
	if len(active) != 1 {
		t.Fatalf("active links = %d, want 1 (an update must not add a second link)", len(active))
	}
	if got := active[ModeBLE].Latency; got != 3*time.Millisecond {
		t.Errorf("Latency = %v, want the refreshed 3ms", got)
	}
}

func TestMultiPathSendToPeer(t *testing.T) {
	setup := func(t *testing.T) (*MultiPathManager, [32]byte, *pipeConn, *pipeConn, *fakeLink, *fakeLink) {
		t.Helper()
		primary := newPipeConn()
		backup := newPipeConn()

		ble := newFakeLink("ble", ModeBLE)
		ble.setConnect(func(string) (net.Conn, error) { return primary.local, nil })
		wd := newFakeLink("wifi-direct", ModeWiFiDirect)
		wd.setConnect(func(string) (net.Conn, error) { return backup.local, nil })

		m := NewMultiPathManager(MultiPathConfig{Links: []Link{ble, wd}, MaxLinks: 3})
		id := testPeerID(45)
		m.handleEvent(PeerEvent{
			Type: PeerDiscovered,
			Peer: PeerInfo{ID: id, Addrs: []string{"10.0.0.1:4443"}, LinkMode: ModeBLE, Latency: 2 * time.Millisecond},
		})
		m.handleEvent(PeerEvent{
			Type: PeerDiscovered,
			Peer: PeerInfo{ID: id, Addrs: []string{"10.0.0.2:4443"}, LinkMode: ModeWiFiDirect, Latency: 40 * time.Millisecond},
		})
		if _, err := m.ConnectToPeer(context.Background(), &PeerInfo{ID: id}); err != nil {
			t.Fatalf("ConnectToPeer: %v", err)
		}
		return m, id, primary, backup, ble, wd
	}

	t.Run("non-failover duplicates the payload to every active link", func(t *testing.T) {
		m, id, primary, backup, ble, wd := setup(t)
		defer m.Stop()

		payload := []byte("link-layer")
		n, err := m.SendToPeer(id, payload)
		if err != nil {
			t.Fatalf("SendToPeer: %v", err)
		}
		if n != len(payload) {
			t.Errorf("SendToPeer returned n = %d, want %d", n, len(payload))
		}
		// BLE is the primary (2ms vs 40ms), so both it and the backup must
		// have received the bytes.
		if !primary.sink.waitForLen(len(payload), 2*time.Second) {
			t.Errorf("primary sink = %q, want %q", primary.sink.bytes(), payload)
		} else if got := string(primary.sink.bytes()); got != string(payload) {
			t.Errorf("primary sink = %q, want %q", got, payload)
		}
		if !backup.sink.waitForLen(len(payload), 2*time.Second) {
			t.Errorf("backup sink = %q, want %q", backup.sink.bytes(), payload)
		} else if got := string(backup.sink.bytes()); got != string(payload) {
			t.Errorf("backup sink = %q, want %q", got, payload)
		}

		links, ok := m.GetPeerLinks(id)
		if !ok {
			t.Fatal("GetPeerLinks reported the peer as unknown")
		}
		for _, mode := range []LinkMode{ModeBLE, ModeWiFiDirect} {
			if got := links[mode].BytesSent; got != uint64(len(payload)) {
				t.Errorf("%s BytesSent = %d, want %d", mode, got, len(payload))
			}
		}
		if got := ble.connectCallCount(); got != 1 {
			t.Errorf("BLE Connect calls = %d, want 1", got)
		}
		if got := wd.connectCallCount(); got != 1 {
			t.Errorf("wifi-direct Connect calls = %d, want 1", got)
		}
	})

	t.Run("failover sends only over the primary link", func(t *testing.T) {
		m, id, primary, backup, _, _ := setup(t)
		defer m.Stop()

		m.SetAggregationMode(AggregationFailover)

		payload := []byte("solo")
		if _, err := m.SendToPeer(id, payload); err != nil {
			t.Fatalf("SendToPeer: %v", err)
		}
		if !primary.sink.waitForLen(len(payload), 2*time.Second) {
			t.Errorf("primary sink = %q, want %q", primary.sink.bytes(), payload)
		} else if got := string(primary.sink.bytes()); got != string(payload) {
			t.Errorf("primary sink = %q, want %q", got, payload)
		}
		// SendToPeer has returned and failover never writes to the backup, so
		// no further bytes can appear there.
		if got := backup.sink.len(); got != 0 {
			t.Errorf("backup sink received %d bytes under failover, want 0", got)
		}
	})

	t.Run("accumulates BytesSent across sends", func(t *testing.T) {
		m, id, primary, _, _, _ := setup(t)
		defer m.Stop()

		for i := 0; i < 3; i++ {
			if _, err := m.SendToPeer(id, []byte("ab")); err != nil {
				t.Fatalf("SendToPeer #%d: %v", i, err)
			}
		}
		if !primary.sink.waitForLen(6, 2*time.Second) {
			t.Errorf("primary sink = %d bytes, want 6", primary.sink.len())
		}
		links, _ := m.GetPeerLinks(id)
		if got := links[ModeBLE].BytesSent; got != 6 {
			t.Errorf("BytesSent = %d, want 6", got)
		}
	})

	t.Run("errors for an unknown peer", func(t *testing.T) {
		m := NewMultiPathManager(MultiPathConfig{})
		defer m.Stop()

		n, err := m.SendToPeer(testPeerID(46), []byte("x"))
		if err == nil {
			t.Fatal("SendToPeer succeeded for an unknown peer, want an error")
		}
		if n != 0 {
			t.Errorf("n = %d, want 0", n)
		}
	})

	t.Run("errors when the primary link has no live connection", func(t *testing.T) {
		ble := newFakeLink("ble", ModeBLE)
		m := NewMultiPathManager(MultiPathConfig{Links: []Link{ble}})
		defer m.Stop()

		id := testPeerID(47)
		m.handleEvent(PeerEvent{
			Type: PeerDiscovered,
			Peer: PeerInfo{ID: id, Addrs: []string{"10.0.0.1:4443"}, LinkMode: ModeBLE},
		})
		// Discover registers the link but never dials it, so Conn is nil.
		n, err := m.SendToPeer(id, []byte("x"))
		if err == nil {
			t.Fatal("SendToPeer succeeded without a connection, want an error")
		}
		if n != 0 {
			t.Errorf("n = %d, want 0", n)
		}
		if ble.connectCallCount() != 0 {
			t.Errorf("SendToPeer dialled the link %d times, want 0", ble.connectCallCount())
		}
	})
}

func TestMultiPathConnectToPeer(t *testing.T) {
	t.Run("rejects an unknown peer", func(t *testing.T) {
		m := NewMultiPathManager(MultiPathConfig{})
		defer m.Stop()

		conns, err := m.ConnectToPeer(context.Background(), &PeerInfo{ID: testPeerID(50)})
		if err == nil {
			t.Fatal("ConnectToPeer succeeded for an unknown peer, want an error")
		}
		if conns != nil {
			t.Error("ConnectToPeer returned connections alongside an error")
		}
	})

	t.Run("dials every active link once and caches the result", func(t *testing.T) {
		ble := newFakeLink("ble", ModeBLE)
		ble.setConnect(func(string) (net.Conn, error) { return newPipeConn().local, nil })
		wd := newFakeLink("wifi-direct", ModeWiFiDirect)
		wd.setConnect(func(string) (net.Conn, error) { return newPipeConn().local, nil })

		m := NewMultiPathManager(MultiPathConfig{Links: []Link{ble, wd}})
		defer m.Stop()

		id := testPeerID(51)
		for _, mode := range []LinkMode{ModeBLE, ModeWiFiDirect} {
			m.handleEvent(PeerEvent{
				Type: PeerDiscovered,
				Peer: PeerInfo{ID: id, Addrs: []string{"10.0.0.1:4443"}, LinkMode: mode},
			})
		}

		peer := &PeerInfo{ID: id}
		first, err := m.ConnectToPeer(context.Background(), peer)
		if err != nil {
			t.Fatalf("ConnectToPeer: %v", err)
		}
		if len(first) != 2 {
			t.Fatalf("connections = %d, want 2", len(first))
		}
		for _, mode := range []LinkMode{ModeBLE, ModeWiFiDirect} {
			if first[mode] == nil {
				t.Errorf("no connection returned for %s", mode)
			}
		}

		second, err := m.ConnectToPeer(context.Background(), peer)
		if err != nil {
			t.Fatalf("second ConnectToPeer: %v", err)
		}
		if len(second) != 2 {
			t.Errorf("connections on reuse = %d, want 2", len(second))
		}
		if got := ble.connectCallCount(); got != 1 {
			t.Errorf("BLE Connect calls = %d, want 1 (result should be cached)", got)
		}
		if got := wd.connectCallCount(); got != 1 {
			t.Errorf("wifi-direct Connect calls = %d, want 1 (result should be cached)", got)
		}
	})

	t.Run("errors when every dial fails", func(t *testing.T) {
		ble := newFakeLink("ble", ModeBLE)
		ble.setConnectErr(errors.New("refused"))

		m := NewMultiPathManager(MultiPathConfig{Links: []Link{ble}})
		defer m.Stop()

		id := testPeerID(52)
		m.handleEvent(PeerEvent{
			Type: PeerDiscovered,
			Peer: PeerInfo{ID: id, Addrs: []string{"10.0.0.1:4443"}, LinkMode: ModeBLE},
		})

		conns, err := m.ConnectToPeer(context.Background(), &PeerInfo{ID: id})
		if err == nil {
			t.Fatal("ConnectToPeer succeeded with only failing links, want an error")
		}
		if conns != nil {
			t.Error("ConnectToPeer returned connections alongside an error")
		}
		if ble.connectCallCount() != 1 {
			t.Errorf("Connect calls = %d, want 1", ble.connectCallCount())
		}
	})
}

func TestMultiPathGettersForUnknownPeer(t *testing.T) {
	m := NewMultiPathManager(MultiPathConfig{})
	defer m.Stop()

	if _, ok := m.GetAggregatedStats(testPeerID(53)); ok {
		t.Error("GetAggregatedStats reported an unknown peer as known")
	}
	if links, ok := m.GetPeerLinks(testPeerID(53)); ok {
		t.Errorf("GetPeerLinks reported an unknown peer as known: %v", links)
	}
}

func TestMultiPathSetAggregationMode(t *testing.T) {
	ble := newFakeLink("ble", ModeBLE)
	wd := newFakeLink("wifi-direct", ModeWiFiDirect)
	m := NewMultiPathManager(MultiPathConfig{Links: []Link{ble, wd}})
	defer m.Stop()

	id := testPeerID(54)
	m.handleEvent(PeerEvent{
		Type: PeerDiscovered,
		Peer: PeerInfo{ID: id, Addrs: []string{"10.0.0.1:4443"}, LinkMode: ModeBLE, Latency: 90 * time.Millisecond},
	})
	m.handleEvent(PeerEvent{
		Type: PeerDiscovered,
		Peer: PeerInfo{ID: id, Addrs: []string{"10.0.0.2:4443"}, LinkMode: ModeWiFiDirect, Latency: 4 * time.Millisecond},
	})

	// The second event installs a 4ms link, which must win on bandwidth.
	stats, _ := m.GetAggregatedStats(id)
	if stats.PrimaryLink != ModeWiFiDirect {
		t.Fatalf("PrimaryLink = %s, want wifi-direct", stats.PrimaryLink)
	}

	m.SetAggregationMode(AggregationFailover)
	stats, _ = m.GetAggregatedStats(id)
	if stats.PrimaryLink != ModeWiFiDirect {
		t.Errorf("PrimaryLink = %s after switching to failover, want the retained wifi-direct", stats.PrimaryLink)
	}
}

func TestMultiPathBestLink(t *testing.T) {
	station := newFakeLink("station", ModeWiFiStation)
	ble := newFakeLink("ble", ModeBLE)

	t.Run("no links", func(t *testing.T) {
		m := NewMultiPathManager(MultiPathConfig{})
		defer m.Stop()
		if got := m.BestLink(); got != nil {
			t.Errorf("BestLink() = %v, want nil", got)
		}
	})

	t.Run("preference order wins", func(t *testing.T) {
		station.setAvailable(true)
		ble.setAvailable(true)
		m := NewMultiPathManager(MultiPathConfig{Links: []Link{ble, station}})
		defer m.Stop()
		if got := m.BestLink(); got != Link(station) {
			t.Errorf("BestLink() = %v, want the wifi-station link", got)
		}
	})

	t.Run("skips unavailable links", func(t *testing.T) {
		station.setAvailable(false)
		ble.setAvailable(true)
		m := NewMultiPathManager(MultiPathConfig{Links: []Link{station, ble}})
		defer m.Stop()
		if got := m.BestLink(); got != Link(ble) {
			t.Errorf("BestLink() = %v, want the ble link", got)
		}
	})

	t.Run("nil when everything is down", func(t *testing.T) {
		station.setAvailable(false)
		ble.setAvailable(false)
		m := NewMultiPathManager(MultiPathConfig{Links: []Link{station, ble}})
		defer m.Stop()
		if got := m.BestLink(); got != nil {
			t.Errorf("BestLink() = %v, want nil", got)
		}
	})
}

func TestMultiPathPeers(t *testing.T) {
	ble := newFakeLink("ble", ModeBLE)
	m := NewMultiPathManager(MultiPathConfig{Links: []Link{ble}})
	defer m.Stop()

	if got := m.Peers(); len(got) != 0 {
		t.Errorf("Peers() = %d entries on a fresh manager, want 0", len(got))
	}

	id := testPeerID(55)
	m.handleEvent(PeerEvent{
		Type: PeerDiscovered,
		Peer: PeerInfo{ID: id, Addrs: []string{"10.0.0.1:4443"}, LinkMode: ModeBLE, Latency: 6 * time.Millisecond},
	})

	peers := m.Peers()
	if len(peers) != 1 {
		t.Fatalf("Peers() = %d entries, want 1", len(peers))
	}
	if peers[0].ID != id {
		t.Errorf("peer ID = %x, want %x", peers[0].ID, id)
	}
	if peers[0].LinkMode != ModeBLE {
		t.Errorf("LinkMode = %s, want ble", peers[0].LinkMode)
	}
	if len(peers[0].Addrs) != 1 || peers[0].Addrs[0] != "10.0.0.1:4443" {
		t.Errorf("Addrs = %v, want [10.0.0.1:4443]", peers[0].Addrs)
	}
	if want := 1.0 / 3.0; peers[0].Score != want {
		t.Errorf("Score = %v, want %v (one of three links)", peers[0].Score, want)
	}
}

func TestMultiPathPeersScoreReflectsLinkCount(t *testing.T) {
	ble := newFakeLink("ble", ModeBLE)
	wd := newFakeLink("wifi-direct", ModeWiFiDirect)
	m := NewMultiPathManager(MultiPathConfig{Links: []Link{ble, wd}})
	defer m.Stop()

	id := testPeerID(56)
	m.handleEvent(PeerEvent{
		Type: PeerDiscovered,
		Peer: PeerInfo{ID: id, Addrs: []string{"10.0.0.1:4443"}, LinkMode: ModeBLE},
	})
	if got := m.Peers()[0].Score; got != 1.0/3.0 {
		t.Errorf("Score with one link = %v, want %v", got, 1.0/3.0)
	}

	m.handleEvent(PeerEvent{
		Type: PeerDiscovered,
		Peer: PeerInfo{ID: id, Addrs: []string{"10.0.0.2:4443"}, LinkMode: ModeWiFiDirect},
	})
	if got := m.Peers()[0].Score; got != 2.0/3.0 {
		t.Errorf("Score with two links = %v, want %v", got, 2.0/3.0)
	}
}

func TestMultiPathRunAndStop(t *testing.T) {
	rec := &peerRecorder{}
	ble := newFakeLink("ble", ModeBLE)
	m := NewMultiPathManager(MultiPathConfig{Links: []Link{ble}, OnPeer: rec.on})

	if err := m.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !waitFor(2*time.Second, func() bool { return ble.discoverCallCount() == 1 }) {
		t.Fatal("Discover was never called")
	}

	id := testPeerID(57)
	ble.emit(PeerEvent{
		Type: PeerDiscovered,
		Peer: PeerInfo{ID: id, Addrs: []string{"10.0.0.1:4443"}, LinkMode: ModeBLE},
	})
	if !waitFor(2*time.Second, func() bool { return m.PeerCount() == 1 }) {
		t.Fatalf("peer not recorded: PeerCount() = %d", m.PeerCount())
	}
	if !waitFor(2*time.Second, func() bool { return rec.count() == 1 }) {
		t.Errorf("OnPeer fired %d times, want 1", rec.count())
	}

	m.Stop()
	if got := ble.stopCallCount(); got != 1 {
		t.Errorf("link Stop called %d times, want 1", got)
	}
}

func TestMultiPathLinkForMode(t *testing.T) {
	ble := newFakeLink("ble", ModeBLE)
	m := NewMultiPathManager(MultiPathConfig{Links: []Link{ble}})
	defer m.Stop()

	if got := m.linkForMode(ModeBLE); got != Link(ble) {
		t.Errorf("linkForMode(ble) = %v, want the registered link", got)
	}
	if got := m.linkForMode(ModeWiFiStation); got != nil {
		t.Errorf("linkForMode(wifi-station) = %v, want nil", got)
	}
}

func TestMultiPathSendToPeerPropagatesWriteError(t *testing.T) {
	ble := newFakeLink("ble", ModeBLE)
	ble.setConnect(func(string) (net.Conn, error) { return writeFailConn{}, nil })

	m := NewMultiPathManager(MultiPathConfig{Links: []Link{ble}})
	defer m.Stop()

	id := testPeerID(58)
	m.handleEvent(PeerEvent{
		Type: PeerDiscovered,
		Peer: PeerInfo{ID: id, Addrs: []string{"10.0.0.1:4443"}, LinkMode: ModeBLE},
	})
	if _, err := m.ConnectToPeer(context.Background(), &PeerInfo{ID: id}); err != nil {
		t.Fatalf("ConnectToPeer: %v", err)
	}

	n, err := m.SendToPeer(id, []byte("payload"))
	if !errors.Is(err, errWriteFailed) {
		t.Fatalf("SendToPeer error = %v, want %v", err, errWriteFailed)
	}
	if n != 0 {
		t.Errorf("n = %d, want 0", n)
	}

	links, ok := m.GetPeerLinks(id)
	if !ok {
		t.Fatal("GetPeerLinks reported the peer as unknown")
	}
	if got := links[ModeBLE].BytesSent; got != 0 {
		t.Errorf("BytesSent = %d after a failed write, want 0", got)
	}
}

func TestMultiPathRunSkipsDisabledAndUnavailableLinks(t *testing.T) {
	enabled := newFakeLink("enabled", ModeBLE)
	unavailable := newFakeLink("unavailable", ModeWiFiDirect)
	disabled := newFakeLink("disabled", ModeWiFiStation)

	m := NewMultiPathManager(MultiPathConfig{
		Links: []Link{enabled, unavailable, disabled},
		Configs: map[LinkMode]LinkConfig{
			ModeBLE:         {Mode: ModeBLE, Enabled: true},
			ModeWiFiDirect:  {Mode: ModeWiFiDirect, Enabled: true},
			ModeWiFiStation: {Mode: ModeWiFiStation, Enabled: false},
		},
	})

	unavailable.setAvailable(false)

	if err := m.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	defer m.Stop()

	if !waitFor(2*time.Second, func() bool { return enabled.discoverCallCount() == 1 }) {
		t.Error("enabled link never started discovery")
	}
	if got := unavailable.discoverCallCount(); got != 0 {
		t.Errorf("unavailable link Discover called %d times, want 0", got)
	}
	if got := disabled.discoverCallCount(); got != 0 {
		t.Errorf("disabled link Discover called %d times, want 0", got)
	}
}

func TestMultiPathStopWithoutRun(t *testing.T) {
	ble := newFakeLink("ble", ModeBLE)
	ble.setStopErr(errors.New("teardown failed"))

	m := NewMultiPathManager(MultiPathConfig{Links: []Link{ble}})
	done := make(chan struct{})
	go func() {
		m.Stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop did not return")
	}
	if got := ble.stopCallCount(); got != 1 {
		t.Errorf("link Stop called %d times, want 1", got)
	}
}

// The PeerLost branch holds the peer link set's lock and then calls updateStats,
// which takes that same non-reentrant lock again. Run the event in a goroutine
// so a regression fails the test instead of hanging it.
func TestMultiPathPeerLostDoesNotDeadlock(t *testing.T) {
	ble := newFakeLink("ble", ModeBLE)
	m := NewMultiPathManager(MultiPathConfig{Links: []Link{ble}})
	defer m.Stop()

	id := testPeerID(61)
	m.handleEvent(PeerEvent{
		Type: PeerDiscovered,
		Peer: PeerInfo{ID: id, Addrs: []string{"10.0.0.1:4443"}, LinkMode: ModeBLE},
	})

	done := make(chan struct{})
	go func() {
		defer close(done)
		m.handleEvent(PeerEvent{Type: PeerLost, Peer: PeerInfo{ID: id, LinkMode: ModeBLE}})
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("handleEvent never returned: PeerLost deadlocked on the peer link set lock")
	}

	// BLE was the only link, so losing it must retire the peer entirely.
	if got := m.PeerCount(); got != 0 {
		t.Errorf("PeerCount() = %d after losing the only link, want 0", got)
	}
	if _, ok := m.GetAggregatedStats(id); ok {
		t.Error("GetAggregatedStats still reports a retired peer")
	}
}

// A sighting can arrive with no address at all; indexing Addrs[0] unconditionally
// panicked. The link mode is still recorded, just undialable.
func TestMultiPathHandleEventWithNoAddresses(t *testing.T) {
	ble := newFakeLink("ble", ModeBLE)
	m := NewMultiPathManager(MultiPathConfig{Links: []Link{ble}})
	defer m.Stop()

	id := testPeerID(62)
	m.handleEvent(PeerEvent{
		Type: PeerDiscovered,
		Peer: PeerInfo{ID: id, LinkMode: ModeBLE},
	})

	links, ok := m.GetPeerLinks(id)
	if !ok {
		t.Fatal("GetPeerLinks reported the peer as unknown")
	}
	conn := links[ModeBLE]
	if conn == nil {
		t.Fatal("no connection recorded for an address-less BLE sighting")
	}
	if conn.PeerAddr != "" {
		t.Errorf("PeerAddr = %q, want empty for an address-less sighting", conn.PeerAddr)
	}

	stats, ok := m.GetAggregatedStats(id)
	if !ok {
		t.Fatal("GetAggregatedStats reported the peer as unknown")
	}
	if stats.TotalLinks != 1 {
		t.Errorf("TotalLinks = %d, want 1", stats.TotalLinks)
	}
}

// A discovery event can name a link mode the manager was never given a Link for,
// so the stored connection has no Link behind it. Nothing may dereference it.
func TestMultiPathHandleEventWithoutRegisteredLink(t *testing.T) {
	m := NewMultiPathManager(MultiPathConfig{})
	defer m.Stop()

	id := testPeerID(63)
	m.handleEvent(PeerEvent{
		Type: PeerDiscovered,
		Peer: PeerInfo{ID: id, Addrs: []string{"10.0.0.1:4443"}, LinkMode: ModeWiFiDirect},
	})

	links, ok := m.GetPeerLinks(id)
	if !ok {
		t.Fatal("GetPeerLinks reported the peer as unknown")
	}
	conn := links[ModeWiFiDirect]
	if conn == nil {
		t.Fatal("no connection recorded for the discovered mode")
	}
	if conn.Link != nil {
		t.Errorf("Link = %v, want nil when no link of that mode is registered", conn.Link)
	}

	// With no Link behind it there is no primary to name: the zero value of
	// LinkMode is wifi-station, so leaving it untouched would claim a link the
	// manager never had.
	stats, ok := m.GetAggregatedStats(id)
	if !ok {
		t.Fatal("GetAggregatedStats reported the peer as unknown")
	}
	if stats.PrimaryLink != ModeNone {
		t.Errorf("PrimaryLink = %s, want none", stats.PrimaryLink)
	}
	if got := m.Peers(); len(got) != 0 {
		t.Errorf("Peers() = %d entries, want 0 without a dialable primary", len(got))
	}

	// Connecting must report the failure rather than dereference a nil Link.
	if conns, err := m.ConnectToPeer(context.Background(), &PeerInfo{ID: id}); err == nil {
		t.Errorf("ConnectToPeer = %v with no nil error, want an error", conns)
	}
}

// AggregationFailover is 0, so the constructor used to read an explicit failover
// request as "unset" and silently substituted bandwidth.
func TestNewMultiPathManagerReachesFailoverThroughTheConstructor(t *testing.T) {
	ble := newFakeLink("ble", ModeBLE)
	wd := newFakeLink("wifi-direct", ModeWiFiDirect)

	failover := AggregationFailover
	m := NewMultiPathManager(MultiPathConfig{Links: []Link{ble, wd}, Aggregation: &failover})
	defer m.Stop()

	if m.aggregation != AggregationFailover {
		t.Fatalf("aggregation = %s, want failover", m.aggregation)
	}

	id := testPeerID(64)
	m.handleEvent(PeerEvent{
		Type: PeerDiscovered,
		Peer: PeerInfo{ID: id, Addrs: []string{"10.0.0.1:4443"}, LinkMode: ModeBLE, Latency: 90 * time.Millisecond},
	})
	// The much faster link arrives second; failover must keep the established
	// primary instead of migrating to it.
	m.handleEvent(PeerEvent{
		Type: PeerDiscovered,
		Peer: PeerInfo{ID: id, Addrs: []string{"10.0.0.2:4443"}, LinkMode: ModeWiFiDirect, Latency: 4 * time.Millisecond},
	})

	stats, ok := m.GetAggregatedStats(id)
	if !ok {
		t.Fatal("GetAggregatedStats reported the peer as unknown")
	}
	if stats.PrimaryLink != ModeBLE {
		t.Errorf("PrimaryLink = %s, want ble (failover keeps the established primary)", stats.PrimaryLink)
	}
}
