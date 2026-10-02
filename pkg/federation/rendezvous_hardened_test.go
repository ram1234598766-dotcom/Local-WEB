package federation

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ram1234598766-dotcom/Local-WEB/pkg/dht"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/discovery"
)

// newRendezvous starts a server over a fresh store with a controllable clock.
func newRendezvous(t *testing.T, now *time.Time, opts ...HandlerOption) (http.Handler, Store) {
	t.Helper()
	store := NewMemoryStore()
	all := append([]HandlerOption{WithClock(func() time.Time { return *now })}, opts...)
	return NewHTTPHandler(store, all...), store
}

func goodPeer(id byte) discovery.PeerInfo {
	var nodeID [32]byte
	for i := range nodeID {
		nodeID[i] = id + byte(i)
	}
	var pub [32]byte
	for i := range pub {
		pub[i] = id*3 + byte(i)
	}
	return discovery.PeerInfo{
		ID:        nodeID,
		PublicKey: pub,
		Name:      "peer-" + string(rune('a'+id%26)),
		Addrs:     []string{"10.0.0.1:4443", "10.0.0.2:4443"},
	}
}

// post registers a peer with a solved proof of work and returns the recorder.
func post(t *testing.T, srv http.Handler, peer discovery.PeerInfo, difficulty int) *httptest.ResponseRecorder {
	t.Helper()
	signed, err := json.Marshal(peer)
	require.NoError(t, err)
	nonce, err := dht.SolvePoW(signed, difficulty)
	require.NoError(t, err)
	body, err := json.Marshal(Registration{Peer: peer, Nonce: nonce})
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(string(body))))
	return rec
}

// TestRendezvousRegistersAndLooksUp is the base case for a server an operator can
// actually run.
func TestRendezvousRegistersAndLooksUp(t *testing.T) {
	now := time.Now()
	srv, store := newRendezvous(t, &now)

	peer := goodPeer(1)
	require.Equal(t, http.StatusOK, post(t, srv, peer, dht.MinPoWDifficulty).Code)
	require.Len(t, store.All(), 1)

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/lookup?node_id="+fmtHex(peer.ID[:]), nil))
	require.Equal(t, http.StatusOK, rec.Code)

	var got discovery.PeerInfo
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Equal(t, peer.ID, got.ID)
	require.Equal(t, peer.Addrs, got.Addrs)
	require.Equal(t, "rendezvous", got.Source)
}

// TestClientAndServerInteroperate is the test that matters: the real client must
// register with the real server, proof of work and all.
func TestClientAndServerInteroperate(t *testing.T) {
	now := time.Now()
	srv, _ := newRendezvous(t, &now)
	ts := httptest.NewServer(srv)
	defer ts.Close()

	client := NewRendezvousClient(ts.URL)
	peer := goodPeer(1)

	require.NoError(t, client.Register(context.Background(), peer))

	found, err := client.Lookup(context.Background(), peer.ID)
	require.NoError(t, err)
	require.Equal(t, peer.ID, found.ID)
	require.Equal(t, peer.Addrs, found.Addrs)

	list, err := client.ListPeers(context.Background())
	require.NoError(t, err)
	require.Len(t, list, 1)
}

// TestRendezvousRequiresProofOfWork is the security test for the write path.
// This endpoint is reached by address rather than by account, so without a charge
// anyone could fill the store and hand other nodes junk addresses.
func TestRendezvousRequiresProofOfWork(t *testing.T) {
	now := time.Now()
	srv, store := newRendezvous(t, &now)

	peer := goodPeer(1)
	body, err := json.Marshal(Registration{Peer: peer, Nonce: []byte("not-a-solution")})
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(string(body))))
	require.Equal(t, http.StatusForbidden, rec.Code)

	// A client must be told how hard to work, or a retry cannot succeed.
	require.Equal(t, itoa(dht.MinPoWDifficulty), rec.Header().Get("X-PoW-Difficulty"))
	require.Empty(t, store.All(), "a rejected registration must not be stored")
}

// TestRendezvousRejectsWorkOverTheWrongBytes checks the gate covers the record. If
// it did not, a client could solve once and then advertise whatever it liked.
func TestRendezvousRejectsWorkOverTheWrongBytes(t *testing.T) {
	now := time.Now()
	srv, store := newRendezvous(t, &now)

	honest := goodPeer(1)
	signed, err := json.Marshal(honest)
	require.NoError(t, err)
	nonce, err := dht.SolvePoW(signed, dht.MinPoWDifficulty)
	require.NoError(t, err)

	tampered := honest
	tampered.Addrs = []string{"203.0.113.1:4443"}
	body, err := json.Marshal(Registration{Peer: tampered, Nonce: nonce})
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(string(body))))
	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Empty(t, store.All())
}

// TestRendezvousRejectsMalformedPeers covers the boundary. Every field is
// client-controlled and is handed straight to other nodes.
func TestRendezvousRejectsMalformedPeers(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*discovery.PeerInfo)
	}{
		{"no name", func(p *discovery.PeerInfo) { p.Name = "" }},
		{"name too long", func(p *discovery.PeerInfo) { p.Name = strings.Repeat("x", maxNameLen+1) }},
		{"no addresses", func(p *discovery.PeerInfo) { p.Addrs = nil }},
		{"too many addresses", func(p *discovery.PeerInfo) {
			p.Addrs = make([]string, maxAddrsPerPeer+1)
			for i := range p.Addrs {
				p.Addrs[i] = "10.0.0.1:4443"
			}
		}},
		{"address is not host:port", func(p *discovery.PeerInfo) { p.Addrs = []string{"not-an-address"} }},
		{"address too long", func(p *discovery.PeerInfo) {
			p.Addrs = []string{strings.Repeat("a", maxAddrLen+1) + ":1"}
		}},
		{"empty address", func(p *discovery.PeerInfo) { p.Addrs = []string{""} }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Now()
			srv, store := newRendezvous(t, &now)
			peer := goodPeer(1)
			tc.mutate(&peer)

			// Solve over the exact bytes that will be sent, so the rejection is
			// about validation and not a missing proof of work.
			rec := post(t, srv, peer, dht.MinPoWDifficulty)
			require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
			require.Empty(t, store.All())
		})
	}
}

// TestRendezvousRateLimitsRegistration keeps one peer from rewriting its entry as
// fast as the server answers.
func TestRendezvousRateLimitsRegistration(t *testing.T) {
	now := time.Now()
	srv, _ := newRendezvous(t, &now)
	peer := goodPeer(1)

	require.Equal(t, http.StatusOK, post(t, srv, peer, dht.MinPoWDifficulty).Code)

	rec := post(t, srv, peer, dht.MinPoWDifficulty)
	require.Equal(t, http.StatusTooManyRequests, rec.Code)
	require.NotEmpty(t, rec.Header().Get("Retry-After"),
		"a throttled client should be told when to come back")

	// After the interval it succeeds again.
	now = now.Add(MinRegisterInterval + time.Second)
	require.Equal(t, http.StatusOK, post(t, srv, peer, dht.MinPoWDifficulty).Code)
}

// TestRendezvousReplacesRatherThanAppends checks a peer that changes its addresses
// replaces the old list rather than leaving stale ones to be handed out.
func TestRendezvousReplacesRatherThanAppends(t *testing.T) {
	now := time.Now()
	srv, store := newRendezvous(t, &now)
	peer := goodPeer(1)

	require.Equal(t, http.StatusOK, post(t, srv, peer, dht.MinPoWDifficulty).Code)

	now = now.Add(MinRegisterInterval + time.Second)
	peer.Addrs = []string{"10.0.0.9:4443"}
	require.Equal(t, http.StatusOK, post(t, srv, peer, dht.MinPoWDifficulty).Code)

	require.Len(t, store.All(), 1, "re-registering must replace, not accumulate")
	require.Equal(t, []string{"10.0.0.9:4443"}, store.All()[0].Addrs)
}

// TestRendezvousRejectsTruncatedNodeID is a real bug this fixes. The old handler
// accepted any id up to 64 characters and zero-padded the remainder, discarding
// the parse error, so a short or non-hex id could resolve to a different peer.
func TestRendezvousRejectsTruncatedNodeID(t *testing.T) {
	now := time.Now()
	srv, _ := newRendezvous(t, &now)
	peer := goodPeer(1)
	require.Equal(t, http.StatusOK, post(t, srv, peer, dht.MinPoWDifficulty).Code)

	for _, id := range []string{
		"",                      // missing
		"ab",                    // far too short
		fmtHex(peer.ID[:])[:10], // truncated prefix of a real id
		strings.Repeat("z", 64), // not hex
		strings.Repeat("0", 65), // too long
	} {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/lookup?node_id="+id, nil))
		require.Equalf(t, http.StatusBadRequest, rec.Code,
			"node_id %q must be rejected, not padded into someone else's id", id)
	}
}

// TestRendezvousHealth checks an operator can see what the server is doing without
// registering a node.
func TestRendezvousHealth(t *testing.T) {
	now := time.Now()
	srv, _ := newRendezvous(t, &now, WithDifficulty(dht.MinPoWDifficulty+2))
	require.Equal(t, http.StatusOK, post(t, srv, goodPeer(1), dht.MinPoWDifficulty+2).Code)

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	require.Equal(t, http.StatusOK, rec.Code)

	var got struct {
		Peers      int `json:"peers"`
		Difficulty int `json:"difficulty"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Equal(t, 1, got.Peers)
	require.Equal(t, dht.MinPoWDifficulty+2, got.Difficulty)
}

// TestWithDifficultyRaisesTooLow checks a threshold below what VerifyPoW accepts
// is raised rather than rejecting every registration.
func TestWithDifficultyRaisesTooLow(t *testing.T) {
	now := time.Now()
	srv, store := newRendezvous(t, &now, WithDifficulty(1))

	// The server must still accept a default-difficulty proof, which it cannot if
	// the threshold stayed at 1.
	require.Equal(t, http.StatusOK, post(t, srv, goodPeer(1), dht.MinPoWDifficulty).Code)
	require.Len(t, store.All(), 1)
}

func TestRendezvousRejectsBadRequests(t *testing.T) {
	now := time.Now()
	srv, _ := newRendezvous(t, &now)

	t.Run("body is not json", func(t *testing.T) {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/register", strings.NewReader("{")))
		require.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("unknown peer", func(t *testing.T) {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/lookup?node_id="+strings.Repeat("0", 64), nil))
		require.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("wrong methods", func(t *testing.T) {
		for _, path := range []string{"/lookup", "/peers", "/health"} {
			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, nil))
			require.Equalf(t, http.StatusMethodNotAllowed, rec.Code, path)
		}
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/register", nil))
		require.Equal(t, http.StatusMethodNotAllowed, rec.Code)
	})
}
