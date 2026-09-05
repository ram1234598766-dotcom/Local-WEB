package security

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/ram1234598766-dotcom/Local-WEB/pkg/crypto"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/discovery"
	"github.com/rs/zerolog/log"
)

func init() {
	log.Logger = log.Output(nil)
}

func TestAuditLogAppendAndVerify(t *testing.T) {
	log := NewAuditLog()
	pid, _, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	if err := log.Log(AuditEvent{Type: AuditConnection, PeerID: pid, Source: "tcp"}); err != nil {
		t.Fatalf("log: %v", err)
	}
	if log.Len() != 1 {
		t.Fatalf("expected 1 entry, got %d", log.Len())
	}
	if err := log.VerifyIntegrity(); err != nil {
		t.Fatalf("verify integrity: %v", err)
	}
}

func TestAuditLogTamperDetection(t *testing.T) {
	log := NewAuditLog()
	pid, _, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	_ = log.Log(AuditEvent{Type: AuditConnection, PeerID: pid, Source: "tcp"})

	// Mutate the internal entry directly.
	log.mu.Lock()
	if len(log.entries) > 0 {
		log.entries[0].event.Type = AuditAuthSuccess
	}
	log.mu.Unlock()

	if err := log.VerifyIntegrity(); err == nil {
		t.Fatal("expected tamper detection")
	}
}

func TestAuditLogExport(t *testing.T) {
	log := NewAuditLog()
	pid, _, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	_ = log.Log(AuditEvent{Type: AuditConnection, PeerID: pid, Source: "tcp"})

	var buf bytes.Buffer
	if err := log.Export(&buf); err != nil {
		t.Fatalf("export: %v", err)
	}
	if !strings.Contains(buf.String(), "connection") {
		t.Fatal("export missing event type")
	}
	if !strings.Contains(buf.String(), "prev_hash") {
		t.Fatal("export missing prev_hash")
	}
	if !strings.Contains(buf.String(), "hash") {
		t.Fatal("export missing hash")
	}
}

func TestAuditLogPeerEvents(t *testing.T) {
	log := NewAuditLog()
	pid1, _, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	pid2, _, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	_ = log.Log(AuditEvent{Type: AuditConnection, PeerID: pid1, Source: "tcp"})
	_ = log.Log(AuditEvent{Type: AuditAuthSuccess, PeerID: pid1, Source: "noise"})
	_ = log.Log(AuditEvent{Type: AuditConnection, PeerID: pid2, Source: "tcp"})

	evts := log.PeerEvents(pid1)
	if len(evts) != 2 {
		t.Fatalf("expected 2 events for peer1, got %d", len(evts))
	}
	evts = log.PeerEvents(pid2)
	if len(evts) != 1 {
		t.Fatalf("expected 1 event for peer2, got %d", len(evts))
	}
}

func TestAuditHelpers(t *testing.T) {
	log := NewAuditLog()
	pid, _, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	log.AuditAuthSuccess(pid, "noise")
	log.AuditAuthFailure(pid, "noise", "bad signature")
	log.AuditConnection(pid, "127.0.0.1:9000")
	log.AuditDisconnection(pid, "127.0.0.1:9000")
	log.AuditCapabilityGrant(pid, pid, []string{"http", "dns"})
	relay, _, _ := crypto.GenerateKeyPair()
	log.AuditRelayUse(pid, relay)

	if log.Len() != 6 {
		t.Fatalf("expected 6 events, got %d", log.Len())
	}
	if err := log.VerifyIntegrity(); err != nil {
		t.Fatalf("integrity: %v", err)
	}
}

func TestAuditEventTimestampDefaults(t *testing.T) {
	log := NewAuditLog()
	pid, _, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	evt := AuditEvent{Type: AuditConnection, PeerID: pid, Source: "tcp"}
	if err := log.Log(evt); err != nil {
		t.Fatalf("log: %v", err)
	}
	ents := log.Entries()
	if ents[0].Timestamp.IsZero() {
		t.Fatal("timestamp not populated")
	}
}

func TestWithContextEnrichesEvent(t *testing.T) {
	pid, _, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	peer := &discovery.PeerInfo{
		ID:     pid,
		Name:   "alpha",
		Addrs:  []string{"10.0.0.1:9000"},
		Source: "ble",
	}
	evt := WithContext(AuditEvent{Type: AuditConnection, PeerID: pid}, peer)
	if evt.Details["peer_name"] != "alpha" {
		t.Fatalf("expected peer_name, got %v", evt.Details)
	}
	if evt.Details["peer_addr"] != "10.0.0.1:9000" {
		t.Fatalf("expected peer_addr, got %v", evt.Details)
	}
	if evt.Source != "ble" {
		t.Fatalf("expected source ble, got %s", evt.Source)
	}
}

func TestWithContextNilPeer(t *testing.T) {
	pid, _, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	evt := WithContext(AuditEvent{Type: AuditConnection, PeerID: pid}, nil)
	if evt.Details == nil {
		t.Fatal("expected non-nil details map")
	}
	if evt.Source != "" {
		t.Fatalf("expected empty source, got %s", evt.Source)
	}
}

func TestMarshalEntriesRoundTrip(t *testing.T) {
	log := NewAuditLog()
	pid, _, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	_ = log.Log(AuditEvent{Type: AuditConnection, PeerID: pid, Source: "tcp"})
	_ = log.Log(AuditEvent{Type: AuditAuthSuccess, PeerID: pid, Source: "noise"})

	data, err := log.MarshalEntries()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	log2 := NewAuditLog()
	if err := log2.UnmarshalEntries(data); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if log2.Len() != 2 {
		t.Fatalf("expected 2 entries, got %d", log2.Len())
	}
	if err := log2.VerifyIntegrity(); err != nil {
		t.Fatalf("integrity after unmarshal: %v", err)
	}
}

func TestAuditLogEmptyIntegrity(t *testing.T) {
	log := NewAuditLog()
	if err := log.VerifyIntegrity(); err != nil {
		t.Fatalf("empty log should be valid: %v", err)
	}
}

func TestAuditEventMarshalJSON(t *testing.T) {
	pid, _, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	evt := AuditEvent{
		Type:      AuditConnection,
		Timestamp: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		PeerID:    pid,
		Source:    "tcp",
		Details:   map[string]string{"addr": "1.2.3.4"},
	}
	b, err := evt.MarshalJSON()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(b)
	if !strings.Contains(s, "connection") {
		t.Fatal("missing type")
	}
	if !strings.Contains(s, "addr") {
		t.Fatal("missing details")
	}
}
