package gui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The editor route calls /api/docs/documents/{id} and /api/docs/presence/{id}.
// Both were unregistered, so opening a document produced two 404s and the editor
// rendered an empty document even though the CRDT held the text.

func TestDocReturnsLiveCRDTState(t *testing.T) {
	api, svc := newDocsAPI(t)
	svc.CreateDocument("d1", "Live Doc")
	if err := api.SaveDoc("d1", "crdt backed content"); err != nil {
		t.Fatalf("SaveDoc: %v", err)
	}

	doc, ok := api.Doc("d1")
	if !ok {
		t.Fatal("Doc: not found")
	}
	if doc.Content != "crdt backed content" {
		t.Errorf("content = %q, want the CRDT text", doc.Content)
	}
	if doc.Title != "Live Doc" {
		t.Errorf("title = %q, want Live Doc", doc.Title)
	}
	if doc.Version == 0 {
		t.Error("expected a non-zero version from the CRDT")
	}
}

func TestDocPresenceAlwaysReturnsUsersArray(t *testing.T) {
	api, svc := newDocsAPI(t)
	svc.CreateDocument("d1", "Doc")

	got := api.DocPresence("d1")
	if got.Users == nil {
		t.Error("users must be an empty array, not nil, or the editor bar breaks")
	}
}

func TestDocsEditorRoutesRegistered(t *testing.T) {
	api, svc := newDocsAPI(t)
	svc.CreateDocument("d1", "Editor Doc")
	if err := api.SaveDoc("d1", "editor content"); err != nil {
		t.Fatalf("SaveDoc: %v", err)
	}
	h := NewHandler(api)

	rec := httptest.NewRecorder()
	h.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/docs/documents/d1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/docs/documents/d1 = %d", rec.Code)
	}
	var doc DocResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if doc.Content != "editor content" {
		t.Errorf("content = %q", doc.Content)
	}

	rec = httptest.NewRecorder()
	h.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/docs/presence/d1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/docs/presence/d1 = %d", rec.Code)
	}
	var presence map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &presence); err != nil {
		t.Fatalf("decode presence: %v", err)
	}
	if _, ok := presence["users"]; !ok {
		t.Errorf("presence payload must carry a users key, got %v", presence)
	}
}

func TestDocsEditorRoutes404ForUnknownDoc(t *testing.T) {
	api, _ := newDocsAPI(t)
	h := NewHandler(api)

	for _, p := range []string{"/api/docs/documents/nope", "/api/docs/content/nope"} {
		rec := httptest.NewRecorder()
		h.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s = %d, want 404", p, rec.Code)
		}
	}

	// Presence for an unknown document is an empty list, not a 404: the editor
	// fetches it unconditionally for any document id.
	rec := httptest.NewRecorder()
	h.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/docs/presence/nope", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("presence for unknown doc = %d, want 200", rec.Code)
	}
}
