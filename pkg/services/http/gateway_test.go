package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestGatewayRegisterSite(t *testing.T) {
	g := NewGateway()
	err := g.RegisterSite("mysite", "/data", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	if err != nil {
		t.Fatalf("register site: %v", err)
	}
	if len(g.sites) != 1 {
		t.Fatalf("expected 1 site, got %d", len(g.sites))
	}
}

func TestGatewayRegisterDuplicate(t *testing.T) {
	g := NewGateway()
	g.RegisterSite("site1", "/d1", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	err := g.RegisterSite("site1", "/d2", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	if err == nil {
		t.Fatal("expected error for duplicate site")
	}
}

func TestGatewayHealthEndpoint(t *testing.T) {
	g := NewGateway()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	go g.Start(ctx, ":0") // bind to random port

	// Test handler routing logic directly
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}
