package transport

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"sync"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/crypto"
	"github.com/rs/zerolog/log"
)

// Server is a QUIC server that multiplexes multiple services over streams.
// It uses quic-go for the underlying QUIC transport (RFC 9000) and verifies
// peer identity via the Noise protocol layer above TLS.
type Server struct {
	mu          sync.RWMutex
	addr        string
	tr          *quic.Transport
	ln          *quic.Listener
	conns       map[[32]byte]*Connection
	handlers    map[ServiceID]StreamHandler
	ctx         context.Context
	cancel      context.CancelFunc
	pubKey      [32]byte
	privKey     [32]byte
	flow        FlowControl
	relay       *Relay
	started     time.Time
	stats       ServerStats
	wg          sync.WaitGroup
	enforceTLS  bool
	allowedSvcs map[ServiceID]bool
}

// ServerStats tracks server-level statistics.
type ServerStats struct {
	TotalConns    uint64
	ActiveConns   uint64
	TotalStreams  uint64
	TotalBytesIn  uint64
	TotalBytesOut uint64
	TotalRelays   uint64
}

// ServerOption configures a Server.
type ServerOption func(*Server)

// WithEnforceTLSVerify controls server certificate verification in the QUIC
// TLS handshake. By default (false), InsecureSkipVerify is set because peer
// identity is authenticated via the Noise XX protocol layer — the TLS layer
// provides encryption and integrity, while Noise provides mutual authentication.
// Set to true only when you have a PKI or certificate pinning to enforce.
func WithEnforceTLSVerify(v bool) ServerOption {
	return func(s *Server) { s.enforceTLS = v }
}

// WithAllowedServices restricts the service IDs a peer may access after Noise
// authentication. An empty set (default) means all registered services are
// allowed. Use to implement per-peer ACLs.
func WithAllowedServices(svcs map[ServiceID]bool) ServerOption {
	return func(s *Server) { s.allowedSvcs = svcs }
}

// NewServer creates a QUIC server bound to addr using quic-go.
func NewServer(ctx context.Context, addr string, pubKey, privKey [32]byte, opts ...ServerOption) (*Server, error) {
	// Generate a self-signed TLS certificate for the QUIC handshake.
	// Peer identity is separately authenticated via Noise XX, so the
	// QUIC TLS layer provides transport encryption + integrity.
	cert, err := GenerateSelfSignedCert()
	if err != nil {
		return nil, fmt.Errorf("generate cert: %w", err)
	}

	tlsCfg := &tls.Config{
		MinVersion:   tls.VersionTLS13,
		NextProtos:   []string{"localweb/1.0"},
		Certificates: []tls.Certificate{cert},
	}

	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, err
	}
	udpConn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		return nil, err
	}

	quicCfg := &quic.Config{
		MaxIncomingStreams: 32,
		MaxIdleTimeout:     30 * time.Second,
	}

	tr := &quic.Transport{Conn: udpConn}
	ln, err := tr.Listen(tlsCfg, quicCfg)
	if err != nil {
		udpConn.Close()
		return nil, fmt.Errorf("quic listen: %w", err)
	}

	ctx, cancel := context.WithCancel(ctx)
	s := &Server{
		addr:     addr,
		tr:       tr,
		ln:       ln,
		conns:    make(map[[32]byte]*Connection),
		handlers: make(map[ServiceID]StreamHandler),
		ctx:      ctx,
		cancel:   cancel,
		pubKey:   pubKey,
		privKey:  privKey,
		flow:     DefaultFlowControl(),
		started:  time.Now(),
	}

	for _, opt := range opts {
		opt(s)
	}

	// Initialize the circuit relay
	s.relay = NewRelay(ctx)

	// Start the accept loop
	go s.acceptLoop()

	log.Info().Str("addr", addr).Msg("transport server started")
	return s, nil
}

// RegisterHandler associates a service ID with a stream handler.
func (s *Server) RegisterHandler(svc ServiceID, handler StreamHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers[svc] = handler
	log.Info().Str("service", string(svc)).Msg("service handler registered")
}

// acceptLoop accepts incoming QUIC connections.
func (s *Server) acceptLoop() {
	for {
		conn, err := s.ln.Accept(s.ctx)
		if err != nil {
			if s.ctx.Err() != nil {
				return
			}
			log.Warn().Err(err).Msg("accept failed")
			time.Sleep(100 * time.Millisecond)
			continue
		}

		s.mu.Lock()
		s.stats.TotalConns++
		s.mu.Unlock()

		go s.handleConn(conn)
	}
}

// handleConn manages a single QUIC connection.
func (s *Server) handleConn(qc *quic.Conn) {
	defer qc.CloseWithError(quic.ApplicationErrorCode(0x01), "closing")
	s.wg.Add(1)
	defer s.wg.Done()

	// Run the Noise XX handshake over the first stream to establish
	// peer identity before multiplexing services.
	peerID, err := s.noiseHandshake(qc)
	if err != nil {
		log.Warn().Err(err).Msg("noise handshake failed")
		return
	}

	conn := &Connection{
		handle:          qc,
		peerID:          peerID,
		addr:            qc.RemoteAddr().String(),
		state:           StateReady,
		services:        make(map[ServiceID]bool),
		server:          s,
		lastSeen:        time.Now(),
		allowedServices: s.allowedSvcs,
	}

	s.mu.Lock()
	s.stats.ActiveConns++
	s.conns[peerID] = conn
	s.mu.Unlock()

	// Accept and dispatch streams
	for {
		stream, err := qc.AcceptStream(s.ctx)
		if err != nil {
			break
		}

		// Read the service ID (first byte on every stream) — loop until
		// at least 1 byte is read; 0 bytes is not a valid service ID.
		svcBuf := make([]byte, 1)
		var svcReadErr error
		for {
			n, err := stream.Read(svcBuf)
			if err != nil {
				svcReadErr = err
				break
			}
			if n > 0 {
				break
			}
		}
		if svcReadErr != nil {
			stream.Close()
			continue
		}

		svcID := ServiceID(svcBuf[0])

		s.mu.RLock()
		handler, ok := s.handlers[svcID]
		s.mu.RUnlock()

		if !ok {
			log.Warn().Str("service", string(svcID)).Msg("unknown service, closing stream")
			stream.Close()
			continue
		}

		// ACL check: if allowedServices is non-empty, the peer must have
		// the requested service in its allowed set.
		if len(conn.allowedServices) > 0 && !conn.allowedServices[svcID] {
			log.Warn().Str("service", string(svcID)).Msg("service not allowed for peer, closing stream")
			stream.Close()
			continue
		}

		s.mu.Lock()
		s.stats.TotalStreams++
		s.mu.Unlock()

		go func(ctx context.Context, stm Stream, sid ServiceID) {
			defer func() {
				if r := recover(); r != nil {
					log.Error().Interface("panic", r).Str("service", string(sid)).Msg("handler panic, closing stream")
					stm.Close()
				}
			}()
			handler(ctx, stm)
		}(s.ctx, newQuicStream(stream, svcID, conn.peerID), svcID)
	}

	s.mu.Lock()
	s.stats.ActiveConns--
	delete(s.conns, peerID)
	s.mu.Unlock()

	log.Info().Str("peer", fmt.Sprintf("%x", peerID[:8])).Msg("connection closed")
}

// noiseHandshake performs a Noise XX handshake on the first stream
// opened by the peer and returns the authenticated peer ID.
func (s *Server) noiseHandshake(qc *quic.Conn) ([32]byte, error) {
	session, err := crypto.NewNoiseResponder(s.pubKey, s.privKey)
	if err != nil {
		return [32]byte{}, err
	}

	stream, err := qc.AcceptStream(s.ctx)
	if err != nil {
		return [32]byte{}, fmt.Errorf("accept noise stream: %w", err)
	}
	defer stream.Close()

	// Read initiator's first message: -> e (32 bytes)
	first := make([]byte, 32)
	if _, err := readFull(stream, first); err != nil {
		return [32]byte{}, fmt.Errorf("read noise first: %w", err)
	}

	next, _, done, err := session.WriteHandshake(first)
	if err != nil {
		return [32]byte{}, err
	}
	_ = done

	// Send responder message: <- e, ee, s, es
	if len(next) > 0 {
		if _, err := stream.Write(next); err != nil {
			return [32]byte{}, fmt.Errorf("write noise response: %w", err)
		}
	}

	// Read the initiator's final -> s, se (encrypted static: 32 + AEAD tag).
	final := make([]byte, 32+16)
	if _, err := readFull(stream, final); err != nil {
		return [32]byte{}, fmt.Errorf("read noise final: %w", err)
	}

	// Complete the responder handshake, authenticating the initiator's
	// static public key.
	if _, _, _, err := session.WriteHandshake(final); err != nil {
		return [32]byte{}, err
	}

	// Close the temporary handshake stream.
	stream.Close()

	// Authenticate by the peer's NodeID (hash of its Noise static key),
	// consistent with how the orchestrator derives and compares peer IDs.
	return crypto.NodeID(session.RemotePublic()), nil
}

// readFull reads len(p) bytes from a stream.
func readFull(stream *quic.Stream, p []byte) (int, error) {
	n := 0
	for n < len(p) {
		m, err := stream.Read(p[n:])
		n += m
		if err != nil {
			return n, err
		}
	}
	return n, nil
}

// readUntilEOF drains a stream until the peer closes its write side.
// It returns the total bytes read (ignored by callers) and any error.
// io.EOF is returned as-is (peer closed cleanly); any other error is
// propagated (e.g. stream reset, connection drop).
func readUntilEOF(stream *quic.Stream) (int, error) {
	buf := make([]byte, 4096)
	total := 0
	for {
		n, err := stream.Read(buf)
		total += n
		if err != nil {
			if errors.Is(err, io.EOF) {
				return total, err
			}
			return total, err
		}
	}
}

// Connect establishes a new QUIC connection to a peer.
func (s *Server) Connect(ctx context.Context, addr string, peerID [32]byte) (*Connection, error) {
	// Check existing connection
	s.mu.RLock()
	if c, ok := s.conns[peerID]; ok && c.state != StateClosed {
		s.mu.RUnlock()
		return c, nil
	}
	s.mu.RUnlock()

	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, err
	}

	cert, err := GenerateSelfSignedCert()
	if err != nil {
		return nil, fmt.Errorf("generate cert: %w", err)
	}

	host, _, _ := net.SplitHostPort(addr)
	tlsCfg := &tls.Config{
		MinVersion:         tls.VersionTLS13,
		NextProtos:         []string{"localweb/1.0"},
		Certificates:       []tls.Certificate{cert},
		InsecureSkipVerify: !s.enforceTLS,
		ServerName:         host,
	}

	qc, err := s.tr.Dial(ctx, udpAddr, tlsCfg, nil)
	if err != nil {
		return nil, fmt.Errorf("dial quic: %w", err)
	}

	// Perform the Noise XX handshake as initiator.
	peerActual, err := s.dialNoise(qc)
	if err != nil {
		qc.CloseWithError(quic.ApplicationErrorCode(0x02), "noise handshake failed")
		return nil, fmt.Errorf("noise handshake: %w", err)
	}

	// Verify the peer's NodeID matches the expected identity.
	if peerActual != peerID {
		qc.CloseWithError(quic.ApplicationErrorCode(0x03), "peer identity mismatch")
		return nil, fmt.Errorf("peer identity mismatch: got %x want %x", peerActual[:8], peerID[:8])
	}

	c := &Connection{
		handle:   qc,
		addr:     addr,
		peerID:   peerID,
		state:    StateReady,
		services: make(map[ServiceID]bool),
		server:   s,
		lastSeen: time.Now(),
	}

	s.mu.Lock()
	s.conns[peerID] = c
	s.stats.TotalConns++
	s.stats.ActiveConns++
	s.mu.Unlock()

	return c, nil
}

// dialNoise performs the Noise XX handshake as initiator.
func (s *Server) dialNoise(qc *quic.Conn) ([32]byte, error) {
	session, err := crypto.NewNoiseInitiator(s.pubKey, s.privKey)
	if err != nil {
		return [32]byte{}, err
	}

	stream, err := qc.OpenStreamSync(s.ctx)
	if err != nil {
		return [32]byte{}, fmt.Errorf("open noise stream: %w", err)
	}

	// -> e (send ephemeral public key)
	first, _, done, err := session.WriteHandshake(nil)
	if err != nil {
		return [32]byte{}, err
	}
	_ = done

	if _, err := stream.Write(first); err != nil {
		return [32]byte{}, fmt.Errorf("write noise init: %w", err)
	}

	// <- e, ee, s, es (read responder message)
	//
	// The responder message is deterministic for our fixed 32-byte statics:
	// responder ephemeral (32) + encrypted responder static (32 + 16 AEAD tag)
	// = 80 bytes.
	respBuf := make([]byte, 32+48)
	if _, err := readFull(stream, respBuf); err != nil {
		return [32]byte{}, fmt.Errorf("read noise response: %w", err)
	}

	// Process <- e, ee, s, es and produce -> s, se. WriteHandshake returns
	// the final initiator message as toSend; it MUST be written to the
	// stream so the responder can complete its side of the handshake.
	toSend, _, _, err := session.WriteHandshake(respBuf)
	if err != nil {
		return [32]byte{}, err
	}

	if len(toSend) > 0 {
		if _, err := stream.Write(toSend); err != nil {
			return [32]byte{}, fmt.Errorf("write noise final: %w", err)
		}
	}

	// Synchronize: wait for the responder to close the handshake stream,
	// which it does only after fully consuming msg3 and completing its
	// side of the XX handshake. This guarantees msg3 delivery.
	// io.EOF means the responder closed cleanly (expected).
	if _, err := readUntilEOF(stream); err != nil && !errors.Is(err, io.EOF) {
		return [32]byte{}, fmt.Errorf("wait for responder close: %w", err)
	}

	// Return the peer's NodeID (hash of its authenticated static key).
	return crypto.NodeID(session.RemotePublic()), nil
}

// OpenStream opens a stream to a peer for a service.
func (s *Server) OpenStream(ctx context.Context, peerID [32]byte, svc ServiceID) (Stream, error) {
	s.mu.RLock()
	conn, ok := s.conns[peerID]
	s.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("no connection to peer %x", peerID[:8])
	}
	return conn.OpenStream(ctx, svc)
}

// SendTo sends a framed message to a peer on a service.
func (s *Server) SendTo(ctx context.Context, peerID [32]byte, svc ServiceID, msgType MessageType, payload []byte) error {
	stream, err := s.OpenStream(ctx, peerID, svc)
	if err != nil {
		return err
	}
	defer stream.Close()

	frame := EncodeFrameBare(msgType, payload)
	if _, err := stream.Write(frame); err != nil {
		return err
	}
	return nil
}

// Peers returns the list of connected peers.
func (s *Server) Peers() []PeerInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]PeerInfo, 0, len(s.conns))
	for _, c := range s.conns {
		out = append(out, PeerInfo{
			ID:       c.peerID,
			Addr:     c.addr,
			State:    c.state,
			LastPong: c.lastSeen,
		})
	}
	return out
}

// Stats returns server statistics.
func (s *Server) Stats() ServerStats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.stats
}

// Relay returns the circuit relay.
func (s *Server) Relay() *Relay {
	return s.relay
}

// Stop gracefully shuts down the server.
func (s *Server) Stop() {
	s.cancel()
	if s.ln != nil {
		s.ln.Close()
	}
	if s.tr != nil {
		s.tr.Close()
	}
	s.wg.Wait()
	log.Info().Msg("transport server stopped")
}

// Connection represents a QUIC connection to a peer.
type Connection struct {
	mu              sync.Mutex
	handle          *quic.Conn
	peerID          [32]byte
	addr            string
	state           ConnectionState
	services        map[ServiceID]bool
	server          *Server
	lastSeen        time.Time
	allowedServices map[ServiceID]bool
}

func (c *Connection) PeerID() [32]byte       { return c.peerID }
func (c *Connection) Addr() string           { return c.addr }
func (c *Connection) State() ConnectionState { return c.state }

// OpenStream opens a new stream for a service.
func (c *Connection) OpenStream(ctx context.Context, svc ServiceID) (Stream, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.state != StateReady {
		return nil, fmt.Errorf("connection not ready (state=%d)", c.state)
	}

	qstream, err := c.handle.OpenStreamSync(ctx)
	if err != nil {
		return nil, err
	}

	// Write the service ID as the first byte on the stream.
	if _, err := qstream.Write([]byte{byte(svc)}); err != nil {
		qstream.Close()
		return nil, err
	}

	c.services[svc] = true
	c.server.mu.Lock()
	c.server.stats.TotalStreams++
	c.server.mu.Unlock()

	return newQuicStream(qstream, svc, c.peerID), nil
}

// AcceptStream accepts an incoming stream.
func (c *Connection) AcceptStream(ctx context.Context) (Stream, error) {
	qstream, err := c.handle.AcceptStream(ctx)
	if err != nil {
		return nil, err
	}
	return newQuicStream(qstream, ServiceControl, c.peerID), nil
}

// RTT returns the round-trip time estimate.
func (c *Connection) RTT() time.Duration {
	// quic-go exposes the RTT via the connection state; approximate here.
	return 10 * time.Millisecond
}

// Close closes the connection.
func (c *Connection) Close() error {
	c.state = StateClosed
	if c.handle != nil {
		return c.handle.CloseWithError(quic.ApplicationErrorCode(0x00), "bye")
	}
	return nil
}

// quicStream adapts a quic-go *quic.Stream to the transport.Stream interface.
type quicStream struct {
	q      *quic.Stream
	svcID  ServiceID
	peerID [32]byte
}

func newQuicStream(q *quic.Stream, svc ServiceID, peerID [32]byte) *quicStream {
	return &quicStream{q: q, svcID: svc, peerID: peerID}
}

func (w *quicStream) Read(p []byte) (int, error)  { return w.q.Read(p) }
func (w *quicStream) Write(p []byte) (int, error) { return w.q.Write(p) }
func (w *quicStream) Close() error                { return w.q.Close() }
func (w *quicStream) ServiceID() ServiceID        { return w.svcID }
func (w *quicStream) ID() uint64                  { return uint64(w.q.StreamID()) }
func (w *quicStream) PeerID() [32]byte            { return w.peerID }

// MaxFrameSize is the maximum allowed payload size for a single frame (1 MiB).
const MaxFrameSize = 1 << 20

// EncodeFrameBare encodes a frame without a node ID.
func EncodeFrameBare(t MessageType, payload []byte) []byte {
	buf := make([]byte, 5+len(payload))
	buf[0] = byte(t)
	binary.BigEndian.PutUint32(buf[1:5], uint32(len(payload)))
	copy(buf[5:], payload)
	return buf
}

// DecodeFrameBare decodes a frame.
func DecodeFrameBare(data []byte) (*Frame, error) {
	if len(data) < 5 {
		return nil, fmt.Errorf("frame too short")
	}
	t := MessageType(data[0])
	length := uint64(binary.BigEndian.Uint32(data[1:5]))
	if length > MaxFrameSize {
		return nil, fmt.Errorf("frame too large: %d > %d", length, MaxFrameSize)
	}
	if uint64(len(data)) < 5+length {
		return nil, fmt.Errorf("frame incomplete")
	}
	return &Frame{
		Type:    t,
		Length:  uint32(length),
		Payload: data[5 : 5+length],
	}, nil
}

// GenerateSelfSignedCert generates a self-signed cert for the QUIC TLS layer.
func GenerateSelfSignedCert() (tls.Certificate, error) {
	pub, priv, err := crypto.GenerateKeyPair()
	if err != nil {
		return tls.Certificate{}, err
	}

	// Build a 64-byte Ed25519 private key from the 32-byte seed.
	fullPriv, err := crypto.SeedToPrivateKey(priv)
	if err != nil {
		return tls.Certificate{}, err
	}

	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * 365 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		DNSNames:     []string{"localweb"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
	}

	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, ed25519.PublicKey(pub[:]), ed25519.PrivateKey(fullPriv))
	if err != nil {
		return tls.Certificate{}, err
	}

	return tls.Certificate{
		Certificate: [][]byte{der},
		PrivateKey:  ed25519.PrivateKey(fullPriv),
	}, nil
}
