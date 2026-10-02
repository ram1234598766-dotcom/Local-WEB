package gui

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

// TestFilesUploadStoresTheFile is the test for the endpoint that replaced the
// SPA's simulated upload, which advanced a counter on a timer and toasted
// success without sending anything.
func TestFilesUploadStoresTheFile(t *testing.T) {
	api := NewAPI([32]byte{1})
	api.SetFileStore(files.NewFileMetadataStore())
	h := NewHandler(api)

	body := []byte("the bytes the user actually uploaded")
	req := httptest.NewRequest(http.MethodPost, "/api/files/upload?name=notes.txt", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body.String())
	}

	list, err := api.Files()
	if err != nil {
		t.Fatalf("Files: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("store holds %d files, want 1", len(list))
	}
	if list[0].Name != "notes.txt" {
		t.Errorf("stored name = %q, want notes.txt", list[0].Name)
	}
	if list[0].Size != int64(len(body)) {
		t.Errorf("stored size = %d, want %d", list[0].Size, len(body))
	}
}

func TestFilesUploadRejectsBadInput(t *testing.T) {
	api := NewAPI([32]byte{1})
	api.SetFileStore(files.NewFileMetadataStore())
	h := NewHandler(api)

	cases := []struct {
		name string
		url  string
		body string
		want int
	}{
		{"no name", "/api/files/upload", "data", http.StatusBadRequest},
		{"empty body", "/api/files/upload?name=a.txt", "", http.StatusBadRequest},
		{"path traversal is flattened", "/api/files/upload?name=../../etc/passwd", "data", http.StatusCreated},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tc.url, strings.NewReader(tc.body))
			rec := httptest.NewRecorder()
			h.mux.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d", rec.Code, tc.want)
			}
		})
	}
}

// TestPackagesAreNotHardcoded guards against the panel returning a single
// invented row again regardless of what was published.
func TestPackagesAreNotHardcoded(t *testing.T) {
	api := NewAPI([32]byte{1})
	got, err := api.Packages()
	if err != nil {
		t.Fatalf("Packages: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("with no registry wired, Packages should be empty, got %d rows: %+v", len(got), got)
	}
}
