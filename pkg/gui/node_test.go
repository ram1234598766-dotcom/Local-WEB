package gui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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
