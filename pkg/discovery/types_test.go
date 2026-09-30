package discovery

import (
	"math"
	"testing"
	"time"
)

// peerID builds a deterministic 32-byte identity so failures are reproducible.
func peerID(n byte) [32]byte {
	var id [32]byte
	id[0] = n
	return id
}

func TestPeerDatabaseAddGetList(t *testing.T) {
	db := NewPeerDatabase()

	if db.Count() != 0 {
		t.Fatalf("expected empty database, got %d", db.Count())
	}
	if len(db.All()) != 0 {
		t.Fatalf("expected no peers, got %d", len(db.All()))
	}

	db.Add(PeerInfo{ID: peerID(1), Name: "alpha", Addrs: []string{"10.0.0.1:4443"}})
	db.Add(PeerInfo{ID: peerID(2), Name: "beta", Addrs: []string{"10.0.0.2:4443"}})

	if db.Count() != 2 {
		t.Fatalf("expected 2 peers, got %d", db.Count())
	}
	if len(db.All()) != 2 {
		t.Fatalf("expected All() to return 2 peers, got %d", len(db.All()))
	}

	got, ok := db.Get(peerID(1))
	if !ok {
		t.Fatal("expected peer 1 to be present")
	}
	if got.Name != "alpha" {
		t.Errorf("expected name %q, got %q", "alpha", got.Name)
	}
	if got.LastSeen.IsZero() {
		t.Error("expected LastSeen to be populated by Add")
	}
	if got.FirstSeen.IsZero() {
		t.Error("expected FirstSeen to be populated by Add")
	}
}

func TestPeerDatabaseGetMissing(t *testing.T) {
	db := NewPeerDatabase()
	db.Add(PeerInfo{ID: peerID(1), Name: "alpha"})

	got, ok := db.Get(peerID(99))
	if ok {
		t.Fatal("expected lookup of unknown ID to report absent")
	}
	if got != nil {
		t.Errorf("expected nil peer for unknown ID, got %+v", got)
	}
}

func TestPeerDatabaseRemove(t *testing.T) {
	db := NewPeerDatabase()
	db.Add(PeerInfo{ID: peerID(1), Name: "alpha"})
	db.Add(PeerInfo{ID: peerID(2), Name: "beta"})

	db.Remove(peerID(1))
	if _, ok := db.Get(peerID(1)); ok {
		t.Fatal("expected peer 1 to be removed")
	}
	if db.Count() != 1 {
		t.Fatalf("expected 1 remaining peer, got %d", db.Count())
	}

	// Removing an unknown peer must be a no-op, not a panic.
	db.Remove(peerID(42))
	if db.Count() != 1 {
		t.Fatalf("expected 1 remaining peer after removing unknown ID, got %d", db.Count())
	}
}

// The same node seen by two discovery transports must collapse into one entry
// and accumulate addresses instead of duplicating.
func TestPeerDatabaseDedupAcrossTransports(t *testing.T) {
	db := NewPeerDatabase()
	id := peerID(7)

	db.Add(PeerInfo{
		ID:        id,
		Name:      "alpha",
		Source:    "mdns",
		Addrs:     []string{"192.168.1.10:4443"},
		Services:  []ServiceInfo{{Name: "http", Port: 8080}},
		Score:     0.9,
		Latency:   2 * time.Millisecond,
		FirstSeen: time.Now().Add(-time.Hour),
	})
	firstSeen := mustGet(t, db, id).FirstSeen

	db.Add(PeerInfo{
		ID:      id,
		Name:    "alpha",
		Source:  "ble",
		Addrs:   []string{"AA:BB:CC:DD:EE:FF", "192.168.1.10:4443"},
		Score:   0.3,
		Latency: 40 * time.Millisecond,
	})

	if db.Count() != 1 {
		t.Fatalf("expected the two transports to merge into 1 peer, got %d", db.Count())
	}

	merged := mustGet(t, db, id)
	if len(merged.Addrs) != 2 {
		t.Fatalf("expected 2 merged addresses, got %v", merged.Addrs)
	}
	if merged.Addrs[0] != "192.168.1.10:4443" || merged.Addrs[1] != "AA:BB:CC:DD:EE:FF" {
		t.Errorf("unexpected merged address order: %v", merged.Addrs)
	}
	if merged.Score != 0.9 {
		t.Errorf("expected the higher score 0.9 to survive the merge, got %v", merged.Score)
	}
	if !merged.FirstSeen.Equal(firstSeen) {
		t.Errorf("expected FirstSeen to be preserved across merge, got %v want %v", merged.FirstSeen, firstSeen)
	}
	if merged.Source != "ble" {
		t.Errorf("expected the latest source to win, got %q", merged.Source)
	}
}

func TestPeerDatabaseAddKeepsHighestScore(t *testing.T) {
	tests := []struct {
		name     string
		existing float64
		incoming float64
		want     float64
	}{
		{"incoming higher wins", 0.2, 0.8, 0.8},
		{"existing higher kept", 0.8, 0.2, 0.8},
		{"equal score kept", 0.5, 0.5, 0.5},
		{"zero incoming does not clobber", 0.7, 0.0, 0.7},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := NewPeerDatabase()
			id := peerID(3)
			db.Add(PeerInfo{ID: id, Score: tt.existing})
			db.Add(PeerInfo{ID: id, Score: tt.incoming})

			if got := mustGet(t, db, id).Score; got != tt.want {
				t.Errorf("expected score %v, got %v", tt.want, got)
			}
		})
	}
}

func TestPeerDatabaseGCEvictsStalePeers(t *testing.T) {
	db := NewPeerDatabase()
	db.Add(PeerInfo{ID: peerID(1), Name: "stale"})
	db.Add(PeerInfo{ID: peerID(2), Name: "fresh"})

	// Add stamps LastSeen with the current time; rewind it to simulate silence.
	stale := mustGet(t, db, peerID(1))
	stale.LastSeen = time.Now().Add(-2 * time.Hour)

	removed := db.GC(time.Hour)
	if removed != 1 {
		t.Errorf("expected GC to remove exactly 1 peer, got %d", removed)
	}
	if _, ok := db.Get(peerID(1)); ok {
		t.Error("expected the stale peer to be evicted")
	}
	if _, ok := db.Get(peerID(2)); !ok {
		t.Error("expected the fresh peer to survive GC")
	}
	if db.Count() != 1 {
		t.Errorf("expected 1 peer after GC, got %d", db.Count())
	}
}

func TestPeerDatabaseGCBoundary(t *testing.T) {
	tests := []struct {
		name        string
		age         time.Duration
		timeout     time.Duration
		wantRemoved int
	}{
		{"much older than ttl is evicted", 48 * time.Hour, 5 * time.Minute, 1},
		{"just inside ttl survives", 1 * time.Minute, 5 * time.Minute, 0},
		{"huge ttl evicts nothing", 1 * time.Hour, 100 * 365 * 24 * time.Hour, 0},
		// A peer seen microseconds ago survives any non-zero TTL. The
		// earlier "zero ttl" case asserted on a sub-nanosecond boundary
		// (GC uses a strict `>`), so whether the peer survived depended on
		// whether the clock had ticked between the two calls — it failed
		// only when other packages loaded the machine.
		{"a just-seen peer survives a normal ttl", 0, time.Hour, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := NewPeerDatabase()
			db.Add(PeerInfo{ID: peerID(1)})
			mustGet(t, db, peerID(1)).LastSeen = time.Now().Add(-tt.age)

			if got := db.GC(tt.timeout); got != tt.wantRemoved {
				t.Errorf("expected GC to remove %d, got %d", tt.wantRemoved, got)
			}
		})
	}
}

func TestPeerDatabaseGCRemovesNothingWhenEmpty(t *testing.T) {
	db := NewPeerDatabase()
	if got := db.GC(time.Nanosecond); got != 0 {
		t.Errorf("expected GC on an empty database to remove 0, got %d", got)
	}
}

func TestPeerDatabaseBestPeersOrdering(t *testing.T) {
	db := NewPeerDatabase()
	scores := map[byte]float64{1: 0.1, 2: 0.9, 3: 0.5, 4: 0.7, 5: 0.3}
	for n, s := range scores {
		db.Add(PeerInfo{ID: peerID(n), Name: string(rune('a' + n)), Score: s})
	}

	top := db.BestPeers(3)
	if len(top) != 3 {
		t.Fatalf("expected 3 peers, got %d", len(top))
	}
	wantOrder := []float64{0.9, 0.7, 0.5}
	for i, want := range wantOrder {
		if top[i].Score != want {
			t.Errorf("position %d: expected score %v, got %v", i, want, top[i].Score)
		}
	}
}

func TestPeerDatabaseBestPeersNBounds(t *testing.T) {
	tests := []struct {
		name      string
		n         int
		wantCount int
	}{
		{"zero returns nothing", 0, 0},
		{"one returns the best", 1, 1},
		{"more than available clamps", 50, 4},
		{"exact count", 4, 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := NewPeerDatabase()
			for n := byte(1); n <= 4; n++ {
				db.Add(PeerInfo{ID: peerID(n), Score: 0.1 * float64(n)})
			}
			got := db.BestPeers(tt.n)
			if len(got) != tt.wantCount {
				t.Fatalf("expected %d peers, got %d", tt.wantCount, len(got))
			}
		})
	}
}

func TestPeerDatabaseBestPeersEmpty(t *testing.T) {
	db := NewPeerDatabase()
	if got := db.BestPeers(5); len(got) != 0 {
		t.Errorf("expected no peers from an empty database, got %d", len(got))
	}
}

// A negative N used to slice with all[:n] and panic outright.
func TestPeerDatabaseBestPeersNegative(t *testing.T) {
	db := NewPeerDatabase()
	db.Add(PeerInfo{ID: peerID(1), Score: 0.9})
	db.Add(PeerInfo{ID: peerID(2), Score: 0.8})

	for _, n := range []int{-1, -100, math.MinInt32} {
		if got := db.BestPeers(n); len(got) != 0 {
			t.Errorf("BestPeers(%d): expected no peers, got %d", n, len(got))
		}
	}
}

func TestMergeAddrs(t *testing.T) {
	tests := []struct {
		name string
		a    []string
		b    []string
		want []string
	}{
		{"disjoint are concatenated", []string{"a:1"}, []string{"b:2"}, []string{"a:1", "b:2"}},
		{"duplicate is dropped", []string{"a:1"}, []string{"a:1"}, []string{"a:1"}},
		{"new addresses are appended", []string{"a:1", "b:2"}, []string{"c:3"}, []string{"a:1", "b:2", "c:3"}},
		{"duplicates within b are collapsed", []string{"a:1"}, []string{"b:2", "b:2"}, []string{"a:1", "b:2"}},
		{"nil existing keeps incoming", nil, []string{"a:1"}, []string{"a:1"}},
		{"nil incoming keeps existing", []string{"a:1"}, nil, []string{"a:1"}},
		{"both nil stays nil", nil, nil, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mergeAddrs(tt.a, tt.b)
			if len(got) != len(tt.want) {
				t.Fatalf("expected %v, got %v", tt.want, got)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("index %d: expected %q, got %q", i, tt.want[i], got[i])
				}
			}
		})
	}
}

func mustGet(t *testing.T, db *PeerDatabase, id [32]byte) *PeerInfo {
	t.Helper()
	p, ok := db.Get(id)
	if !ok {
		t.Fatalf("expected peer %x to be present", id[:4])
	}
	return p
}
