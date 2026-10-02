package gui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/services/files"
)

// The Transfers panel used to return a hardcoded empty array with a comment
// saying the daemon did not drive a transfer loop. These tests pin it to real
// sync-engine state instead, so it cannot silently go back to fabricating.

func newTestEngine(t *testing.T) (files.SyncEngine, files.BlockStore) {
	t.Helper()
	store := files.NewMemoryStore()
	t.Cleanup(func() { _ = store.Close() })
	return files.NewSyncEngine(store, nil, [32]byte{1, 2, 3}, time.Minute), store
}

func TestTransfersEmptyWithoutEngine(t *testing.T) {
	api := NewAPI([32]byte{1})
	got := api.Transfers()
	if got == nil {
		t.Fatal("Transfers must return an empty array, not nil")
	}
	if len(got) != 0 {
		t.Fatalf("expected no transfers without an engine, got %d", len(got))
	}
}

func TestTransfersReportsRealPeerProgress(t *testing.T) {
	api := NewAPI([32]byte{1})
	engine, store := newTestEngine(t)

	ctx := context.Background()
	payload := []byte("content the peer holds")
	c, err := files.CidFor(payload)
	if err != nil {
		t.Fatalf("CidFor: %v", err)
	}
	block := &files.Block{Data: payload, CID: c}
	if err := store.Put(ctx, block); err != nil {
		t.Fatalf("seed: %v", err)
	}

	engine.RecordPeerHave([32]byte{9, 9, 9}, []cid.Cid{block.CID})
	api.SetSyncEngine(engine)

	got := api.Transfers()
	if len(got) != 1 {
		t.Fatalf("expected one transfer row, got %d", len(got))
	}
	row := got[0]
	if row.BlocksTotal != 1 {
		t.Errorf("BlocksTotal = %d, want 1", row.BlocksTotal)
	}
	if row.TotalSize != int64(len(payload)) {
		t.Errorf("TotalSize = %d, want %d", row.TotalSize, len(payload))
	}
	if row.PeerName == "" {
		t.Error("PeerName should name the peer, not be blank")
	}
	if row.Status == "" {
		t.Error("Status should be one of idle/in-flight/complete/failed")
	}
}

func TestFilesTransfersEndpointReturnsRealShape(t *testing.T) {
	api := NewAPI([32]byte{1})
	engine, _ := newTestEngine(t)
	engine.RecordPeerHave([32]byte{7, 7}, nil)
	api.SetSyncEngine(engine)

	h := NewHandler(api)
	rec := httptest.NewRecorder()
	h.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/files/transfers", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var rows []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
		t.Fatalf("response is not a JSON array: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected one row, got %d", len(rows))
	}
	// The SPA reads these fields, so they have to be present even at zero.
	for _, field := range []string{"name", "status", "peer_name", "total_size", "blocks_total", "blocks_done"} {
		if _, ok := rows[0][field]; !ok {
			t.Errorf("field %q missing from the transfer row", field)
		}
	}
}
