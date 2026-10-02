package gui

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"golang.org/x/crypto/sha3"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ram1234598766-dotcom/Local-WEB/pkg/crypto"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/discovery"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/security"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/services/docs"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/services/files"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/services/registry"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/services/voice"
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

	// syncEngine backs /api/files/transfers with the Files service's real
	// per-peer transfer state. It is nil until the daemon builds one, in which
	// case Transfers reports an empty list instead of invented progress.
	syncEngine files.SyncEngine

	// registry is the live package registry. Nil until the daemon supplies one,
	// in which case Packages reports an empty list rather than a hardcoded row.
	registry registry.Registry

	// docsSvc backs the Docs panel. The documents, comments and autosave
	// endpoints all read and write through it, so the panel shows real CRDT
	// state instead of the hardcoded placeholder document this used to return.
	docsSvc *docs.Service

	// docAutosave holds the last autosaved content per document, and docComments
	// the comment thread. Neither has service-layer storage, so they live on the
	// API with explicit in-memory semantics rather than being invented per read.
	docAutosave map[string]string
	docComments map[string][]CommentResponse

	// voiceSrv backs the Voice panel with the voice service's real call and track
	// state. It is nil until the daemon constructs the service, in which case the
	// panel reports that rather than a fabricated call.
	voiceSrv *voice.VoiceServer
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
// the files that are actually on this node.
// actual file metadata instead of an empty or fabricated list.
func (a *NodeAPI) SetFileStore(fs files.FileStore) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.fileStore = fs
}

// StoreFile writes an uploaded file into the real file store and returns its
// stored metadata. It fails when no store is wired rather than reporting a
// success that did not happen.
func (a *NodeAPI) StoreFile(ctx context.Context, name string, data []byte) (*files.FileMeta, error) {
	a.mu.RLock()
	fs := a.fileStore
	a.mu.RUnlock()
	if fs == nil {
		return nil, fmt.Errorf("this node has no file store, so nothing was saved")
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("refusing to store an empty file")
	}

	now := time.Now().UTC()
	// The file's CID is the CID of its content, so the same bytes always
	// produce the same id and a re-upload is recognisable as the same file.
	c, err := files.CidFor(data)
	if err != nil {
		return nil, fmt.Errorf("identify file: %w", err)
	}
	meta := &files.FileMeta{
		CID:      c,
		Name:     name,
		Size:     int64(len(data)),
		MimeType: mimeForName(name),
		Modified: now,
		Created:  now,
		Version:  1,
	}
	if err := fs.PutFile(ctx, meta, data); err != nil {
		return nil, fmt.Errorf("store file: %w", err)
	}
	return meta, nil
}

// mimeForName guesses a content type from the extension, defaulting to a binary
// stream rather than claiming to know.
func mimeForName(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".txt", ".md":
		return "text/plain"
	case ".json":
		return "application/json"
	case ".html":
		return "text/html"
	case ".css":
		return "text/css"
	case ".js":
		return "text/javascript"
	case ".go":
		return "text/x-go"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".pdf":
		return "application/pdf"
	default:
		return "application/octet-stream"
	}
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

	// Block counts, so the panel can show real progress rather than a bar
	// that is decorative.
	BlocksTotal  int    `json:"blocks_total"`
	BlocksDone   int    `json:"blocks_done"`
	BlocksFailed int    `json:"blocks_failed"`
	BytesRecv    int64  `json:"bytes_recv"`
	Error        string `json:"error,omitempty"`
}

// SetSyncEngine records the Files sync engine so /api/files/transfers can
// report real transfer state. Until the daemon supplies one, Transfers returns
// an empty list.
func (a *NodeAPI) SetSyncEngine(se files.SyncEngine) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.syncEngine = se
}

// Transfers reports in-flight and completed file transfers per peer.
//
// Every field is derived from the sync engine's own counters. An empty list is
// the honest answer when the daemon has no engine or nothing has synced yet; a
// fabricated progress bar is not.
func (a *NodeAPI) Transfers() []TransferResponse {
	a.mu.RLock()
	engine := a.syncEngine
	a.mu.RUnlock()
	if engine == nil {
		return []TransferResponse{}
	}

	progress := engine.Progress()
	out := make([]TransferResponse, 0, len(progress))
	for _, p := range progress {
		// Every field comes from the engine's own counters, measured against
		// the local block store. An empty list is the honest answer when the
		// daemon has no engine or nothing has synced; a fabricated progress bar
		// is not.
		status := "in-flight"
		switch {
		case p.Err != "":
			status = "failed"
		case len(p.InFlight) == 0 && p.BytesRecv > 0:
			status = "complete"
		case len(p.InFlight) == 0:
			status = "idle"
		}

		out = append(out, TransferResponse{
			Name:         fmt.Sprintf("sync-%x", p.PeerID[:6]),
			Status:       status,
			PeerName:     fmt.Sprintf("%x", p.PeerID[:6]),
			TotalSize:    p.TotalBytes,
			BytesRecv:    int64(p.BytesRecv),
			BlocksTotal:  len(p.Have),
			BlocksDone:   len(p.Have) - len(p.InFlight),
			BlocksFailed: len(p.InFlight),
			Error:        p.Err,
		})
	}
	return out
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

// SetVoiceService supplies the voice service so the Voice panel can report real
// calls rather than a hardcoded one.
func (a *NodeAPI) SetVoiceService(svc *voice.VoiceServer) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.voiceSrv = svc
}

// VoiceStatusResponse is what the Voice panel is allowed to claim.
//
// The dashboard previously refused to start calls with a hardcoded sentence about
// the voice service having no encoder and not being started. Both halves of that
// were fixed, so a static sentence became a new lie. This reports the real
// capability instead: whether the service is registered, whether the build can
// send audio and video, and what is actually blocking a call.
type VoiceStatusResponse struct {
	// ServiceLive is true when the daemon constructed the voice service, so the
	// ServiceVoice stream handler is registered.
	ServiceLive bool `json:"service_live"`
	// Signaling is true when a signalling transport is configured, which is what
	// makes a call placeable. Without it the service can carry media but cannot
	// exchange an offer.
	Signaling bool `json:"signaling"`
	// OpusEncoder and VPXEncoder report what this binary can send. Receiving needs
	// neither, because the pure-Go Opus decoder is always linked.
	OpusEncoder bool `json:"opus_encoder"`
	VPXEncoder  bool `json:"vpx_encoder"`
	// CanSendAudio and CanSendVideo are the two facts a user actually acts on.
	CanSendAudio bool `json:"can_send_audio"`
	CanSendVideo bool `json:"can_send_video"`
	// Calls is the real call list from the voice service.
	Calls []VoiceCallResponse `json:"calls"`
	// Reason is empty when a call could be placed, and otherwise says what stops
	// it, so the UI does not have to invent its own explanation.
	Reason string `json:"reason,omitempty"`
	// EncoderHint is the build flag that would turn on the missing encoder.
	EncoderHint string `json:"encoder_hint,omitempty"`
}

// VoiceCallResponse is one call as the dashboard shows it.
type VoiceCallResponse struct {
	ID    string `json:"id"`
	State string `json:"state"`
}

// PlaceCallResponse is what a placed call reports back.
type PlaceCallResponse struct {
	CallID string `json:"call_id"`
	// Channel is the signalling channel the offer was published on, which is what
	// the peer reads to find it.
	Channel string `json:"channel"`
	// State is what actually happened. "offer_sent" is not "connected": nothing
	// yet exchanges SDP or media, and saying otherwise would be the lie this
	// endpoint exists to avoid.
	State string `json:"state"`
	// Tracks are the local track descriptions carried in the signed offer.
	Tracks []voice.TrackInfo `json:"tracks"`
}

// PlaceCall publishes a signed call offer through the voice service.
//
// It creates the call and sends an offer, and reports "offer_sent". It does not
// connect media: there is no SDP exchange wired into the signalling path yet, so
// a caller that needs a working session has to drive WebRTC directly.
func (a *NodeAPI) PlaceCall(ctx context.Context, peerHex string, enableVideo bool) (PlaceCallResponse, error) {
	a.mu.RLock()
	svc := a.voiceSrv
	a.mu.RUnlock()

	if svc == nil {
		return PlaceCallResponse{}, fmt.Errorf("the voice service is not running")
	}
	if svc.SignalingChannel() == nil {
		return PlaceCallResponse{}, voice.ErrNoSignalingChannel
	}

	peerBytes, err := hex.DecodeString(peerHex)
	if err != nil || len(peerBytes) != 32 {
		return PlaceCallResponse{}, fmt.Errorf("peer_id must be 64 hex characters, got %d characters", len(peerHex))
	}
	var peer [32]byte
	copy(peer[:], peerBytes)

	tracks := []voice.TrackInfo{{
		ID:        "audio-1",
		Kind:      voice.TrackKindAudio,
		Direction: voice.TrackDirectionSendRecv,
		Codec:     voice.CodecOpus,
		MimeType:  "audio/opus",
	}}
	if enableVideo {
		codec := voice.CodecVP8
		mime := voice.VP8MimeType
		if voice.VPXEncoderLinked() {
			codec = voice.CodecVP9
			mime = voice.VP9MimeType
		}
		tracks = append(tracks, voice.TrackInfo{
			ID:        "video-1",
			Kind:      voice.TrackKindVideo,
			Direction: voice.TrackDirectionSendRecv,
			Codec:     codec,
			MimeType:  mime,
		})
	}

	// The channel name is derived from the peer, so both sides land on the same
	// channel without having agreed on a name first.
	channel := "call:" + peerHex

	sig, err := svc.PlaceCall(ctx, voice.CallConfig{
		Callee:     voice.PeerID(peer),
		ChannelID:  channel,
		AudioCodec: voice.CodecOpus,
		VideoCodec: func() voice.CodecID {
			if enableVideo {
				return tracks[1].Codec
			}
			return 0
		}(),
		EnableVideo: enableVideo,
	}, tracks)
	if err != nil {
		return PlaceCallResponse{}, err
	}

	callID := sig.CallID()
	return PlaceCallResponse{
		CallID:  hex.EncodeToString(callID[:]),
		Channel: channel,
		State:   "offer_sent",
		Tracks:  tracks,
	}, nil
}

// AwaitSignalResponse is one signalling message read back.
type AwaitSignalResponse struct {
	Found   bool   `json:"found"`
	CallID  string `json:"call_id,omitempty"`
	Type    string `json:"type,omitempty"`
	Sender  string `json:"sender,omitempty"`
	Message string `json:"message,omitempty"`
}

// AwaitSignal waits for a signalling message of a type from a peer.
//
// wait_ms bounds the wait so the endpoint cannot be used to pin a goroutine
// indefinitely; the caller polls it the way the dashboard polls its other
// long-lived state.
func (a *NodeAPI) AwaitSignal(ctx context.Context, peerHex, sigType string, wait time.Duration) (AwaitSignalResponse, error) {
	a.mu.RLock()
	svc := a.voiceSrv
	a.mu.RUnlock()

	if svc == nil {
		return AwaitSignalResponse{}, fmt.Errorf("the voice service is not running")
	}

	peerBytes, err := hex.DecodeString(peerHex)
	if err != nil || len(peerBytes) != 32 {
		return AwaitSignalResponse{}, fmt.Errorf("peer_id must be 64 hex characters")
	}
	var peer [32]byte
	copy(peer[:], peerBytes)

	parsed, err := voice.ParseSignalType(sigType)
	if err != nil {
		return AwaitSignalResponse{}, err
	}

	msg, err := svc.AwaitSignal(ctx, "call:"+peerHex, voice.PeerID(peer), parsed)
	if err != nil {
		// A timeout is not a failure: nothing matched within the window, which is
		// what the dashboard is asking.
		if errors.Is(err, context.DeadlineExceeded) {
			return AwaitSignalResponse{Found: false}, nil
		}
		return AwaitSignalResponse{}, err
	}

	callID := msg.CallID
	return AwaitSignalResponse{
		Found:   true,
		CallID:  hex.EncodeToString(callID[:]),
		Type:    msg.Type.String(),
		Sender:  hex.EncodeToString(msg.Sender[:]),
		Message: string(msg.Payload),
	}, nil
}

// VoiceStatus reports the voice service's real state.
func (a *NodeAPI) VoiceStatus() VoiceStatusResponse {
	a.mu.RLock()
	svc := a.voiceSrv
	a.mu.RUnlock()

	resp := VoiceStatusResponse{
		ServiceLive: svc != nil,
		Signaling:   svc != nil && svc.SignalingChannel() != nil,
		OpusEncoder: voice.OpusEncoderLinked(),
		VPXEncoder:  voice.VPXEncoderLinked(),
		Calls:       []VoiceCallResponse{},
	}
	resp.CanSendAudio = resp.OpusEncoder
	resp.CanSendVideo = resp.VPXEncoder

	if svc != nil {
		for _, c := range svc.ActiveCalls() {
			id := c.ID()
			resp.Calls = append(resp.Calls, VoiceCallResponse{
				ID:    hex.EncodeToString(id[:]),
				State: c.State().String(),
			})
		}
	}

	switch {
	case !resp.ServiceLive:
		resp.Reason = "the voice service is not running, so a peer opening a voice stream gets no handler"
	case !resp.CanSendAudio:
		// Receiving still works: the decoder is pure Go and always present. Saying
		// the service is unavailable would be as wrong as the old claim that it had
		// no encoder at all.
		resp.Reason = "this build has no audio encoder, so this node can receive a call but not send audio"
		resp.EncoderHint = "rebuild with -tags libopus (needs libopus)"
	}
	return resp
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

	// Cursor and selection. These were missing, so a collaborator appeared in
	// the list with no indication of where in the document they were: the panel
	// showed that someone was present and nothing else.
	Line   int `json:"line"`
	Column int `json:"column"`

	HasSelection bool `json:"has_selection"`
	SelStartLine int  `json:"sel_start_line"`
	SelStartCol  int  `json:"sel_start_col"`
	SelEndLine   int  `json:"sel_end_line"`
	SelEndCol    int  `json:"sel_end_col"`
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
		u := PresenceUser{
			PeerID:    hex.EncodeToString(p.PeerID[:]),
			PeerName:  p.PeerName,
			Connected: p.Connected,
			LastSeen:  p.LastSeen.UTC().Format(time.RFC3339),
			Line:      p.Cursor.Line,
			Column:    p.Cursor.Column,
		}
		if p.Selection != nil {
			u.HasSelection = true
			u.SelStartLine = p.Selection.StartLine
			u.SelStartCol = p.Selection.StartCol
			u.SelEndLine = p.Selection.EndLine
			u.SelEndCol = p.Selection.EndCol
		}
		users = append(users, u)
	}
	return PresenceResponse{Users: users}
}

// UpdateDocPresence records a peer's cursor and pushes the new collaborator
// list to every SSE client.
//
// The editor used to call a function whose body was a comment about how it
// would be implemented, so presence was never real. Pushing over SSE rather than
// polling means the panel updates when a collaborator actually moves.
func (a *NodeAPI) UpdateDocPresence(docID, peerName string, line, column int) {
	a.mu.RLock()
	svc := a.docsSvc
	a.mu.RUnlock()
	if svc == nil || docID == "" || peerName == "" {
		return
	}

	// A browser collaborator presents a name, not a node key, so the id is
	// derived from it. This is not an authenticated identity.
	peerID := crypto.NodeID(sha3.Sum256([]byte("presence:" + peerName)))
	svc.UpdatePresence(context.Background(), docID, peerName, peerID, line, column)
	a.BroadcastDocPresence(docID)
}

// BroadcastDocPresence pushes the current collaborator list for a document to
// every SSE client.
func (a *NodeAPI) BroadcastDocPresence(docID string) {
	// DocPresence takes the read lock itself, so it is called before taking one
	// here. A recursive RLock can deadlock against a waiting writer.
	presence := a.DocPresence(docID)

	a.mu.RLock()
	defer a.mu.RUnlock()
	for ch := range a.sseClients {
		select {
		case ch <- SSEEvent{Type: "presence", Data: map[string]any{
			"doc_id": docID,
			"users":  presence.Users,
		}}:
		default:
			// A client that is not draining its channel is dropped rather than
			// allowed to block every broadcaster.
		}
	}
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

// SetRegistry records the live package registry so the Registry and DHT panels
// show packages that were really published, rather than a hardcoded row.
func (a *NodeAPI) SetRegistry(r registry.Registry) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.registry = r
}

// Packages returns what the registry actually holds.
//
// It used to return one hardcoded row for localweb-cli regardless of what the
// node had published, so the panel always looked populated and never reflected
// a real publish.
func (a *NodeAPI) Packages() ([]PackageResponse, error) {
	a.mu.RLock()
	reg := a.registry
	a.mu.RUnlock()

	if reg == nil {
		return []PackageResponse{}, nil
	}
	metas, err := reg.List()
	if err != nil {
		return nil, fmt.Errorf("list packages: %w", err)
	}
	out := make([]PackageResponse, 0, len(metas))
	for _, m := range metas {
		out = append(out, PackageResponse{
			Name:      m.Name,
			Version:   m.Version,
			Author:    m.Author,
			Installed: a.installedPackageIDs()[m.ID],
		})
	}
	return out, nil
}

// installedPackageIDs indexes the installed set for a lookup per package.
func (a *NodeAPI) installedPackageIDs() map[string]bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := make(map[string]bool, len(a.registryInstalled))
	for _, p := range a.registryInstalled {
		out[p.ID] = true
	}
	return out
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
