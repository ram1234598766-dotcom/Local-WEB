package link

import (
	"bytes"
	"context"
	"errors"
	"net"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

var errWriteFailed = errors.New("write failed")

// writeFailConn is a net.Conn whose Write always fails. The embedded
// interface stays nil because only Write is ever reached on this path.
type writeFailConn struct{ net.Conn }

func (writeFailConn) Write([]byte) (int, error) { return 0, errWriteFailed }

func init() {
	// The package logs heavily through zerolog; keep test output readable.
	log.Logger = log.Output(nil)
}

// fakeLink is an in-memory Link. It satisfies the whole Link interface without
// touching WiFi, BLE or USB hardware, so Manager and MultiPathManager can be
// driven deterministically from tests.
type fakeLink struct {
	mu sync.Mutex

	name           string
	mode           LinkMode
	requiresWiFi   bool
	requiresRouter bool
	available      bool
	bandwidth      int
	maxPeers       int

	discoverErr error
	events      chan PeerEvent

	connectErr error
	connectFn  func(addr string) (net.Conn, error)

	connectCalls  []string
	discoverCalls int
	stopCalls     int
	stopErr       error
}

func newFakeLink(name string, mode LinkMode) *fakeLink {
	return &fakeLink{
		name:      name,
		mode:      mode,
		available: true,
		bandwidth: 100,
		maxPeers:  8,
		events:    make(chan PeerEvent, 8),
	}
}

func (f *fakeLink) Name() string         { return f.name }
func (f *fakeLink) Mode() LinkMode       { return f.mode }
func (f *fakeLink) RequiresWiFi() bool   { return f.requiresWiFi }
func (f *fakeLink) RequiresRouter() bool { return f.requiresRouter }
func (f *fakeLink) Bandwidth() int       { return f.bandwidth }
func (f *fakeLink) MaxPeers() int        { return f.maxPeers }

func (f *fakeLink) IsAvailable(context.Context) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.available
}

func (f *fakeLink) Discover(context.Context) (<-chan PeerEvent, error) {
	f.mu.Lock()
	f.discoverCalls++
	err := f.discoverErr
	f.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return f.events, nil
}

func (f *fakeLink) Connect(_ context.Context, addr string) (net.Conn, error) {
	f.mu.Lock()
	f.connectCalls = append(f.connectCalls, addr)
	fn, err := f.connectFn, f.connectErr
	f.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if fn != nil {
		return fn(addr)
	}
	local, _ := net.Pipe()
	return local, nil
}

func (f *fakeLink) Advertise(PeerInfo) error { return nil }

func (f *fakeLink) Stop() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopCalls++
	return f.stopErr
}

// All accessors below take the mutex so they are safe to call while the
// manager's goroutines are running under -race.

func (f *fakeLink) setAvailable(v bool) {
	f.mu.Lock()
	f.available = v
	f.mu.Unlock()
}

func (f *fakeLink) setDiscoverErr(err error) {
	f.mu.Lock()
	f.discoverErr = err
	f.mu.Unlock()
}

func (f *fakeLink) setStopErr(err error) {
	f.mu.Lock()
	f.stopErr = err
	f.mu.Unlock()
}

func (f *fakeLink) setConnect(fn func(addr string) (net.Conn, error)) {
	f.mu.Lock()
	f.connectFn = fn
	f.mu.Unlock()
}

func (f *fakeLink) setConnectErr(err error) {
	f.mu.Lock()
	f.connectErr = err
	f.mu.Unlock()
}

func (f *fakeLink) discoverCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.discoverCalls
}

func (f *fakeLink) stopCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.stopCalls
}

func (f *fakeLink) connectCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.connectCalls)
}

func (f *fakeLink) connectedAddrs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.connectCalls...)
}

func (f *fakeLink) emit(evt PeerEvent) { f.events <- evt }

// pipeSink collects every byte written to the far end of a net.Pipe.
type pipeSink struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func newPipeSink() *pipeSink { return &pipeSink{} }

func (s *pipeSink) pump(remote net.Conn) {
	go func() {
		chunk := make([]byte, 4096)
		for {
			n, err := remote.Read(chunk)
			if n > 0 {
				s.mu.Lock()
				s.buf.Write(chunk[:n])
				s.mu.Unlock()
			}
			if err != nil {
				return
			}
		}
	}()
}

func (s *pipeSink) bytes() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]byte(nil), s.buf.Bytes()...)
}

func (s *pipeSink) len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Len()
}

// waitForLen blocks until the sink has collected at least n bytes. A net.Pipe
// write returns as soon as the reader's Read hands over the bytes, which is
// before the drain goroutine has appended them to the buffer.
func (s *pipeSink) waitForLen(n int, d time.Duration) bool {
	return waitFor(d, func() bool { return s.len() >= n })
}

// pipeConn is one end of an in-memory net.Pipe plus the sink draining the
// other end, so the multipath send path can be exercised without a socket.
type pipeConn struct {
	local net.Conn
	sink  *pipeSink
}

func newPipeConn() *pipeConn {
	local, remote := net.Pipe()
	sink := newPipeSink()
	sink.pump(remote)
	return &pipeConn{local: local, sink: sink}
}

// peerRecorder captures the OnPeer callback. The callback runs on the
// manager's event goroutine, so access is mutex-guarded.
type peerRecorder struct {
	mu     sync.Mutex
	events []PeerEvent
}

func (r *peerRecorder) on(evt PeerEvent) {
	r.mu.Lock()
	r.events = append(r.events, evt)
	r.mu.Unlock()
}

func (r *peerRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.events)
}

func (r *peerRecorder) types() []EventType {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]EventType, 0, len(r.events))
	for _, e := range r.events {
		out = append(out, e.Type)
	}
	return out
}

// waitFor polls cond until it holds or d elapses.
func waitFor(d time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(d)
	for {
		if cond() {
			return true
		}
		if time.Now().After(deadline) {
			return cond()
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// testPeerID derives a stable, distinct 32-byte peer ID from n.
func testPeerID(n byte) [32]byte {
	var id [32]byte
	id[0] = n
	id[31] = n ^ 0xff
	return id
}

func testNodeID(n byte) [32]byte {
	var id [32]byte
	for i := range id {
		id[i] = n + byte(i)
	}
	return id
}
