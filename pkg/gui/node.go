package gui

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ram1234598766-dotcom/Local-WEB/pkg/crypto"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/discovery"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/security"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/services/docs"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/services/files"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/services/registry"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/store"
)

type NodeAPI struct {
	mu         sync.RWMutex
	pubKey     [32]byte
	nodeID     [32]byte
	startTime  time.Time
	store      *store.Store
	peerStore  *store.PeerStore
	discovery  *discovery.Orchestrator
	auditLog   *security.AuditLog
	sseClients map[chan SSEEvent]struct{}

	// services records whether each protocol service the daemon actually
	// started is running. It is populated by SetServiceRunning as each service
	// comes up, so /api/services/health reports what is true rather than a
	// hardcoded list. Absent keys mean "not running".
	services map[string]bool

	// httpsSites is the gateway's real route table, reported by HTTPSites.
	httpsSites []HTTPSiteResponse

	// fileStore and registryInstalled back the Files and Registry panels. Both
	// are nil until the daemon wires the corresponding service, and the
	// handlers report that rather than returning invented rows.
	fileStore         files.FileStore
	registryInstalled []registry.PackageMeta

	// docsSvc backs the Docs panel. The documents, comments and autosave
	// endpoints all read and write through it, so the panel shows real CRDT
	// state instead of the hardcoded placeholder document this used to return.
	docsSvc *docs.Service

	// docAutosave holds the last autosaved content per document, and docComments
	// the comment thread. Neither has service-layer storage, so they live on the
	// API with explicit in-memory semantics rather than being invented per read.
	docAutosave map[string]string
	docComments map[string][]CommentResponse
}

type SSEEvent struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
}

type StatusResponse struct {
	NodeID    string `json:"node_id"`
	PublicKey string `json:"public_key"`
	StartedAt string `json:"started_at"`
	Uptime    string `json:"uptime"`
	PeerCount int    `json:"peer_count"`
	StorePath string `json:"store_path"`
}

type PeerResponse struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Addrs    []string `json:"addrs"`
	Score    float64  `json:"score"`
	Latency  string   `json:"latency"`
	Source   string   `json:"source"`
	LastSeen string   `json:"last_seen"`
}

type DHTNodeResponse struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Addrs    string `json:"addrs"`
	LastSeen string `json:"last_seen"`
}

type AuditLogResponse struct {
	Type      string            `json:"type"`
	Timestamp string            `json:"timestamp"`
	PeerID    string            `json:"peer_id"`
	Source    string            `json:"source"`
	Details   map[string]string `json:"details"`
}

type SyncStatusResponse struct {
	Documents  int  `json:"documents"`
	PendingOps int  `json:"pending_ops"`
	Connected  bool `json:"connected"`
}

type DNSRecordResponse struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Value    string `json:"value"`
	TTL      int    `json:"ttl"`
	Verified bool   `json:"verified"`
}

type HTTPSiteResponse struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Routes int    `json:"routes"`
}

type EmailMessageResponse struct {
	From    string `json:"from"`
	Subject string `json:"subject"`
	Date    string `json:"date"`
	Read    bool   `json:"read"`
}

type QRCodeResponse struct {
	QRCode string `json:"qr_code"` // base64 encoded PNG
	NodeID string `json:"node_id"`
	Name   string `json:"name"`
}

type IdentityBackupRequest struct {
	Passphrase string `json:"passphrase"`
	Name       string `json:"name"`
}

type IdentityBackupResponse struct {
	Backup  string `json:"backup"` // base64 encoded JSON
	Success bool   `json:"success"`
	Message string `json:"message"`
}

type IdentityRestoreRequest struct {
	Backup     string `json:"backup"` // base64 encoded JSON
	Passphrase string `json:"passphrase"`
}

type IdentityRestoreResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	NodeID  string `json:"node_id"`
	Name    string `json:"name"`
}

type OnboardingStatusResponse struct {
	Step        int    `json:"step"` // 0=welcome, 1=name, 2=qr, 3=backup, 4=complete
	HasIdentity bool   `json:"has_identity"`
	NodeName    string `json:"node_name"`
	NeedsBackup bool   `json:"needs_backup"`
}

type MessageResponse struct {
	Channel string `json:"channel"`
	From    string `json:"from"`
	Text    string `json:"text"`
	Time    string `json:"time"`
}

type DocumentResponse struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Peers    int    `json:"peers"`
	LastSync string `json:"last_sync"`
}

// CommentResponse is one entry in a document's comment thread. app.js reads
// author, text and created.
type CommentResponse struct {
	ID      string `json:"id"`
	Author  string `json:"author"`
	Text    string `json:"text"`
	Created string `json:"created"`
}

// maxCommentLen and maxDocBytes bound untrusted request bodies so a single
// request cannot grow the in-memory maps without limit.
const (
	maxCommentLen = 4096
	maxDocBytes   = 4 << 20
)

type PackageResponse struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	Author    string `json:"author"`
	Installed bool   `json:"installed"`
}

func NewAPI(pubKey [32]byte) *NodeAPI {
	return &NodeAPI{
		pubKey:      pubKey,
		nodeID:      crypto.NodeID(pubKey),
		startTime:   time.Now(),
		sseClients:  make(map[chan SSEEvent]struct{}),
		docAutosave: make(map[string]string),
		docComments: make(map[string][]CommentResponse),
	}
}

func (a *NodeAPI) SetStore(s *store.Store) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.store = s
}

func (a *NodeAPI) SetPeerStore(ps *store.PeerStore) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.peerStore = ps
}

func (a *NodeAPI) SetDiscovery(d *discovery.Orchestrator) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.discovery = d
}

func (a *NodeAPI) SetAuditLog(al *security.AuditLog) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.auditLog = al
}

func (a *NodeAPI) Status() StatusResponse {
	a.mu.RLock()
	defer a.mu.RUnlock()

	resp := StatusResponse{
		NodeID:    fmt.Sprintf("%x", a.nodeID[:8]),
		PublicKey: fmt.Sprintf("%x", a.pubKey[:]),
		StartedAt: a.startTime.Format(time.RFC3339),
		Uptime:    time.Since(a.startTime).String(),
	}

	if a.peerStore != nil {
		count, _ := a.peerStore.CountPeers(context.Background())
		resp.PeerCount = count
	}

	if a.store != nil {
		resp.StorePath = a.store.Path()
	}

	return resp
}

func (a *NodeAPI) Peers() ([]PeerResponse, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.peerStore == nil {
		return nil, fmt.Errorf("peer store not initialized")
	}

	peers, err := a.peerStore.ListPeers(context.Background())
	if err != nil {
		return nil, fmt.Errorf("list peers: %w", err)
	}

	out := make([]PeerResponse, 0, len(peers))
	for _, p := range peers {
		out = append(out, PeerResponse{
			ID:       fmt.Sprintf("%x", p.ID[:8]),
			Name:     p.Name,
			Addrs:    p.Addrs,
			Score:    p.Score,
			Latency:  p.Latency.String(),
			Source:   p.Source,
			LastSeen: p.LastSeen.Format(time.RFC3339),
		})
	}
	return out, nil
}

func (a *NodeAPI) DHTNodes() ([]DHTNodeResponse, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.discovery == nil {
		return nil, fmt.Errorf("discovery not initialized")
	}

	peers := a.discovery.Peers()
	out := make([]DHTNodeResponse, 0, len(peers))
	for _, p := range peers {
		out = append(out, DHTNodeResponse{
			ID:       fmt.Sprintf("%x", p.ID[:8]),
			Name:     p.Name,
			Addrs:    fmt.Sprintf("%v", p.Addrs),
			LastSeen: p.LastSeen.Format(time.RFC3339),
		})
	}
	return out, nil
}

func (a *NodeAPI) AuditLogVerified() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.auditLog == nil {
		return false
	}
	return a.auditLog.VerifyIntegrity() == nil
}

// AuditIntegrity reports the state of the audit chain as three distinct
// outcomes rather than one bool. "No audit log" and "audit log has been
// tampered with" are different facts and callers must be able to tell them
// apart: the first means there is nothing to verify, the second means
// verification succeeded and the answer is bad news.
type AuditState string

const (
	// AuditStateUnavailable means no audit log is attached, so integrity
	// cannot be determined at all.
	AuditStateUnavailable AuditState = "unavailable"
	// AuditStateVerified means the chain verified.
	AuditStateVerified AuditState = "verified"
	// AuditStateTampered means a chain exists and its hash chain is broken.
	AuditStateTampered AuditState = "tampered"
)

// AuditIntegrity returns the audit chain state and whether a chain exists.
func (a *NodeAPI) AuditIntegrity() (AuditState, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.auditLog == nil {
		return AuditStateUnavailable, false
	}
	if a.auditLog.VerifyIntegrity() != nil {
		return AuditStateTampered, true
	}
	return AuditStateVerified, true
}

func (a *NodeAPI) AuditLog() ([]AuditLogResponse, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.auditLog == nil {
		return nil, fmt.Errorf("audit log not initialized")
	}

	entries := a.auditLog.Entries()
	out := make([]AuditLogResponse, 0, len(entries))
	for _, e := range entries {
		out = append(out, AuditLogResponse{
			Type:      string(e.Type),
			Timestamp: e.Timestamp.Format(time.RFC3339Nano),
			PeerID:    fmt.Sprintf("%x", e.PeerID[:8]),
			Source:    e.Source,
			Details:   e.Details,
		})
	}
	return out, nil
}

func (a *NodeAPI) SyncStatus() SyncStatusResponse {
	a.mu.RLock()
	defer a.mu.RUnlock()

	resp := SyncStatusResponse{
		Connected: a.discovery != nil,
	}

	if a.peerStore != nil {
		count, _ := a.peerStore.CountPeers(context.Background())
		resp.Documents = count
	}

	return resp
}

func (a *NodeAPI) DNSRecords() ([]DNSRecordResponse, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.peerStore == nil {
		return nil, fmt.Errorf("peer store not initialized")
	}
	peers, err := a.peerStore.ListPeers(context.Background())
	if err != nil {
		return nil, err
	}
	out := make([]DNSRecordResponse, 0, len(peers))
	for _, p := range peers {
		// A peer with no addresses has no A record to publish. Selecting
		// Addrs[0] unconditionally would panic on any such peer.
		if len(p.Addrs) == 0 {
			continue
		}
		out = append(out, DNSRecordResponse{
			Name:  p.Name + ".localweb",
			Type:  "A",
			Value: p.Addrs[0],
			TTL:   4500,
			// These records are synthesised from the peer store, not read out
			// of a signed DNS zone, so they are not DNSSEC-verified.
			Verified: false,
		})
	}
	return out, nil
}

// ServiceHealth reports the liveness of each component the daemon actually
// starts. The nine protocol services are listed explicitly and reported as
// down unless the daemon registers them, because reporting them as healthy
// while nothing is listening is worse than reporting nothing.
// serviceNames is every protocol service the GUI knows about. A name being
// absent from NodeAPI.services means the daemon did not start it.
var serviceNames = []string{
	"dns", "http", "email", "messaging", "files", "docs", "registry", "voice", "vpn",
}

// SetServiceRunning records the real state of a protocol service. The daemon
// calls this as each service starts and stops, so the health endpoint reflects
// what is actually running.
func (a *NodeAPI) SetServiceRunning(name string, running bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.services == nil {
		a.services = make(map[string]bool, len(serviceNames))
	}
	a.services[name] = running
}

// SetServiceLive reports the running state of a service, panicking on an unknown
// name. A typo here would silently create a health key the GUI never reads, so
// this guards the one map that ServiceHealth is built from.
func (a *NodeAPI) SetServiceLive(running bool, name string) {
	for _, known := range serviceNames {
		if known == name {
			a.SetServiceRunning(name, running)
			return
		}
	}
	panic("gui: SetServiceLive called with unknown service " + name)
}

// SetHTTPSites records the HTTP gateway's real route table.
func (a *NodeAPI) SetHTTPSites(sites []HTTPSiteResponse) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.httpsSites = sites
}

// SetFileStore records the real Files block store so /api/files/list can report
// actual file metadata instead of an empty or fabricated list.
func (a *NodeAPI) SetFileStore(fs files.FileStore) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.fileStore = fs
}

// SetRegistryInstalled records the packages installed on this node.
func (a *NodeAPI) SetRegistryInstalled(pkgs []registry.PackageMeta) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.registryInstalled = pkgs
}

// FileResponse mirrors the JSON shape the Files panel reads: id, name, size,
// mime_type and modified. The field names are the ones app.js already uses.
type FileResponse struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Size     int64  `json:"size"`
	MimeType string `json:"mime_type"`
	Modified string `json:"modified"`
	Version  int64  `json:"version"`
}

// Files returns the real contents of the file store.
//
// It returns an empty list, not an error, when no store is wired: an
// unconfigured Files service is a legitimately empty file list, and the SPA
// treats a 404 as a hard error.
func (a *NodeAPI) Files() ([]FileResponse, error) {
	a.mu.RLock()
	fs := a.fileStore
	a.mu.RUnlock()

	if fs == nil {
		return []FileResponse{}, nil
	}

	metas, err := fs.ListFiles(context.Background())
	if err != nil {
		return nil, fmt.Errorf("list files: %w", err)
	}

	out := make([]FileResponse, 0, len(metas))
	for _, m := range metas {
		if m == nil {
			continue
		}
		out = append(out, FileResponse{
			ID:       m.CID.String(),
			Name:     m.Name,
			Size:     m.Size,
			MimeType: m.MimeType,
			Modified: m.Modified.UTC().Format(time.RFC3339),
			Version:  m.Version,
		})
	}
	return out, nil
}

// TransferResponse mirrors the Transfers panel: status, elapsed_ms, speed_bps,
// peer_name and total_size.
type TransferResponse struct {
	Name      string `json:"name"`
	Status    string `json:"status"`
	ElapsedMS int64  `json:"elapsed_ms"`
	SpeedBPS  int64  `json:"speed_bps"`
	PeerName  string `json:"peer_name"`
	TotalSize int64  `json:"total_size"`
}

// Transfers reports in-flight and recent file transfers.
//
// The transfer machinery lives in the Files service, which the daemon does not
// yet drive a transfer loop from, so this reports an empty list rather than
// fabricating progress. Phase 7 item 7.2 is where that gets wired.
func (a *NodeAPI) Transfers() []TransferResponse {
	return []TransferResponse{}
}

// RegistryInstalled returns the packages installed on this node.
func (a *NodeAPI) RegistryInstalled() []registry.PackageMeta {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.registryInstalled == nil {
		return []registry.PackageMeta{}
	}
	return a.registryInstalled
}

func (a *NodeAPI) ServiceHealth() map[string]bool {
	a.mu.RLock()
	defer a.mu.RUnlock()

	health := map[string]bool{
		"store":     a.store != nil,
		"peerStore": a.peerStore != nil,
		"discovery": a.discovery != nil,
		"audit":     a.auditLog != nil,
		"gui":       true,
	}
	for _, svc := range serviceNames {
		health[svc] = a.services[svc]
	}
	return health
}

func (a *NodeAPI) HTTPSites() ([]HTTPSiteResponse, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.httpsSites == nil {
		// The gateway is not running, so there is no route table to report.
		// Returning invented rows here is what made this endpoint lie.
		return []HTTPSiteResponse{}, nil
	}
	return a.httpsSites, nil
}

func (a *NodeAPI) EmailMessages() ([]EmailMessageResponse, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.peerStore == nil {
		return nil, fmt.Errorf("peer store not initialized")
	}
	peers, err := a.peerStore.ListPeers(context.Background())
	if err != nil {
		return nil, err
	}
	out := make([]EmailMessageResponse, 0, len(peers))
	for _, p := range peers {
		out = append(out, EmailMessageResponse{
			From:    p.Name,
			Subject: "Connection established",
			Date:    p.LastSeen.Format(time.RFC3339),
			Read:    true,
		})
	}
	return out, nil
}

func (a *NodeAPI) Messages() ([]MessageResponse, error) {
	return []MessageResponse{
		{Channel: "general", From: "system", Text: "Welcome to LocalWEB messaging", Time: time.Now().Format(time.RFC3339)},
	}, nil
}

// SetDocsService records the real collaborative document service so the Docs
// panel lists and edits actual CRDT documents.
func (a *NodeAPI) SetDocsService(svc *docs.Service) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.docsSvc = svc
}

// Documents lists the documents held by the Docs service.
//
// This previously returned a single hardcoded "Getting Started" document
// regardless of node state. Without a wired service the honest answer is no
// documents, which is what the SPA renders as an empty list.
func (a *NodeAPI) Documents() ([]DocumentResponse, error) {
	a.mu.RLock()
	svc := a.docsSvc
	a.mu.RUnlock()

	if svc == nil {
		return []DocumentResponse{}, nil
	}

	all := svc.AllDocuments()
	out := make([]DocumentResponse, 0, len(all))
	for _, d := range all {
		if d == nil {
			continue
		}
		out = append(out, DocumentResponse{
			ID:       d.ID(),
			Name:     d.Title(),
			Peers:    len(svc.GetDocPresence(d.ID())),
			LastSync: time.Now().UTC().Format(time.RFC3339),
		})
	}
	return out, nil
}

// DocResponse is the document payload the editor route reads: content, version,
// title, id. app.js dereferences doc.content and doc.version directly.
type DocResponse struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Content string `json:"content"`
	Version int64  `json:"version"`
}

// PresenceUser is one collaborator shown in the editor's presence bar. The
// editor reads presence.users, so the key must stay "users".
type PresenceUser struct {
	PeerID    string `json:"peer_id"`
	PeerName  string `json:"peer_name"`
	Connected bool   `json:"connected"`
	LastSeen  string `json:"last_seen"`
}

// PresenceResponse is the wrapper the editor expects: a "users" array.
type PresenceResponse struct {
	Users []PresenceUser `json:"users"`
}

// DocContent returns a document's current text from the CRDT.
func (a *NodeAPI) DocContent(docID string) (string, bool) {
	a.mu.RLock()
	svc := a.docsSvc
	a.mu.RUnlock()

	if svc == nil {
		return "", false
	}
	d := svc.GetDocument(docID)
	if d == nil {
		return "", false
	}
	return d.Text(), true
}

// Doc returns the full document payload for the editor route.
func (a *NodeAPI) Doc(docID string) (DocResponse, bool) {
	a.mu.RLock()
	svc := a.docsSvc
	a.mu.RUnlock()

	if svc == nil {
		return DocResponse{}, false
	}
	d := svc.GetDocument(docID)
	if d == nil {
		return DocResponse{}, false
	}
	return DocResponse{
		ID:      d.ID(),
		Title:   d.Title(),
		Content: d.Text(),
		Version: d.Version(),
	}, true
}

// DocPresence returns the live collaborators for a document.
func (a *NodeAPI) DocPresence(docID string) PresenceResponse {
	a.mu.RLock()
	svc := a.docsSvc
	a.mu.RUnlock()

	if svc == nil {
		return PresenceResponse{Users: []PresenceUser{}}
	}
	peers := svc.GetDocPresence(docID)
	users := make([]PresenceUser, 0, len(peers))
	for _, p := range peers {
		users = append(users, PresenceUser{
			PeerID:    hex.EncodeToString(p.PeerID[:]),
			PeerName:  p.PeerName,
			Connected: p.Connected,
			LastSeen:  p.LastSeen.UTC().Format(time.RFC3339),
		})
	}
	return PresenceResponse{Users: users}
}

// CreateDoc creates a document through the Docs service.
func (a *NodeAPI) CreateDoc(docID, title string) (string, error) {
	a.mu.RLock()
	svc := a.docsSvc
	a.mu.RUnlock()

	if svc == nil {
		return "", fmt.Errorf("docs service not running")
	}
	if docID == "" {
		return "", fmt.Errorf("document id is required")
	}
	d := svc.CreateDocument(docID, title)
	if d == nil {
		return "", fmt.Errorf("could not create document %q", docID)
	}
	return d.ID(), nil
}

// SaveDoc replaces a document's content by applying it through the CRDT.
//
// The service has no whole-document replace primitive, so the content is applied
// as a diff against the current text: delete the old lines, then insert the new
// ones. That keeps every mutation a real RGA operation instead of a side-channel
// string assignment that peers would never converge with.
func (a *NodeAPI) SaveDoc(docID, content string) error {
	if len(content) > maxDocBytes {
		return fmt.Errorf("document too large: %d bytes, limit %d", len(content), maxDocBytes)
	}

	a.mu.RLock()
	svc := a.docsSvc
	a.mu.RUnlock()

	if svc == nil {
		return fmt.Errorf("docs service not running")
	}
	d := svc.GetDocument(docID)
	if d == nil {
		return fmt.Errorf("document %q not found", docID)
	}

	ctx := context.Background()
	for i := d.LineCount() - 1; i >= 0; i-- {
		if _, _, err := svc.DeleteLine(ctx, docID, a.pubKey, i); err != nil {
			return fmt.Errorf("delete line %d: %w", i, err)
		}
	}
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if _, _, err := svc.InsertText(ctx, docID, a.pubKey, i, line); err != nil {
			return fmt.Errorf("insert line %d: %w", i, err)
		}
	}

	a.mu.Lock()
	a.docAutosave[docID] = content
	a.mu.Unlock()
	return nil
}

// DocComments returns a document's comment thread, oldest first.
func (a *NodeAPI) DocComments(docID string) []CommentResponse {
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := a.docComments[docID]
	if out == nil {
		return []CommentResponse{}
	}
	return append([]CommentResponse(nil), out...)
}

// AddDocComment appends a comment after validating untrusted input.
func (a *NodeAPI) AddDocComment(docID, author, text string) (CommentResponse, error) {
	if docID == "" {
		return CommentResponse{}, fmt.Errorf("document id is required")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return CommentResponse{}, fmt.Errorf("comment text is required")
	}
	if len(text) > maxCommentLen {
		return CommentResponse{}, fmt.Errorf("comment too long: %d bytes, limit %d", len(text), maxCommentLen)
	}
	if len(author) > 256 {
		author = author[:256]
	}

	c := CommentResponse{
		ID:      fmt.Sprintf("c-%d", time.Now().UnixNano()),
		Author:  author,
		Text:    text,
		Created: time.Now().UTC().Format(time.RFC3339),
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	a.docComments[docID] = append(a.docComments[docID], c)
	return c, nil
}

func (a *NodeAPI) Packages() ([]PackageResponse, error) {
	return []PackageResponse{
		{Name: "localweb-cli", Version: "1.0.0", Author: "system", Installed: false},
	}, nil
}

func (a *NodeAPI) Subscribe() chan SSEEvent {
	ch := make(chan SSEEvent, 16)
	a.mu.Lock()
	a.sseClients[ch] = struct{}{}
	a.mu.Unlock()
	return ch
}

func (a *NodeAPI) Unsubscribe(ch chan SSEEvent) {
	a.mu.Lock()
	delete(a.sseClients, ch)
	a.mu.Unlock()
}

func (a *NodeAPI) BroadcastEvent(evt SSEEvent) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	for ch := range a.sseClients {
		select {
		case ch <- evt:
		default:
		}
	}
}

func (a *NodeAPI) OnboardingStatus() OnboardingStatusResponse {
	a.mu.RLock()
	defer a.mu.RUnlock()

	hasIdentity := a.store != nil
	needsBackup := false
	if a.store != nil {
		// Check if backup exists (simplified - always true for demo)
		needsBackup = true
	}

	return OnboardingStatusResponse{
		Step:        0,
		HasIdentity: hasIdentity,
		NodeName:    "localweb-node",
		NeedsBackup: needsBackup,
	}
}

func (a *NodeAPI) GenerateQRCode() (QRCodeResponse, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	qrBytes, err := crypto.GenerateIdentityQR(a.nodeID, a.pubKey, "localweb-node")
	if err != nil {
		return QRCodeResponse{}, err
	}

	return QRCodeResponse{
		QRCode: base64.StdEncoding.EncodeToString(qrBytes),
		NodeID: fmt.Sprintf("%x", a.nodeID[:8]),
		Name:   "localweb-node",
	}, nil
}

func (a *NodeAPI) BackupIdentity(req IdentityBackupRequest) (IdentityBackupResponse, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if req.Passphrase == "" {
		return IdentityBackupResponse{Success: false, Message: "passphrase required"}, nil
	}
	if req.Name == "" {
		req.Name = "localweb-node"
	}

	backup, err := crypto.BackupIdentity(a.pubKey, a.pubKey, req.Name, req.Passphrase)
	if err != nil {
		return IdentityBackupResponse{Success: false, Message: err.Error()}, nil
	}

	backupJSON, err := crypto.ExportIdentityBackupJSON(backup)
	if err != nil {
		return IdentityBackupResponse{Success: false, Message: err.Error()}, nil
	}

	return IdentityBackupResponse{
		Backup:  base64.StdEncoding.EncodeToString(backupJSON),
		Success: true,
		Message: "identity backed up successfully",
	}, nil
}

func (a *NodeAPI) RestoreIdentity(req IdentityRestoreRequest) (IdentityRestoreResponse, error) {
	if req.Backup == "" {
		return IdentityRestoreResponse{Success: false, Message: "backup data required"}, nil
	}
	if req.Passphrase == "" {
		return IdentityRestoreResponse{Success: false, Message: "passphrase required"}, nil
	}

	backupJSON, err := base64.StdEncoding.DecodeString(req.Backup)
	if err != nil {
		return IdentityRestoreResponse{Success: false, Message: "invalid backup encoding"}, nil
	}

	backup, err := crypto.ImportIdentityBackupJSON(backupJSON)
	if err != nil {
		return IdentityRestoreResponse{Success: false, Message: err.Error()}, nil
	}

	_, _, err = crypto.RestoreIdentity(backup, req.Passphrase)
	if err != nil {
		return IdentityRestoreResponse{Success: false, Message: err.Error()}, nil
	}

	return IdentityRestoreResponse{
		Success: true,
		Message: "identity restored successfully",
		NodeID:  backup.NodeID,
		Name:    backup.Name,
	}, nil
}

func (a *NodeAPI) MarshalJSON() ([]byte, error) {
	return json.Marshal(a.Status())
}
