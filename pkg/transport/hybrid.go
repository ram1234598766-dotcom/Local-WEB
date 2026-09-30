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
	"golang.org/x/crypto/hkdf"
	"golang.org/x/crypto/sha3"

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

// DeriveTransportKey derives a purpose-specific key from the hybrid session
// key using HKDF-SHA3-256 with the context string as the info parameter.
//
// Domain separation by context is the point: a key derived for one service
// must not be usable for another. This previously returned its input
// unchanged, which meant the Kyber contribution reached nothing.
func (hkd *HybridKeyDerivation) DeriveTransportKey(sessionKey [32]byte, context string) [32]byte {
	hkd.mu.Lock()
	defer hkd.mu.Unlock()

	r := hkdf.New(sha3.New256, sessionKey[:], nil,
		append([]byte("LocalWEB-v2-transport"), []byte(context)...))
	var key [32]byte
	if _, err := io.ReadFull(r, key[:]); err != nil {
		// HKDF never fails on a 32-byte read with a SHA3-256 PRF, but a
		// zero key would be a silent security failure if that ever changed.
		log.Error().Err(err).Msg("HKDF transport key derivation failed")
		return [32]byte{}
	}
	return key
}

const (
	hybridHandshakeStream = "hybrid-handshake"
	// hybridKyberCtSize is the Kyber-1024 ciphertext size carried in msg2.
	hybridKyberCtSize = 1568
	// hybridKyberPubSize is the Kyber-1024 public key size carried in msg1.
	hybridKyberPubSize = 1568
	// hybridMsg1Size is msg1: initiator PQ public key + Noise "-> e".
	hybridMsg1Size = hybridKyberPubSize + 32
	// hybridMsg2Size is msg2: Kyber ciphertext + Noise "<- e, ee, s, es".
	hybridMsg2Size = hybridKyberCtSize + 32 + 32 + 16
	// hybridMsg3Size is msg3: Noise "-> s, se" (encrypted static + AEAD tag).
	hybridMsg3Size = 32 + 16
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

func (s *HybridServer) dialNoise(qc *quic.Conn) (HandshakeResult, error) {
	if s.useHybrid {
		return s.dialHybrid(qc)
	}
	return s.Server.dialNoise(qc)
}

func (s *HybridServer) noiseHandshake(qc *quic.Conn) (HandshakeResult, error) {
	if s.useHybrid {
		return s.hybridHandshake(qc)
	}
	return s.Server.noiseHandshake(qc)
}

func (s *HybridServer) dialHybrid(qc *quic.Conn) (HandshakeResult, error) {
	session, err := crypto.NewHybridInitiator(s.pubKey, s.privKey)
	if err != nil {
		return HandshakeResult{}, err
	}

	stream, err := qc.OpenStreamSync(s.ctx)
	if err != nil {
		return HandshakeResult{}, fmt.Errorf("open hybrid stream: %w", err)
	}

	// msg1: initiator PQ public key + Noise "-> e"
	first, _, done, err := session.WriteHandshake(nil)
	if err != nil {
		return HandshakeResult{}, err
	}
	_ = done
	if len(first) != hybridMsg1Size {
		return HandshakeResult{}, fmt.Errorf("unexpected hybrid msg1 size %d, want %d", len(first), hybridMsg1Size)
	}

	if _, err := stream.Write(first); err != nil {
		return HandshakeResult{}, fmt.Errorf("write hybrid init: %w", err)
	}

	// msg2: Kyber ciphertext + Noise "<- e, ee, s, es"
	respBuf := make([]byte, hybridMsg2Size)
	if _, err := readFull(stream, respBuf); err != nil {
		return HandshakeResult{}, fmt.Errorf("read hybrid response: %w", err)
	}

	toSend, _, _, err := session.WriteHandshake(respBuf)
	if err != nil {
		return HandshakeResult{}, err
	}
	if len(toSend) != hybridMsg3Size {
		return HandshakeResult{}, fmt.Errorf("unexpected hybrid msg3 size %d, want %d", len(toSend), hybridMsg3Size)
	}

	if _, err := stream.Write(toSend); err != nil {
		return HandshakeResult{}, fmt.Errorf("write hybrid final: %w", err)
	}

	// Wait for responder to close
	if _, err := readUntilEOF(stream); err != nil && !errors.Is(err, io.EOF) {
		return HandshakeResult{}, fmt.Errorf("wait for responder close: %w", err)
	}

	return handshakeResult(session), nil
}

func (s *HybridServer) hybridHandshake(qc *quic.Conn) (HandshakeResult, error) {
	session, err := crypto.NewHybridResponder(s.pubKey, s.privKey)
	if err != nil {
		return HandshakeResult{}, err
	}

	stream, err := qc.AcceptStream(s.ctx)
	if err != nil {
		return HandshakeResult{}, fmt.Errorf("accept hybrid stream: %w", err)
	}
	defer stream.Close()

	// msg1: initiator PQ public key + Noise "-> e"
	first := make([]byte, hybridMsg1Size)
	if _, err := readFull(stream, first); err != nil {
		return HandshakeResult{}, fmt.Errorf("read hybrid first: %w", err)
	}

	next, _, done, err := session.WriteHandshake(first)
	if err != nil {
		return HandshakeResult{}, err
	}
	_ = done
	if len(next) != hybridMsg2Size {
		return HandshakeResult{}, fmt.Errorf("unexpected hybrid msg2 size %d, want %d", len(next), hybridMsg2Size)
	}

	// msg2: Kyber ciphertext + Noise "<- e, ee, s, es"
	if _, err := stream.Write(next); err != nil {
		return HandshakeResult{}, fmt.Errorf("write hybrid response: %w", err)
	}

	// msg3: Noise "-> s, se"
	final := make([]byte, hybridMsg3Size)
	if _, err := readFull(stream, final); err != nil {
		return HandshakeResult{}, fmt.Errorf("read hybrid final: %w", err)
	}

	// Complete the responder handshake
	if _, _, complete, err := session.WriteHandshake(final); err != nil {
		return HandshakeResult{}, err
	} else if !complete {
		return HandshakeResult{}, errors.New("hybrid handshake did not complete")
	}

	stream.Close()

	return handshakeResult(session), nil
}

func init() {
	// Register hybrid handshake capability
	// This is a placeholder for future capability negotiation
}
