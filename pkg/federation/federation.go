package federation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/ram1234598766-dotcom/Local-WEB/pkg/discovery"
)

// EntryTTL is the default TTL for a registered peer.
const EntryTTL = 5 * time.Minute

// Store is the interface that the HTTP handler uses to persist peer info.
type Store interface {
	Get(id [32]byte) (discovery.PeerInfo, bool)
	Put(peer discovery.PeerInfo, ttl time.Duration)
	GC() int
	All() []discovery.PeerInfo
}

// MemoryStore is a thread-safe in-memory implementation of Store.
type MemoryStore struct {
	mu      sync.RWMutex
	entries map[[32]byte]storeEntry
}

type storeEntry struct {
	peer    discovery.PeerInfo
	expires time.Time
}

// NewMemoryStore creates a new in-memory store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		entries: make(map[[32]byte]storeEntry),
	}
}

func (s *MemoryStore) Get(id [32]byte) (discovery.PeerInfo, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry, ok := s.entries[id]
	if !ok || time.Now().After(entry.expires) {
		return discovery.PeerInfo{}, false
	}
	return entry.peer, true
}

func (s *MemoryStore) Put(peer discovery.PeerInfo, ttl time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[peer.ID] = storeEntry{
		peer:    peer,
		expires: time.Now().Add(ttl),
	}
}

func (s *MemoryStore) GC() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	count := 0
	for id, entry := range s.entries {
		if now.After(entry.expires) {
			delete(s.entries, id)
			count++
		}
	}
	return count
}

func (s *MemoryStore) All() []discovery.PeerInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]discovery.PeerInfo, 0, len(s.entries))
	for _, entry := range s.entries {
		out = append(out, entry.peer)
	}
	return out
}

// HTTPHandler exposes a Store over HTTP with simple JSON endpoints.
type HTTPHandler struct {
	store Store
}

// NewHTTPHandler returns an http.Handler for the rendezvous protocol.
func NewHTTPHandler(store Store) http.Handler {
	h := &HTTPHandler{store: store}

	mux := http.NewServeMux()
	mux.HandleFunc("/register", h.handleRegister)
	mux.HandleFunc("/lookup", h.handleLookup)
	return mux
}

func (h *HTTPHandler) handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<10))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var peer discovery.PeerInfo
	if err := json.Unmarshal(body, &peer); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if len(peer.Addrs) == 0 {
		http.Error(w, "no addresses provided", http.StatusBadRequest)
		return
	}
	peer.Source = "rendezvous"
	peer.LastSeen = time.Now()
	h.store.Put(peer, EntryTTL)
	w.WriteHeader(http.StatusOK)
}

func (h *HTTPHandler) handleLookup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	idHex := r.URL.Query().Get("node_id")
	if len(idHex) == 0 {
		http.Error(w, "missing node_id", http.StatusBadRequest)
		return
	}
	var id [32]byte
	if len(idHex) <= 64 {
		for i := 0; i < len(idHex)/2; i++ {
			var b byte
			fmt.Sscanf(idHex[i*2:i*2+2], "%02x", &b)
			id[i] = b
		}
	}
	peer, ok := h.store.Get(id)
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	data, err := json.Marshal(peer)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}

// RendezvousClient talks to a rendezvous server to register and discover peers.
type RendezvousClient struct {
	baseURL string
	http    *http.Client
}

// NewRendezvousClient creates a client that speaks the rendezvous HTTP API.
func NewRendezvousClient(baseURL string) *RendezvousClient {
	return &RendezvousClient{
		baseURL: baseURL,
		http: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Register publishes this node's peer info to the rendezvous server.
func (c *RendezvousClient) Register(ctx context.Context, peer discovery.PeerInfo) error {
	data, err := json.Marshal(peer)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/register", bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("register: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("register: %s", resp.Status)
	}
	return nil
}

// Lookup queries the rendezvous server for a peer's public endpoint.
func (c *RendezvousClient) Lookup(ctx context.Context, id [32]byte) (discovery.PeerInfo, error) {
	idHex := fmtHex(id[:])
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/lookup?node_id="+idHex, nil)
	if err != nil {
		return discovery.PeerInfo{}, fmt.Errorf("create request: %w", err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return discovery.PeerInfo{}, fmt.Errorf("lookup: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return discovery.PeerInfo{}, fmt.Errorf("peer not found")
	}
	if resp.StatusCode != http.StatusOK {
		return discovery.PeerInfo{}, fmt.Errorf("lookup: %s", resp.Status)
	}
	var peer discovery.PeerInfo
	if err := json.NewDecoder(resp.Body).Decode(&peer); err != nil {
		return discovery.PeerInfo{}, fmt.Errorf("decode: %w", err)
	}
	return peer, nil
}

func fmtHex(data []byte) string {
	const hexd = "0123456789abcdef"
	out := make([]byte, len(data)*2)
	for i, b := range data {
		out[i*2] = hexd[b>>4]
		out[i*2+1] = hexd[b&0x0f]
	}
	return string(out)
}
