package federation

import (
	"context"
	"encoding/hex"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ram1234598766-dotcom/Local-WEB/pkg/discovery"
)

// The rendezvous discovery loop re-registered on every tick and then only logged
// "polling for peers". It never read a peer list and never emitted a PeerEvent,
// so this discovery mode could not surface a single peer. These tests pin the
// behaviour that was missing: enumeration, event emission, deduplication and
// expiry.

func newTestRendezvous(t *testing.T) (*httptest.Server, *MemoryStore) {
	t.Helper()
	store := NewMemoryStore()
	srv := httptest.NewServer(NewHTTPHandler(store))
	t.Cleanup(srv.Close)
	return srv, store
}

func nodeID(b byte) [32]byte {
	var id [32]byte
	for i := range id {
		id[i] = b
	}
	return id
}

// The server needs a /peers route for a node to enumerate anyone at all; lookup
// alone only answers for ids the caller already has.
func TestHTTPPeersEndpointListsRegisteredPeers(t *testing.T) {
	store := NewMemoryStore()
	srv := httptest.NewServer(NewHTTPHandler(store))
	defer srv.Close()

	store.Put(discovery.PeerInfo{ID: nodeID(1), Name: "one", Addrs: []string{"1.2.3.4:4443"}}, time.Minute)
	store.Put(discovery.PeerInfo{ID: nodeID(2), Name: "two", Addrs: []string{"5.6.7.8:4443"}}, time.Minute)

	client := NewRendezvousClient(srv.URL)
	peers, err := client.ListPeers(context.Background())
	if err != nil {
		t.Fatalf("ListPeers: %v", err)
	}
	if len(peers) != 2 {
		t.Fatalf("GET /peers returned %d peers, want 2", len(peers))
	}

	names := map[string]bool{}
	for _, p := range peers {
		names[p.Name] = true
	}
	if !names["one"] || !names["two"] {
		t.Errorf("GET /peers missed a registered peer, got %v", names)
	}
}

func TestHTTPPeersEndpointDropsExpiredEntries(t *testing.T) {
	store := NewMemoryStore()
	srv := httptest.NewServer(NewHTTPHandler(store))
	defer srv.Close()

	store.Put(discovery.PeerInfo{ID: nodeID(1), Name: "live", Addrs: []string{"1.2.3.4:4443"}}, time.Minute)
	store.Put(discovery.PeerInfo{ID: nodeID(2), Name: "stale", Addrs: []string{"5.6.7.8:4443"}}, -time.Minute)

	client := NewRendezvousClient(srv.URL)
	peers, err := client.ListPeers(context.Background())
	if err != nil {
		t.Fatalf("ListPeers: %v", err)
	}
	if len(peers) != 1 || peers[0].Name != "live" {
		t.Errorf("expired entry was listed: %+v", peers)
	}
}

func TestHTTPPeersEndpointRejectsNonGET(t *testing.T) {
	srv := httptest.NewServer(NewHTTPHandler(NewMemoryStore()))
	defer srv.Close()

	resp, err := srv.Client().Post(srv.URL+"/peers", "application/json", nil)
	if err != nil {
		t.Fatalf("POST /peers: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 405 {
		t.Errorf("POST /peers = %d, want 405", resp.StatusCode)
	}
}

func TestRendezvousDiscoveryEmitsFoundPeer(t *testing.T) {
	store := NewMemoryStore()
	srv := httptest.NewServer(NewHTTPHandler(store))
	defer srv.Close()

	// A second node has registered itself with the server.
	remote := discovery.PeerInfo{
		ID:       nodeID(9),
		Name:     "far-node",
		Addrs:    []string{"198.51.100.7:4443"},
		Source:   "rendezvous",
		LastSeen: time.Now(),
	}
	store.Put(remote, time.Minute)

	mode := NewRendezvousDiscoveryMode(RendezvousModeConfig{
		ServerURL:    srv.URL,
		PollInterval: time.Hour, // the test drives pollOnce directly
	})
	mode.client = NewRendezvousClient(srv.URL)
	mode.events = make(chan discovery.PeerEvent, 8)
	mode.ctx, mode.cancel = context.WithCancel(context.Background())
	defer mode.cancel()

	self := discovery.PeerInfo{ID: nodeID(1), Name: "me", Addrs: []string{"127.0.0.1:4443"}}
	seen := map[[32]byte]peerState{}

	if err := mode.pollOnce(self, seen); err != nil {
		t.Fatalf("pollOnce: %v", err)
	}

	select {
	case evt := <-mode.events:
		if evt.Type != discovery.PeerFound {
			t.Errorf("event type = %v, want PeerFound", evt.Type)
		}
		if evt.Peer.ID != remote.ID {
			t.Errorf("peer id = %x, want %x", evt.Peer.ID[:4], remote.ID[:4])
		}
		if evt.Peer.Name != "far-node" {
			t.Errorf("peer name = %q, want far-node", evt.Peer.Name)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected a PeerFound event; the loop emitted nothing")
	}
}

func TestRendezvousDiscoveryDoesNotEmitSelf(t *testing.T) {
	store := NewMemoryStore()
	srv := httptest.NewServer(NewHTTPHandler(store))
	defer srv.Close()

	selfID := nodeID(1)
	store.Put(discovery.PeerInfo{ID: selfID, Name: "me", Addrs: []string{"127.0.0.1:4443"}}, time.Minute)

	mode := NewRendezvousDiscoveryMode(RendezvousModeConfig{ServerURL: srv.URL})
	mode.client = NewRendezvousClient(srv.URL)
	mode.events = make(chan discovery.PeerEvent, 8)
	mode.ctx, mode.cancel = context.WithCancel(context.Background())
	defer mode.cancel()

	self := discovery.PeerInfo{ID: selfID, Name: "me", Addrs: []string{"127.0.0.1:4443"}}
	if err := mode.pollOnce(self, map[[32]byte]peerState{}); err != nil {
		t.Fatalf("pollOnce: %v", err)
	}

	select {
	case evt := <-mode.events:
		t.Errorf("received an event for ourselves: %+v", evt)
	default:
	}
}

func TestRendezvousDiscoveryEmitsOnceForStablePeer(t *testing.T) {
	store := NewMemoryStore()
	srv := httptest.NewServer(NewHTTPHandler(store))
	defer srv.Close()

	remote := discovery.PeerInfo{
		ID: nodeID(9), Name: "far", Addrs: []string{"198.51.100.7:4443"},
		LastSeen: time.Now(),
	}
	store.Put(remote, time.Minute)

	mode := NewRendezvousDiscoveryMode(RendezvousModeConfig{ServerURL: srv.URL})
	mode.client = NewRendezvousClient(srv.URL)
	mode.events = make(chan discovery.PeerEvent, 8)
	mode.ctx, mode.cancel = context.WithCancel(context.Background())
	defer mode.cancel()

	self := discovery.PeerInfo{ID: nodeID(1), Addrs: []string{"127.0.0.1:4443"}}
	seen := map[[32]byte]peerState{}

	if err := mode.pollOnce(self, seen); err != nil {
		t.Fatalf("first pollOnce: %v", err)
	}
	<-mode.events // the PeerFound

	// Same LastSeen, so nothing changed and no second event is warranted.
	if err := mode.pollOnce(self, seen); err != nil {
		t.Fatalf("second pollOnce: %v", err)
	}
	select {
	case evt := <-mode.events:
		t.Errorf("an unchanged peer produced a second event: %+v", evt)
	default:
	}
}

func TestRendezvousDiscoveryEmitsUpdatedWhenPeerChanges(t *testing.T) {
	store := NewMemoryStore()
	srv := httptest.NewServer(NewHTTPHandler(store))
	defer srv.Close()

	remote := discovery.PeerInfo{
		ID: nodeID(9), Name: "far", Addrs: []string{"198.51.100.7:4443"},
		LastSeen: time.Now(),
	}
	store.Put(remote, time.Minute)

	mode := NewRendezvousDiscoveryMode(RendezvousModeConfig{ServerURL: srv.URL})
	mode.client = NewRendezvousClient(srv.URL)
	mode.events = make(chan discovery.PeerEvent, 8)
	mode.ctx, mode.cancel = context.WithCancel(context.Background())
	defer mode.cancel()

	self := discovery.PeerInfo{ID: nodeID(1), Addrs: []string{"127.0.0.1:4443"}}
	seen := map[[32]byte]peerState{}

	if err := mode.pollOnce(self, seen); err != nil {
		t.Fatalf("first pollOnce: %v", err)
	}
	<-mode.events

	// The peer re-advertises at a new address.
	store.Put(discovery.PeerInfo{
		ID: nodeID(9), Name: "far", Addrs: []string{"203.0.113.4:4443"},
		LastSeen: time.Now().Add(time.Minute),
	}, time.Minute)

	if err := mode.pollOnce(self, seen); err != nil {
		t.Fatalf("second pollOnce: %v", err)
	}
	select {
	case evt := <-mode.events:
		if evt.Type != discovery.PeerUpdated {
			t.Errorf("event type = %v, want PeerUpdated", evt.Type)
		}
		if len(evt.Peer.Addrs) == 0 || evt.Peer.Addrs[0] != "203.0.113.4:4443" {
			t.Errorf("event carried the wrong addresses: %v", evt.Peer.Addrs)
		}
	default:
		t.Error("expected a PeerUpdated event after the peer changed")
	}
}

func TestRendezvousDiscoverySkipsPeersWithoutAddresses(t *testing.T) {
	store := NewMemoryStore()
	srv := httptest.NewServer(NewHTTPHandler(store))
	defer srv.Close()

	store.Put(discovery.PeerInfo{ID: nodeID(9), Name: "no-addr"}, time.Minute)

	mode := NewRendezvousDiscoveryMode(RendezvousModeConfig{ServerURL: srv.URL})
	mode.client = NewRendezvousClient(srv.URL)
	mode.events = make(chan discovery.PeerEvent, 8)
	mode.ctx, mode.cancel = context.WithCancel(context.Background())
	defer mode.cancel()

	self := discovery.PeerInfo{ID: nodeID(1), Addrs: []string{"127.0.0.1:4443"}}
	if err := mode.pollOnce(self, map[[32]byte]peerState{}); err != nil {
		t.Fatalf("pollOnce: %v", err)
	}
	select {
	case evt := <-mode.events:
		t.Errorf("emitted an undialable peer: %+v", evt)
	default:
	}
}

func TestRendezvousDiscoveryReportsExpiredPeer(t *testing.T) {
	store := NewMemoryStore()
	srv := httptest.NewServer(NewHTTPHandler(store))
	defer srv.Close()

	remote := discovery.PeerInfo{
		ID: nodeID(9), Name: "gone", Addrs: []string{"198.51.100.7:4443"},
		LastSeen: time.Now(),
	}
	store.Put(remote, time.Minute)

	mode := NewRendezvousDiscoveryMode(RendezvousModeConfig{ServerURL: srv.URL})
	mode.client = NewRendezvousClient(srv.URL)
	mode.events = make(chan discovery.PeerEvent, 8)
	mode.ctx, mode.cancel = context.WithCancel(context.Background())
	defer mode.cancel()

	self := discovery.PeerInfo{ID: nodeID(1), Addrs: []string{"127.0.0.1:4443"}}
	seen := map[[32]byte]peerState{}

	if err := mode.pollOnce(self, seen); err != nil {
		t.Fatalf("pollOnce: %v", err)
	}
	<-mode.events // PeerFound

	// The peer stops re-registering and its server entry lapses, so the server
	// stops listing it. Re-putting it with an elapsed TTL reproduces that without
	// sleeping.
	store.Put(remote, -time.Minute)

	// Even with the entry gone, a peer is not declared lost until it has been
	// absent for longer than one expiry window, so a single missed listing does
	// not flap it.
	if err := mode.pollOnce(self, seen); err != nil {
		t.Fatalf("second pollOnce: %v", err)
	}
	select {
	case evt := <-mode.events:
		t.Errorf("peer was declared lost after a single missed listing: %+v", evt)
	default:
	}

	// Age the observation past the expiry window and try again.
	st := seen[remote.ID]
	st.observedAt = time.Now().Add(-2 * pollExpiry)
	seen[remote.ID] = st

	if err := mode.pollOnce(self, seen); err != nil {
		t.Fatalf("third pollOnce: %v", err)
	}
	select {
	case evt := <-mode.events:
		if evt.Type != discovery.PeerLost {
			t.Errorf("event type = %v, want PeerLost", evt.Type)
		}
		if evt.Peer.ID != remote.ID {
			t.Errorf("lost peer id = %x, want %x", evt.Peer.ID[:4], remote.ID[:4])
		}
	default:
		t.Error("expected a PeerLost event for the expired peer")
	}

	if _, still := seen[remote.ID]; still {
		t.Error("an expired peer should be dropped from the seen set")
	}
}

func TestRendezvousListPeersDecodesServerList(t *testing.T) {
	store := NewMemoryStore()
	srv := httptest.NewServer(NewHTTPHandler(store))
	defer srv.Close()

	store.Put(discovery.PeerInfo{ID: nodeID(3), Name: "three", Addrs: []string{"3.3.3.3:1"}}, time.Minute)

	client := NewRendezvousClient(srv.URL)
	peers, err := client.ListPeers(context.Background())
	if err != nil {
		t.Fatalf("ListPeers: %v", err)
	}
	if len(peers) != 1 {
		t.Fatalf("expected 1 peer, got %d", len(peers))
	}
	if peers[0].Name != "three" {
		t.Errorf("peer name = %q", peers[0].Name)
	}
}

func TestRendezvousListPeersPropagatesServerFailure(t *testing.T) {
	srv := httptest.NewServer(nil) // no handler: every path 404s
	defer srv.Close()

	client := NewRendezvousClient(srv.URL + "/nope")
	if _, err := client.ListPeers(context.Background()); err == nil {
		t.Error("expected an error when /peers is unavailable")
	}
}

// The lookup route parses hex by hand and silently accepted malformed input;
// confirm the well-formed path still works after the new route was added.
func TestLookupStillWorksAlongsidePeersRoute(t *testing.T) {
	store := NewMemoryStore()
	srv := httptest.NewServer(NewHTTPHandler(store))
	defer srv.Close()

	want := nodeID(4)
	store.Put(discovery.PeerInfo{ID: want, Name: "four", Addrs: []string{"4.4.4.4:1"}}, time.Minute)

	client := NewRendezvousClient(srv.URL)
	peer, err := client.Lookup(context.Background(), want)
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if peer.Name != "four" {
		t.Errorf("name = %q, want four", peer.Name)
	}

	// Sanity check the hex encoding used in the query.
	if got := fmtHex(want[:]); len(got) != 64 {
		t.Errorf("fmtHex produced %d chars, want 64", len(got))
	}
	if _, err := hex.DecodeString(fmtHex(want[:])); err != nil {
		t.Errorf("fmtHex output is not valid hex: %v", err)
	}
}
