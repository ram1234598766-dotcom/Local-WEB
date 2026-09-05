package gui

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/ram1234598766-dotcom/Local-WEB/pkg/crypto"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/discovery"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/security"
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

type PackageResponse struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	Author    string `json:"author"`
	Installed bool   `json:"installed"`
}

func NewAPI(pubKey [32]byte) *NodeAPI {
	return &NodeAPI{
		pubKey:     pubKey,
		nodeID:     crypto.NodeID(pubKey),
		startTime:  time.Now(),
		sseClients: make(map[chan SSEEvent]struct{}),
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
		out = append(out, DNSRecordResponse{
			Name:     p.Name + ".localweb",
			Type:     "A",
			Value:    p.Addrs[0],
			TTL:      4500,
			Verified: true,
		})
	}
	return out, nil
}

func (a *NodeAPI) HTTPSites() ([]HTTPSiteResponse, error) {
	return []HTTPSiteResponse{
		{Name: "localhost:8080", Status: "active", Routes: 1},
		{Name: "gui.localweb", Status: "active", Routes: 2},
	}, nil
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

func (a *NodeAPI) Documents() ([]DocumentResponse, error) {
	return []DocumentResponse{
		{ID: "doc-001", Name: "Getting Started", Peers: 0, LastSync: time.Now().Format(time.RFC3339)},
	}, nil
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

func (a *NodeAPI) MarshalJSON() ([]byte, error) {
	return json.Marshal(a.Status())
}
