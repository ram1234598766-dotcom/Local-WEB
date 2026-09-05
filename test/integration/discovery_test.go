//go:build integration

package integration

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/mrityunjay/LocalWEB/pkg/discovery"
	"github.com/mrityunjay/LocalWEB/pkg/link"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// PeerDatabase unit-level integration tests
// ---------------------------------------------------------------------------

func TestPeerDatabaseAddGetRemove(t *testing.T) {
	db := discovery.NewPeerDatabase()
	id := [32]byte{1}
	peer := discovery.PeerInfo{ID: id, Name: "alice", Score: 0.5}

	db.Add(peer)
	p, ok := db.Get(id)
	require.True(t, ok, "peer should exist after Add")
	require.Equal(t, "alice", p.Name)

	db.Remove(id)
	_, ok = db.Get(id)
	require.False(t, ok, "peer should not exist after Remove")
}

func TestPeerDatabaseAddMergesAddresses(t *testing.T) {
	db := discovery.NewPeerDatabase()
	id := [32]byte{2}
	db.Add(discovery.PeerInfo{ID: id, Name: "bob", Addrs: []string{"10.0.0.1:4443"}})
	db.Add(discovery.PeerInfo{ID: id, Name: "bob", Addrs: []string{"10.0.0.2:4443"}})

	p, _ := db.Get(id)
	require.Len(t, p.Addrs, 2, "addresses should be merged")
}

func TestPeerDatabaseBestPeers(t *testing.T) {
	db := discovery.NewPeerDatabase()
	db.Add(discovery.PeerInfo{ID: [32]byte{1}, Name: "low", Score: 0.1})
	db.Add(discovery.PeerInfo{ID: [32]byte{2}, Name: "high", Score: 0.9})
	db.Add(discovery.PeerInfo{ID: [32]byte{3}, Name: "mid", Score: 0.5})

	best := db.BestPeers(2)
	require.Len(t, best, 2)
	require.Equal(t, "high", best[0].Name)
	require.Equal(t, "mid", best[1].Name)
}

func TestPeerDatabaseGCRemovesStale(t *testing.T) {
	db := discovery.NewPeerDatabase()
	db.Add(discovery.PeerInfo{ID: [32]byte{1}, Name: "old"})
	db.Add(discovery.PeerInfo{ID: [32]byte{2}, Name: "new"})

	// Manually backdate the old peer's LastSeen
	p, ok := db.Get([32]byte{1})
	require.True(t, ok)
	p.LastSeen = time.Now().Add(-10 * time.Minute)

	removed := db.GC(5 * time.Minute)
	require.Equal(t, 1, removed, "should remove 1 stale peer")
	require.Equal(t, 1, db.Count(), "should have 1 peer remaining")
}

func TestPeerDatabaseCount(t *testing.T) {
	db := discovery.NewPeerDatabase()
	require.Zero(t, db.Count())
	db.Add(discovery.PeerInfo{ID: [32]byte{1}, Name: "a"})
	db.Add(discovery.PeerInfo{ID: [32]byte{2}, Name: "b"})
	db.Add(discovery.PeerInfo{ID: [32]byte{3}, Name: "c"})
	require.Equal(t, 3, db.Count())
}

func TestPeerDatabaseAll(t *testing.T) {
	db := discovery.NewPeerDatabase()
	db.Add(discovery.PeerInfo{ID: [32]byte{1}, Name: "a"})
	db.Add(discovery.PeerInfo{ID: [32]byte{2}, Name: "b"})
	all := db.All()
	require.Len(t, all, 2)
}

// ---------------------------------------------------------------------------
// Orchestrator event handling
// ---------------------------------------------------------------------------

func TestOrchestratorPeerFoundUpdatesDB(t *testing.T) {
	orch := discovery.NewOrchestrator(discovery.OrchestratorConfig{
		NodeID:      [32]byte{99},
		PublicKey:   [32]byte{99},
		Name:        "test-node",
		LinkManager: nil,
	})

	received := make(chan discovery.PeerEvent, 1)
	orch.OnPeer(func(evt discovery.PeerEvent) {
		received <- evt
	})

	peerID := [32]byte{1}
	evt := discovery.PeerEvent{
		Type: discovery.PeerFound,
		Peer: discovery.PeerInfo{ID: peerID, Name: "discovered-peer", Addrs: []string{"10.0.0.5:4443"}},
	}
	orch.HandleEvent(evt)

	peers := orch.Peers()
	require.Len(t, peers, 1)
	require.Equal(t, "discovered-peer", peers[0].Name)
	require.Equal(t, 1, orch.PeerCount())
}

func TestOrchestratorSkipsSelf(t *testing.T) {
	orch := discovery.NewOrchestrator(discovery.OrchestratorConfig{
		NodeID: [32]byte{42},
		Name:   "self",
	})
	orch.OnPeer(func(discovery.PeerEvent) {})

	orch.HandleEvent(discovery.PeerEvent{
		Type: discovery.PeerFound,
		Peer: discovery.PeerInfo{ID: [32]byte{42}, Name: "self"},
	})
	require.Zero(t, orch.PeerCount(), "self should not be added")
}

func TestOrchestratorPeerLostDecaysScore(t *testing.T) {
	orch := discovery.NewOrchestrator(discovery.OrchestratorConfig{
		NodeID: [32]byte{1},
		Name:   "host",
	})
	peerID := [32]byte{2}
	orch.HandleEvent(discovery.PeerEvent{
		Type: discovery.PeerFound,
		Peer: discovery.PeerInfo{ID: peerID, Name: "peer", Score: 0.5},
	})
	require.Equal(t, 1, orch.PeerCount())

	orch.HandleEvent(discovery.PeerEvent{
		Type: discovery.PeerLost,
		Peer: discovery.PeerInfo{ID: peerID, Name: "peer", Score: 0.5},
	})
	// Score decayed from 0.5 to 0.25 — still above 0.1 threshold, peer stays
	require.Equal(t, 1, orch.PeerCount())
}

func TestPeerDatabaseBestPeersWithScores(t *testing.T) {
	db := discovery.NewPeerDatabase()
	db.Add(discovery.PeerInfo{ID: [32]byte{1}, Name: "low", Score: 0.1})
	db.Add(discovery.PeerInfo{ID: [32]byte{2}, Name: "high", Score: 0.9})
	db.Add(discovery.PeerInfo{ID: [32]byte{3}, Name: "mid", Score: 0.5})

	best := db.BestPeers(2)
	require.Len(t, best, 2)
	require.Equal(t, "high", best[0].Name)
	require.Equal(t, "mid", best[1].Name)
}

// ---------------------------------------------------------------------------
// Two-node discovery across all link types
// ---------------------------------------------------------------------------

func TestTwoNodeDiscoveryAllLinkTypes(t *testing.T) {
	for _, mode := range allLinkModes {
		mode := mode
		t.Run(mode.String(), func(t *testing.T) {
			requiresWiFi := mode == link.ModeWiFiStation || mode == link.ModeWiFiDirect || mode == link.ModeAdHocWiFi
			fakeMode := newFakeDiscoveryMode(mode.String(), requiresWiFi)

			orch := discovery.NewOrchestrator(discovery.OrchestratorConfig{
				NodeID:      [32]byte{byte(mode)},
				PublicKey:   [32]byte{byte(mode)},
				Name:        "host-" + mode.String(),
				Modes:       []discovery.DiscoveryMode{fakeMode},
				LinkManager: nil,
			})

			var foundPeer discovery.PeerInfo
			var found bool
			done := make(chan struct{})
			orch.OnPeer(func(evt discovery.PeerEvent) {
				if evt.Type == discovery.PeerFound {
					foundPeer = evt.Peer
					found = true
					close(done)
				}
			})

			err := orch.Run()
			require.NoError(t, err)

			// Simulate the remote peer advertising
			remotePeer := discovery.PeerInfo{
				ID:       [32]byte{byte(mode), 1},
				Name:     "remote-" + mode.String(),
				Addrs:    []string{"10.0.0.2:4443"},
				Services: []discovery.ServiceInfo{{Name: "dns", Port: 5353}},
			}
			fakeMode.events <- discovery.PeerEvent{
				Type: discovery.PeerFound,
				Peer: remotePeer,
				Time: time.Now(),
			}

			select {
			case <-done:
				require.True(t, found, "peer should be discovered via %s", mode.String())
				require.Equal(t, "remote-"+mode.String(), foundPeer.Name)
			case <-time.After(2 * time.Second):
				t.Fatalf("timeout waiting for peer discovery via %s", mode.String())
			}

			orch.Stop()
		})
	}
}

// ---------------------------------------------------------------------------
// Concurrent discovery safety
// ---------------------------------------------------------------------------

func TestConcurrentPeerDatabaseAccess(t *testing.T) {
	db := discovery.NewPeerDatabase()
	var wg sync.WaitGroup
	iterations := 100

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				id := [32]byte{byte(idx)}
				db.Add(discovery.PeerInfo{ID: id, Name: fmt.Sprintf("peer-%d", idx), Score: float64(j) / float64(iterations)})
				db.Get(id)
				db.All()
				db.BestPeers(5)
			}
		}(i)
	}
	wg.Wait()
	require.Equal(t, 10, db.Count(), "all 10 unique peers should be present")
}

// ---------------------------------------------------------------------------
// Peer score computation
// ---------------------------------------------------------------------------

func TestPeerScoreOrdering(t *testing.T) {
	db := discovery.NewPeerDatabase()
	db.Add(discovery.PeerInfo{ID: [32]byte{1}, Name: "low"})
	db.Add(discovery.PeerInfo{ID: [32]byte{2}, Name: "high"})

	// Manually set different scores (simulating computed scores)
	low, _ := db.Get([32]byte{1})
	low.Score = 0.2
	high, _ := db.Get([32]byte{2})
	high.Score = 0.9

	best := db.BestPeers(1)
	require.Equal(t, "high", best[0].Name, "peer with higher score should rank first")
}
