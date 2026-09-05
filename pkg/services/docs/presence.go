package docs

import (
	"sync"
	"time"

	"github.com/ram1234598766-dotcom/Local-WEB/pkg/discovery"
)

// Cursor represents a user's cursor position in a document.
type Cursor struct {
	PeerID   [32]byte
	PeerName string
	DocID    string
	Line     int
	Column   int
	Color    string
}

// Selection represents a text selection range.
type Selection struct {
	PeerID    [32]byte
	PeerName  string
	DocID     string
	StartLine int
	StartCol  int
	EndLine   int
	EndCol    int
}

// PeerPresence tracks a peer's cursor and selection state.
type PeerPresence struct {
	PeerID    [32]byte
	PeerName  string
	Cursor    Cursor
	Selection *Selection
	LastSeen  time.Time
	Connected bool
}

// PresenceService tracks cursor positions and selections for a document.
type PresenceService struct {
	mu       sync.RWMutex
	docID    string
	presence map[[32]byte]*PeerPresence
	timeout  time.Duration
}

// PresenceConfig configures a PresenceService.
type PresenceConfig struct {
	DocID   string
	Timeout time.Duration
}

// NewPresenceService creates a new presence tracker.
func NewPresenceService(cfg PresenceConfig) *PresenceService {
	if cfg.Timeout == 0 {
		cfg.Timeout = 5 * time.Second
	}
	return &PresenceService{
		docID:    cfg.DocID,
		presence: make(map[[32]byte]*PeerPresence),
		timeout:  cfg.Timeout,
	}
}

// DocID returns the document this service tracks.
func (p *PresenceService) DocID() string {
	return p.docID
}

// UpdateCursor updates a peer's cursor position.
func (p *PresenceService) UpdateCursor(peerID [32]byte, peerName string, line, column int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	pp, ok := p.presence[peerID]
	if !ok {
		pp = &PeerPresence{
			PeerID:    peerID,
			PeerName:  peerName,
			Connected: true,
		}
		p.presence[peerID] = pp
	}
	pp.Cursor = Cursor{PeerID: peerID, PeerName: peerName, DocID: p.docID, Line: line, Column: column}
	pp.LastSeen = time.Now()
}

// UpdateSelection updates a peer's text selection.
func (p *PresenceService) UpdateSelection(peerID [32]byte, peerName string, startLine, startCol, endLine, endCol int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	pp, ok := p.presence[peerID]
	if !ok {
		pp = &PeerPresence{
			PeerID:    peerID,
			PeerName:  peerName,
			Connected: true,
		}
		p.presence[peerID] = pp
	}
	pp.Selection = &Selection{
		PeerID: peerID, PeerName: peerName, DocID: p.docID,
		StartLine: startLine, StartCol: startCol,
		EndLine: endLine, EndCol: endCol,
	}
	pp.LastSeen = time.Now()
}

// RemovePeer removes a peer's presence.
func (p *PresenceService) RemovePeer(peerID [32]byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if pp, ok := p.presence[peerID]; ok {
		pp.Connected = false
	}
	delete(p.presence, peerID)
}

// GetPeer returns a peer's presence, if known.
func (p *PresenceService) GetPeer(peerID [32]byte) (*PeerPresence, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	pp, ok := p.presence[peerID]
	if !ok {
		return nil, false
	}
	cp := *pp
	return &cp, true
}

// GetAll returns all active peer presences, expiring stale entries.
func (p *PresenceService) GetAll() []PeerPresence {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	var out []PeerPresence
	for id, pp := range p.presence {
		if now.Sub(pp.LastSeen) > p.timeout {
			delete(p.presence, id)
			continue
		}
		out = append(out, *pp)
	}
	return out
}

// Count returns the number of active peers.
func (p *PresenceService) Count() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	now := time.Now()
	count := 0
	for _, pp := range p.presence {
		if now.Sub(pp.LastSeen) <= p.timeout {
			count++
		}
	}
	return count
}

// Touch refreshes a peer's last-seen time without changing cursor/selection.
func (p *PresenceService) Touch(peerID [32]byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if pp, ok := p.presence[peerID]; ok {
		pp.LastSeen = time.Now()
	}
}

// MarkConnected marks a peer as connected.
func (p *PresenceService) MarkConnected(peerID [32]byte, peerName string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	pp, ok := p.presence[peerID]
	if !ok {
		pp = &PeerPresence{PeerID: peerID, PeerName: peerName}
		p.presence[peerID] = pp
	}
	pp.Connected = true
	pp.LastSeen = time.Now()
}

// MarkDisconnected marks a peer as disconnected.
func (p *PresenceService) MarkDisconnected(peerID [32]byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if pp, ok := p.presence[peerID]; ok {
		pp.Connected = false
	}
}

// Marshal serializes all presence data.
func (p *PresenceService) Marshal() []byte {
	p.mu.RLock()
	defer p.mu.RUnlock()
	now := time.Now().UnixNano()
	active := 0
	for _, pp := range p.presence {
		if now-pp.LastSeen.UnixNano() <= p.timeout.Nanoseconds() {
			active++
		}
	}
	buf := make([]byte, 0, active*(32+4+4+4+4))
	for _, pp := range p.presence {
		if now-pp.LastSeen.UnixNano() > p.timeout.Nanoseconds() {
			continue
		}
		buf = append(buf, pp.PeerID[:]...)
		buf = append(buf, encodeUint32(uint32(pp.Cursor.Line))...)
		buf = append(buf, encodeUint32(uint32(pp.Cursor.Column))...)
	}
	return buf
}

// Unmarshal deserializes presence data (peer IDs, line, col).
func (p *PresenceService) Unmarshal(data []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	entrySize := 32 + 4 + 4
	if len(data)%entrySize != 0 {
		return
	}
	count := len(data) / entrySize
	for i := 0; i < count; i++ {
		offset := i * entrySize
		var peerID [32]byte
		copy(peerID[:], data[offset:offset+32])
		line := int(decodeUint32(data[offset+32 : offset+36]))
		col := int(decodeUint32(data[offset+36 : offset+40]))
		pp, ok := p.presence[peerID]
		if !ok {
			pp = &PeerPresence{PeerID: peerID, PeerName: "", Connected: true}
			p.presence[peerID] = pp
		}
		pp.Cursor = Cursor{PeerID: peerID, PeerName: pp.PeerName, DocID: p.docID, Line: line, Column: col}
		pp.LastSeen = time.Now()
	}
}

// FromDiscovery populates presence from discovery peer info.
func (p *PresenceService) FromDiscovery(peers []discovery.PeerInfo) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, peer := range peers {
		pp, ok := p.presence[peer.ID]
		if !ok {
			pp = &PeerPresence{
				PeerID:    peer.ID,
				PeerName:  peer.Name,
				Connected: true,
			}
			p.presence[peer.ID] = pp
		}
		pp.LastSeen = time.Now()
	}
}
