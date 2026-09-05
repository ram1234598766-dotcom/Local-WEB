package security

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/mrityunjay/LocalWEB/pkg/crypto"
	"github.com/mrityunjay/LocalWEB/pkg/discovery"
	"github.com/rs/zerolog/log"
)

// AuditEventType identifies the class of a security event.
type AuditEventType string

const (
	AuditAuthSuccess     AuditEventType = "auth_success"
	AuditAuthFailure     AuditEventType = "auth_failure"
	AuditConnection      AuditEventType = "connection"
	AuditDisconnection   AuditEventType = "disconnection"
	AuditCapabilityGrant AuditEventType = "capability_grant"
	AuditRelayUse        AuditEventType = "relay_use"
)

// AuditEvent records a single security-relevant event.
type AuditEvent struct {
	Type      AuditEventType
	Timestamp time.Time
	PeerID    [32]byte
	Source    string
	Details   map[string]string
}

// MarshalJSON customises JSON encoding for AuditEvent.
func (e AuditEvent) MarshalJSON() ([]byte, error) {
	type Alias AuditEvent
	return json.Marshal(&struct {
		Timestamp string `json:"timestamp"`
		PeerID    string `json:"peer_id"`
		Alias
	}{
		Timestamp: e.Timestamp.Format(time.RFC3339Nano),
		PeerID:    hex.EncodeToString(e.PeerID[:]),
		Alias:     (Alias)(e),
	})
}

// entry is an append-only log entry with a hash chain for tamper detection.
type entry struct {
	event    AuditEvent
	prevHash [32]byte
	hash     [32]byte
}

// AuditLog provides an append-only, tamper-evident audit trail.
type AuditLog struct {
	mu       sync.RWMutex
	entries  []entry
	lastHash [32]byte
}

// NewAuditLog creates a new audit log.
func NewAuditLog() *AuditLog {
	return &AuditLog{}
}

// Log appends a new event to the audit log. It fails closed if the entry
// cannot be hashed or chained.
func (l *AuditLog) Log(evt AuditEvent) error {
	if evt.Timestamp.IsZero() {
		evt.Timestamp = time.Now()
	}
	if evt.Details == nil {
		evt.Details = make(map[string]string)
	}

	b, err := json.Marshal(evt)
	if err != nil {
		return err
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	e := entry{
		event:    evt,
		prevHash: l.lastHash,
	}
	h := crypto.SHA3Hash(append(l.lastHash[:], b...))
	e.hash = h
	l.lastHash = h
	l.entries = append(l.entries, e)

	log.Info().
		Str("type", string(evt.Type)).
		Str("peer", formatPeerID(evt.PeerID)).
		Msg("audit event logged")

	return nil
}

// VerifyIntegrity walks the hash chain and returns nil if no entry has been
// altered.
func (l *AuditLog) VerifyIntegrity() error {
	l.mu.RLock()
	defer l.mu.RUnlock()

	var prev [32]byte
	for i, e := range l.entries {
		b, err := json.Marshal(e.event)
		if err != nil {
			return fmt.Errorf("entry %d: marshal: %w", i, err)
		}
		expected := crypto.SHA3Hash(append(prev[:], b...))
		if e.prevHash != prev {
			return fmt.Errorf("entry %d: prev hash mismatch", i)
		}
		if e.hash != expected {
			return fmt.Errorf("entry %d: hash mismatch", i)
		}
		if e.hash != l.lastHash && i == len(l.entries)-1 {
			return fmt.Errorf("entry %d: last entry hash does not match log tip", i)
		}
		prev = e.hash
	}
	return nil
}

// Len returns the number of events in the log.
func (l *AuditLog) Len() int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return len(l.entries)
}

// Export writes the audit log as JSON lines to w for forensic analysis.
func (l *AuditLog) Export(w io.Writer) error {
	l.mu.RLock()
	defer l.mu.RUnlock()

	for _, e := range l.entries {
		b, err := json.Marshal(struct {
			Event    AuditEvent `json:"event"`
			PrevHash string     `json:"prev_hash"`
			Hash     string     `json:"hash"`
		}{
			Event:    e.event,
			PrevHash: hex.EncodeToString(e.prevHash[:]),
			Hash:     hex.EncodeToString(e.hash[:]),
		})
		if err != nil {
			return err
		}
		if _, err := w.Write(append(b, '\n')); err != nil {
			return err
		}
	}
	return nil
}

// Entries returns a copy of all events (read-only).
func (l *AuditLog) Entries() []AuditEvent {
	l.mu.RLock()
	defer l.mu.RUnlock()

	out := make([]AuditEvent, len(l.entries))
	for i, e := range l.entries {
		out[i] = e.event
	}
	return out
}

// PeerEvents returns all events involving a specific peer.
func (l *AuditLog) PeerEvents(peerID [32]byte) []AuditEvent {
	l.mu.RLock()
	defer l.mu.RUnlock()

	var out []AuditEvent
	for _, e := range l.entries {
		if e.event.PeerID == peerID {
			out = append(out, e.event)
		}
	}
	return out
}

// AuditAuthSuccess logs a successful authentication event.
func (l *AuditLog) AuditAuthSuccess(peerID [32]byte, src string) {
	_ = l.Log(AuditEvent{
		Type:    AuditAuthSuccess,
		PeerID:  peerID,
		Source:  src,
		Details: map[string]string{"result": "success"},
	})
}

// AuditAuthFailure logs a failed authentication event with the reason.
func (l *AuditLog) AuditAuthFailure(peerID [32]byte, src, reason string) {
	_ = l.Log(AuditEvent{
		Type:    AuditAuthFailure,
		PeerID:  peerID,
		Source:  src,
		Details: map[string]string{"result": "failure", "reason": reason},
	})
}

// AuditConnection logs a new connection event.
func (l *AuditLog) AuditConnection(peerID [32]byte, addr string) {
	_ = l.Log(AuditEvent{
		Type:    AuditConnection,
		PeerID:  peerID,
		Source:  addr,
		Details: map[string]string{"addr": addr},
	})
}

// AuditDisconnection logs a disconnection event.
func (l *AuditLog) AuditDisconnection(peerID [32]byte, addr string) {
	_ = l.Log(AuditEvent{
		Type:    AuditDisconnection,
		PeerID:  peerID,
		Source:  addr,
		Details: map[string]string{"addr": addr},
	})
}

// AuditCapabilityGrant logs a capability grant event.
func (l *AuditLog) AuditCapabilityGrant(issuer, peerID [32]byte, services []string) {
	_ = l.Log(AuditEvent{
		Type:   AuditCapabilityGrant,
		PeerID: peerID,
		Source: hex.EncodeToString(issuer[:8]),
		Details: map[string]string{
			"issuer":   hex.EncodeToString(issuer[:8]),
			"services": joinStrings(services),
		},
	})
}

// AuditRelayUse logs a relay usage event.
func (l *AuditLog) AuditRelayUse(peerID [32]byte, relayID [32]byte) {
	_ = l.Log(AuditEvent{
		Type:    AuditRelayUse,
		PeerID:  peerID,
		Source:  hex.EncodeToString(relayID[:8]),
		Details: map[string]string{"relay_id": hex.EncodeToString(relayID[:8])},
	})
}

// MarshalEntries serialises all entries to a transportable binary format.
func (l *AuditLog) MarshalEntries() ([]byte, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	buf := new(bytes.Buffer)
	if err := binary.Write(buf, binary.BigEndian, uint64(len(l.entries))); err != nil {
		return nil, err
	}
	for _, e := range l.entries {
		eb, err := json.Marshal(e.event)
		if err != nil {
			return nil, err
		}
		if err := binary.Write(buf, binary.BigEndian, uint32(len(eb))); err != nil {
			return nil, err
		}
		buf.Write(eb)
		buf.Write(e.prevHash[:])
		buf.Write(e.hash[:])
	}
	return buf.Bytes(), nil
}

// UnmarshalEntries restores entries from the binary format.
func (l *AuditLog) UnmarshalEntries(data []byte) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	buf := bytes.NewReader(data)
	var count uint64
	if err := binary.Read(buf, binary.BigEndian, &count); err != nil {
		return err
	}

	l.entries = make([]entry, 0, count)
	var lastHash [32]byte
	for i := uint64(0); i < count; i++ {
		var len uint32
		if err := binary.Read(buf, binary.BigEndian, &len); err != nil {
			return err
		}
		eb := make([]byte, len)
		if _, err := io.ReadFull(buf, eb); err != nil {
			return err
		}
		var evt AuditEvent
		if err := json.Unmarshal(eb, &evt); err != nil {
			return err
		}
		var prev, hash [32]byte
		if _, err := io.ReadFull(buf, prev[:]); err != nil {
			return err
		}
		if _, err := io.ReadFull(buf, hash[:]); err != nil {
			return err
		}

		l.entries = append(l.entries, entry{
			event:    evt,
			prevHash: prev,
			hash:     hash,
		})
		if prev != lastHash {
			return fmt.Errorf("entry %d: prev hash mismatch", i)
		}
		lastHash = hash
	}
	l.lastHash = lastHash
	return nil
}

// WithContext enriches an AuditEvent with peer info from discovery.
func WithContext(evt AuditEvent, peer *discovery.PeerInfo) AuditEvent {
	if evt.Details == nil {
		evt.Details = make(map[string]string)
	}
	if peer == nil {
		return evt
	}
	if peer.Name != "" {
		evt.Details["peer_name"] = peer.Name
	}
	if len(peer.Addrs) > 0 {
		evt.Details["peer_addr"] = peer.Addrs[0]
	}
	evt.Source = peer.Source
	return evt
}

func joinStrings(ss []string) string {
	var b bytes.Buffer
	for i, s := range ss {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(s)
	}
	return b.String()
}
