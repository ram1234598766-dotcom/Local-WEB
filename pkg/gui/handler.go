package gui

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/ram1234598766-dotcom/Local-WEB/pkg/services/voice"
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

	// sseHeartbeat is how often an SSE comment is written to each /api/events
	// connection. An event stream that emits nothing is indistinguishable from a
	// dead connection to a browser and gets reaped by proxies, so the stream
	// must produce periodic bytes. Documented in docs/api/WS_API.md as 30s.
	sseHeartbeat time.Duration
}

// defaultSSEHeartbeat is the interval documented for /api/events.
const defaultSSEHeartbeat = 30 * time.Second

func NewHandler(api *NodeAPI) *Handler {
	h := &Handler{api: api, sseHeartbeat: defaultSSEHeartbeat}
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
	mux.HandleFunc("/api/voice/status", h.handleVoiceStatus)
	mux.HandleFunc("/api/voice/call", h.handleVoiceCall)
	mux.HandleFunc("/api/voice/signal", h.handleVoiceSignal)
	mux.HandleFunc("/api/dns/records", h.handleDNSRecords)
	mux.HandleFunc("/api/http/sites", h.handleHTTPSites)
	mux.HandleFunc("/api/email/messages", h.handleEmailMessages)
	mux.HandleFunc("/api/messaging/messages", h.handleMessages)
	mux.HandleFunc("/api/docs/documents", h.handleDocuments)
	mux.HandleFunc("/api/registry/packages", h.handlePackages)

	// Files and Registry reads. The SPA calls these on every Files/Registry
	// render, so a missing route was a guaranteed 404 in the browser.
	mux.HandleFunc("/api/files/list", h.handleFilesList)
	mux.HandleFunc("/api/files/transfers", h.handleFilesTransfers)
	mux.HandleFunc("/api/files/upload", h.handleFilesUpload)
	mux.HandleFunc("/api/registry/installed", h.handleRegistryInstalled)

	// Docs mutations. app.js calls these on save, autosave and comment submit;
	// without routes each action 404'd while the UI still showed a success toast.
	mux.HandleFunc("/api/docs/create", h.handleDocsCreate)
	mux.HandleFunc("/api/docs/save/", h.handleDocsSave)
	mux.HandleFunc("/api/docs/autosave/", h.handleDocsAutosave)
	mux.HandleFunc("/api/docs/comments/", h.handleDocsComments)
	mux.HandleFunc("/api/docs/content/", h.handleDocsContent)
	mux.HandleFunc("/api/docs/documents/", h.handleDocsDocument)
	mux.HandleFunc("/api/docs/presence/", h.handleDocsPresence)

	// Onboarding wizard endpoints
	mux.HandleFunc("/api/onboarding/status", h.handleOnboardingStatus)
	mux.HandleFunc("/api/onboarding/qr", h.handleOnboardingQR)
	mux.HandleFunc("/api/onboarding/backup", h.handleOnboardingBackup)
	mux.HandleFunc("/api/onboarding/restore", h.handleOnboardingRestore)

	mux.HandleFunc("/api/events", h.handleEvents)
	mux.HandleFunc("/healthz", h.handleHealthz)
	mux.HandleFunc("/readyz", h.handleReadyz)

	h.mux = mux
	return h
}

// handleFilesList reports the real contents of the file store.
func (h *Handler) handleFilesList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	list, err := h.api.Files()
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(list)
}

// maxUploadBytes bounds a single upload. Without it a body of any declared
// length is read into memory.
const maxUploadBytes = 256 << 20

// handleFilesUpload stores an uploaded file through the real file store.
//
// The SPA used to "upload" by advancing a counter on a timer and toasting a
// success, having sent nothing: the file never reached disk and the message
// was false. This is the endpoint that makes the toast mean something.
func (h *Handler) handleFilesUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	name := r.URL.Query().Get("name")
	if name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	// A path is not a name: this arrives from an untrusted query parameter and
	// ends up as a map key and a stored filename.
	name = filepath.Base(filepath.Clean("/" + name))
	if name == "." || name == string(filepath.Separator) || name == "" {
		http.Error(w, "invalid name", http.StatusBadRequest)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if len(data) == 0 {
		http.Error(w, "empty upload", http.StatusBadRequest)
		return
	}

	created, err := h.api.StoreFile(r.Context(), name, data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id":   created.CID.String(),
		"name": created.Name,
		"size": created.Size,
		"cid":  created.CID.String(),
	})
}

// handleFilesTransfers reports in-flight and completed transfers per peer.
func (h *Handler) handleFilesTransfers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(h.api.Transfers())
}

// handleRegistryInstalled reports packages installed on this node.
func (h *Handler) handleRegistryInstalled(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(h.api.RegistryInstalled())
}

// docContentBody is the request shape app.js posts for save and autosave.
type docContentBody struct {
	Content string `json:"content"`
}

// docIDFromPath extracts the document id from a ".../<docID>" route.
func docIDFromPath(prefix, path string) (string, bool) {
	id := strings.TrimPrefix(path, prefix)
	if id == "" || strings.Contains(id, "/") {
		return "", false
	}
	return id, true
}

// decodeDocContent reads and bounds a content body from an untrusted request.
func decodeDocContent(w http.ResponseWriter, r *http.Request) (string, bool) {
	// http.MaxBytesReader caps the read before decoding, so an oversized body
	// cannot be buffered in full.
	r.Body = http.MaxBytesReader(w, r.Body, maxDocBytes+1024)
	var body docContentBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return "", false
	}
	if len(body.Content) > maxDocBytes {
		http.Error(w, "document too large", http.StatusRequestEntityTooLarge)
		return "", false
	}
	return body.Content, true
}

func (h *Handler) handleDocsCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// app.js posts {name}; the docs service calls the same field Title. Accept
	// both so the title the user typed actually reaches the document.
	var body struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Title string `json:"title"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	title := body.Title
	if title == "" {
		title = body.Name
	}
	if body.ID == "" {
		body.ID = fmt.Sprintf("doc-%d", time.Now().UnixNano())
	}
	id, err := h.api.CreateDoc(body.ID, title)
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"id": id, "title": title})
}

// handleDocsSave applies an explicit save through the CRDT.
func (h *Handler) handleDocsSave(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodPut {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	docID, ok := docIDFromPath("/api/docs/save/", r.URL.Path)
	if !ok {
		http.Error(w, "document id required", http.StatusBadRequest)
		return
	}
	content, ok := decodeDocContent(w, r)
	if !ok {
		return
	}
	if err := h.api.SaveDoc(docID, content); err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "saved"})
}

// handleDocsAutosave records autosaved content without failing the editor.
func (h *Handler) handleDocsAutosave(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodPut {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	docID, ok := docIDFromPath("/api/docs/autosave/", r.URL.Path)
	if !ok {
		http.Error(w, "document id required", http.StatusBadRequest)
		return
	}
	content, ok := decodeDocContent(w, r)
	if !ok {
		return
	}
	// Autosave is best-effort: a failure is reported but the editor keeps
	// working, matching how app.js treats it.
	if err := h.api.SaveDoc(docID, content); err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "autosaved"})
}

// handleDocsComments serves the comment thread on GET and appends on POST.
func (h *Handler) handleDocsComments(w http.ResponseWriter, r *http.Request) {
	docID, ok := docIDFromPath("/api/docs/comments/", r.URL.Path)
	if !ok {
		http.Error(w, "document id required", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(h.api.DocComments(docID))
	case http.MethodPost:
		var body struct {
			Author string `json:"author"`
			Text   string `json:"text"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxCommentLen+1024)
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid JSON body", http.StatusBadRequest)
			return
		}
		comment, err := h.api.AddDocComment(docID, body.Author, body.Text)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(comment)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleDocsContent returns a document's live CRDT text.
func (h *Handler) handleDocsContent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	docID, ok := docIDFromPath("/api/docs/content/", r.URL.Path)
	if !ok {
		http.Error(w, "document id required", http.StatusBadRequest)
		return
	}
	content, found := h.api.DocContent(docID)
	if !found {
		http.Error(w, "document not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"id": docID, "content": content})
}

// handleDocsDocument serves a single document for the editor route. app.js
// reads doc.content and doc.version from this payload.
func (h *Handler) handleDocsDocument(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	docID, ok := docIDFromPath("/api/docs/documents/", r.URL.Path)
	if !ok {
		http.Error(w, "document id required", http.StatusBadRequest)
		return
	}
	doc, found := h.api.Doc(docID)
	if !found {
		http.Error(w, "document not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(doc)
}

// handleDocsPresence reads and updates a document's collaborator list.
//
// GET returns {"users": [...]} so the editor's presence bar renders rather than
// throwing on undefined. POST records a collaborator's cursor and pushes the new
// list to every SSE client, which is what makes presence live: without the POST
// side the list was always empty because nothing ever told the node where anyone
// was.
func (h *Handler) handleDocsPresence(w http.ResponseWriter, r *http.Request) {
	docID, ok := docIDFromPath("/api/docs/presence/", r.URL.Path)
	if !ok {
		http.Error(w, "document id required", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(h.api.DocPresence(docID))

	case http.MethodPost:
		var body struct {
			PeerName string `json:"peer_name"`
			Line     int    `json:"line"`
			Column   int    `json:"column"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&body); err != nil {
			http.Error(w, "invalid json: "+err.Error(), http.StatusBadRequest)
			return
		}
		// Line and column are attacker-controlled and reach the editor, so they
		// are bounded rather than trusted.
		if body.Line < 0 || body.Line > 1_000_000 || body.Column < 0 || body.Column > 1_000_000 {
			http.Error(w, "cursor position out of range", http.StatusBadRequest)
			return
		}
		if body.PeerName == "" {
			http.Error(w, "peer_name required", http.StatusBadRequest)
			return
		}
		if len(body.PeerName) > 128 {
			http.Error(w, "peer_name too long", http.StatusBadRequest)
			return
		}

		h.api.UpdateDocPresence(docID, body.PeerName, body.Line, body.Column)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(h.api.DocPresence(docID))

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
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
	state, hasLog := h.api.AuditIntegrity()

	// No audit log attached: the server cannot answer the question, which is a
	// genuine server-side unavailability. But a chain that exists and fails
	// verification is a successful query with a negative result, so it returns
	// 200 with verified=false. Returning 500 for tampering would report a
	// monitoring failure for what is actually a security finding.
	if !hasLog {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"verified":  false,
			"integrity": string(AuditStateUnavailable),
			"timestamp": time.Now().Format(time.RFC3339),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"verified":  state == AuditStateVerified,
		"integrity": string(state),
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
		"services": h.api.ServiceHealth(),
	})
}

// handleVoiceStatus reports what the voice service can actually do.
//
// The Voice panel used to be told "no encoder and not started" in a hardcoded
// string. Both of those were fixed, so the sentence became false in the other
// direction. This reports the real state, including whether this build can send
// audio and video, so the UI never has to guess.
func (h *Handler) handleVoiceStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(h.api.VoiceStatus())
}

// isNodeIDHex reports whether s is exactly 64 hex characters, which is how a
// 32-byte node identity is rendered.
func isNodeIDHex(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') {
			continue
		}
		return false
	}
	return true
}

// handleVoiceCall publishes a signed call offer through the voice service.
//
// The body is bounded because it is untrusted input on an endpoint that reaches
// the signalling transport.
func (h *Handler) handleVoiceCall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		PeerID      string `json:"peer_id"`
		EnableVideo bool   `json:"enable_video"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body); err != nil {
		http.Error(w, "invalid json: "+err.Error(), http.StatusBadRequest)
		return
	}
	if len(body.PeerID) > 128 {
		http.Error(w, "peer_id too long", http.StatusBadRequest)
		return
	}
	// Validated here rather than in the API so a malformed request is a client
	// error. Letting it through to PlaceCall would turn bad input into a 503,
	// which tells an operator their node is broken when it is fine.
	if !isNodeIDHex(body.PeerID) {
		http.Error(w, "peer_id must be 64 hex characters", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	resp, err := h.api.PlaceCall(ctx, body.PeerID, body.EnableVideo)
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// handleVoiceSignal waits for one signalling message of a type from a peer.
//
// The wait is bounded by wait_ms so this cannot be used to hold a goroutine open
// indefinitely; a caller polls it.
func (h *Handler) handleVoiceSignal(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	peer := r.URL.Query().Get("peer_id")
	sigType := r.URL.Query().Get("type")
	if peer == "" || sigType == "" {
		http.Error(w, "peer_id and type are required", http.StatusBadRequest)
		return
	}
	if len(peer) > 128 || len(sigType) > 32 {
		http.Error(w, "query parameter too long", http.StatusBadRequest)
		return
	}
	if !isNodeIDHex(peer) {
		http.Error(w, "peer_id must be 64 hex characters", http.StatusBadRequest)
		return
	}
	// Reject an unknown signal type before waiting on it, so a typo does not
	// become a 30-second poll that finds nothing.
	if _, err := voice.ParseSignalType(sigType); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	wait := 5 * time.Second
	if v := r.URL.Query().Get("wait_ms"); v != "" {
		ms, err := strconv.Atoi(v)
		if err != nil || ms < 0 || ms > 30000 {
			http.Error(w, "wait_ms must be 0..30000", http.StatusBadRequest)
			return
		}
		wait = time.Duration(ms) * time.Millisecond
	}

	ctx, cancel := context.WithTimeout(r.Context(), wait)
	defer cancel()

	resp, err := h.api.AwaitSignal(ctx, peer, sigType, wait)
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
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

func (h *Handler) handleOnboardingStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(h.api.OnboardingStatus())
}

func (h *Handler) handleOnboardingQR(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	qr, err := h.api.GenerateQRCode()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(qr)
}

func (h *Handler) handleOnboardingBackup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req IdentityBackupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	resp, err := h.api.BackupIdentity(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(resp)
}

func (h *Handler) handleOnboardingRestore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req IdentityRestoreRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	resp, err := h.api.RestoreIdentity(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(resp)
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

	interval := h.sseHeartbeat
	if interval <= 0 {
		interval = defaultSSEHeartbeat
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

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
		case <-ticker.C:
			// An SSE comment: ignored by EventSource, but it keeps the
			// connection warm and proves the stream is alive.
			fmt.Fprint(w, ": heartbeat\n\n")
			flusher.Flush()
		case <-notify:
			return
		}
	}
}
