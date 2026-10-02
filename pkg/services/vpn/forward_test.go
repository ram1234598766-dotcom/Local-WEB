package vpn

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// fakeTUN is an in-memory Interface. It lets the forwarding loop be tested with
// no TUN device, no privileges and no network, which is the only way this code
// can be tested at all on a machine without CAP_NET_ADMIN.
type fakeTUN struct {
	mu       sync.Mutex
	inbound  chan []byte // packets written into the device by the tunnel
	outbound chan []byte // packets the test hands to the device
	closed   chan struct{}
	once     sync.Once
}

func newFakeTUN() *fakeTUN {
	return &fakeTUN{
		inbound:  make(chan []byte, 16),
		outbound: make(chan []byte, 16),
		closed:   make(chan struct{}),
	}
}

func (f *fakeTUN) Name() string                  { return "fake0" }
func (f *fakeTUN) Up() error                     { return nil }
func (f *fakeTUN) Down() error                   { return nil }
func (f *fakeTUN) Addrs() ([]string, error)      { return []string{"10.7.0.1/24"}, nil }
func (f *fakeTUN) AddRoute(dst, gw string) error { return nil }

// Read returns a packet the test injected, or reports the device closed.
func (f *fakeTUN) Read(buf []byte) (int, error) {
	select {
	case p := <-f.outbound:
		return copy(buf, p), nil
	case <-f.closed:
		return 0, errors.New("device closed")
	}
}

// Write takes a packet delivered by the tunnel.
func (f *fakeTUN) Write(p []byte) (int, error) {
	cp := make([]byte, len(p))
	copy(cp, p)
	select {
	case f.inbound <- cp:
		return len(p), nil
	case <-f.closed:
		return 0, errors.New("device closed")
	}
}

func (f *fakeTUN) Close() error {
	f.once.Do(func() { close(f.closed) })
	return nil
}

// inject hands a packet to the device as if the kernel had routed it there.
func (f *fakeTUN) inject(p []byte) { f.outbound <- p }

// netPipe is the network between two nodes: whatever one endpoint sends comes
// out of the other. Modelling it explicitly is what makes the test prove
// anything, because a carrier that delivered to its own queue would look like
// it worked while never crossing the tunnel.
type netPipe struct {
	toA chan []byte
	toB chan []byte
}

func newNetPipe() *netPipe {
	return &netPipe{toA: make(chan []byte, 16), toB: make(chan []byte, 16)}
}

// endpoint is one side of the pipe, used as a node's Carrier.
type endpoint struct {
	out   chan<- []byte
	in    <-chan []byte
	peer  chan struct{}
	peers chan struct{}
	once  sync.Once
}

func (p *netPipe) endpointA() *endpoint {
	return &endpoint{out: p.toB, in: p.toA, peer: make(chan struct{}), peers: make(chan struct{}, 2)}
}

func (p *netPipe) endpointB() *endpoint {
	return &endpoint{out: p.toA, in: p.toB, peer: make(chan struct{}), peers: make(chan struct{}, 2)}
}

func (e *endpoint) Send(ctx context.Context, packet []byte) error {
	cp := make([]byte, len(packet))
	copy(cp, packet)
	select {
	case e.out <- cp:
		return nil
	case <-e.peer:
		return errors.New("carrier closed")
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (e *endpoint) Recv(ctx context.Context) ([]byte, error) {
	select {
	case p := <-e.in:
		return p, nil
	case <-e.peer:
		return nil, errors.New("carrier closed")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (e *endpoint) Close() error {
	e.once.Do(func() { close(e.peer) })
	return nil
}

// TestAttachForwardsPacketsBothWays is the item 7.5 test: a packet injected
// into one node's device comes out of the other node's device.
//
// The server previously opened a TUN device, brought it up and never read or
// wrote a packet, so no packet could ever cross. The existing tests created
// tunnels and closed them and never involved a device at all.
func TestAttachForwardsPacketsBothWays(t *testing.T) {
	devA := newFakeTUN()
	devB := newFakeTUN()
	t.Cleanup(func() { _ = devA.Close(); _ = devB.Close() })

	nodeA := NewServerWithInterface([32]byte{1}, devA)
	nodeB := NewServerWithInterface([32]byte{2}, devB)

	ctx := context.Background()

	// Each node's carrier is one end of a shared pipe.
	pipe := newNetPipe()
	carrierA := pipe.endpointA()
	carrierB := pipe.endpointB()
	t.Cleanup(func() { _ = carrierA.Close(); _ = carrierB.Close() })

	tunnelA, err := nodeA.Attach(ctx, [32]byte{2}, carrierA)
	if err != nil {
		t.Fatalf("attach A: %v", err)
	}
	if _, err := nodeB.Attach(ctx, [32]byte{1}, carrierB); err != nil {
		t.Fatalf("attach B: %v", err)
	}
	t.Cleanup(func() { _ = nodeA.CloseTunnel(tunnelA) })

	packet := []byte{0x45, 0x00, 0x00, 0x14, 0xde, 0xad, 0xbe, 0xef}
	devA.inject(packet)

	select {
	case got := <-devB.inbound:
		if string(got) != string(packet) {
			t.Fatalf("B's device received %v, want %v", got, packet)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the packet never reached B's device")
	}

	// And back the other way, because a tunnel that only carries one direction
	// is half a tunnel.
	devB.inject([]byte{0x45, 0x00, 0x00, 0x08, 0x01, 0x02, 0x03, 0x04})
	select {
	case got := <-devA.inbound:
		if len(got) != 8 {
			t.Fatalf("A's device received %d bytes, want 8", len(got))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the return packet never reached A's device")
	}

	forwarded, delivered := nodeA.Stats()
	if forwarded != 1 {
		t.Errorf("A forwarded %d packets, want 1", forwarded)
	}
	if delivered != 1 {
		t.Errorf("A delivered %d packets, want 1", delivered)
	}
}

func TestAttachWithoutDeviceIsAnError(t *testing.T) {
	// A userspace-only node has no device, and must say so rather than
	// pretending a tunnel exists.
	s := NewServerWithInterface([32]byte{1}, nil)
	if _, err := s.Attach(context.Background(), [32]byte{2}, newNetPipe().endpointA()); err == nil {
		t.Fatal("expected an error when there is no TUN device")
	}
}

func TestAttachWithoutCarrierIsAnError(t *testing.T) {
	s := NewServerWithInterface([32]byte{1}, newFakeTUN())
	if _, err := s.Attach(context.Background(), [32]byte{2}, nil); err == nil {
		t.Fatal("expected an error when there is no carrier")
	}
}

// TestCloseTunnelStopsForwarding proves closing a tunnel actually stops the
// goroutines rather than leaving them reading a device for a tunnel that is gone.
func TestCloseTunnelStopsForwarding(t *testing.T) {
	dev := newFakeTUN()
	t.Cleanup(func() { _ = dev.Close() })

	s := NewServerWithInterface([32]byte{1}, dev)
	carrier := newNetPipe().endpointA()
	t.Cleanup(func() { _ = carrier.Close() })

	id, err := s.Attach(context.Background(), [32]byte{2}, carrier)
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	if err := s.CloseTunnel(id); err != nil {
		t.Fatalf("close: %v", err)
	}
	if len(s.Tunnels()) != 0 {
		t.Fatal("the tunnel should be gone")
	}

	s.mu.Lock()
	_, stillRunning := s.running[id]
	s.mu.Unlock()
	if stillRunning {
		t.Error("the forwarding loop was left running after the tunnel closed")
	}
}
