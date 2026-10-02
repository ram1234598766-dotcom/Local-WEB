package vpn

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/ram1234598766-dotcom/Local-WEB/pkg/transport"
	"github.com/rs/zerolog/log"
)

// ServiceHandler returns the transport handler for ServiceVPN.
//
// The subtle part is what this must NOT do: it must not read from the stream. A
// handler that consumed bytes would take every packet before the carrier's
// Recv ever saw it, and the tunnel would carry nothing while looking connected.
//
// It also must not return. Returning lets the transport close the stream, which
// ends the carrier. So the handler hands the stream to a carrier, starts the
// forwarding loops and then parks for the life of the tunnel. One parked
// goroutine per peer is the price of owning a stream for as long as the tunnel
// exists, and it is bounded by the peer count.
func (s *Server) ServiceHandler(peerFrom func(stream transport.Stream) [32]byte) transport.StreamHandler {
	return func(ctx context.Context, stream transport.Stream) {
		peer := peerFrom(stream)
		carrier := AcceptCarrier(stream)
		if _, err := s.Attach(ctx, peer, carrier); err != nil {
			log.Warn().Err(err).Str("peer", fmt.Sprintf("%x", peer[:8])).
				Msg("vpn: could not attach an incoming tunnel")
			_ = carrier.Close()
			return
		}
		// Park until the stream or the server goes away. A read here would
		// steal the packets; a return would close the stream.
		<-ctx.Done()
		_ = carrier.Close()
	}
}

// ErrCarrierClosed is returned once a carrier has been shut down.
var ErrCarrierClosed = fmt.Errorf("vpn carrier is closed")

// maxPacketSize bounds one tunnelled packet. It matches the MTU the forwarding
// loop enforces, so a peer cannot make the receiver allocate more than a device
// frame ever holds.
const maxPacketSize = DefaultMTU + 64

// StreamCarrier moves tunnelled IP packets over a Local-WEB service stream.
//
// This is the piece that was missing: ServiceVPN was allocated in the transport's
// service list and nothing used it, so the forwarding loop could only ever be
// driven by an injected carrier and no packet could reach a real peer.
//
// Wire format, per packet:
//
//	uint16 length | length bytes of the IP packet
//
// The length prefix is what makes a stream usable for datagrams: a stream has no
// message boundaries of its own, so without it the reader would have no idea
// where one packet ends.
type StreamCarrier struct {
	stream transport.Stream

	// recvMu serialises reads. A pion stream yields a partial read, so a single
	// unbounded Read cannot be assumed to deliver a whole packet.
	recvMu sync.Mutex
	closed bool
	mu     sync.Mutex
}

// NewStreamCarrier wraps an already-open service stream.
func NewStreamCarrier(stream transport.Stream) *StreamCarrier {
	return &StreamCarrier{stream: stream}
}

// OpenCarrier dials a peer and opens a VPN stream to it.
func OpenCarrier(ctx context.Context, srv *transport.Server, peer [32]byte) (*StreamCarrier, error) {
	if srv == nil {
		return nil, errors.New("no transport server")
	}
	stream, err := srv.OpenStream(ctx, peer, transport.ServiceVPN)
	if err != nil {
		return nil, fmt.Errorf("open vpn stream to %x: %w", peer[:8], err)
	}
	return NewStreamCarrier(stream), nil
}

// AcceptCarrier wraps a stream a peer opened to us.
func AcceptCarrier(stream transport.Stream) *StreamCarrier {
	return NewStreamCarrier(stream)
}

// Send writes one packet to the peer.
func (c *StreamCarrier) Send(ctx context.Context, packet []byte) error {
	if len(packet) == 0 {
		return errors.New("refusing to send an empty packet")
	}
	if len(packet) > maxPacketSize {
		return fmt.Errorf("packet of %d bytes exceeds the %d byte limit", len(packet), maxPacketSize)
	}
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if closed {
		return ErrCarrierClosed
	}

	frame := make([]byte, 2+len(packet))
	binary.BigEndian.PutUint16(frame[:2], uint16(len(packet)))
	copy(frame[2:], packet)

	if _, err := c.stream.Write(frame); err != nil {
		return fmt.Errorf("write vpn packet: %w", err)
	}
	return nil
}

// Recv reads one packet from the peer.
//
// A read deadline is set per call so a peer that stops sending cannot pin this
// goroutine for ever; the forwarding loop is expected to keep calling.
func (c *StreamCarrier) Recv(ctx context.Context) ([]byte, error) {
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if closed {
		return nil, ErrCarrierClosed
	}

	c.recvMu.Lock()
	defer c.recvMu.Unlock()

	var header [2]byte
	if err := c.readFull(ctx, header[:]); err != nil {
		return nil, err
	}
	length := int(binary.BigEndian.Uint16(header[:]))
	if length == 0 {
		return nil, errors.New("peer sent a zero length packet")
	}
	if length > maxPacketSize {
		return nil, fmt.Errorf("peer claims a %d byte packet, over the %d limit", length, maxPacketSize)
	}

	packet := make([]byte, length)
	if err := c.readFull(ctx, packet); err != nil {
		return nil, err
	}
	return packet, nil
}

// readFull fills buf or returns an error. A partial read is a protocol error
// here, not something to retry silently, so it is surfaced.
func (c *StreamCarrier) readFull(ctx context.Context, buf []byte) error {
	if err := c.stream.SetReadDeadline(time.Now().Add(30 * time.Second)); err != nil {
		// Not every stream implementation supports deadlines; carry on without.
		_ = err
	}
	got := 0
	for got < len(buf) {
		n, err := c.stream.Read(buf[got:])
		if n > 0 {
			got += n
			continue
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return ErrCarrierClosed
			}
			return fmt.Errorf("read vpn packet: %w", err)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	return nil
}

// Close shuts the carrier and its stream down. It is safe to call twice.
func (c *StreamCarrier) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	c.mu.Unlock()
	return c.stream.Close()
}

// Closed reports whether the carrier has been shut down.
func (c *StreamCarrier) Closed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}
