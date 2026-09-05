package registry

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ram1234598766-dotcom/Local-WEB/pkg/crypto"
	"github.com/stretchr/testify/require"
)

func TestHTTPServerHealth(t *testing.T) {
	srv := newTestServer(t)
	defer srv.server.Close()

	resp := get(t, srv, "/health")
	require.Equal(t, http.StatusOK, resp.Code)
	require.Contains(t, resp.Body.String(), "ok")
}

func TestHTTPServerListPackages(t *testing.T) {
	srv := newTestServer(t)
	defer srv.server.Close()

	pub, priv, _ := crypto.GenerateKeyPair()
	pkg := testPackage("list-app", "1.0.0", "author")
	_, _ = srv.registry.Publish(pkg, pub, priv)

	resp := get(t, srv, "/api/v1/packages")
	require.Equal(t, http.StatusOK, resp.Code)

	var pkgs []PackageMeta
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &pkgs))
	require.Len(t, pkgs, 1)
	require.Equal(t, "list-app", pkgs[0].Name)
}

func TestHTTPServerGetPackage(t *testing.T) {
	srv := newTestServer(t)
	defer srv.server.Close()

	pub, priv, _ := crypto.GenerateKeyPair()
	pkg := testPackage("get-app", "2.0.0", "author2")
	id, _ := srv.registry.Publish(pkg, pub, priv)

	resp := get(t, srv, "/api/v1/packages/"+id)
	require.Equal(t, http.StatusOK, resp.Code)

	var meta PackageMeta
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &meta))
	require.Equal(t, "get-app", meta.Name)
	require.Equal(t, "2.0.0", meta.Version)
}

func TestHTTPServerGetPackageNotFound(t *testing.T) {
	srv := newTestServer(t)
	defer srv.server.Close()

	resp := get(t, srv, "/api/v1/packages/nonexistent")
	require.Equal(t, http.StatusNotFound, resp.Code)
}

func TestHTTPServerDeletePackage(t *testing.T) {
	srv := newTestServer(t)
	defer srv.server.Close()

	pub, priv, _ := crypto.GenerateKeyPair()
	pkg := testPackage("del-app", "1.0.0", "author")
	id, _ := srv.registry.Publish(pkg, pub, priv)

	resp := del(t, srv, "/api/v1/packages/"+id)
	require.Equal(t, http.StatusNoContent, resp.Code)

	resp2 := get(t, srv, "/api/v1/packages/"+id)
	require.Equal(t, http.StatusNotFound, resp2.Code)
}

func TestHTTPServerDeleteNotFound(t *testing.T) {
	srv := newTestServer(t)
	defer srv.server.Close()

	resp := del(t, srv, "/api/v1/packages/nonexistent")
	require.Equal(t, http.StatusNotFound, resp.Code)
}

func TestHTTPServerSearch(t *testing.T) {
	srv := newTestServer(t)
	defer srv.server.Close()

	pub, priv, _ := crypto.GenerateKeyPair()
	pkg1 := testPackage("search-app-alpha", "1.0.0", "alice")
	pkg2 := testPackage("search-app-beta", "1.0.1", "bob")
	_, _ = srv.registry.Publish(pkg1, pub, priv)
	_, _ = srv.registry.Publish(pkg2, pub, priv)

	resp := get(t, srv, "/api/v1/search?q=alpha")
	require.Equal(t, http.StatusOK, resp.Code)

	var result SearchResult
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
	require.Equal(t, 1, result.Total)
	require.Len(t, result.Packages, 1)
	require.Equal(t, "search-app-alpha", result.Packages[0].Name)
}

func TestHTTPServerSearchEmpty(t *testing.T) {
	srv := newTestServer(t)
	defer srv.server.Close()

	resp := get(t, srv, "/api/v1/search?q=nonexistent")
	require.Equal(t, http.StatusOK, resp.Code)

	var result SearchResult
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
	require.Equal(t, 0, result.Total)
}

func TestHTTPServerSearchLimit(t *testing.T) {
	srv := newTestServer(t)
	defer srv.server.Close()

	pub, priv, _ := crypto.GenerateKeyPair()
	for i := 0; i < 5; i++ {
		pkg := testPackage(fmt.Sprintf("lim-app-%d", i), "1.0.0", "author")
		_, _ = srv.registry.Publish(pkg, pub, priv)
	}

	resp := get(t, srv, "/api/v1/search?q=&limit=3")
	require.Equal(t, http.StatusOK, resp.Code)

	var result SearchResult
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
	require.Equal(t, 5, result.Total)
	require.Len(t, result.Packages, 3)
}

func TestHTTPServerPublish(t *testing.T) {
	srv := newTestServer(t)
	defer srv.server.Close()

	pub, priv, _ := crypto.GenerateKeyPair()
	pkg := testPackage("pub-app", "1.0.0", "publisher")
	data, err := EncodeLWPKG(pkg)
	require.NoError(t, err)
	sig, _ := crypto.Sign(priv, data)

	reqBody, err := json.Marshal(PublishRequest{
		PackageData:     data,
		Signature:       sig,
		PublisherPubKey: pub,
	})
	require.NoError(t, err)

	resp := post(t, srv, "/api/v1/packages", bytes.NewReader(reqBody))
	require.Equal(t, http.StatusCreated, resp.Code)

	var result map[string]string
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
	require.NotEmpty(t, result["id"])
}

func TestHTTPServerMethodNotAllowed(t *testing.T) {
	srv := newTestServer(t)
	defer srv.server.Close()

	req := httptest.NewRequest(http.MethodDelete, srv.URL()+"/api/v1/packages", nil)
	w := httptest.NewRecorder()
	srv.server.Handler.ServeHTTP(w, req)
	require.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestHTTPServerPublishBadRequest(t *testing.T) {
	srv := newTestServer(t)
	defer srv.server.Close()

	resp := post(t, srv, "/api/v1/packages", bytes.NewReader([]byte("{}")))
	require.Equal(t, http.StatusBadRequest, resp.Code)
}

func TestHTTPServerPublishInvalidPackage(t *testing.T) {
	srv := newTestServer(t)
	defer srv.server.Close()

	pub, priv, _ := crypto.GenerateKeyPair()
	sig, _ := crypto.Sign(priv, []byte("payload"))

	reqBody, _ := json.Marshal(PublishRequest{
		PackageData:     []byte("not-a-valid-lwpkg"),
		Signature:       sig,
		PublisherPubKey: pub,
	})
	resp := post(t, srv, "/api/v1/packages", bytes.NewReader(reqBody))
	require.Equal(t, http.StatusBadRequest, resp.Code)
}

func TestHTTPServerPublishInvalidKeyLength(t *testing.T) {
	srv := newTestServer(t)
	defer srv.server.Close()

	reqBody, _ := json.Marshal(PublishRequest{
		PackageData:     []byte("data"),
		Signature:       make([]byte, Ed25519SignatureSize),
		PublisherPubKey: [32]byte{1: 1},
	})
	resp := post(t, srv, "/api/v1/packages", bytes.NewReader(reqBody))
	require.Equal(t, http.StatusBadRequest, resp.Code)
}

func TestHTTPServerSearchByAuthor(t *testing.T) {
	srv := newTestServer(t)
	defer srv.server.Close()

	pub, priv, _ := crypto.GenerateKeyPair()
	pkg1 := testPackage("auth-app", "1.0.0", "alice")
	pkg2 := testPackage("auth-app-2", "1.0.0", "bob")
	_, _ = srv.registry.Publish(pkg1, pub, priv)
	_, _ = srv.registry.Publish(pkg2, pub, priv)

	resp := get(t, srv, "/api/v1/search?author=alice")
	require.Equal(t, http.StatusOK, resp.Code)

	var result SearchResult
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
	require.Equal(t, 1, result.Total)
	require.Equal(t, "alice", result.Packages[0].Author)
}

func newTestServer(t *testing.T) *HTTPServer {
	t.Helper()
	reg := NewMemoryRegistry()
	srv := NewHTTPServer(ServerConfig{Registry: reg, Addr: "127.0.0.1:0"})
	err := srv.Start()
	require.NoError(t, err)
	srv.mu.Lock()
	srv.url = "http://" + srv.server.Addr
	srv.mu.Unlock()
	return srv
}

func testPackage(name, version, author string) *LWPKG {
	return &LWPKG{
		Manifest: &Manifest{
			Name:        name,
			Version:     version,
			Description: "Test " + name,
			Author:      author,
			Entry:       "main.go",
			Size:        100,
			Checksums:   map[string]string{"main.go": sha256hex([]byte("x"))},
			Created:     timeNow(),
		},
		Files: map[string][]byte{"main.go": []byte("x")},
	}
}

func get(t *testing.T, srv *HTTPServer, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, srv.URL()+path, nil)
	w := httptest.NewRecorder()
	srv.server.Handler.ServeHTTP(w, req)
	return w
}

func post(t *testing.T, srv *HTTPServer, path string, body *bytes.Reader) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, srv.URL()+path, body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.server.Handler.ServeHTTP(w, req)
	return w
}

func del(t *testing.T, srv *HTTPServer, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodDelete, srv.URL()+path, nil)
	w := httptest.NewRecorder()
	srv.server.Handler.ServeHTTP(w, req)
	return w
}
