package discovery

import (
	"context"
	"errors"
	"math"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/ram1234598766-dotcom/Local-WEB/pkg/link"
	"github.com/rs/zerolog/log"
)

func init() {
	log.Logger = log.Output(discard{})
}

// discard silences zerolog for the whole test binary without touching source files.
type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }

// scoreEqual compares scores with a tolerance: computeScore accumulates 0.05/0.1
// increments, so the arithmetic lands a few ULPs off the decimal literal.
func scoreEqual(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}

// fakeMode is a scriptable DiscoveryMode used to drive the orchestrator.
type fakeMode struct {
	name         string
	requiresWiFi bool
	startErr     error
	stopErr      error

	mu       sync.Mutex
	events   chan PeerEvent
	started  int
	stopped  int
	nodeID   [32]byte
	nodeName string
}

func newFakeMode(name string, requiresWiFi bool) *fakeMode {
	return &fakeMode{name: name, requiresWiFi: requiresWiFi, events: make(chan PeerEvent, 8)}
}

func (f *fakeMode) Name() string             { return f.name }
func (f *fakeMode) RequiresWiFi() bool       { return f.requiresWiFi }
func (f *fakeMode) Advertise(PeerInfo) error { return nil }

func (f *fakeMode) Start(ctx context.Context, nodeID [32]byte, name string) (<-chan PeerEvent, error) {
	f.mu.Lock()
	f.started++
	f.nodeID = nodeID
	f.nodeName = name
	f.mu.Unlock()

	if f.startErr != nil {
		return nil, f.startErr
	}
	return f.events, nil
}

func (f *fakeMode) Stop() error {
	f.mu.Lock()
	f.stopped++
	f.mu.Unlock()
	return f.stopErr
}

func (f *fakeMode) counts() (int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.started, f.stopped
}

func (f *fakeMode) identity() ([32]byte, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.nodeID, f.nodeName
}

func (f *fakeMode) emit(t *testing.T, evt PeerEvent) {
	t.Helper()
	select {
	case f.events <- evt:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out handing an event to the fake discovery mode")
	}
}

// fakeLink implements link.Link so hasWiFi can be exercised without hardware.
type fakeLink struct {
	mode      link.LinkMode
	available bool
}

func (f *fakeLink) Name() string        { return f.mode.String() }
func (f *fakeLink) Mode() link.LinkMode { return f.mode }
func (f *fakeLink) RequiresWiFi() bool {
	return f.mode == link.ModeWiFiStation || f.mode == link.ModeWiFiDirect
}
func (f *fakeLink) RequiresRouter() bool             { return f.mode == link.ModeWiFiStation }
func (f *fakeLink) IsAvailable(context.Context) bool { return f.available }
func (f *fakeLink) Bandwidth() int                   { return 100 }
func (f *fakeLink) MaxPeers() int                    { return 8 }
func (f *fakeLink) Discover(context.Context) (<-chan link.PeerEvent, error) {
	return nil, errors.New("not used in tests")
}
func (f *fakeLink) Connect(context.Context, string) (net.Conn, error) {
	return nil, errors.New("not used in tests")
}
func (f *fakeLink) Advertise(link.PeerInfo) error { return errors.New("not used in tests") }
func (f *fakeLink) Stop() error                   { return nil }

func TestNewOrchestrator(t *testing.T) {
	var id [32]byte
	id[0] = 0xAB
	o := NewOrchestrator(OrchestratorConfig{NodeID: id, Name: "self", Modes: nil})

	if o == nil {
		t.Fatal("expected a non-nil orchestrator")
	}
	if o.PeerCount() != 0 {
		t.Errorf("expected a fresh orchestrator to know 0 peers, got %d", o.PeerCount())
	}
	if o.nodeID != id {
		t.Errorf("expected node ID %x, got %x", id[:2], o.nodeID[:2])
	}
	if o.name != "self" {
		t.Errorf("expected name %q, got %q", "self", o.name)
	}
	if o.db == nil {
		t.Error("expected the peer database to be constructed")
	}
	if o.events == nil {
		t.Error("expected the event channel to be constructed")
	}
}

func TestNewOrchestratorWiresOnPeerHandler(t *testing.T) {
	got := make(chan PeerEvent, 4)
	o := NewOrchestrator(OrchestratorConfig{OnPeer: func(e PeerEvent) { got <- e }})

	o.HandleEvent(PeerEvent{Type: PeerFound, Peer: PeerInfo{ID: peerID(1), Name: "alpha"}})

	select {
	case evt := <-got:
		if evt.Peer.Name != "alpha" {
			t.Errorf("expected the config handler to see %q, got %q", "alpha", evt.Peer.Name)
		}
	case <-time.After(time.Second):
		t.Fatal("config OnPeer handler was never invoked")
	}
}

func TestOrchestratorHandleEventAddsPeer(t *testing.T) {
	o := NewOrchestrator(OrchestratorConfig{NodeID: peerID(255)})

	o.HandleEvent(PeerEvent{
		Type: PeerFound,
		Peer: PeerInfo{ID: peerID(1), Name: "alpha", Addrs: []string{"10.0.0.1:4443"}},
		Time: time.Now(),
	})

	if o.PeerCount() != 1 {
		t.Fatalf("expected 1 peer, got %d", o.PeerCount())
	}
	got, ok := o.GetPeer(peerID(1))
	if !ok {
		t.Fatal("expected the peer to be retrievable")
	}
	if got.Name != "alpha" {
		t.Errorf("expected name %q, got %q", "alpha", got.Name)
	}
	// A brand new peer starts from the fixed baseline score, not a computed one.
	if !scoreEqual(got.Score, 0.5) {
		t.Errorf("expected the initial score 0.5, got %v", got.Score)
	}
	if got.FirstSeen.IsZero() {
		t.Error("expected FirstSeen to be stamped")
	}
	if len(o.Peers()) != 1 {
		t.Errorf("expected Peers() to return 1 entry, got %d", len(o.Peers()))
	}
}

func TestOrchestratorSkipsSelf(t *testing.T) {
	self := peerID(9)
	notified := 0
	o := NewOrchestrator(OrchestratorConfig{NodeID: self})
	o.OnPeer(func(PeerEvent) { notified++ })

	o.HandleEvent(PeerEvent{Type: PeerFound, Peer: PeerInfo{ID: self, Name: "me"}})

	if o.PeerCount() != 0 {
		t.Errorf("expected the node's own advert to be ignored, got %d peers", o.PeerCount())
	}
	if notified != 0 {
		t.Errorf("expected no handler notification for a self event, got %d", notified)
	}
}

func TestOrchestratorPeerUpdatedMergesAddresses(t *testing.T) {
	o := NewOrchestrator(OrchestratorConfig{NodeID: peerID(255)})
	id := peerID(4)

	o.HandleEvent(PeerEvent{Type: PeerFound, Peer: PeerInfo{
		ID: id, Name: "alpha", Source: "mdns", Addrs: []string{"10.0.0.1:4443"},
	}})
	o.HandleEvent(PeerEvent{Type: PeerUpdated, Peer: PeerInfo{
		ID: id, Name: "alpha", Source: "ble", Addrs: []string{"10.0.0.2:4443"},
	}})

	if o.PeerCount() != 1 {
		t.Fatalf("expected the update to merge into the existing peer, got %d", o.PeerCount())
	}
	got, _ := o.GetPeer(id)
	if len(got.Addrs) != 2 {
		t.Fatalf("expected addresses from both links to be merged, got %v", got.Addrs)
	}
	if got.Addrs[0] != "10.0.0.1:4443" || got.Addrs[1] != "10.0.0.2:4443" {
		t.Errorf("unexpected merged addresses: %v", got.Addrs)
	}
	if got.Source != "ble" {
		t.Errorf("expected the newest source to win, got %q", got.Source)
	}
}

func TestOrchestratorPeerUpdatedRecomputesScore(t *testing.T) {
	o := NewOrchestrator(OrchestratorConfig{NodeID: peerID(255)})
	id := peerID(5)

	o.HandleEvent(PeerEvent{Type: PeerFound, Peer: PeerInfo{ID: id, Name: "alpha"}})
	baseline, _ := o.GetPeer(id)

	// A fresh, low-latency, strong-signal sighting must beat the 0.5 baseline,
	// which proves the update path really recomputes rather than copying.
	o.HandleEvent(PeerEvent{Type: PeerUpdated, Peer: PeerInfo{
		ID:        id,
		Name:      "alpha",
		LastSeen:  time.Now(),
		Latency:   2 * time.Millisecond,
		RSSI:      -30,
		Source:    "ble",
		PublicKey: [32]byte{7},
	}})

	got, _ := o.GetPeer(id)
	if got.Score <= baseline.Score {
		t.Fatalf("expected the recomputed score %v to exceed the baseline %v", got.Score, baseline.Score)
	}
	if !scoreEqual(got.Score, 0.9) {
		t.Errorf("expected the maximum reachable score 0.9, got %v", got.Score)
	}
	if !got.FirstSeen.Equal(baseline.FirstSeen) {
		t.Error("expected FirstSeen to be preserved across an update")
	}
}

func TestOrchestratorBestPeers(t *testing.T) {
	o := NewOrchestrator(OrchestratorConfig{NodeID: peerID(255)})

	// The first sighting of a peer is always scored at the 0.5 baseline, so a
	// meaningful ranking needs a second sighting from each peer.
	o.HandleEvent(PeerEvent{Type: PeerFound, Peer: PeerInfo{ID: peerID(1), Name: "weak"}})
	o.HandleEvent(PeerEvent{Type: PeerFound, Peer: PeerInfo{ID: peerID(2), Name: "strong"}})
	o.HandleEvent(PeerEvent{Type: PeerUpdated, Peer: PeerInfo{
		ID: peerID(1), Name: "weak", LastSeen: time.Now().Add(-time.Hour), Latency: time.Second, RSSI: -120,
	}})
	o.HandleEvent(PeerEvent{Type: PeerUpdated, Peer: PeerInfo{
		ID: peerID(2), Name: "strong", LastSeen: time.Now(), Latency: time.Millisecond, RSSI: -30,
	}})

	weak, _ := o.GetPeer(peerID(1))
	strong, _ := o.GetPeer(peerID(2))
	if strong.Score <= weak.Score {
		t.Fatalf("expected the strong peer (%v) to outscore the weak one (%v)", strong.Score, weak.Score)
	}

	top := o.BestPeers(1)
	if len(top) != 1 {
		t.Fatalf("expected 1 peer, got %d", len(top))
	}
	if top[0].ID != peerID(2) {
		t.Errorf("expected the highest scoring peer, got %x", top[0].ID[:2])
	}
}

func TestOrchestratorPeerLostDecaysThenRemoves(t *testing.T) {
	o := NewOrchestrator(OrchestratorConfig{NodeID: peerID(255)})
	id := peerID(6)
	o.HandleEvent(PeerEvent{Type: PeerFound, Peer: PeerInfo{ID: id, Name: "alpha"}})
	o.HandleEvent(PeerEvent{Type: PeerFound, Peer: PeerInfo{ID: id, Name: "alpha", LastSeen: time.Now()}})

	// 0.9 -> 0.45 -> 0.225, all still at or above the 0.1 floor.
	o.HandleEvent(PeerEvent{Type: PeerLost, Peer: PeerInfo{ID: id}})
	if o.PeerCount() != 1 {
		t.Fatalf("expected the peer to survive the first PeerLost, got %d peers", o.PeerCount())
	}
	got, _ := o.GetPeer(id)
	if !scoreEqual(got.Score, 0.45) {
		t.Errorf("expected the score to decay to 0.45, got %v", got.Score)
	}

	o.HandleEvent(PeerEvent{Type: PeerLost, Peer: PeerInfo{ID: id}})
	if o.PeerCount() != 1 {
		t.Fatalf("expected the peer to survive the second PeerLost, got %d peers", o.PeerCount())
	}

	// 0.225 * 0.5 = 0.1125, still above the floor.
	o.HandleEvent(PeerEvent{Type: PeerLost, Peer: PeerInfo{ID: id}})
	got, _ = o.GetPeer(id)
	if got.Score <= 0.1 && o.PeerCount() == 1 {
		t.Fatal("expected the peer to be gone once the score fell under the floor")
	}

	// One more halving crosses the floor and the peer is dropped.
	o.HandleEvent(PeerEvent{Type: PeerLost, Peer: PeerInfo{ID: id}})
	if o.PeerCount() != 0 {
		t.Fatalf("expected the peer to be removed after repeated PeerLost, got %d", o.PeerCount())
	}
	if _, ok := o.GetPeer(id); ok {
		t.Error("expected Get to miss after removal")
	}
}

func TestOrchestratorPeerLostUnknownPeer(t *testing.T) {
	o := NewOrchestrator(OrchestratorConfig{NodeID: peerID(255)})

	o.HandleEvent(PeerEvent{Type: PeerLost, Peer: PeerInfo{ID: peerID(77)}})

	if o.PeerCount() != 0 {
		t.Errorf("expected an unknown PeerLost to be a no-op, got %d peers", o.PeerCount())
	}
}

func TestOrchestratorOnPeerNotifiesEveryHandler(t *testing.T) {
	o := NewOrchestrator(OrchestratorConfig{NodeID: peerID(255)})
	first := make(chan PeerEvent, 4)
	second := make(chan PeerEvent, 4)
	o.OnPeer(func(e PeerEvent) { first <- e })
	o.OnPeer(func(e PeerEvent) { second <- e })

	o.HandleEvent(PeerEvent{Type: PeerFound, Peer: PeerInfo{ID: peerID(1), Name: "alpha"}})

	for i, ch := range []chan PeerEvent{first, second} {
		select {
		case <-ch:
		case <-time.After(time.Second):
			t.Fatalf("handler %d was not notified", i)
		}
	}
}

func TestOrchestratorOnPeerRegisteredBeforeRun(t *testing.T) {
	mode := newFakeMode("mdns", true)
	notified := make(chan PeerEvent, 4)

	// Handlers registered before Run are safe: the event loop is not running yet.
	o := NewOrchestrator(OrchestratorConfig{NodeID: peerID(255), Modes: []DiscoveryMode{mode}})
	o.OnPeer(func(e PeerEvent) { notified <- e })
	o.OnPeer(func(e PeerEvent) { notified <- e })

	if err := o.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	t.Cleanup(o.Stop)

	mode.emit(t, PeerEvent{Type: PeerFound, Peer: PeerInfo{ID: peerID(8), Name: "alpha"}})

	for i := 0; i < 2; i++ {
		select {
		case <-notified:
		case <-time.After(3 * time.Second):
			t.Fatalf("handler %d was never notified", i)
		}
	}
}

func TestOrchestratorHandleEventConcurrent(t *testing.T) {
	o := NewOrchestrator(OrchestratorConfig{NodeID: peerID(255)})

	// PeerDatabase guards its own map, so concurrent discoveries of distinct
	// peers must land without corruption or loss.
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			o.HandleEvent(PeerEvent{Type: PeerFound, Peer: PeerInfo{ID: peerID(byte(i)), Name: "x"}})
		}()
		go func() {
			defer wg.Done()
			o.PeerCount()
			o.Peers()
		}()
	}
	wg.Wait()

	if o.PeerCount() != 32 {
		t.Errorf("expected all 32 distinct peers to be recorded, got %d", o.PeerCount())
	}
}

// Registering handlers after Run is the normal pattern, and the event loop fans
// handlers out concurrently with it, so the slice must be read under the lock.
func TestOrchestratorOnPeerConcurrentWithEventLoop(t *testing.T) {
	mode := newFakeMode("mdns", true)
	o := NewOrchestrator(OrchestratorConfig{NodeID: peerID(255), Modes: []DiscoveryMode{mode}})
	if err := o.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	t.Cleanup(o.Stop)

	const iterations = 500
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			o.OnPeer(func(PeerEvent) {})
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			o.HandleEvent(PeerEvent{Type: PeerFound, Peer: PeerInfo{ID: peerID(byte(i)), Name: "alpha"}})
		}
	}()
	wg.Wait()

	// Every registered handler must still be reachable afterwards.
	seen := make(chan PeerEvent, 1)
	o.OnPeer(func(evt PeerEvent) { seen <- evt })
	o.HandleEvent(PeerEvent{Type: PeerFound, Peer: PeerInfo{ID: peerID(251), Name: "last"}})

	select {
	case evt := <-seen:
		if evt.Peer.Name != "last" {
			t.Errorf("expected the late handler to see the final event, got %q", evt.Peer.Name)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("a handler registered after the concurrent burst was never notified")
	}
}

func TestComputeScore(t *testing.T) {
	old := &PeerInfo{Score: 0.42}

	tests := []struct {
		name    string
		new     PeerInfo
		want    float64
		comment string
	}{
		{
			name: "fresh fast strong signal is the maximum",
			new:  PeerInfo{LastSeen: time.Now(), Latency: time.Millisecond, RSSI: -30},
			want: 0.9,
		},
		{
			name: "mid latency and weak signal halve the boosts",
			new:  PeerInfo{LastSeen: time.Now(), Latency: 30 * time.Millisecond, RSSI: -70},
			want: 0.8,
		},
		{
			name: "slow link and out of range signal get no bonus",
			new:  PeerInfo{LastSeen: time.Now(), Latency: 100 * time.Millisecond, RSSI: -90},
			want: 0.7,
		},
		{
			name: "stale sighting loses the freshness boost",
			new:  PeerInfo{LastSeen: time.Now().Add(-time.Minute), Latency: time.Millisecond, RSSI: -30},
			want: 0.7,
		},
		{
			name: "latency boundary at 10ms drops to the medium tier",
			new:  PeerInfo{LastSeen: time.Now(), Latency: 10 * time.Millisecond, RSSI: -30},
			want: 0.85,
		},
		{
			name: "latency boundary at 50ms drops to no bonus",
			new:  PeerInfo{LastSeen: time.Now(), Latency: 50 * time.Millisecond, RSSI: -30},
			want: 0.8,
		},
		{
			name: "rssi boundary at -60 drops to the medium tier",
			new:  PeerInfo{LastSeen: time.Now(), Latency: time.Millisecond, RSSI: -60},
			want: 0.85,
		},
		{
			name: "rssi boundary at -80 drops to no bonus",
			new:  PeerInfo{LastSeen: time.Now(), Latency: time.Millisecond, RSSI: -80},
			want: 0.8,
		},
		{
			name: "zero value peer still scores above the base",
			new:  PeerInfo{},
			want: 0.7,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			peer := tt.new
			if got := computeScore(old, &peer); !scoreEqual(got, tt.want) {
				t.Errorf("expected score %v, got %v", tt.want, got)
			}
		})
	}
}

func TestComputeScoreIsNotConstant(t *testing.T) {
	old := &PeerInfo{}
	fresh := PeerInfo{LastSeen: time.Now(), Latency: time.Millisecond, RSSI: -30}
	stale := PeerInfo{LastSeen: time.Now().Add(-time.Hour), Latency: time.Second, RSSI: -120}

	good := computeScore(old, &fresh)
	bad := computeScore(old, &stale)

	if good == bad {
		t.Fatalf("expected the score to vary with freshness, latency and signal, both were %v", good)
	}
	if good <= bad {
		t.Errorf("expected the good sighting (%v) to outscore the bad one (%v)", good, bad)
	}
}

func TestComputeScoreStaysWithinUnitRange(t *testing.T) {
	inputs := []PeerInfo{
		{LastSeen: time.Now(), Latency: 0, RSSI: 0},
		{LastSeen: time.Now(), Latency: -time.Hour, RSSI: math.MaxInt32},
		{LastSeen: time.Now(), Latency: 0, RSSI: math.MinInt32},
	}
	for i := range inputs {
		if got := computeScore(nil, &inputs[i]); got < 0 || got > 1.0 {
			t.Errorf("score %v outside [0,1]", got)
		}
	}
}

func TestOrchestratorRunStartsNonWiFiMode(t *testing.T) {
	ble := newFakeMode("ble", false)
	o := NewOrchestrator(OrchestratorConfig{
		NodeID: peerID(255),
		Name:   "self",
		Modes:  []DiscoveryMode{ble},
	})

	if err := o.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	t.Cleanup(o.Stop)

	started, _ := ble.counts()
	if started != 1 {
		t.Fatalf("expected the mode to be started once, got %d", started)
	}
	nodeID, name := ble.identity()
	if nodeID != peerID(255) || name != "self" {
		t.Errorf("expected the orchestrator identity to be handed to the mode, got %x/%q", nodeID[:2], name)
	}
}

func TestOrchestratorRunStampsSourceOnEvents(t *testing.T) {
	mdns := newFakeMode("mdns", true)
	notified := make(chan PeerEvent, 4)
	o := NewOrchestrator(OrchestratorConfig{NodeID: peerID(255), Modes: []DiscoveryMode{mdns}})
	o.OnPeer(func(e PeerEvent) { notified <- e })

	if err := o.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	t.Cleanup(o.Stop)

	mdns.emit(t, PeerEvent{Type: PeerFound, Peer: PeerInfo{ID: peerID(8), Name: "alpha", Addrs: []string{"10.0.0.9:4443"}}})

	select {
	case evt := <-notified:
		if evt.Peer.Source != "mdns" {
			t.Errorf("expected the event source to be stamped with the mode name, got %q", evt.Peer.Source)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the orchestrator never surfaced the mode event")
	}

	got, ok := o.GetPeer(peerID(8))
	if !ok {
		t.Fatal("expected the discovered peer to reach the database")
	}
	if got.Name != "alpha" {
		t.Errorf("expected name %q, got %q", "alpha", got.Name)
	}
}

func TestOrchestratorRunContinuesAfterModeStartError(t *testing.T) {
	broken := newFakeMode("broken", false)
	broken.startErr = errors.New("no interface")
	good := newFakeMode("good", false)

	o := NewOrchestrator(OrchestratorConfig{NodeID: peerID(255), Modes: []DiscoveryMode{broken, good}})
	if err := o.Run(); err != nil {
		t.Fatalf("Run should tolerate a single failing mode, got %v", err)
	}
	t.Cleanup(o.Stop)

	if n, _ := good.counts(); n != 1 {
		t.Errorf("expected the healthy mode to still start, got %d starts", n)
	}
}

func TestOrchestratorRunSkipsClosedEventChannel(t *testing.T) {
	mode := newFakeMode("mdns", true)
	o := NewOrchestrator(OrchestratorConfig{NodeID: peerID(255), Modes: []DiscoveryMode{mode}})
	if err := o.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	t.Cleanup(o.Stop)

	close(mode.events)
	// The consumer goroutine must exit; the orchestrator stays responsive.
	o.HandleEvent(PeerEvent{Type: PeerFound, Peer: PeerInfo{ID: peerID(2), Name: "still-alive"}})
	if o.PeerCount() != 1 {
		t.Errorf("expected the orchestrator to keep handling events, got %d peers", o.PeerCount())
	}
}

func TestOrchestratorStopStopsEveryMode(t *testing.T) {
	m1 := newFakeMode("mdns", true)
	m2 := newFakeMode("ble", false)
	o := NewOrchestrator(OrchestratorConfig{NodeID: peerID(255), Modes: []DiscoveryMode{m1, m2}})

	if err := o.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	o.Stop()

	if _, stopped := m1.counts(); stopped != 1 {
		t.Errorf("expected mdns to be stopped once, got %d", stopped)
	}
	if _, stopped := m2.counts(); stopped != 1 {
		t.Errorf("expected ble to be stopped once, got %d", stopped)
	}
	if _, open := <-o.events; open {
		t.Error("expected the event channel to be closed by Stop")
	}
}

func TestOrchestratorStopAfterStartErrorStillStopsMode(t *testing.T) {
	// A mode whose Start failed is still torn down by Stop; the orchestrator must
	// not blow up doing so.
	mode := newFakeMode("broken", false)
	mode.startErr = errors.New("no interface")
	o := NewOrchestrator(OrchestratorConfig{NodeID: peerID(255), Modes: []DiscoveryMode{mode}})
	if err := o.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	o.Stop()

	if _, stopped := mode.counts(); stopped != 1 {
		t.Errorf("expected the failed mode to be stopped once, got %d", stopped)
	}
}

func TestOrchestratorStopIsQuietOnModeError(t *testing.T) {
	mode := newFakeMode("mdns", true)
	mode.stopErr = errors.New("already dead")
	o := NewOrchestrator(OrchestratorConfig{NodeID: peerID(255), Modes: []DiscoveryMode{mode}})
	if err := o.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	o.Stop()

	if _, stopped := mode.counts(); stopped != 1 {
		t.Errorf("expected Stop to be attempted once despite the error, got %d", stopped)
	}
}

// Stopping twice would otherwise close an already closed event channel.
func TestOrchestratorStopIsIdempotent(t *testing.T) {
	mode := newFakeMode("mdns", true)
	o := NewOrchestrator(OrchestratorConfig{NodeID: peerID(255), Modes: []DiscoveryMode{mode}})
	if err := o.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}

	o.Stop()
	o.Stop()
	o.Stop()

	if _, stopped := mode.counts(); stopped != 1 {
		t.Errorf("expected the mode to be torn down exactly once, got %d", stopped)
	}
	if _, open := <-o.events; open {
		t.Error("expected the event channel to be closed by Stop")
	}
}

func TestOrchestratorHasWiFi(t *testing.T) {
	tests := []struct {
		name        string
		linkManager *link.Manager
		want        bool
	}{
		{
			name:        "no link manager assumes wifi",
			linkManager: nil,
			want:        true,
		},
		{
			name:        "no links means no wifi",
			linkManager: link.NewManager(link.ManagerConfig{Preferences: []link.LinkMode{}}),
			want:        false,
		},
		{
			name:        "wifi station link means wifi",
			linkManager: link.NewManager(link.ManagerConfig{Links: []link.Link{&fakeLink{mode: link.ModeWiFiStation, available: true}}}),
			want:        true,
		},
		{
			name:        "wifi direct link means wifi",
			linkManager: link.NewManager(link.ManagerConfig{Links: []link.Link{&fakeLink{mode: link.ModeWiFiDirect, available: true}}}),
			want:        true,
		},
		{
			name:        "ble link does not count as wifi",
			linkManager: link.NewManager(link.ManagerConfig{Links: []link.Link{&fakeLink{mode: link.ModeBLE, available: true}}}),
			want:        false,
		},
		{
			name:        "unavailable wifi link does not count",
			linkManager: link.NewManager(link.ManagerConfig{Links: []link.Link{&fakeLink{mode: link.ModeWiFiStation, available: false}}}),
			want:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := NewOrchestrator(OrchestratorConfig{LinkManager: tt.linkManager})
			if got := o.hasWiFi(); got != tt.want {
				t.Errorf("expected hasWiFi() = %v, got %v", tt.want, got)
			}
		})
	}
}

func TestOrchestratorRunSkipsWifiModesWithoutWifi(t *testing.T) {
	mdns := newFakeMode("mdns", true)
	ble := newFakeMode("ble", false)
	o := NewOrchestrator(OrchestratorConfig{
		NodeID:      peerID(255),
		Modes:       []DiscoveryMode{mdns, ble},
		LinkManager: link.NewManager(link.ManagerConfig{Preferences: []link.LinkMode{}}),
	})

	if err := o.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	t.Cleanup(o.Stop)

	if n, _ := mdns.counts(); n != 0 {
		t.Errorf("expected the wifi-only mode to be skipped, got %d starts", n)
	}
	if n, _ := ble.counts(); n != 1 {
		t.Errorf("expected the non-wifi mode to run, got %d starts", n)
	}
}

func TestOrchestratorRunSkipsWifiModesWithWifi(t *testing.T) {
	mdns := newFakeMode("mdns", true)
	o := NewOrchestrator(OrchestratorConfig{
		NodeID:      peerID(255),
		Modes:       []DiscoveryMode{mdns},
		LinkManager: link.NewManager(link.ManagerConfig{Links: []link.Link{&fakeLink{mode: link.ModeWiFiStation, available: true}}}),
	})

	if err := o.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	t.Cleanup(o.Stop)

	if n, _ := mdns.counts(); n != 1 {
		t.Errorf("expected the wifi mode to run when wifi is present, got %d starts", n)
	}
}

func TestOrchestratorRunWithNoModes(t *testing.T) {
	o := NewOrchestrator(OrchestratorConfig{NodeID: peerID(255)})
	if err := o.Run(); err != nil {
		t.Fatalf("Run with no modes should succeed, got %v", err)
	}
	o.Stop()

	if o.PeerCount() != 0 {
		t.Errorf("expected no peers, got %d", o.PeerCount())
	}
}
