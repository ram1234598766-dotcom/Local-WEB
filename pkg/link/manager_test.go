package link

import (
	"context"
	"errors"
	"math"
	"net"
	"testing"
	"time"
)

func TestNewManagerDefaults(t *testing.T) {
	m := NewManager(ManagerConfig{})
	if m == nil {
		t.Fatal("NewManager returned nil")
	}
	defer m.Stop()

	if len(m.preferences) != len(defaultPreferences()) {
		t.Fatalf("preferences = %d entries, want %d", len(m.preferences), len(defaultPreferences()))
	}
	for i, want := range defaultPreferences() {
		if m.preferences[i] != want {
			t.Errorf("preferences[%d] = %s, want %s", i, m.preferences[i], want)
		}
	}
	if len(m.configs) != len(DefaultLinkConfigs()) {
		t.Errorf("configs = %d entries, want %d", len(m.configs), len(DefaultLinkConfigs()))
	}
	if cap(m.events) != 64 {
		t.Errorf("events channel capacity = %d, want 64", cap(m.events))
	}
	if m.active != nil {
		t.Errorf("active = %v, want nil before any selection", m.active)
	}
	if got := m.PeerCount(); got != 0 {
		t.Errorf("PeerCount() = %d on a fresh manager, want 0", got)
	}
	if m.ctx == nil || m.ctx.Err() != nil {
		t.Error("manager context should start live")
	}
}

func TestNewManagerHonoursConfig(t *testing.T) {
	rec := &peerRecorder{}
	ble := newFakeLink("ble", ModeBLE)
	prefs := []LinkMode{ModeBLE}
	cfgs := map[LinkMode]LinkConfig{ModeBLE: {Mode: ModeBLE, Enabled: true}}

	m := NewManager(ManagerConfig{
		Links:        []Link{ble},
		Preferences:  prefs,
		Configs:      cfgs,
		OnPeer:       rec.on,
		AutoEscalate: false,
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
	if len(m.links) != 1 || m.links[0] != Link(ble) {
		t.Errorf("links not stored: %v", m.links)
	}
}

func TestManagerBestLink(t *testing.T) {
	station := newFakeLink("station", ModeWiFiStation)
	ble := newFakeLink("ble", ModeBLE)

	tests := []struct {
		name           string
		links          []Link
		prefs          []LinkMode
		stationOffline bool
		bleOffline     bool
		want           Link
	}{
		{
			name:  "no links",
			links: nil,
			prefs: nil,
			want:  nil,
		},
		{
			name:  "preference beats slice order",
			links: []Link{ble, station},
			prefs: nil,
			want:  station,
		},
		{
			name:           "skips unavailable preferred link",
			links:          []Link{station, ble},
			prefs:          nil,
			stationOffline: true,
			want:           ble,
		},
		{
			name:           "nil when nothing is available",
			links:          []Link{station},
			prefs:          nil,
			stationOffline: true,
			want:           nil,
		},
		{
			name:       "nil when only an unlisted mode is available",
			links:      []Link{ble},
			prefs:      nil,
			bleOffline: true,
			want:       nil,
		},
		{
			name:  "custom preferences override defaults",
			links: []Link{station, ble},
			prefs: []LinkMode{ModeBLE},
			want:  ble,
		},
		{
			name:  "preference list not matching any link",
			links: []Link{ble},
			prefs: []LinkMode{ModeUSBTether, ModeAdHocWiFi},
			want:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			station.setAvailable(!tt.stationOffline)
			ble.setAvailable(!tt.bleOffline)

			m := NewManager(ManagerConfig{Links: tt.links, Preferences: tt.prefs})
			defer m.Stop()

			if got := m.BestLink(); got != tt.want {
				t.Errorf("BestLink() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestManagerLinkForMode(t *testing.T) {
	ble := newFakeLink("ble", ModeBLE)
	m := NewManager(ManagerConfig{Links: []Link{ble}})
	defer m.Stop()

	if got := m.linkForMode(ModeBLE); got != Link(ble) {
		t.Errorf("linkForMode(ble) = %v, want the registered link", got)
	}
	if got := m.linkForMode(ModeWiFiStation); got != nil {
		t.Errorf("linkForMode(wifi-station) = %v, want nil", got)
	}
}

func TestManagerHandleEventDiscoveredAddsPeer(t *testing.T) {
	rec := &peerRecorder{}
	m := NewManager(ManagerConfig{OnPeer: rec.on})
	defer m.Stop()

	id := testPeerID(1)
	m.handleEvent(PeerEvent{
		Type: PeerDiscovered,
		Peer: PeerInfo{
			ID:       id,
			Name:     "alpha",
			Addrs:    []string{"10.0.0.1:4443"},
			LinkMode: ModeBLE,
			LastSeen: time.Now(),
		},
	})

	if got := m.PeerCount(); got != 1 {
		t.Fatalf("PeerCount() = %d, want 1", got)
	}
	peers := m.Peers()
	if len(peers) != 1 {
		t.Fatalf("Peers() returned %d entries, want 1", len(peers))
	}
	if peers[0].ID != id {
		t.Errorf("stored peer ID = %x, want %x", peers[0].ID, id)
	}
	if peers[0].Score != 0.5 {
		t.Errorf("initial score = %v, want 0.5", peers[0].Score)
	}
	if got := rec.types(); len(got) != 1 || got[0] != PeerDiscovered {
		t.Errorf("OnPeer types = %v, want [PeerDiscovered]", got)
	}
}

func TestManagerHandleEventUpdateReplacesWorseScore(t *testing.T) {
	m := NewManager(ManagerConfig{})
	defer m.Stop()

	id := testPeerID(2)
	m.handleEvent(PeerEvent{
		Type: PeerDiscovered,
		Peer: PeerInfo{ID: id, Name: "original", LastSeen: time.Now().Add(-time.Hour)},
	})

	// Fresh, low-latency, strong-signal sighting scores well above the 0.5
	// initial score, so the stored record must be replaced.
	m.handleEvent(PeerEvent{
		Type: PeerUpdated,
		Peer: PeerInfo{
			ID:       id,
			Name:     "improved",
			LinkMode: ModeWiFiDirect,
			LastSeen: time.Now(),
			Latency:  time.Millisecond,
			RSSI:     -40,
		},
	})

	peers := m.Peers()
	if len(peers) != 1 {
		t.Fatalf("Peers() returned %d entries, want 1", len(peers))
	}
	if peers[0].Name != "improved" {
		t.Errorf("peer name = %q, want the higher-scoring observation %q", peers[0].Name, "improved")
	}
	if want := 0.9; math.Abs(peers[0].Score-want) > 1e-9 {
		t.Errorf("score = %v, want %v", peers[0].Score, want)
	}
	if peers[0].LinkMode != ModeWiFiDirect {
		t.Errorf("LinkMode = %s, want wifi-direct", peers[0].LinkMode)
	}
}

func TestManagerHandleEventUpdateKeepsBetterExisting(t *testing.T) {
	m := NewManager(ManagerConfig{})
	defer m.Stop()

	id := testPeerID(3)
	m.handleEvent(PeerEvent{
		Type: PeerDiscovered,
		Peer: PeerInfo{ID: id, Name: "original", LastSeen: time.Now().Add(-time.Hour)},
	})
	m.handleEvent(PeerEvent{
		Type: PeerUpdated,
		Peer: PeerInfo{
			ID:       id,
			Name:     "improved",
			LastSeen: time.Now(),
			Latency:  time.Millisecond,
			RSSI:     -40,
		},
	})

	// A stale, slow, weak observation scores 0.5, which does not beat the
	// stored 0.9, so the better record must survive.
	m.handleEvent(PeerEvent{
		Type: PeerUpdated,
		Peer: PeerInfo{
			ID:       id,
			Name:     "worse",
			LastSeen: time.Now().Add(-time.Hour),
			Latency:  200 * time.Millisecond,
			RSSI:     -100,
		},
	})

	peers := m.Peers()
	if len(peers) != 1 {
		t.Fatalf("Peers() returned %d entries, want 1", len(peers))
	}
	if peers[0].Name != "improved" {
		t.Errorf("peer name = %q, want the retained %q", peers[0].Name, "improved")
	}
	if want := 0.9; math.Abs(peers[0].Score-want) > 1e-9 {
		t.Errorf("score = %v, want the retained %v", peers[0].Score, want)
	}
	if got := m.PeerCount(); got != 1 {
		t.Errorf("PeerCount() = %d, want 1", got)
	}
}

func TestManagerHandleEventLost(t *testing.T) {
	rec := &peerRecorder{}
	m := NewManager(ManagerConfig{OnPeer: rec.on})
	defer m.Stop()

	id := testPeerID(4)
	m.handleEvent(PeerEvent{Type: PeerDiscovered, Peer: PeerInfo{ID: id}})
	if got := m.PeerCount(); got != 1 {
		t.Fatalf("PeerCount() = %d, want 1", got)
	}

	m.handleEvent(PeerEvent{Type: PeerLost, Peer: PeerInfo{ID: id}})
	if got := m.PeerCount(); got != 0 {
		t.Errorf("PeerCount() = %d after PeerLost, want 0", got)
	}
	if got := len(m.Peers()); got != 0 {
		t.Errorf("Peers() returned %d entries after PeerLost, want 0", got)
	}

	// Losing an unknown peer must be a no-op, not a panic.
	m.handleEvent(PeerEvent{Type: PeerLost, Peer: PeerInfo{ID: testPeerID(5)}})
	if got := m.PeerCount(); got != 0 {
		t.Errorf("PeerCount() = %d, want 0", got)
	}
	if got := rec.count(); got != 3 {
		t.Errorf("OnPeer fired %d times, want 3 (every event is forwarded)", got)
	}
}

func TestManagerPeersReturnsFreshSlice(t *testing.T) {
	m := NewManager(ManagerConfig{})
	defer m.Stop()

	id := testPeerID(6)
	m.handleEvent(PeerEvent{Type: PeerDiscovered, Peer: PeerInfo{ID: id, Name: "kept"}})

	snap := m.Peers()
	if len(snap) != 1 {
		t.Fatalf("Peers() returned %d entries, want 1", len(snap))
	}

	// Overwriting an entry in the snapshot must not reach the manager's own
	// peer table.
	replacement := &PeerInfo{ID: testPeerID(7), Name: "injected"}
	snap[0] = replacement
	if snap[0] != replacement {
		t.Fatal("writing to the snapshot did not take effect")
	}

	again := m.Peers()
	if len(again) != 1 {
		t.Fatalf("Peers() returned %d entries after the snapshot was written, want 1", len(again))
	}
	if again[0].ID != id {
		t.Errorf("manager peer ID = %x, want the original %x", again[0].ID, id)
	}
	if again[0].Name != "kept" {
		t.Errorf("manager peer name = %q, want the original %q", again[0].Name, "kept")
	}
}

func TestManagerActiveLinkDefaultsToNil(t *testing.T) {
	m := NewManager(ManagerConfig{Links: []Link{newFakeLink("ble", ModeBLE)}})
	defer m.Stop()

	if got := m.ActiveLink(); got != nil {
		t.Errorf("ActiveLink() = %v, want nil (nothing ever assigns m.active)", got)
	}
}

func TestManagerConnectToPeer(t *testing.T) {
	errFail := errors.New("dial refused")

	t.Run("uses the discovery link first", func(t *testing.T) {
		ble := newFakeLink("ble", ModeBLE)
		station := newFakeLink("station", ModeWiFiStation)
		m := NewManager(ManagerConfig{Links: []Link{ble, station}})
		defer m.Stop()

		ble.setConnect(func(string) (net.Conn, error) { return newPipeConn().local, nil })
		station.setConnect(func(string) (net.Conn, error) {
			t.Error("station link should not be dialled when BLE succeeds")
			return nil, errFail
		})

		peer := &PeerInfo{ID: testPeerID(10), LinkMode: ModeBLE, Addrs: []string{"fe80::1:4443"}}
		conn, err := m.ConnectToPeer(context.Background(), peer)
		if err != nil {
			t.Fatalf("ConnectToPeer: %v", err)
		}
		if conn == nil {
			t.Fatal("ConnectToPeer returned a nil conn with a nil error")
		}
		_ = conn.Close()
		if got := ble.connectedAddrs(); len(got) != 1 || got[0] != "fe80::1:4443" {
			t.Errorf("BLE dialled %v, want [fe80::1:4443]", got)
		}
		if got := station.connectCallCount(); got != 0 {
			t.Errorf("station Connect calls = %d, want 0", got)
		}
	})

	t.Run("falls back to another link when the discovery link fails", func(t *testing.T) {
		ble := newFakeLink("ble", ModeBLE)
		station := newFakeLink("station", ModeWiFiStation)
		m := NewManager(ManagerConfig{Links: []Link{ble, station}})
		defer m.Stop()

		ble.setConnectErr(errFail)
		station.setConnect(func(string) (net.Conn, error) { return newPipeConn().local, nil })

		peer := &PeerInfo{ID: testPeerID(11), LinkMode: ModeBLE, Addrs: []string{"10.0.0.9:4443"}}
		conn, err := m.ConnectToPeer(context.Background(), peer)
		if err != nil {
			t.Fatalf("ConnectToPeer: %v", err)
		}
		_ = conn.Close()
		if got := station.connectCallCount(); got != 1 {
			t.Errorf("station Connect calls = %d, want 1", got)
		}
	})

	t.Run("errors when no link can reach the peer", func(t *testing.T) {
		ble := newFakeLink("ble", ModeBLE)
		ble.setConnectErr(errFail)
		m := NewManager(ManagerConfig{Links: []Link{ble}})
		defer m.Stop()

		peer := &PeerInfo{ID: testPeerID(12), LinkMode: ModeBLE, Addrs: []string{"10.0.0.9:4443"}}
		conn, err := m.ConnectToPeer(context.Background(), peer)
		if err == nil {
			t.Fatal("ConnectToPeer succeeded, want an error")
		}
		if conn != nil {
			t.Error("ConnectToPeer returned a conn alongside an error")
		}
	})

	t.Run("errors when the peer advertises no addresses", func(t *testing.T) {
		ble := newFakeLink("ble", ModeBLE)
		m := NewManager(ManagerConfig{Links: []Link{ble}})
		defer m.Stop()

		peer := &PeerInfo{ID: testPeerID(13), LinkMode: ModeBLE}
		if _, err := m.ConnectToPeer(context.Background(), peer); err == nil {
			t.Fatal("ConnectToPeer succeeded with no addresses, want an error")
		}
		if got := ble.connectCallCount(); got != 0 {
			t.Errorf("Connect calls = %d, want 0", got)
		}
	})
}

// computeScore is the only quality signal the package has, so it must actually
// respond to recency, latency and signal strength.
func TestComputeScore(t *testing.T) {
	fresh := time.Now()
	stale := time.Now().Add(-time.Hour)

	tests := []struct {
		name string
		peer PeerInfo
		want float64
	}{
		{
			name: "stale, slow, weak",
			peer: PeerInfo{LastSeen: stale, Latency: 100 * time.Millisecond, RSSI: -100},
			want: 0.5,
		},
		{
			name: "fresh only",
			peer: PeerInfo{LastSeen: fresh, Latency: 100 * time.Millisecond, RSSI: -100},
			want: 0.7,
		},
		{
			name: "mid latency and mid signal",
			peer: PeerInfo{LastSeen: stale, Latency: 30 * time.Millisecond, RSSI: -70},
			want: 0.6,
		},
		{
			name: "all bonuses applied",
			peer: PeerInfo{LastSeen: fresh, Latency: 5 * time.Millisecond, RSSI: -50},
			want: 0.9,
		},
		{
			name: "recency and latency but weak signal",
			peer: PeerInfo{LastSeen: fresh, Latency: 5 * time.Millisecond, RSSI: -100},
			want: 0.8,
		},
		{
			name: "signal only",
			peer: PeerInfo{LastSeen: stale, Latency: 100 * time.Millisecond, RSSI: -40},
			want: 0.6,
		},
		{
			name: "mid latency with strong signal",
			peer: PeerInfo{LastSeen: stale, Latency: 20 * time.Millisecond, RSSI: -50},
			want: 0.65,
		},
		{
			// Latency 0 and RSSI 0 read as "excellent" even though neither was
			// ever measured; a zero-value observation is not a bad peer.
			name: "unmeasured zero value",
			peer: PeerInfo{},
			want: 0.7,
		},
	}

	old := &PeerInfo{Score: 0.1}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeScore(old, &tt.peer)
			if math.Abs(got-tt.want) > 1e-9 {
				t.Errorf("computeScore() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestComputeScoreNeverExceedsOne(t *testing.T) {
	// The documented maximum bonus stack is 0.5 + 0.2 + 0.1 + 0.1 = 0.9, so
	// the clamp at 1.0 is currently unreachable. Assert that the output always
	// stays inside the documented 0.0-1.0 range.
	cases := []PeerInfo{
		{LastSeen: time.Now(), Latency: 0, RSSI: 0},
		{LastSeen: time.Now(), Latency: time.Nanosecond, RSSI: 1},
		{LastSeen: time.Now(), Latency: 5 * time.Millisecond, RSSI: -1},
		{},
	}
	for _, p := range cases {
		got := computeScore(&PeerInfo{}, &p)
		if got < 0 || got > 1 {
			t.Errorf("computeScore() = %v, want within [0,1]", got)
		}
	}
}

func TestComputeScoreIsMonotonic(t *testing.T) {
	fresh := time.Now()
	stale := time.Now().Add(-time.Hour)

	// The floor: nothing measured and nothing good about the sighting.
	worst := PeerInfo{LastSeen: stale, Latency: 100 * time.Millisecond, RSSI: -100}
	worstScore := computeScore(&PeerInfo{}, &worst)
	if worstScore != 0.5 {
		t.Fatalf("baseline score = %v, want the 0.5 base", worstScore)
	}

	// Every single improvement must strictly raise the score.
	improvements := map[string]PeerInfo{
		"fresher":  {LastSeen: fresh, Latency: 100 * time.Millisecond, RSSI: -100},
		"faster":   {LastSeen: stale, Latency: 5 * time.Millisecond, RSSI: -100},
		"stronger": {LastSeen: stale, Latency: 100 * time.Millisecond, RSSI: -40},
	}
	for name, p := range improvements {
		if got := computeScore(&PeerInfo{}, &p); got <= worstScore {
			t.Errorf("%s sighting scored %v, want more than %v", name, got, worstScore)
		}
	}

	best := PeerInfo{LastSeen: fresh, Latency: 5 * time.Millisecond, RSSI: -50}
	bestScore := computeScore(&PeerInfo{}, &best)
	if bestScore <= worstScore {
		t.Fatalf("best-case score %v is not above the baseline %v", bestScore, worstScore)
	}

	// Degrading any one dimension from the best case must lower the score.
	degraded := map[string]PeerInfo{
		"stale":   {LastSeen: stale, Latency: 5 * time.Millisecond, RSSI: -50},
		"slower":  {LastSeen: fresh, Latency: 200 * time.Millisecond, RSSI: -50},
		"weaker":  {LastSeen: fresh, Latency: 5 * time.Millisecond, RSSI: -120},
		"nothing": {LastSeen: stale, Latency: 200 * time.Millisecond, RSSI: -120},
	}
	for name, p := range degraded {
		if got := computeScore(&PeerInfo{}, &p); got >= bestScore {
			t.Errorf("%s sighting scored %v, want less than %v", name, got, bestScore)
		}
	}
}

func TestComputeScoreIgnoresPreviousRecord(t *testing.T) {
	new_ := &PeerInfo{LastSeen: time.Now(), Latency: 5 * time.Millisecond, RSSI: -50}
	highOld := &PeerInfo{Score: 1.0, Latency: time.Millisecond, RSSI: -10, Addrs: []string{"a", "b"}}
	lowOld := &PeerInfo{Score: 0.0}

	a := computeScore(highOld, new_)
	b := computeScore(lowOld, new_)
	if a != b {
		t.Errorf("score depends on the previous record: %v vs %v", a, b)
	}
}

func TestManagerRunDiscoversPeersAndStops(t *testing.T) {
	rec := &peerRecorder{}
	ble := newFakeLink("ble", ModeBLE)
	m := NewManager(ManagerConfig{Links: []Link{ble}, OnPeer: rec.on})

	if err := m.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !waitFor(2*time.Second, func() bool { return ble.discoverCallCount() == 1 }) {
		t.Fatalf("Discover called %d times, want 1", ble.discoverCallCount())
	}

	id := testPeerID(20)
	ble.emit(PeerEvent{
		Type: PeerDiscovered,
		Peer: PeerInfo{
			ID:       id,
			Name:     "remote",
			Addrs:    []string{"10.0.0.5:4443"},
			LinkMode: ModeBLE,
			LastSeen: time.Now(),
		},
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
	if m.ctx.Err() == nil {
		t.Error("manager context not canceled by Stop")
	}
}

func TestManagerRunSkipsDisabledAndUnavailableLinks(t *testing.T) {
	enabled := newFakeLink("enabled", ModeBLE)
	unavailable := newFakeLink("unavailable", ModeWiFiDirect)
	disabled := newFakeLink("disabled", ModeWiFiStation)

	m := NewManager(ManagerConfig{
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
		t.Fatal("enabled link never started discovery")
	}
	if got := unavailable.discoverCallCount(); got != 0 {
		t.Errorf("unavailable link Discover called %d times, want 0", got)
	}
	if got := disabled.discoverCallCount(); got != 0 {
		t.Errorf("disabled link Discover called %d times, want 0", got)
	}
}

func TestManagerRunWithFailingDiscoveryStillStops(t *testing.T) {
	ble := newFakeLink("ble", ModeBLE)
	ble.setDiscoverErr(errors.New("radio busy"))

	m := NewManager(ManagerConfig{Links: []Link{ble}})
	if err := m.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !waitFor(2*time.Second, func() bool { return ble.discoverCallCount() == 1 }) {
		t.Fatal("Discover was never attempted")
	}

	done := make(chan struct{})
	go func() {
		m.Stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop did not return after a failed discovery")
	}

	if got := ble.stopCallCount(); got != 1 {
		t.Errorf("link Stop called %d times, want 1", got)
	}
}

func TestManagerStopPropagatesLinkStopErrors(t *testing.T) {
	ble := newFakeLink("ble", ModeBLE)
	ble.setStopErr(errors.New("teardown failed"))

	m := NewManager(ManagerConfig{Links: []Link{ble}})
	// Stop must still complete and still stop every link even when one errors.
	m.Stop()

	if got := ble.stopCallCount(); got != 1 {
		t.Errorf("link Stop called %d times, want 1", got)
	}
}

func TestManagerConcurrentHandleEvent(t *testing.T) {
	rec := &peerRecorder{}
	m := NewManager(ManagerConfig{OnPeer: rec.on})
	defer m.Stop()

	const writers = 8
	const peers = 6

	done := make(chan struct{})
	for w := 0; w < writers; w++ {
		go func() {
			defer func() { done <- struct{}{} }()
			for p := 0; p < peers; p++ {
				m.handleEvent(PeerEvent{
					Type: PeerDiscovered,
					Peer: PeerInfo{
						ID:       testPeerID(byte(p)),
						LastSeen: time.Now(),
					},
				})
			}
		}()
	}
	for w := 0; w < writers; w++ {
		<-done
	}

	if got := m.PeerCount(); got != peers {
		t.Errorf("PeerCount() = %d, want %d", got, peers)
	}
	if got := rec.count(); got != writers*peers {
		t.Errorf("OnPeer fired %d times, want %d", got, writers*peers)
	}
}

// Auto-escalation runs inside handleEvent, which already holds m.mu; taking the
// same RWMutex again is not reentrant and would wedge the event goroutine (and
// with it Stop) forever. Run the call in a goroutine so a regression fails the
// test instead of hanging it.
func TestManagerAutoEscalateDoesNotDeadlock(t *testing.T) {
	tests := []struct {
		name string
		typ  EventType
	}{
		{"discovered", PeerDiscovered},
		{"updated", PeerUpdated},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewManager(ManagerConfig{AutoEscalate: true})
			defer m.Stop()

			done := make(chan struct{})
			go func() {
				defer close(done)
				m.handleEvent(PeerEvent{
					Type: tt.typ,
					Peer: PeerInfo{
						ID:       testPeerID(60),
						Addrs:    []string{"10.0.0.1:4443"},
						LinkMode: ModeBLE,
						LastSeen: time.Now(),
					},
				})
			}()

			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("handleEvent never returned: auto-escalation deadlocked on m.mu")
			}

			if got := m.PeerCount(); got != 1 {
				t.Errorf("PeerCount() = %d, want 1", got)
			}
		})
	}
}
