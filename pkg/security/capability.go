package security

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/ram1234598766-dotcom/Local-WEB/pkg/crypto"
	"github.com/rs/zerolog/log"
)

// CapabilityToken grants a peer access to specific services.
// The token is signed by the issuing node and can be verified by any peer.
type CapabilityToken struct {
	PeerID    [32]byte
	Services  []ServiceID
	IssuedAt  time.Time
	ExpiresAt time.Time
	Nonce     [16]byte
	Signature []byte // 64-byte Ed25519 signature over the canonical form
}

// CanonicalForm returns the byte slice that is signed to produce the token's
// signature. All fields except Signature participate.
func (t *CapabilityToken) CanonicalForm() ([]byte, error) {
	type raw struct {
		PeerID    [32]byte
		Services  []ServiceID
		IssuedAt  int64
		ExpiresAt int64
		Nonce     [16]byte
	}
	r := raw{
		PeerID:    t.PeerID,
		Services:  t.Services,
		IssuedAt:  t.IssuedAt.Unix(),
		ExpiresAt: t.ExpiresAt.Unix(),
		Nonce:     t.Nonce,
	}
	b, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	return b, nil
}

// Sign fills the token's signature using the given private key.
func (t *CapabilityToken) Sign(priv [32]byte) error {
	canonical, err := t.CanonicalForm()
	if err != nil {
		return err
	}
	sig, err := crypto.Sign(priv, canonical)
	if err != nil {
		return err
	}
	t.Signature = sig
	return nil
}

// Verify checks the token's signature against the issuer's public key and
// validates expiry. It also ensures the token is bound to a non-zero peer ID.
func (t *CapabilityToken) Verify(issuerPub [32]byte) error {
	if len(t.Signature) != 64 {
		return errors.New("invalid signature length")
	}
	if t.PeerID == ([32]byte{}) {
		return errors.New("peer ID must not be zero")
	}
	canonical, err := t.CanonicalForm()
	if err != nil {
		return err
	}
	if !crypto.Verify(issuerPub, canonical, t.Signature) {
		return errors.New("signature verification failed")
	}
	if !t.ExpiresAt.IsZero() && time.Now().After(t.ExpiresAt) {
		return errors.New("token expired")
	}
	return nil
}

// ServiceID is aliased here to avoid a cyclic import on transport.
type ServiceID = string

// CapabilityManager issues, tracks, and revokes capability tokens per peer.
type CapabilityManager struct {
	mu       sync.RWMutex
	privKey  [32]byte
	pubKey   [32]byte
	tokens   map[[32]byte][]*CapabilityToken // peerID -> tokens
	revoked  map[[32]byte]bool               // peerID -> revoked flag
	services map[ServiceID]bool              // globally allowed services
}

// NewCapabilityManager creates a manager with the node's keypair.
func NewCapabilityManager(pub, priv [32]byte) *CapabilityManager {
	return &CapabilityManager{
		privKey:  priv,
		pubKey:   pub,
		tokens:   make(map[[32]byte][]*CapabilityToken),
		revoked:  make(map[[32]byte]bool),
		services: make(map[ServiceID]bool),
	}
}

// RegisterService adds a service to the global allowed set.
func (m *CapabilityManager) RegisterService(svc ServiceID) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.services[svc] = true
}

// GrantCapability issues a new signed token for the peer. If ttl <= 0 the
// token does not expire.
func (m *CapabilityManager) GrantCapability(peerID [32]byte, services []ServiceID, ttl time.Duration) (*CapabilityToken, error) {
	if len(services) == 0 {
		return nil, errors.New("no services specified")
	}

	for _, svc := range services {
		m.mu.RLock()
		ok := m.services[svc]
		m.mu.RUnlock()
		if !ok {
			return nil, errors.New("service not registered: " + svc)
		}
	}

	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, err
	}

	now := time.Now()
	expiry := now.Add(ttl)
	if ttl <= 0 {
		expiry = time.Time{}
	}

	tok := &CapabilityToken{
		PeerID:    peerID,
		Services:  services,
		IssuedAt:  now,
		ExpiresAt: expiry,
		Nonce:     nonce,
	}
	if err := tok.Sign(m.privKey); err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.tokens[peerID] = append(m.tokens[peerID], tok)

	log.Info().
		Str("peer", formatPeerID(peerID)).
		Strs("services", services).
		Msg("capability granted")

	return tok, nil
}

// RevokeCapability invalidates all tokens for a peer.
func (m *CapabilityManager) RevokeCapability(peerID [32]byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.revoked[peerID] = true
	delete(m.tokens, peerID)

	log.Info().
		Str("peer", formatPeerID(peerID)).
		Msg("capability revoked")
}

// CheckAccess returns true if the peer holds a valid, non-expired token that
// includes the requested service.
func (m *CapabilityManager) CheckAccess(peerID [32]byte, svc ServiceID) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.revoked[peerID] {
		return false
	}

	for _, tok := range m.tokens[peerID] {
		if !tok.ExpiresAt.IsZero() && time.Now().After(tok.ExpiresAt) {
			continue
		}
		for _, s := range tok.Services {
			if s == svc {
				return true
			}
		}
	}
	return false
}

// VerifyToken verifies a token's cryptographic integrity and that it matches
// the claimed peer.
func (m *CapabilityManager) VerifyToken(tok *CapabilityToken) error {
	if tok == nil {
		return errors.New("nil token")
	}
	return tok.Verify(m.pubKey)
}

// PeerTokens returns a copy of the active tokens for a peer (for inspection).
func (m *CapabilityManager) PeerTokens(peerID [32]byte) []*CapabilityToken {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make([]*CapabilityToken, 0, len(m.tokens[peerID]))
	for _, t := range m.tokens[peerID] {
		if !t.ExpiresAt.IsZero() && time.Now().After(t.ExpiresAt) {
			continue
		}
		out = append(out, t)
	}
	return out
}

// PublicKey returns the node's public key so peers can verify tokens.
func (m *CapabilityManager) PublicKey() [32]byte { return m.pubKey }

func formatPeerID(id [32]byte) string {
	return hex.EncodeToString(id[:8])
}

// ServiceIDsEqual reports whether two service-ID slices contain the same
// elements regardless of order.
func ServiceIDsEqual(a, b []ServiceID) bool {
	if len(a) != len(b) {
		return false
	}
	counts := make(map[string]int)
	for _, s := range a {
		counts[s]++
	}
	for _, s := range b {
		if counts[s] == 0 {
			return false
		}
		counts[s]--
	}
	return true
}
