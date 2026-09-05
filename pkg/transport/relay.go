package transport

import (
	"context"
	"encoding/binary"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

const (
	relayMaxStreams  = 100 // Max concurrent streams per relay
	relayIdleTO      = 60 * time.Second
	relayHandshakeTO = 10 * time.Second
)

// Relay provides circuit relay functionality (relay data between peers).
type Relay struct {
	mu       sync.RWMutex
	circuits map[string]*Circuit // circuitID → circuit
	load     float64             // Current load 0.0 → 1.0
	capacity int                 // Max circuits
	ctx      context.Context
	cancel   context.CancelFunc
}

// Circuit represents an active relayed connection.
type Circuit struct {
	ID        string // Circuit identifier
	Initiator [32]byte
	Target    [32]byte
	CreatedAt time.Time
	BytesIn   uint64
	BytesOut  uint64
	InStream  Stream
	OutStream Stream
}

// NewRelay creates a new circuit relay.
func NewRelay(ctx context.Context) *Relay {
	ctx, cancel := context.WithCancel(ctx)
	return &Relay{
		circuits: make(map[string]*Circuit),
		capacity: relayMaxStreams,
		ctx:      ctx,
		cancel:   cancel,
	}
}

// Register returns this node's relay advertisement info.
func (r *Relay) Info() RelayInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return RelayInfo{
		Capacity: r.capacity,
		Load:     r.load,
	}
}

// AcceptCircuit registers a new circuit through this node.
// initiator connects to target via this relay.
func (r *Relay) AcceptCircuit(id string, initiator, target [32]byte, in, out Stream) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if len(r.circuits) >= r.capacity {
		return fmt.Errorf("relay at capacity")
	}

	r.circuits[id] = &Circuit{
		ID:        id,
		Initiator: initiator,
		Target:    target,
		CreatedAt: time.Now(),
		InStream:  in,
		OutStream: out,
	}
	r.load = float64(len(r.circuits)) / float64(r.capacity)

	// Pump data in both directions
	go r.pump(id, in, out)
	go r.pumpID(id, out, in)

	log.Info().
		Str("initiator", fmt.Sprintf("%x", initiator[:8])).
		Str("target", fmt.Sprintf("%x", target[:8])).
		Str("circuit", id).
		Msg("relay circuit established")

	return nil
}

// pump copies data from src to dst.
func (r *Relay) pump(id string, src, dst Stream) {
	buf := make([]byte, 64*1024)
	for {
		n, err := src.Read(buf)
		if n > 0 {
			if _, werr := dst.Write(buf[:n]); werr != nil {
				break
			}
			r.recordBytes(id, uint64(n))
		}
		if err != nil {
			break
		}
	}
	r.CloseCircuit(id)
}

// pumpID copies data from src to dst (reverse direction).
func (r *Relay) pumpID(id string, src, dst Stream) {
	buf := make([]byte, 64*1024)
	for {
		n, err := src.Read(buf)
		if n > 0 {
			if _, werr := dst.Write(buf[:n]); werr != nil {
				break
			}
		}
		if err != nil {
			break
		}
	}
	r.CloseCircuit(id)
}

func (r *Relay) recordBytes(id string, n uint64) {
	r.mu.Lock()
	if c, ok := r.circuits[id]; ok {
		c.BytesIn += n
	}
	r.mu.Unlock()
}

// CloseCircuit tears down a circuit.
func (r *Relay) CloseCircuit(id string) {
	r.mu.Lock()
	c, ok := r.circuits[id]
	if ok {
		delete(r.circuits, id)
		if c.InStream != nil {
			c.InStream.Close()
		}
		if c.OutStream != nil {
			c.OutStream.Close()
		}
	}
	r.load = float64(len(r.circuits)) / float64(r.capacity)
	r.mu.Unlock()
}

// Circuits returns a snapshot of active circuits.
func (r *Relay) Circuits() []Circuit {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Circuit, 0, len(r.circuits))
	for _, c := range r.circuits {
		out = append(out, *c)
	}
	return out
}

// Stop shuts down the relay.
func (r *Relay) Stop() {
	r.cancel()
	r.mu.Lock()
	for id := range r.circuits {
		r.CloseCircuit(id)
	}
	r.mu.Unlock()
	log.Info().Msg("relay stopped")
}

// --- Relay Wire Protocol Helpers ---

// EncodeRelayExtend encodes a RELAY_EXTEND message.
func EncodeRelayExtend(circuitID string, target [32]byte) []byte {
	buf := make([]byte, 1+4+len(circuitID)+32)
	buf[0] = byte(MsgRelay)
	binary.BigEndian.PutUint32(buf[1:5], uint32(len(circuitID)))
	copy(buf[5:], circuitID)
	copy(buf[5+len(circuitID):], target[:])
	return buf
}

// DecodeRelayExtend parses a RELAY_EXTEND message.
// Wire format: [MsgRelay(1)] [circuitID_len(4)] [circuitID(N)] [target(32)]
func DecodeRelayExtend(data []byte) (string, [32]byte, error) {
	if len(data) < 5 {
		return "", [32]byte{}, fmt.Errorf("relay extend too short")
	}
	if MessageType(data[0]) != MsgRelay {
		return "", [32]byte{}, fmt.Errorf("relay extend invalid")
	}
	idLen := int(binary.BigEndian.Uint32(data[1:5]))
	if len(data) < 5+idLen+32 {
		return "", [32]byte{}, fmt.Errorf("relay extend invalid")
	}
	circuitID := string(data[5 : 5+idLen])
	var target [32]byte
	copy(target[:], data[5+idLen:])
	return circuitID, target, nil
}
