package transport

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/rs/zerolog/log"

	"github.com/ram1234598766-dotcom/Local-WEB/pkg/crypto"
)

// HybridHandshakeConfig holds configuration for hybrid handshake behavior.
type HybridHandshakeConfig struct {
	// Timeout for individual handshake messages
	MessageTimeout time.Duration
	// Maximum retries for handshake
	MaxRetries int
	// Enable PQ-only mode (no classical X25519)
	PQOnly bool
	// Custom entropy source for Kyber operations
	EntropySource io.Reader
}

var DefaultHybridConfig = HybridHandshakeConfig{
	MessageTimeout: 10 * time.Second,
	MaxRetries:     3,
	EntropySource:  rand.Reader,
}

// HybridConnectionState tracks the state of a hybrid handshake.
type HybridConnectionState struct {
	mu           sync.Mutex
	RemoteAddr   net.Addr
	LocalAddr    net.Addr
	StartTime    time.Time
	HandshakeErr error
	Complete     bool
	Retries      int
}

func NewHybridConnectionState(localAddr, remoteAddr net.Addr) *HybridConnectionState {
	log.Debug().
		Str("local", localAddr.String()).
		Str("remote", remoteAddr.String()).
		Msg("new hybrid connection state")
	return &HybridConnectionState{
		RemoteAddr: remoteAddr,
		LocalAddr:  localAddr,
		StartTime:  time.Now(),
	}
}

func (hcs *HybridConnectionState) MarkComplete() {
	hcs.mu.Lock()
	defer hcs.mu.Unlock()
	hcs.Complete = true
}

func (hcs *HybridConnectionState) SetError(err error) {
	hcs.mu.Lock()
	defer hcs.mu.Unlock()
	hcs.HandshakeErr = err
}

func (hcs *HybridConnectionState) IncrementRetries() int {
	hcs.mu.Lock()
	defer hcs.mu.Unlock()
	hcs.Retries++
	return hcs.Retries
}

func (hcs *HybridConnectionState) GetRetries() int {
	hcs.mu.Lock()
	defer hcs.mu.Unlock()
	return hcs.Retries
}

func (hcs *HybridConnectionState) IsComplete() bool {
	hcs.mu.Lock()
	defer hcs.mu.Unlock()
	return hcs.Complete
}

func (hcs *HybridConnectionState) GetError() error {
	hcs.mu.Lock()
	defer hcs.mu.Unlock()
	return hcs.HandshakeErr
}

// HybridKeyDerivation provides additional key derivation functions
// for the hybrid handshake context.
type HybridKeyDerivation struct {
	mu sync.Mutex
}

func NewHybridKeyDerivation() *HybridKeyDerivation {
	return &HybridKeyDerivation{}
}

// DeriveTransportKey derives a transport encryption key from the hybrid session key.
func (hkd *HybridKeyDerivation) DeriveTransportKey(sessionKey [32]byte, context string) [32]byte {
	hkd.mu.Lock()
	defer hkd.mu.Unlock()

	var key [32]byte
	copy(key[:], sessionKey[:])
	// In production, use HKDF with context
	_ = context
	return key
}

const (
	hybridHandshakeStream = "hybrid-handshake"
	hybridKyberCtSize     = 1568 // Kyber-1024 ciphertext size
)

// HybridServer wraps Server with hybrid handshake support.
type HybridServer struct {
	*Server
	useHybrid bool
}

func NewHybridServer(ctx context.Context, addr string, pub, priv [32]byte, useHybrid bool) (*HybridServer, error) {
	s, err := NewServer(ctx, addr, pub, priv)
	if err != nil {
		return nil, err
	}
	return &HybridServer{Server: s, useHybrid: useHybrid}, nil
}

func (s *HybridServer) dialNoise(qc *quic.Conn) ([32]byte, error) {
	if s.useHybrid {
		return s.dialHybrid(qc)
	}
	return s.Server.dialNoise(qc)
}

func (s *HybridServer) noiseHandshake(qc *quic.Conn) ([32]byte, error) {
	if s.useHybrid {
		return s.hybridHandshake(qc)
	}
	return s.Server.noiseHandshake(qc)
}

func (s *HybridServer) dialHybrid(qc *quic.Conn) ([32]byte, error) {
	session, err := crypto.NewHybridInitiator(s.pubKey, s.privKey)
	if err != nil {
		return [32]byte{}, err
	}

	stream, err := qc.OpenStreamSync(s.ctx)
	if err != nil {
		return [32]byte{}, fmt.Errorf("open hybrid stream: %w", err)
	}

	// -> e + Kyber ct
	first, _, done, err := session.WriteHandshake(nil)
	if err != nil {
		return [32]byte{}, err
	}
	_ = done

	if _, err := stream.Write(first); err != nil {
		return [32]byte{}, fmt.Errorf("write hybrid init: %w", err)
	}

	// <- e, ee, s, es + Kyber ct
	respBuf := make([]byte, hybridKyberCtSize+80) // 736 + 80
	if _, err := readFull(stream, respBuf); err != nil {
		return [32]byte{}, fmt.Errorf("read hybrid response: %w", err)
	}

	toSend, _, _, err := session.WriteHandshake(respBuf)
	if err != nil {
		return [32]byte{}, err
	}

	if len(toSend) > 0 {
		if _, err := stream.Write(toSend); err != nil {
			return [32]byte{}, fmt.Errorf("write hybrid final: %w", err)
		}
	}

	// Wait for responder to close
	if _, err := readUntilEOF(stream); err != nil && !errors.Is(err, io.EOF) {
		return [32]byte{}, fmt.Errorf("wait for responder close: %w", err)
	}

	return crypto.NodeID(session.RemotePublic()), nil
}

func (s *HybridServer) hybridHandshake(qc *quic.Conn) ([32]byte, error) {
	session, err := crypto.NewHybridResponder(s.pubKey, s.privKey)
	if err != nil {
		return [32]byte{}, err
	}

	stream, err := qc.AcceptStream(s.ctx)
	if err != nil {
		return [32]byte{}, fmt.Errorf("accept hybrid stream: %w", err)
	}
	defer stream.Close()

	// Read initiator's first message: e + Kyber ct
	first := make([]byte, hybridKyberCtSize+32) // 736 + 32
	if _, err := readFull(stream, first); err != nil {
		return [32]byte{}, fmt.Errorf("read hybrid first: %w", err)
	}

	next, _, done, err := session.WriteHandshake(first)
	if err != nil {
		return [32]byte{}, err
	}
	_ = done

	// Send responder message: e, ee, s, es + Kyber ct
	if len(next) > 0 {
		if _, err := stream.Write(next); err != nil {
			return [32]byte{}, fmt.Errorf("write hybrid response: %w", err)
		}
	}

	// Read the initiator's final -> s, se (encrypted static: 32 + AEAD tag)
	final := make([]byte, 32+16)
	if _, err := readFull(stream, final); err != nil {
		return [32]byte{}, fmt.Errorf("read hybrid final: %w", err)
	}

	// Complete the responder handshake
	if _, _, _, err := session.WriteHandshake(final); err != nil {
		return [32]byte{}, err
	}

	stream.Close()

	return crypto.NodeID(session.RemotePublic()), nil
}

func init() {
	// Register hybrid handshake capability
	// This is a placeholder for future capability negotiation
}
