package chaos

import (
	"errors"
	"io"
	"math/rand"
	"net"
	"sync"
	"time"
)

// FaultyConn wraps a net.Conn to simulate network faults:
// packet loss, latency, duplication, and partitioning.
// It is NOT a real network connection — use NewPipe for that.
// FaultyConn is primarily used for chaos testing existing connections.
type FaultyConn struct {
	mu        sync.Mutex
	closed    bool
	buf       []byte
	pending   []byte
	lossRate  float64
	latency   time.Duration
	duplicate int
	partition bool
	rng       *rand.Rand
}

// NewFaultyConn creates a FaultyConn that returns data from an internal buffer.
// `size` is the amount of data to return per read (for synthetic testing).
// `lossRate` is the probability (0.0–1.0) that any given read drops the packet.
func NewFaultyConn(size int, lossRate float64) *FaultyConn {
	return &FaultyConn{
		buf:      make([]byte, size),
		lossRate: lossRate,
		rng:      rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// SetBuffer sets the internal buffer data that reads will return.
func (c *FaultyConn) SetBuffer(data []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pending = data
}

// WithLatency sets the latency applied to all reads.
func WithLatency(c *FaultyConn, d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.latency = d
}

// WithDuplicate sets the duplication factor (0 = no duplication, 2 = each packet sent twice).
func WithDuplicate(c *FaultyConn, n int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.duplicate = n
}

// WithPartition sets the partition mode. When true, all reads return 0 bytes.
func WithPartition(c *FaultyConn, p bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.partition = p
}

func (c *FaultyConn) Read(b []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return 0, errors.New("connection closed")
	}

	if c.partition {
		return 0, nil
	}

	if c.latency > 0 {
		time.Sleep(c.latency)
	}

	if c.rng.Float64() < c.lossRate {
		return 0, nil
	}

	data := c.buf
	if len(c.pending) > 0 {
		data = c.pending
		c.pending = c.pending[:0]
	} else if len(data) == 0 {
		return 0, io.EOF
	}

	n := copy(b, data)

	// Duplicate mode: queue a copy for the next read
	if c.duplicate > 0 {
		c.pending = append(c.pending, data[:n]...)
		c.duplicate--
	}

	return n, nil
}

func (c *FaultyConn) Write(b []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return 0, errors.New("connection closed")
	}

	if c.partition {
		return 0, nil
	}

	if c.rng.Float64() < c.lossRate {
		return 0, nil
	}

	return len(b), nil
}

func (c *FaultyConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	return nil
}

func (c *FaultyConn) LocalAddr() net.Addr {
	return &net.IPAddr{IP: net.IPv4zero}
}

func (c *FaultyConn) RemoteAddr() net.Addr {
	return &net.IPAddr{IP: net.IPv4zero}
}

func (c *FaultyConn) SetDeadline(t time.Time) error {
	return nil
}

func (c *FaultyConn) SetReadDeadline(t time.Time) error {
	return nil
}

func (c *FaultyConn) SetWriteDeadline(t time.Time) error {
	return nil
}

// Pipe creates a pair of connected net.Conns with configurable packet loss.
// pipeConn represents one end of a pair of connected faulty pipes.
type pipeConn struct {
	mu       sync.Mutex
	closed   bool
	buf      []byte
	lossRate float64
	rng      *rand.Rand
	peer     *pipeConn
}

// NewPipe creates a pair of net.Conns simulating a lossy channel.
// `bufSize` is the buffer size; `lossRate` is the fraction of packets dropped.
func NewPipe(bufSize int, lossRate float64) (net.Conn, net.Conn) {
	a := &pipeConn{
		buf:      make([]byte, 0, bufSize),
		lossRate: lossRate,
		rng:      rand.New(rand.NewSource(time.Now().UnixNano())),
	}
	b := &pipeConn{
		buf:      make([]byte, 0, bufSize),
		lossRate: lossRate,
		rng:      rand.New(rand.NewSource(time.Now().UnixNano() + 1)),
	}
	a.peer = b
	b.peer = a
	return a, b
}

func (c *pipeConn) Read(b []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return 0, errors.New("closed")
	}
	if len(c.buf) == 0 {
		return 0, io.EOF
	}
	if c.rng.Float64() < c.lossRate {
		c.buf = c.buf[:0]
		return 0, nil
	}
	n := copy(b, c.buf)
	c.buf = c.buf[:0]
	return n, nil
}

func (c *pipeConn) Write(b []byte) (int, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return 0, errors.New("closed")
	}
	if c.rng.Float64() < c.lossRate {
		c.mu.Unlock()
		return len(b), nil
	}
	peer := c.peer
	peer.mu.Lock()
	peer.buf = append(peer.buf, b...)
	peer.mu.Unlock()
	c.mu.Unlock()
	return len(b), nil
}

func (c *pipeConn) Close() error {
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	return nil
}

func (c *pipeConn) LocalAddr() net.Addr {
	return &net.IPAddr{IP: net.IPv4zero}
}

func (c *pipeConn) RemoteAddr() net.Addr {
	return &net.IPAddr{IP: net.IPv4zero}
}

func (c *pipeConn) SetDeadline(t time.Time) error      { return nil }
func (c *pipeConn) SetReadDeadline(t time.Time) error  { return nil }
func (c *pipeConn) SetWriteDeadline(t time.Time) error { return nil }
