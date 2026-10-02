package vpn

import (
	"context"
	"testing"
	"time"

	"github.com/ram1234598766-dotcom/Local-WEB/pkg/crypto"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/transport"
)

// carrierPair is two real Local-WEB nodes, each with a transport server, a fake
// TUN device and a VPN server, connected over loopback QUIC.
type carrierPair struct {
	srvA, srvB       *transport.Server
	vpnA, vpnB       *Server
	devA, devB       *fakeTUN
	idA, idB         [32]byte
	streamA, streamB transport.Stream
}

// newCarrierPair builds two connected nodes. The service stream between them is
// handed back so the test can attach a carrier at each end, which is the shape
// Attach expects: a stream that already exists rather than one the carrier
// dials itself.
func newCarrierPair(t *testing.T) *carrierPair {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	// t.Cleanup, not defer: a deferred cancel fires when this helper returns,
	// which cancels the servers' own context and tears the connection down
	// before a single packet is sent.
	t.Cleanup(cancel)

	pubA, privA, err := crypto.GenerateX25519KeyPair()
	if err != nil {
		t.Fatalf("keypair A: %v", err)
	}
	pubB, privB, err := crypto.GenerateX25519KeyPair()
	if err != nil {
		t.Fatalf("keypair B: %v", err)
	}
	idA := crypto.NodeID(pubA)
	idB := crypto.NodeID(pubB)

	srvA, err := transport.NewServer(ctx, "127.0.0.1:0", pubA, privA)
	if err != nil {
		t.Fatalf("server A: %v", err)
	}
	t.Cleanup(srvA.Stop)
	srvB, err := transport.NewServer(ctx, "127.0.0.1:0", pubB, privB)
	if err != nil {
		t.Fatalf("server B: %v", err)
	}
	t.Cleanup(srvB.Stop)

	// B advertises the VPN service and hands the stream it accepts to the test,
	// which wraps it in a carrier.
	//
	// The handler must NOT read from the stream: a service handler that consumed
	// bytes would steal every packet from the carrier that is supposed to own
	// them. It hands the stream over and then parks, because returning would let
	// the transport close it.
	accepted := make(chan transport.Stream, 1)
	parked := make(chan struct{})
	t.Cleanup(func() { close(parked) })
	srvB.RegisterHandler(transport.ServiceVPN, func(sctx context.Context, s transport.Stream) {
		select {
		case accepted <- s:
		default:
		}
		select {
		case <-parked:
		case <-sctx.Done():
		}
	})

	if _, err := srvA.Connect(ctx, srvB.Addr(), idB); err != nil {
		t.Fatalf("A connect B: %v", err)
	}
	waitForTransportPeer(t, srvB, idA)
	// Both ends must report the connection up before a stream is opened on it;
	// opening early produced a stream that died with "closing" as soon as the
	// loop moved on.
	waitForConnected(t, srvA, idB)
	waitForConnected(t, srvB, idA)

	streamA, err := srvA.OpenStream(ctx, idB, transport.ServiceVPN)
	if err != nil {
		t.Fatalf("A open vpn stream: %v", err)
	}
	t.Cleanup(func() { _ = streamA.Close() })

	select {
	case streamB := <-accepted:
		pair := &carrierPair{
			srvA: srvA, srvB: srvB, idA: idA, idB: idB,
			streamA: streamA, streamB: streamB,
		}
		pair.devA = newFakeTUN()
		pair.devB = newFakeTUN()
		t.Cleanup(func() { _ = pair.devA.Close(); _ = pair.devB.Close() })
		pair.vpnA = NewServerWithInterface(idA, pair.devA)
		pair.vpnB = NewServerWithInterface(idB, pair.devB)
		return pair
	case <-time.After(30 * time.Second):
		t.Fatal("B never saw the vpn stream")
		return nil
	}
}

func waitForTransportPeer(t *testing.T, srv *transport.Server, want [32]byte) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		for _, p := range srv.Peers() {
			if p.ID == want {
				return
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("transport peer %x never appeared", want[:8])
}

func waitForConnected(t *testing.T, srv *transport.Server, want [32]byte) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		for _, p := range srv.Peers() {
			if p.ID == want && (p.State == transport.StateConnected || p.State == transport.StateReady) {
				return
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("transport peer %x never reached a usable state", want[:8])
}

// TestCarrierDeliversPacketsOverARealTransport is the item 7.5 transport test:
// an IP packet injected into node A's device is carried over a real QUIC
// ServiceVPN stream, framed by the carrier, and written to node B's device.
func TestCarrierDeliversPacketsOverARealTransport(t *testing.T) {
	pair := newCarrierPair(t)

	carrierA := NewStreamCarrier(pair.streamA)
	carrierB := AcceptCarrier(pair.streamB)
	t.Cleanup(func() { _ = carrierA.Close(); _ = carrierB.Close() })

	tunnelA, err := pair.vpnA.Attach(context.Background(), pair.idB, carrierA)
	if err != nil {
		t.Fatalf("attach A: %v", err)
	}
	if _, err := pair.vpnB.Attach(context.Background(), pair.idA, carrierB); err != nil {
		t.Fatalf("attach B: %v", err)
	}
	t.Cleanup(func() { _ = pair.vpnA.CloseTunnel(tunnelA) })

	// A realistic IPv4 header followed by payload.
	packet := []byte{
		0x45, 0x00, 0x00, 0x3c, 0x1c, 0x46, 0x40, 0x00,
		0x40, 0x06, 0x00, 0x00, 0x0a, 0x00, 0x00, 0x02,
		0x0a, 0x07, 0x00, 0x01,
	}
	for i := 20; i < 40; i++ {
		packet = append(packet, byte(i))
	}
	pair.devA.inject(packet)

	select {
	case got := <-pair.devB.inbound:
		if len(got) != len(packet) {
			t.Fatalf("B's device got %d bytes, want %d", len(got), len(packet))
		}
		for i := range packet {
			if got[i] != packet[i] {
				t.Fatalf("byte %d differs: got %#x want %#x", i, got[i], packet[i])
			}
		}
	case <-time.After(20 * time.Second):
		fwd, del := pair.vpnA.Stats()
		fwdB, delB := pair.vpnB.Stats()
		t.Fatalf("the packet never crossed the real transport: A forwarded=%d delivered=%d, B forwarded=%d delivered=%d, carrierA closed=%v",
			fwd, del, fwdB, delB, carrierA.Closed())
	}

	forwarded, _ := pair.vpnA.Stats()
	if forwarded != 1 {
		t.Errorf("A forwarded %d packets, want 1", forwarded)
	}
}

// TestCarrierRoundTripPreservesEveryByte sends a sequence of differently sized
// packets, which is what a length-prefixed framing exists to get right: a
// receiver with no boundaries would merge them.
func TestCarrierRoundTripPreservesEveryByte(t *testing.T) {
	pair := newCarrierPair(t)

	carrierA := NewStreamCarrier(pair.streamA)
	carrierB := AcceptCarrier(pair.streamB)
	t.Cleanup(func() { _ = carrierA.Close(); _ = carrierB.Close() })

	tunnelA, err := pair.vpnA.Attach(context.Background(), pair.idB, carrierA)
	if err != nil {
		t.Fatalf("attach A: %v", err)
	}
	if _, err := pair.vpnB.Attach(context.Background(), pair.idA, carrierB); err != nil {
		t.Fatalf("attach B: %v", err)
	}
	t.Cleanup(func() { _ = pair.vpnA.CloseTunnel(tunnelA) })

	sizes := []int{40, 300, 64, 1400, 41}
	for i, n := range sizes {
		packet := make([]byte, n)
		for j := range packet {
			packet[j] = byte(i*7 + j)
		}
		pair.devA.inject(packet)

		select {
		case got := <-pair.devB.inbound:
			if len(got) != n {
				t.Fatalf("packet %d: got %d bytes, want %d", i, len(got), n)
			}
			for j := range packet {
				if got[j] != packet[j] {
					t.Fatalf("packet %d byte %d differs", i, j)
				}
			}
		case <-time.After(20 * time.Second):
			t.Fatalf("packet %d (%d bytes) never arrived", i, n)
		}
	}
}

// TestCarrierRejectsOversizedPacket keeps a peer from making the receiver
// allocate far more than a device frame holds.
func TestCarrierRejectsOversizedPacket(t *testing.T) {
	carrier := &StreamCarrier{}
	if err := carrier.Send(context.Background(), make([]byte, maxPacketSize+1)); err == nil {
		t.Fatal("expected an error for an oversized packet")
	}
	if err := carrier.Send(context.Background(), nil); err == nil {
		t.Fatal("expected an error for an empty packet")
	}
}
