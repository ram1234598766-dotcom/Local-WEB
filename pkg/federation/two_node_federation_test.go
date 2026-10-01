package federation

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ram1234598766-dotcom/Local-WEB/pkg/discovery"
)

// Two nodes, one rendezvous server, real HTTP: this is the federation scenario
// Phase 4.1 describes, and it is the only way to show the discovery mode
// actually connects two nodes that cannot see each other on any local network.
func TestTwoNodesDiscoverEachOtherThroughRendezvous(t *testing.T) {
	store := NewMemoryStore()
	srv := httptest.NewServer(NewHTTPHandler(store))
	defer srv.Close()

	newMode := func() *RendezvousDiscoveryMode {
		m := NewRendezvousDiscoveryMode(RendezvousModeConfig{
			ServerURL:    srv.URL,
			RegisterSelf: true,
			// Long interval: the test drives one poll explicitly rather than
			// waiting on a minute-long ticker.
			PollInterval: time.Hour,
		})
		m.events = make(chan discovery.PeerEvent, 16)
		m.ctx, m.cancel = context.WithCancel(context.Background())
		m.client = NewRendezvousClient(srv.URL)
		return m
	}

	nodeA := newMode()
	defer nodeA.cancel()
	nodeB := newMode()
	defer nodeB.cancel()

	idA, idB := nodeID(0xA1), nodeID(0xB2)
	selfA := discovery.PeerInfo{ID: idA, Name: "node-a", Addrs: []string{"10.0.0.1:4443"}}
	selfB := discovery.PeerInfo{ID: idB, Name: "node-b", Addrs: []string{"203.0.113.9:4443"}}

	// Both nodes register with the rendezvous server, as they do on each tick.
	for _, self := range []discovery.PeerInfo{selfA, selfB} {
		if err := nodeA.client.Register(context.Background(), self); err != nil {
			t.Fatalf("register %s: %v", self.Name, err)
		}
	}

	// Node A polls and must see node B, and only node B.
	seenA := map[[32]byte]peerState{}
	if err := nodeA.pollOnce(selfA, seenA); err != nil {
		t.Fatalf("node A pollOnce: %v", err)
	}

	select {
	case evt := <-nodeA.events:
		if evt.Peer.ID != idB {
			t.Errorf("node A discovered %x, want node B %x", evt.Peer.ID[:2], idB[:2])
		}
		if evt.Peer.Name != "node-b" {
			t.Errorf("peer name = %q, want node-b", evt.Peer.Name)
		}
		if len(evt.Peer.Addrs) == 0 || evt.Peer.Addrs[0] != "203.0.113.9:4443" {
			t.Errorf("node A cannot dial what it discovered: addrs=%v", evt.Peer.Addrs)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("node A discovered nobody; federation is not working end to end")
	}

	select {
	case evt := <-nodeA.events:
		t.Errorf("node A received an unexpected extra event: %+v", evt)
	default:
	}

	// Symmetry: node B must see node A.
	seenB := map[[32]byte]peerState{}
	if err := nodeB.pollOnce(selfB, seenB); err != nil {
		t.Fatalf("node B pollOnce: %v", err)
	}
	select {
	case evt := <-nodeB.events:
		if evt.Peer.ID != idA {
			t.Errorf("node B discovered %x, want node A %x", evt.Peer.ID[:2], idA[:2])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("node B discovered nobody")
	}
}
