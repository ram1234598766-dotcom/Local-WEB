package gui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ram1234598766-dotcom/Local-WEB/pkg/crypto"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/security"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/store"
)

func TestNodeStatus(t *testing.T) {
	pubKey := [32]byte{1, 2, 3, 4, 5}
	api := NewAPI(pubKey)

	status := api.Status()
	if status.NodeID == "" {
		t.Error("expected non-empty node ID")
	}
	if status.PublicKey == "" {
		t.Error("expected non-empty public key")
	}
}

func TestNodePeersEmpty(t *testing.T) {
	pubKey := [32]byte{1, 2, 3}
	api := NewAPI(pubKey)

	peers, err := api.Peers()
	if err == nil {
		t.Error("expected error when peer store not set")
	}
	_ = peers
}

func TestStatusHandler(t *testing.T) {
	pubKey := [32]byte{1, 2, 3}
	api := NewAPI(pubKey)
	h := NewHandler(api)

	req := httptest.NewRequest("GET", "/api/status", nil)
	w := httptest.NewRecorder()
	h.handleStatus(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp StatusResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.NodeID == "" {
		t.Error("expected non-empty node ID")
	}
}

func TestPeersHandlerNoStore(t *testing.T) {
	pubKey := [32]byte{1, 2, 3}
	api := NewAPI(pubKey)
	h := NewHandler(api)

	req := httptest.NewRequest("GET", "/api/peers", nil)
	w := httptest.NewRecorder()
	h.handlePeers(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
}

func TestPeersHandlerWithStore(t *testing.T) {
	pubKey := [32]byte{1, 2, 3}
	api := NewAPI(pubKey)
	s := store.OpenInMemory()
	api.SetStore(s)
	api.SetPeerStore(store.NewPeerStore(s))

	h := NewHandler(api)

	req := httptest.NewRequest("GET", "/api/peers", nil)
	w := httptest.NewRecorder()
	h.handlePeers(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var peers []PeerResponse
	if err := json.NewDecoder(w.Body).Decode(&peers); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if peers == nil {
		t.Error("expected non-nil peers slice")
	}
}

func TestHealthzHandler(t *testing.T) {
	pubKey := [32]byte{1, 2, 3}
	api := NewAPI(pubKey)
	h := NewHandler(api)

	req := httptest.NewRequest("GET", "/healthz", nil)
	w := httptest.NewRecorder()
	h.handleHealthz(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestReadyzHandler(t *testing.T) {
	pubKey := [32]byte{1, 2, 3}
	api := NewAPI(pubKey)
	h := NewHandler(api)

	req := httptest.NewRequest("GET", "/readyz", nil)
	w := httptest.NewRecorder()
	h.handleReadyz(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestEventsHandler(t *testing.T) {
	pubKey := [32]byte{1, 2, 3}
	api := NewAPI(pubKey)
	h := NewHandler(api)

	req := httptest.NewRequest("GET", "/api/events", nil)
	w := httptest.NewRecorder()

	ctx, cancel := context.WithCancel(req.Context())
	req = req.WithContext(ctx)

	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	h.handleEvents(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestServicesHealthHandler(t *testing.T) {
	pubKey := [32]byte{1, 2, 3}
	api := NewAPI(pubKey)
	h := NewHandler(api)

	req := httptest.NewRequest("GET", "/api/services/health", nil)
	w := httptest.NewRecorder()
	h.handleServicesHealth(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestSyncStatusHandler(t *testing.T) {
	pubKey := [32]byte{1, 2, 3}
	api := NewAPI(pubKey)
	h := NewHandler(api)

	req := httptest.NewRequest("GET", "/api/crdt/sync-status", nil)
	w := httptest.NewRecorder()
	h.handleSyncStatus(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp SyncStatusResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
}

func TestAuditVerifyHandler(t *testing.T) {
	pubKey := [32]byte{1, 2, 3}
	api := NewAPI(pubKey)
	h := NewHandler(api)

	req := httptest.NewRequest("GET", "/api/audit-log/verify", nil)
	w := httptest.NewRecorder()
	h.handleAuditVerify(w, req)

	// With no audit log attached the server genuinely cannot answer, so a 5xx is
	// correct here.
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when audit log not set, got %d", w.Code)
	}

	var verifyResp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&verifyResp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if verifyResp["verified"] != false {
		t.Error("expected verified=false when no audit log")
	}
	if verifyResp["integrity"] != string(AuditStateUnavailable) {
		t.Errorf("expected integrity=%q, got %v", AuditStateUnavailable, verifyResp["integrity"])
	}
}

// The SSE stream must emit periodic bytes even when no events occur, otherwise
// it is indistinguishable from a dead connection and gets reaped by proxies.
// docs/api/WS_API.md documents a ": heartbeat" comment every 30s.
func TestEventsHandlerEmitsHeartbeat(t *testing.T) {
	api := NewAPI([32]byte{1, 2, 3})
	h := NewHandler(api)
	h.sseHeartbeat = 20 * time.Millisecond

	srv := httptest.NewServer(h.mux)
	defer srv.Close()

	req, err := http.NewRequest("GET", srv.URL+"/api/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(req.Context(), 3*time.Second)
	defer cancel()

	resp, err := http.DefaultClient.Do(req.WithContext(ctx))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("expected text/event-stream, got %q", ct)
	}

	buf := make([]byte, 64)
	deadline := time.Now().Add(2 * time.Second)
	saw := false
	for time.Now().Before(deadline) && !saw {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			if strings.Contains(string(buf[:n]), ": heartbeat") {
				saw = true
				break
			}
		}
		if err != nil {
			t.Fatalf("read before heartbeat: %v", err)
		}
	}

	if !saw {
		t.Fatal("no heartbeat comment received within 2s; the stream looks dead to clients")
	}
}

// A named event must arrive as `event: <type>` with a JSON data line the client
// can parse.
func TestEventsHandlerStreamsNamedEvents(t *testing.T) {
	api := NewAPI([32]byte{1, 2, 3})
	h := NewHandler(api)
	h.sseHeartbeat = time.Hour // heartbeat off, so only the event is observed

	srv := httptest.NewServer(h.mux)
	defer srv.Close()

	// Give the handler a moment to subscribe before broadcasting.
	go func() {
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			api.BroadcastEvent(SSEEvent{Type: "peer_connected", Data: map[string]string{"peer": "abc"}})
			time.Sleep(25 * time.Millisecond)
		}
	}()

	req, err := http.NewRequest("GET", srv.URL+"/api/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(req.Context(), 3*time.Second)
	defer cancel()

	resp, err := http.DefaultClient.Do(req.WithContext(ctx))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	buf := make([]byte, 512)
	var got strings.Builder
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			got.Write(buf[:n])
			if strings.Contains(got.String(), "peer_connected") {
				break
			}
		}
		if err != nil {
			break
		}
	}

	out := got.String()
	if !strings.Contains(out, "event: peer_connected") {
		t.Errorf("expected a named event frame, got:\n%s", out)
	}
	if !strings.Contains(out, `"peer":"abc"`) {
		t.Errorf("expected the data payload, got:\n%s", out)
	}
}

// A chain that exists and fails verification is a successful query with a
// negative answer, so it must be 200. Reporting 500 would make every tamper
// detection look like a server fault to monitoring.
func TestAuditVerifyHandlerTamperedReturns200(t *testing.T) {
	pubKey := [32]byte{1, 2, 3}
	api := NewAPI(pubKey)

	al := security.NewAuditLog()
	pid, _, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	if err := al.Log(security.AuditEvent{Type: security.AuditConnection, PeerID: pid, Source: "tcp"}); err != nil {
		t.Fatal(err)
	}
	api.SetAuditLog(al)

	// Untampered first: 200 and verified.
	h := NewHandler(api)
	w := httptest.NewRecorder()
	h.handleAuditVerify(w, httptest.NewRequest("GET", "/api/audit-log/verify", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("clean chain: expected 200, got %d", w.Code)
	}
	var clean map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&clean); err != nil {
		t.Fatal(err)
	}
	if clean["verified"] != true || clean["integrity"] != string(AuditStateVerified) {
		t.Fatalf("clean chain: got verified=%v integrity=%v", clean["verified"], clean["integrity"])
	}

	// Now corrupt the persisted chain. UnmarshalEntries trusts the stored
	// per-entry hashes rather than recomputing them, so rewriting the final
	// 32 bytes (the last entry's recorded hash) models a tampered or corrupted
	// store without needing access to unexported fields.
	data, err := al.MarshalEntries()
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < 32 {
		t.Fatalf("marshalled audit log too short: %d bytes", len(data))
	}
	tail := data[len(data)-32:]
	tail[0] ^= 0xff
	if err := al.UnmarshalEntries(data); err != nil {
		t.Fatal(err)
	}
	if err := al.VerifyIntegrity(); err == nil {
		t.Fatal("test setup failed: expected the corrupted chain to fail verification")
	}

	w2 := httptest.NewRecorder()
	h.handleAuditVerify(w2, httptest.NewRequest("GET", "/api/audit-log/verify", nil))
	if w2.Code != http.StatusOK {
		t.Fatalf("tampered chain: expected 200, got %d", w2.Code)
	}
	var tampered map[string]interface{}
	if err := json.NewDecoder(w2.Body).Decode(&tampered); err != nil {
		t.Fatal(err)
	}
	if tampered["verified"] != false {
		t.Error("expected verified=false on a tampered chain")
	}
	if tampered["integrity"] != string(AuditStateTampered) {
		t.Errorf("expected integrity=%q, got %v", AuditStateTampered, tampered["integrity"])
	}
}

func TestDHTTableHandlerNoDiscovery(t *testing.T) {
	pubKey := [32]byte{1, 2, 3}
	api := NewAPI(pubKey)
	h := NewHandler(api)

	req := httptest.NewRequest("GET", "/api/dht/table", nil)
	w := httptest.NewRecorder()
	h.handleDHTTable(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
}
