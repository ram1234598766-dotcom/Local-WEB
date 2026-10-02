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

	"strconv"

	"github.com/ram1234598766-dotcom/Local-WEB/pkg/dht"
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
//
// The write path is proof-of-work gated and rate limited. A rendezvous server is
// reached by address rather than by account, because requiring registration would
// make bootstrapping the network circular, so without that gate this endpoint is
// a way for anyone who learns the address to fill memory and to hand other nodes
// junk addresses.
type HTTPHandler struct {
	store Store

	// difficulty is the proof-of-work threshold a registration must meet.
	difficulty int

	// mu guards lastRegister, which concurrent registrations write. The store has
	// its own locking; this map is handler state and needed its own.
	mu sync.Mutex
	// lastRegister is per node, for rate limiting. Kept here rather than in the
	// store so that a peer expiring does not reset its own limit.
	lastRegister map[[32]byte]time.Time

	// now is injectable so expiry and rate limiting can be tested without sleeping.
	now func() time.Time
}

// HandlerOption configures an HTTPHandler.
type HandlerOption func(*HTTPHandler)

// WithDifficulty sets the proof-of-work threshold. A value below the DHT minimum
// is raised, because dht.VerifyPoW rejects anything lower and every registration
// would then fail.
func WithDifficulty(d int) HandlerOption {
	return func(h *HTTPHandler) {
		if d < dht.MinPoWDifficulty {
			d = dht.MinPoWDifficulty
		}
		if d > dht.MaxPoWDifficulty {
			d = dht.MaxPoWDifficulty
		}
		h.difficulty = d
	}
}

// WithClock replaces the time source. Intended for tests.
func WithClock(now func() time.Time) HandlerOption {
	return func(h *HTTPHandler) { h.now = now }
}

// MinRegisterInterval is how often one node may re-register.
//
// Without it a single peer can rewrite its entry as fast as the server answers,
// which is a way to keep the store permanently dirty.
const MinRegisterInterval = 10 * time.Second

// NewHTTPHandler returns an http.Handler for the rendezvous protocol.
func NewHTTPHandler(store Store, opts ...HandlerOption) http.Handler {
	h := &HTTPHandler{
		store:        store,
		difficulty:   dht.MinPoWDifficulty,
		lastRegister: make(map[[32]byte]time.Time),
		now:          time.Now,
	}
	for _, opt := range opts {
		opt(h)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/register", h.handleRegister)
	mux.HandleFunc("/lookup", h.handleLookup)
	mux.HandleFunc("/peers", h.handleListPeers)
	mux.HandleFunc("/health", h.handleHealth)
	return mux
}

// handleRegister stores a peer advertisement.
//
// The body is a Registration envelope rather than a bare PeerInfo so the proof of
// work can cover exactly the bytes being registered. Wrapping is what makes the
// gate meaningful: without it a client could solve once and then advertise
// whatever it liked.
func (h *HTTPHandler) handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRegisterBody))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var env Registration
	if err := json.Unmarshal(body, &env); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	peer := env.Peer

	// The proof of work covers the marshalled peer, recomputed from the record as
	// received rather than trusted from the envelope.
	signed, err := json.Marshal(peer)
	if err != nil {
		http.Error(w, "cannot encode peer", http.StatusBadRequest)
		return
	}
	if !dht.VerifyPoW(signed, env.Nonce, h.difficulty) {
		// Tell the client how hard to work, so a retry can succeed. Without this a
		// client configured below the server's threshold could never register.
		w.Header().Set("X-PoW-Difficulty", itoa(h.difficulty))
		http.Error(w, "invalid proof of work", http.StatusForbidden)
		return
	}

	if err := validatePeerPeer(peer.Addrs, peer.Name, len(peer.Services)); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	now := h.now()

	// Check and claim the rate-limit slot under one lock. Splitting the read and
	// the write would let two concurrent registrations for the same node both pass
	// the check.
	h.mu.Lock()
	if last, ok := h.lastRegister[peer.ID]; ok && now.Sub(last) < MinRegisterInterval {
		retry := int(MinRegisterInterval.Seconds() - now.Sub(last).Seconds())
		if retry < 1 {
			retry = 1
		}
		h.mu.Unlock()
		w.Header().Set("Retry-After", itoa(retry))
		http.Error(w, "registering too frequently", http.StatusTooManyRequests)
		return
	}
	h.lastRegister[peer.ID] = now
	h.mu.Unlock()

	peer.Source = "rendezvous"
	peer.LastSeen = now
	// Put replaces rather than appends, so a peer that changed its addresses
	// replaces the old list instead of leaving stale ones to be handed out.
	h.store.Put(peer, EntryTTL)

	w.WriteHeader(http.StatusOK)
}

// handleHealth reports what the server is doing, so an operator can check it
// without registering a node of their own.
func (h *HTTPHandler) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	peers := 0
	if h.store != nil {
		peers = len(h.store.All())
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = fmt.Fprintf(w, `{"peers":%d,"difficulty":%d}`, peers, h.difficulty)
}

func (h *HTTPHandler) handleLookup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id, err := parseNodeID(r.URL.Query().Get("node_id"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
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

	// powDifficulty is the proof-of-work threshold to register at. It starts at the
	// DHT's minimum and is raised if a server asks for more, so a client works for
	// both a default and a hardened server without being configured.
	powDifficulty int
}

// NewRendezvousClient creates a client that speaks the rendezvous HTTP API.
func NewRendezvousClient(baseURL string) *RendezvousClient {
	return &RendezvousClient{
		baseURL: baseURL,
		http: &http.Client{
			Timeout: 10 * time.Second,
		},
		powDifficulty: dht.MinPoWDifficulty,
	}
}

// Register publishes this node's peer info to the rendezvous server.
// Registration is the request body for POST /register.
//
// The peer is wrapped rather than posted bare so the proof of work can cover
// exactly the bytes being registered. A server that is public and unauthenticated
// needs the work to be checked, and without a wrapper the client would have no
// way to tell the server which bytes to verify.
type Registration struct {
	Peer  discovery.PeerInfo `json:"peer"`
	Nonce []byte             `json:"nonce"`
}

// Register advertises this peer with the rendezvous server.
//
// It solves a proof of work first. That is not decoration: the endpoint is public
// and unauthenticated, so an uncharged write is an open way to fill the store and
// to poison other nodes with junk addresses. It is the same anti-Sybil mechanism
// the DHT already uses for the same job.
//
// A 403 carrying X-PoW-Difficulty is retried once at that difficulty, because a
// server may be configured harder than the client's default and a client that
// gave up on the first refusal could never register at all.
func (c *RendezvousClient) Register(ctx context.Context, peer discovery.PeerInfo) error {
	payload, err := json.Marshal(peer)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	difficulty := c.powDifficulty
	for attempt := 0; attempt < 2; attempt++ {
		nonce, err := dht.SolvePoW(payload, difficulty)
		if err != nil {
			return fmt.Errorf("solve proof of work: %w", err)
		}

		body, err := json.Marshal(Registration{Peer: peer, Nonce: nonce})
		if err != nil {
			return fmt.Errorf("marshal registration: %w", err)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/register", bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := c.http.Do(req)
		if err != nil {
			return fmt.Errorf("register: %w", err)
		}
		status := resp.StatusCode
		hint := resp.Header.Get("X-PoW-Difficulty")
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()

		if status == http.StatusOK {
			return nil
		}
		if status == http.StatusForbidden && hint != "" && attempt == 0 {
			if d, convErr := strconv.Atoi(hint); convErr == nil && d != difficulty {
				difficulty = d
				c.powDifficulty = d
				continue
			}
		}
		return fmt.Errorf("register: %s", resp.Status)
	}
	return fmt.Errorf("register: server kept refusing the registration")
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

// handleListPeers returns every peer currently held by the store.
//
// Lookup only answers for an id the caller already knows, which is no use to a
// node trying to find anyone in the first place. Enumeration is what lets a
// node discover that a peer exists across the internet, so the discovery loop
// needs this endpoint. Expired entries are dropped rather than returned.
func (h *HTTPHandler) handleListPeers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Give expired entries a chance to be reaped before listing.
	if gc, ok := h.store.(interface{ GC() int }); ok {
		gc.GC()
	}
	peers := h.store.All()
	if peers == nil {
		peers = []discovery.PeerInfo{}
	}
	data, err := json.Marshal(peers)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}

// maxPeerListBytes bounds the list response the client will read.
const maxPeerListBytes = 4 << 20

// ListPeers asks the rendezvous server which peers it currently knows about.
//
// This is the enumeration half of federation: without it a node can only confirm
// peers it already has an id for, so it could never learn that anyone exists.
func (c *RendezvousClient) ListPeers(ctx context.Context) ([]discovery.PeerInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/peers", nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list peers: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list peers: %s", resp.Status)
	}
	var peers []discovery.PeerInfo
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxPeerListBytes)).Decode(&peers); err != nil {
		return nil, fmt.Errorf("decode peer list: %w", err)
	}
	return peers, nil
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
