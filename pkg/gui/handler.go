package gui

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/rs/zerolog/log"
)

//go:embed static/index.html
var indexHTML []byte

//go:embed static/styles.css
var stylesCSS []byte

//go:embed static/app.js
var appJS []byte

type Handler struct {
	api    *NodeAPI
	mux    *http.ServeMux
	server *http.Server
}

func NewHandler(api *NodeAPI) *Handler {
	h := &Handler{api: api}
	mux := http.NewServeMux()

	mux.HandleFunc("/", h.handleIndex)
	mux.HandleFunc("/static/styles.css", h.handleStyles)
	mux.HandleFunc("/static/app.js", h.handleAppJS)
	mux.HandleFunc("/api/status", h.handleStatus)
	mux.HandleFunc("/api/peers", h.handlePeers)
	mux.HandleFunc("/api/dht/table", h.handleDHTTable)
	mux.HandleFunc("/api/audit-log", h.handleAuditLog)
	mux.HandleFunc("/api/audit-log/verify", h.handleAuditVerify)
	mux.HandleFunc("/api/crdt/sync-status", h.handleSyncStatus)
	mux.HandleFunc("/api/services/health", h.handleServicesHealth)
	mux.HandleFunc("/api/dns/records", h.handleDNSRecords)
	mux.HandleFunc("/api/http/sites", h.handleHTTPSites)
	mux.HandleFunc("/api/email/messages", h.handleEmailMessages)
	mux.HandleFunc("/api/messaging/messages", h.handleMessages)
	mux.HandleFunc("/api/docs/documents", h.handleDocuments)
	mux.HandleFunc("/api/registry/packages", h.handlePackages)
	mux.HandleFunc("/api/events", h.handleEvents)
	mux.HandleFunc("/healthz", h.handleHealthz)
	mux.HandleFunc("/readyz", h.handleReadyz)

	h.mux = mux
	return h
}

func (h *Handler) ListenAndServe(addr string) error {
	h.server = &http.Server{
		Addr:              addr,
		Handler:           h.mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Info().Str("addr", addr).Msg("GUI API listening")
	return h.server.ListenAndServe()
}

func (h *Handler) Shutdown(ctx context.Context) error {
	if h.server == nil {
		return nil
	}
	return h.server.Shutdown(ctx)
}

func (h *Handler) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(indexHTML)
}

func (h *Handler) handleStyles(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Write(stylesCSS)
}

func (h *Handler) handleAppJS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Write(appJS)
}

func (h *Handler) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(h.api.Status())
}

func (h *Handler) handlePeers(w http.ResponseWriter, r *http.Request) {
	peers, err := h.api.Peers()
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(peers)
}

func (h *Handler) handleDHTTable(w http.ResponseWriter, r *http.Request) {
	nodes, err := h.api.DHTNodes()
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"nodes": nodes,
		"count": len(nodes),
	})
}

func (h *Handler) handleAuditLog(w http.ResponseWriter, r *http.Request) {
	events, err := h.api.AuditLog()
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(events)
}

func (h *Handler) handleAuditVerify(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	verified := h.api.AuditLogVerified()
	if !verified {
		w.WriteHeader(http.StatusInternalServerError)
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"verified":  verified,
		"timestamp": time.Now().Format(time.RFC3339),
	})
}

func (h *Handler) handleSyncStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(h.api.SyncStatus())
}

func (h *Handler) handleServicesHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"services": map[string]bool{
			"dns":       true,
			"http":      true,
			"email":     true,
			"messaging": true,
			"files":     true,
			"docs":      true,
			"registry":  true,
			"voice":     true,
			"vpn":       true,
		},
	})
}

func (h *Handler) handleDNSRecords(w http.ResponseWriter, r *http.Request) {
	records, err := h.api.DNSRecords()
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(records)
}

func (h *Handler) handleHTTPSites(w http.ResponseWriter, r *http.Request) {
	sites, err := h.api.HTTPSites()
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(sites)
}

func (h *Handler) handleEmailMessages(w http.ResponseWriter, r *http.Request) {
	msgs, err := h.api.EmailMessages()
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(msgs)
}

func (h *Handler) handleMessages(w http.ResponseWriter, r *http.Request) {
	msgs, err := h.api.Messages()
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(msgs)
}

func (h *Handler) handleDocuments(w http.ResponseWriter, r *http.Request) {
	docs, err := h.api.Documents()
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(docs)
}

func (h *Handler) handlePackages(w http.ResponseWriter, r *http.Request) {
	pkgs, err := h.api.Packages()
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(pkgs)
}

func (h *Handler) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (h *Handler) handleReadyz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	status := h.api.Status()
	if status.NodeID == "" {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{"status": "not ready"})
		return
	}
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ready"})
}

func (h *Handler) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher.Flush()

	ch := h.api.Subscribe()
	defer h.api.Unsubscribe(ch)

	notify := r.Context().Done()
	for {
		select {
		case evt, ok := <-ch:
			if !ok {
				return
			}
			data, err := json.Marshal(evt)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", evt.Type, data)
			flusher.Flush()
		case <-notify:
			return
		}
	}
}
