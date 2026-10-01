package gui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ram1234598766-dotcom/Local-WEB/pkg/services/docs"
)

func newDocsAPI(t *testing.T) (*NodeAPI, *docs.Service) {
	t.Helper()
	svc := docs.NewService(docs.ServiceConfig{NodeID: "test-node"})
	if svc == nil {
		t.Fatal("docs.NewService returned nil")
	}
	api := NewAPI([32]byte{7})
	api.SetDocsService(svc)
	return api, svc
}

// Documents previously returned a hardcoded "doc-001 Getting Started" row for
// every node. It must now reflect the real service.

func TestDocumentsEmptyWithoutService(t *testing.T) {
	api := NewAPI([32]byte{1})

	got, err := api.Documents()
	if err != nil {
		t.Fatalf("Documents: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected no documents without a docs service, got %+v", got)
	}
}

func TestDocumentsReflectRealService(t *testing.T) {
	api, svc := newDocsAPI(t)

	svc.CreateDocument("alpha", "Alpha")
	svc.CreateDocument("beta", "Beta")

	got, err := api.Documents()
	if err != nil {
		t.Fatalf("Documents: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 documents, got %d: %+v", len(got), got)
	}
	names := map[string]bool{}
	for _, d := range got {
		names[d.Name] = true
		if d.LastSync == "" {
			t.Errorf("document %q has no last_sync", d.ID)
		}
	}
	if !names["Alpha"] || !names["Beta"] {
		t.Errorf("expected the real document titles, got %v", names)
	}
}

func TestSaveDocAppliesThroughCRDT(t *testing.T) {
	api, svc := newDocsAPI(t)
	svc.CreateDocument("doc1", "Doc One")

	if err := api.SaveDoc("doc1", "alpha\nbeta\ngamma"); err != nil {
		t.Fatalf("SaveDoc: %v", err)
	}

	// The text must be readable back from the CRDT, which proves the save went
	// through the service rather than a side-channel string map.
	text, found := api.DocContent("doc1")
	if !found {
		t.Fatal("document not found after save")
	}
	if !strings.Contains(text, "alpha") || !strings.Contains(text, "gamma") {
		t.Errorf("saved content missing from CRDT text: %q", text)
	}
}

func TestSaveDocOverwritesPreviousContent(t *testing.T) {
	api, svc := newDocsAPI(t)
	svc.CreateDocument("doc1", "Doc One")

	if err := api.SaveDoc("doc1", "first version"); err != nil {
		t.Fatalf("first SaveDoc: %v", err)
	}
	if err := api.SaveDoc("doc1", "second version"); err != nil {
		t.Fatalf("second SaveDoc: %v", err)
	}

	text, _ := api.DocContent("doc1")
	if strings.Contains(text, "first version") {
		t.Errorf("stale content survived the overwrite: %q", text)
	}
	if !strings.Contains(text, "second version") {
		t.Errorf("expected the new content, got %q", text)
	}
}

func TestSaveDocRejectsUnknownDocument(t *testing.T) {
	api, _ := newDocsAPI(t)

	if err := api.SaveDoc("missing", "text"); err == nil {
		t.Error("expected an error saving an unknown document")
	}
}

func TestSaveDocRejectsOversizedContent(t *testing.T) {
	api, svc := newDocsAPI(t)
	svc.CreateDocument("doc1", "Doc One")

	huge := strings.Repeat("x", maxDocBytes+1)
	if err := api.SaveDoc("doc1", huge); err == nil {
		t.Error("expected oversized content to be rejected")
	}
}

// --- HTTP routes ---

func TestDocsRoutesAreRegistered(t *testing.T) {
	api, svc := newDocsAPI(t)
	h := NewHandler(api)
	svc.CreateDocument("doc1", "Doc One")

	// create
	rec := httptest.NewRecorder()
	h.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/docs/create",
		strings.NewReader(`{"id":"doc2","title":"Two"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/docs/create = %d, body %s", rec.Code, rec.Body.String())
	}

	// save
	rec = httptest.NewRecorder()
	h.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/docs/save/doc1",
		strings.NewReader(`{"content":"hello from the panel"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/docs/save/doc1 = %d, body %s", rec.Code, rec.Body.String())
	}

	// autosave
	rec = httptest.NewRecorder()
	h.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/docs/autosave/doc1",
		strings.NewReader(`{"content":"autosaved"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/docs/autosave/doc1 = %d, body %s", rec.Code, rec.Body.String())
	}

	// comments GET then POST then GET
	rec = httptest.NewRecorder()
	h.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/docs/comments/doc1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/docs/comments/doc1 = %d", rec.Code)
	}
	var list []CommentResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("comments GET is not a JSON array: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("expected an empty thread, got %d", len(list))
	}

	rec = httptest.NewRecorder()
	h.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/docs/comments/doc1",
		strings.NewReader(`{"author":"alice","text":"looks good"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/docs/comments/doc1 = %d, body %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	h.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/docs/comments/doc1", nil))
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("comments GET after POST: %v", err)
	}
	if len(list) != 1 || list[0].Author != "alice" || list[0].Text != "looks good" {
		t.Errorf("comment not stored correctly: %+v", list)
	}
}

// app.js posts {"name": ...} to /api/docs/create, while the docs service calls
// the field Title. If the handler only reads "title" the document is created
// with an empty title and the Docs list shows a blank row.
func TestDocsCreateAcceptsNameAndTitle(t *testing.T) {
	api, _ := newDocsAPI(t)
	h := NewHandler(api)

	for _, body := range []string{`{"id":"d1","name":"Named via name"}`, `{"id":"d2","title":"Named via title"}`} {
		rec := httptest.NewRecorder()
		h.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/docs/create", strings.NewReader(body)))
		if rec.Code != http.StatusOK {
			t.Fatalf("create %s = %d, body %s", body, rec.Code, rec.Body.String())
		}
	}

	list, err := api.Documents()
	if err != nil {
		t.Fatalf("Documents: %v", err)
	}
	titles := map[string]string{}
	for _, d := range list {
		titles[d.ID] = d.Name
	}
	if titles["d1"] != "Named via name" {
		t.Errorf(`d1 title = %q, want "Named via name"; app.js sends the "name" field`, titles["d1"])
	}
	if titles["d2"] != "Named via title" {
		t.Errorf("d2 title = %q, want Named via title", titles["d2"])
	}
}

func TestDocsRoutesValidateInput(t *testing.T) {
	api, svc := newDocsAPI(t)
	h := NewHandler(api)
	svc.CreateDocument("doc1", "Doc One")

	// malformed JSON
	rec := httptest.NewRecorder()
	h.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/docs/save/doc1",
		strings.NewReader(`not json`)))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("malformed save body = %d, want 400", rec.Code)
	}

	// missing doc id in the path
	rec = httptest.NewRecorder()
	h.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/docs/save/",
		strings.NewReader(`{"content":"x"}`)))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("save without a doc id = %d, want 400", rec.Code)
	}

	// empty comment text
	rec = httptest.NewRecorder()
	h.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/docs/comments/doc1",
		strings.NewReader(`{"author":"a","text":"   "}`)))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("empty comment = %d, want 400", rec.Code)
	}

	// wrong method
	rec = httptest.NewRecorder()
	h.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/docs/save/doc1", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("DELETE /api/docs/save/doc1 = %d, want 405", rec.Code)
	}
}

func TestDocsContent404ForUnknownDocument(t *testing.T) {
	api, _ := newDocsAPI(t)
	h := NewHandler(api)

	rec := httptest.NewRecorder()
	h.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/docs/content/nope", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown document content = %d, want 404", rec.Code)
	}
}
