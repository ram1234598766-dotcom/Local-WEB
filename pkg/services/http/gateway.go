package http

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"
)

type Gateway struct {
	mu        sync.RWMutex
	sites     map[string]*Site
	server    *http.Server
	listening bool
}

type Site struct {
	Name    string
	Root    string
	Handler http.Handler
}

func NewGateway() *Gateway {
	return &Gateway{
		sites: make(map[string]*Site),
	}
}

func (g *Gateway) RegisterSite(name, root string, handler http.Handler) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, exists := g.sites[name]; exists {
		return fmt.Errorf("site %q already registered", name)
	}
	g.sites[name] = &Site{Name: name, Root: root, Handler: handler}
	return nil
}

func (g *Gateway) Start(ctx context.Context, addr string) error {
	g.mu.Lock()
	if g.listening {
		g.mu.Unlock()
		return nil
	}
	g.listening = true
	g.mu.Unlock()

	mux := http.NewServeMux()
	g.mu.RLock()
	for name, site := range g.sites {
		prefix := "/" + name + "/"
		mux.Handle(prefix, http.StripPrefix(prefix, site.Handler))
	}
	g.mu.RUnlock()

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	g.server = &http.Server{
		Addr:    addr,
		Handler: loggingMiddleware(mux),
	}

	log.Printf("http gateway listening on %s", addr)
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		g.server.Shutdown(shutdownCtx)
	}()

	return g.server.ListenAndServe()
}

func (g *Gateway) Stop() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.server == nil {
		return nil
	}
	return g.server.Close()
}

func (g *Gateway) Close() error {
	return g.Stop()
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start))
	})
}

var _ io.Closer = (*Gateway)(nil)
