package gui

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ipfs/go-cid"

	"github.com/ram1234598766-dotcom/Local-WEB/pkg/services/files"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/services/registry"
)

// The SPA calls /api/files/list and /api/files/transfers on every Files render
// and /api/registry/installed on every Registry render. All three were
// unregistered, so those screens always produced a 404 in the browser.

func TestFilesListRouteRegistered(t *testing.T) {
	api := NewAPI([32]byte{1})
	h := NewHandler(api)

	req := httptest.NewRequest(http.MethodGet, "/api/files/list", nil)
	rec := httptest.NewRecorder()
	h.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for /api/files/list, got %d", rec.Code)
	}
	var got []FileResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("response is not a JSON array: %v", err)
	}
	if got == nil {
		t.Error("expected an empty array rather than JSON null")
	}
}

func TestFilesTransfersRouteRegistered(t *testing.T) {
	api := NewAPI([32]byte{1})
	h := NewHandler(api)

	req := httptest.NewRequest(http.MethodGet, "/api/files/transfers", nil)
	rec := httptest.NewRecorder()
	h.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for /api/files/transfers, got %d", rec.Code)
	}
	var got []TransferResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("response is not a JSON array: %v", err)
	}
	if got == nil {
		t.Error("expected an empty array rather than JSON null")
	}
}

func TestRegistryInstalledRouteRegistered(t *testing.T) {
	api := NewAPI([32]byte{1})
	h := NewHandler(api)

	req := httptest.NewRequest(http.MethodGet, "/api/registry/installed", nil)
	rec := httptest.NewRecorder()
	h.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for /api/registry/installed, got %d", rec.Code)
	}
	var got []registry.PackageMeta
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("response is not a JSON array: %v", err)
	}
	if got == nil {
		t.Error("expected an empty array rather than JSON null")
	}
}

// The JSON tags matter: app.js reads f.mime_type and t.elapsed_ms, so a
// differently-named field silently renders "undefined" in the panel.
func TestFileResponseFieldNamesMatchSPA(t *testing.T) {
	api := NewAPI([32]byte{1})

	data := []byte("hello world\n")
	sum := sha256.Sum256(data)
	store := files.NewFileMetadataStore()
	if err := store.PutFile(context.Background(), &files.FileMeta{
		CID:      cid.NewCidV1(cid.Raw, sum[:]),
		Name:     "notes.txt",
		Size:     int64(len(data)),
		MimeType: "text/plain",
		Created:  time.Now(),
		Modified: time.Now(),
	}, data); err != nil {
		t.Fatalf("PutFile: %v", err)
	}
	api.SetFileStore(store)

	list, err := api.Files()
	if err != nil {
		t.Fatalf("Files: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 file, got %d", len(list))
	}

	raw, err := json.Marshal(list[0])
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{"id", "name", "size", "mime_type", "modified"} {
		if _, ok := fields[key]; !ok {
			t.Errorf("missing JSON field %q required by app.js", key)
		}
	}
	if fields["mime_type"] != "text/plain" {
		t.Errorf("mime_type = %v, want text/plain", fields["mime_type"])
	}
	if list[0].ID == "" {
		t.Error("expected a non-empty id derived from the CID")
	}
}

func TestFilesEmptyWhenStoreNotWired(t *testing.T) {
	api := NewAPI([32]byte{1})

	list, err := api.Files()
	if err != nil {
		t.Fatalf("an unwired store should not be an error, got %v", err)
	}
	if len(list) != 0 {
		t.Errorf("expected no files without a store, got %d", len(list))
	}
}

func TestRegistryInstalledReflectsWiring(t *testing.T) {
	api := NewAPI([32]byte{1})

	if got := api.RegistryInstalled(); got == nil {
		t.Error("expected an empty slice, not nil")
	}

	api.SetRegistryInstalled([]registry.PackageMeta{{Name: "demo", Version: "1.0.0"}})
	got := api.RegistryInstalled()
	if len(got) != 1 || got[0].Name != "demo" {
		t.Errorf("expected the wired package, got %+v", got)
	}
}

func TestFileRoutesRejectNonGET(t *testing.T) {
	api := NewAPI([32]byte{1})
	h := NewHandler(api)

	for _, path := range []string{"/api/files/list", "/api/files/transfers", "/api/registry/installed"} {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		rec := httptest.NewRecorder()
		h.mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: expected 405 for POST, got %d", path, rec.Code)
		}
	}
}
