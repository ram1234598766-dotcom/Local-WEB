package federation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ram1234598766-dotcom/Local-WEB/pkg/discovery"
)

func TestRendezvousRegisterAndLookup(t *testing.T) {
	store := NewMemoryStore()
	srv := httptest.NewServer(NewHTTPHandler(store))
	defer srv.Close()

	client := NewRendezvousClient(srv.URL)

	peer := discovery.PeerInfo{
		ID:        [32]byte{1, 2, 3, 4, 5},
		PublicKey: [32]byte{6, 7, 8, 9, 10},
		Name:      "test-peer",
		Addrs:     []string{"1.2.3.4:4443", "[::1]:4443"},
		Services:  nil,
		Source:    "rendezvous",
		LastSeen:  time.Now(),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Register(ctx, peer); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	found, err := client.Lookup(ctx, peer.ID)
	if err != nil {
		t.Fatalf("Lookup failed: %v", err)
	}

	if found.Name != peer.Name {
		t.Errorf("Name mismatch: got %q want %q", found.Name, peer.Name)
	}
	if len(found.Addrs) != len(peer.Addrs) {
		t.Errorf("Addrs mismatch: got %v want %v", found.Addrs, peer.Addrs)
	}
	for i, addr := range peer.Addrs {
		if found.Addrs[i] != addr {
			t.Errorf("Addr[%d] mismatch: got %q want %q", i, found.Addrs[i], addr)
		}
	}
	if found.PublicKey != peer.PublicKey {
		t.Errorf("PublicKey mismatch")
	}
}

func TestRendezvousLookupNotFound(t *testing.T) {
	store := NewMemoryStore()
	srv := httptest.NewServer(NewHTTPHandler(store))
	defer srv.Close()

	client := NewRendezvousClient(srv.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := client.Lookup(ctx, [32]byte{99, 99, 99})
	if err == nil {
		t.Fatal("expected error for unknown peer, got nil")
	}
}

func TestRendezvousRenewStale(t *testing.T) {
	store := NewMemoryStore()
	srv := httptest.NewServer(NewHTTPHandler(store))
	defer srv.Close()

	client := NewRendezvousClient(srv.URL)

	peer := discovery.PeerInfo{
		ID:    [32]byte{1, 2, 3},
		Name:  "p1",
		Addrs: []string{"10.0.0.1:4443"},
	}

	ctx := context.Background()

	if err := client.Register(ctx, peer); err != nil {
		t.Fatalf("Register: %v", err)
	}

	found, err := client.Lookup(ctx, peer.ID)
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if found.Addrs[0] != "10.0.0.1:4443" {
		t.Fatalf("initial addr mismatch: %s", found.Addrs[0])
	}

	peer.Addrs = []string{"10.0.0.2:4443"}
	if err := client.Register(ctx, peer); err != nil {
		t.Fatalf("Renew Register: %v", err)
	}

	found, err = client.Lookup(ctx, peer.ID)
	if err != nil {
		t.Fatalf("Lookup after renew: %v", err)
	}
	if found.Addrs[0] != "10.0.0.2:4443" {
		t.Errorf("addr after renew: got %s want 10.0.0.2:4443", found.Addrs[0])
	}
}

func TestRendezvousStoreGC(t *testing.T) {
	store := NewMemoryStore()

	peer := discovery.PeerInfo{
		ID:     [32]byte{1},
		Name:   "old",
		Addrs:  []string{"1.2.3.4:4443"},
		Source: "rendezvous",
	}
	// directly insert expired entry for GC testing
	store.mu.Lock()
	store.entries[peer.ID] = storeEntry{
		peer:    peer,
		expires: time.Now().Add(-2 * time.Minute),
	}
	store.mu.Unlock()

	time.Sleep(10 * time.Millisecond)

	removed := store.GC()
	if removed != 1 {
		t.Errorf("expected 1 entry GC'd, got %d", removed)
	}

	_, ok := store.Get(peer.ID)
	if ok {
		t.Error("expected peer to be removed after GC")
	}
}

func TestHTTPHandlerRegisterRoute(t *testing.T) {
	store := NewMemoryStore()
	srv := httptest.NewServer(NewHTTPHandler(store))
	defer srv.Close()

	peer := discovery.PeerInfo{
		ID:    [32]byte{5, 6, 7},
		Name:  "httptest",
		Addrs: []string{"1.2.3.4:4443"},
	}
	body, _ := json.Marshal(peer)

	req, _ := http.NewRequest("POST", srv.URL+"/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestRendezvousClientTimeout(t *testing.T) {
	blockingSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
	}))
	defer blockingSrv.Close()

	client := NewRendezvousClient(blockingSrv.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	peer := discovery.PeerInfo{ID: [32]byte{1}, Name: "timeout"}
	err := client.Register(ctx, peer)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	_ = err
}

func TestPeerInfoJSONRoundTrip(t *testing.T) {
	orig := discovery.PeerInfo{
		ID:        [32]byte{1, 2, 3},
		PublicKey: [32]byte{4, 5, 6},
		Name:      "json-roundtrip",
		Addrs:     []string{"1.2.3.4:4443"},
		Source:    "rendezvous",
		Score:     0.9,
	}

	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded discovery.PeerInfo
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.Name != orig.Name {
		t.Errorf("Name: got %q want %q", decoded.Name, orig.Name)
	}
	if decoded.Score != orig.Score {
		t.Errorf("Score: got %v want %v", decoded.Score, orig.Score)
	}
}

func TestRendezvousConcurrent(t *testing.T) {
	store := NewMemoryStore()
	srv := httptest.NewServer(NewHTTPHandler(store))
	defer srv.Close()

	client := NewRendezvousClient(srv.URL)

	peers := make([]discovery.PeerInfo, 50)
	for i := 0; i < 50; i++ {
		peers[i] = discovery.PeerInfo{
			ID:    [32]byte{byte(i)},
			Name:  fmt.Sprintf("peer-%d", i),
			Addrs: []string{fmt.Sprintf("10.0.0.%d:4443", i)},
		}
	}

	ctx := context.Background()
	done := make(chan error, len(peers))
	for _, p := range peers {
		go func(p discovery.PeerInfo) {
			done <- client.Register(ctx, p)
		}(p)
	}

	for i := 0; i < len(peers); i++ {
		if err := <-done; err != nil {
			t.Fatalf("Register error: %v", err)
		}
	}

	for _, p := range peers {
		found, err := client.Lookup(ctx, p.ID)
		if err != nil {
			t.Fatalf("Lookup peer %d: %v", p.ID[0], err)
		}
		if found.Name != p.Name {
			t.Errorf("peer %d: got name %q want %q", p.ID[0], found.Name, p.Name)
		}
	}
}
