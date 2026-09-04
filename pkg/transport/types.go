package transport

import (
	"context"
	"time"
)

// ServiceID is the 1-byte identifier for multiplexed services.
type ServiceID byte

const (
	ServiceControl ServiceID = 'C' // Control channel
	ServiceDNS     ServiceID = 'D' // DNS over QUIC
	ServiceHTTP    ServiceID = 'H' // HTTP/3
	ServiceMsg     ServiceID = 'M' // Messaging
	ServiceFS      ServiceID = 'F' // File transfer
	ServiceRelay   ServiceID = 'R' // Circuit relay
	ServiceVoice   ServiceID = 'V' // Voice/Video
	ServiceVPN     ServiceID = 'W' // WireGuard mesh
	ServiceDocs    ServiceID = 'O' // Collaborative docs
	ServiceReg     ServiceID = 'G' // App registry
)

// MessageType enumerates wire message types.
type MessageType uint8

const (
	MsgPing      MessageType = 0x01
	MsgPong      MessageType = 0x02
	MsgFindNode  MessageType = 0x03
	MsgFoundNode MessageType = 0x04
	MsgStore     MessageType = 0x05
	MsgFindValue MessageType = 0x06
	MsgFoundVal  MessageType = 0x07
	MsgAnnounce  MessageType = 0x08
	MsgHeartbeat MessageType = 0x09
	MsgRelay     MessageType = 0x0A
	MsgRelayACK  MessageType = 0x0B
)

// ConnectionState tracks the state of a QUIC connection.
type ConnectionState int

const (
	StateConnecting ConnectionState = iota
	StateConnected
	StateHandshaking
	StateReady
	StateDraining
	StateClosed
)

// PeerInfo holds transport-level peer information.
type PeerInfo struct {
	ID        [32]byte
	Addr      string
	PubKey    [32]byte
	Latency   time.Duration
	State     ConnectionState
	Streams   int
	LastPong  time.Time
	Score     float64
}

// StreamHandler is a function that handles a multiplexed stream.
type StreamHandler func(ctx context.Context, stream Stream)

// Stream wraps a QUIC stream with framing.
type Stream interface {
	Read(p []byte) (n int, err error)
	Write(p []byte) (n int, err error)
	Close() error
	ServiceID() ServiceID
	ID() uint64
}

// RelayInfo holds information about a circuit relay.
type RelayInfo struct {
	ID       [32]byte
	Addr     string
	Capacity int
	Load     float64
	Score    float64
}

// Frame represents a single frame on a stream.
type Frame struct {
	Type    MessageType
	Length  uint32
	Payload []byte
}

// FlowControl manages per-stream and connection-level flow control.
type FlowControl struct {
	streamWindow    uint32 // Per-stream send window
	connWindow      uint32 // Connection-level send window
	streamRecvWin   uint32 // Per-stream receive window
	connRecvWin     uint32 // Connection-level receive window
	maxStreamWindow uint32
	maxConnWindow   uint32
}

// DefaultFlowControl returns sensible flow control defaults.
func DefaultFlowControl() FlowControl {
	return FlowControl{
		streamWindow:    1 << 20,  // 1MB
		connWindow:      16 << 20, // 16MB
		streamRecvWin:   1 << 20,
		connRecvWin:     16 << 20,
		maxStreamWindow: 16 << 20, // 16MB max
		maxConnWindow:   256 << 20, // 256MB max
	}
}

// StreamState tracks the state of a multiplexed stream.
type StreamState int

const (
	StreamOpen StreamState = iota
	StreamHalfClosed
	StreamClosed
	StreamReset
)

// ManagedStream wraps a QUIC stream with state tracking.
type ManagedStream struct {
	id       uint64
	svcID    ServiceID
	state    StreamState
	sendBuf  chan []byte
	recvBuf  chan []byte
	priority int
}
