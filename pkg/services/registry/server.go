package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// HTTPServer serves the registry HTTP API.
type HTTPServer struct {
	mu        sync.RWMutex
	registry  Registry
	packages  map[string]*PackageMeta
	started   time.Time
	server    *http.Server
	url       string
}

// ServerConfig configures the registry HTTP server.
type ServerConfig struct {
	Registry Registry
	Addr     string
}

// NewHTTPServer creates a new registry HTTP server.
func NewHTTPServer(cfg ServerConfig) *HTTPServer {
	if cfg.Registry == nil {
		cfg.Registry = NewMemoryRegistry()
	}
	s := &HTTPServer{
		registry: cfg.Registry,
		packages: make(map[string]*PackageMeta),
		started:  time.Now(),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/api/v1/packages", s.handlePackages)
	mux.HandleFunc("/api/v1/packages/", s.handlePackageByID)
	mux.HandleFunc("/api/v1/search", s.handleSearch)

	s.server = &http.Server{
		Addr:    cfg.Addr,
		Handler: loggingMiddleware(mux),
	}
	return s
}

// Start begins serving on the configured address.
func (s *HTTPServer) Start() error {
	go func() {
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Printf("registry server error: %v\n", err)
		}
	}()
	s.mu.Lock()
	s.url = "http://" + s.server.Addr
	s.mu.Unlock()
	return nil
}

// URL returns the server's listening URL.
func (s *HTTPServer) URL() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.url
}

// Stop gracefully shuts down the server.
func (s *HTTPServer) Stop(ctx context.Context) error {
	if s.server == nil {
		return nil
	}
	return s.server.Shutdown(ctx)
}

// handleHealth responds to health checks.
func (s *HTTPServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handlePackages handles POST (publish) and GET (list) on /api/v1/packages.
func (s *HTTPServer) handlePackages(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.listPackages(w, r)
	case http.MethodPost:
		s.publishPackage(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handlePackageByID handles GET and DELETE for a specific package.
func (s *HTTPServer) handlePackageByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/packages/")
	if id == "" {
		http.Error(w, "missing package ID", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.getPackage(w, r, id)
	case http.MethodDelete:
		s.deletePackage(w, r, id)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleSearch handles package search queries.
func (s *HTTPServer) handleSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	q := SearchQuery{
		Query:  r.URL.Query().Get("q"),
		Author: r.URL.Query().Get("author"),
		Limit:  atoiDefault(r.URL.Query().Get("limit"), DefaultSearchLimit),
		Offset: atoiDefault(r.URL.Query().Get("offset"), 0),
	}
	if q.Limit <= 0 || q.Limit > MaxSearchLimit {
		q.Limit = DefaultSearchLimit
	}
	result, err := s.registry.Search(q)
	if err != nil {
		http.Error(w, fmt.Sprintf("search failed: %v", err), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *HTTPServer) listPackages(w http.ResponseWriter, r *http.Request) {
	packages, err := s.registry.List()
	if err != nil {
		http.Error(w, fmt.Sprintf("list failed: %v", err), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, packages)
}

func (s *HTTPServer) publishPackage(w http.ResponseWriter, r *http.Request) {
	var req PublishRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("bad request: %v", err), http.StatusBadRequest)
		return
	}
	if req.PackageData == nil || len(req.Signature) == 0 {
		http.Error(w, "package data and signature required", http.StatusBadRequest)
		return
	}
	if len(req.PublisherPubKey) != Ed25519PublicKeySize {
		http.Error(w, "invalid publisher public key length", http.StatusBadRequest)
		return
	}

	combined := make([]byte, len(req.PackageData)+len(req.Signature))
	copy(combined, req.PackageData)
	copy(combined[len(req.PackageData):], req.Signature)

	pkg, sigOut, err := DecodeLWPKG(combined)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid package: %v", err), http.StatusBadRequest)
		return
	}
	_ = sigOut
	if err := VerifyIntegrity(pkg); err != nil {
		http.Error(w, fmt.Sprintf("integrity check failed: %v", err), http.StatusBadRequest)
		return
	}

	signer := &CryptoSigner{}
	if !signer.Verify(req.PublisherPubKey, combined[:len(combined)-Ed25519SignatureSize], req.Signature) {
		http.Error(w, "signature verification failed", http.StatusUnauthorized)
		return
	}

	id, err := s.registry.Publish(pkg, req.PublisherPubKey, [32]byte{})
	if err != nil {
		if err == ErrPackageExists {
			http.Error(w, "package already exists", http.StatusConflict)
			return
		}
		http.Error(w, fmt.Sprintf("publish failed: %v", err), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

func (s *HTTPServer) getPackage(w http.ResponseWriter, r *http.Request, id string) {
	meta, err := s.registry.Get(id)
	if err != nil {
		if err == ErrPackageNotFound {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		http.Error(w, fmt.Sprintf("get failed: %v", err), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, meta)
}

func (s *HTTPServer) deletePackage(w http.ResponseWriter, r *http.Request, id string) {
	if err := s.registry.Delete(id); err != nil {
		if err == ErrPackageNotFound {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		http.Error(w, fmt.Sprintf("delete failed: %v", err), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// PublishRequest is the JSON body for publishing a package.
type PublishRequest struct {
	PackageData     []byte
	Signature       []byte
	PublisherPubKey [32]byte
}

// loggingMiddleware wraps an HTTP handler with request logging.
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		_ = time.Since(start).Milliseconds()
	})
}

// writeJSON writes a JSON response.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// atoiDefault converts s to int, falling back to def on parse failure.
func atoiDefault(s string, def int) int {
	var n int
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil {
		return def
	}
	return n
}

// ServeHTTP lets HTTPServer satisfy http.Handler for embedding.
func (s *HTTPServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if s.server != nil && s.server.Handler != nil {
		s.server.Handler.ServeHTTP(w, r)
	}
}
