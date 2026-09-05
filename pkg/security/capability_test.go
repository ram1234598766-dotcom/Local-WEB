package security

import (
	"bytes"
	"testing"
	"time"

	"github.com/ram1234598766-dotcom/Local-WEB/pkg/crypto"
	"github.com/rs/zerolog/log"
)

func init() {
	log.Logger = log.Output(nil)
}

func TestCapabilityTokenSignVerify(t *testing.T) {
	pub, priv, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}

	tok := &CapabilityToken{
		PeerID:    pub,
		Services:  []ServiceID{"http", "dns"},
		IssuedAt:  time.Now(),
		ExpiresAt: time.Now().Add(time.Hour),
	}
	var nonce [16]byte
	copy(nonce[:], []byte("testnonce12345678"))
	tok.Nonce = nonce

	if err := tok.Sign(priv); err != nil {
		t.Fatalf("sign: %v", err)
	}
	if len(tok.Signature) != 64 {
		t.Fatalf("expected 64-byte signature, got %d", len(tok.Signature))
	}
	if err := tok.Verify(pub); err != nil {
		t.Fatalf("verify: %v", err)
	}
}

func TestCapabilityTokenExpiry(t *testing.T) {
	pub, priv, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}

	tok := &CapabilityToken{
		PeerID:    pub,
		Services:  []ServiceID{"http"},
		IssuedAt:  time.Now(),
		ExpiresAt: time.Now().Add(-time.Minute),
	}
	var nonce [16]byte
	copy(nonce[:], []byte("expirednonce12345"))
	tok.Nonce = nonce
	tok.Sign(priv)

	if err := tok.Verify(pub); err == nil {
		t.Fatal("expected expiry error")
	}
}

func TestCapabilityTokenBadSignature(t *testing.T) {
	pub, _, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}

	tok := &CapabilityToken{
		PeerID:    pub,
		Services:  []ServiceID{"http"},
		IssuedAt:  time.Now(),
		ExpiresAt: time.Now().Add(time.Hour),
	}
	var nonce [16]byte
	copy(nonce[:], []byte("badnonce12345678"))
	tok.Nonce = nonce
	tok.Signature = make([]byte, 64)

	if err := tok.Verify(pub); err == nil {
		t.Fatal("expected signature error")
	}
}

func TestCapabilityManagerGrantCheckRevoke(t *testing.T) {
	pub, priv, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}

	mgr := NewCapabilityManager(pub, priv)
	mgr.RegisterService("http")
	mgr.RegisterService("dns")

	peer, _, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}

	tok, err := mgr.GrantCapability(peer, []ServiceID{"http", "dns"}, time.Hour)
	if err != nil {
		t.Fatalf("grant: %v", err)
	}
	if err := mgr.VerifyToken(tok); err != nil {
		t.Fatalf("verify token: %v", err)
	}
	if !mgr.CheckAccess(peer, "http") {
		t.Fatal("expected http access")
	}
	if !mgr.CheckAccess(peer, "dns") {
		t.Fatal("expected dns access")
	}
	if mgr.CheckAccess(peer, "relay") {
		t.Fatal("did not expect relay access")
	}

	mgr.RevokeCapability(peer)
	if mgr.CheckAccess(peer, "http") {
		t.Fatal("expected access denied after revoke")
	}
}

func TestCapabilityManagerNoServices(t *testing.T) {
	pub, priv, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	mgr := NewCapabilityManager(pub, priv)

	peer, _, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.GrantCapability(peer, nil, time.Hour); err == nil {
		t.Fatal("expected error for nil services")
	}
}

func TestServiceIDsEqual(t *testing.T) {
	if !ServiceIDsEqual([]ServiceID{"a", "b"}, []ServiceID{"b", "a"}) {
		t.Fatal("expected equal")
	}
	if ServiceIDsEqual([]ServiceID{"a"}, []ServiceID{"b"}) {
		t.Fatal("expected not equal")
	}
	if ServiceIDsEqual([]ServiceID{"a", "a"}, []ServiceID{"a"}) {
		t.Fatal("expected not equal for different lengths")
	}
}

func TestPeerTokens(t *testing.T) {
	pub, priv, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	mgr := NewCapabilityManager(pub, priv)
	mgr.RegisterService("http")

	peer, _, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.GrantCapability(peer, []ServiceID{"http"}, time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.GrantCapability(peer, []ServiceID{"dns"}, time.Hour); err == nil {
		// expected because dns not registered
	}

	toks := mgr.PeerTokens(peer)
	if len(toks) != 1 {
		t.Fatalf("expected 1 token, got %d", len(toks))
	}
}

func TestCanonicalFormStable(t *testing.T) {
	pub, _, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	tok := &CapabilityToken{
		PeerID:    pub,
		Services:  []ServiceID{"http"},
		IssuedAt:  time.Unix(1000, 0),
		ExpiresAt: time.Unix(2000, 0),
	}
	var nonce [16]byte
	copy(nonce[:], []byte("nonce1234567890"))
	tok.Nonce = nonce

	a, _ := tok.CanonicalForm()
	tok.Services = []ServiceID{"dns"}
	b, _ := tok.CanonicalForm()
	if bytes.Equal(a, b) {
		t.Fatal("canonical form changed after mutating services")
	}
}
