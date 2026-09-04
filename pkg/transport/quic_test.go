package transport

import (
	"context"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/mrityunjayjha/LocalWEB/pkg/crypto"
)

// TestTransportRoundTrip spins up two real quic-go servers, connects
// them, and exchanges a framed message over a DNS service stream.
func TestTransportRoundTrip(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pubA, privA, err := crypto.GenerateX25519KeyPair()
	if err != nil {
		t.Fatalf("keypair A: %v", err)
	}
	pubB, privB, err := crypto.GenerateX25519KeyPair()
	if err != nil {
		t.Fatalf("keypair B: %v", err)
	}

	idB := crypto.NodeID(pubB)

	// Server A on a random port
	srvA, err := NewServer(ctx, "127.0.0.1:0", pubA, privA)
	if err != nil {
		t.Fatalf("server A: %v", err)
	}
	defer srvA.Stop()

	// Capture A's bound port
	addrA := srvA.ln.Addr().String()

	// Server B advertises a DNS handler
	gotMsg := make(chan string, 1)
	srvB, err := NewServer(ctx, "127.0.0.1:0", pubB, privB)
	if err != nil {
		t.Fatalf("server B: %v", err)
	}
	defer srvB.Stop()

	srvB.RegisterHandler(ServiceDNS, func(ctx context.Context, s Stream) {
		buf := make([]byte, 1024)
		n, err := s.Read(buf)
		if n == 0 {
			gotMsg <- "ERR:read"
			return
		}
		if err != nil && err != io.EOF {
			gotMsg <- "ERR:read"
			return
		}
		frame, err := DecodeFrameBare(buf[:n])
		if err != nil {
			gotMsg <- "ERR:frame"
			return
		}
		gotMsg <- string(frame.Payload)
	})

	// B's address
	addrB := srvB.ln.Addr().String()

	// A connects to B
	conn, err := srvA.Connect(ctx, addrB, idB)
	if err != nil {
		t.Fatalf("connect A->B: %v", err)
	}

	// Open a DNS stream and send a framed message
	stream, err := conn.OpenStream(ctx, ServiceDNS)
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	payload := []byte("hello-localweb")
	frame := EncodeFrameBare(MsgPing, payload)
	if _, err := stream.Write(frame); err != nil {
		t.Fatalf("write: %v", err)
	}
	stream.Close()

	select {
	case got := <-gotMsg:
		if got != "hello-localweb" {
			t.Fatalf("unexpected payload: %q", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for message on server B")
	}

	// Verify A sees B as a peer
	peers := srvA.Peers()
	found := false
	for _, p := range peers {
		if p.ID == idB {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("server A did not register peer B; peers=%+v", peers)
	}

	t.Logf("round trip OK: A(%s) -> B(%s), peers=%d", addrA, addrB, len(peers))
	_ = addrA
}

// TestConnectIdentityMismatch verifies that a connection to the wrong
// expected NodeID is rejected after the Noise handshake.
func TestConnectIdentityMismatch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pubA, privA, _ := crypto.GenerateX25519KeyPair()
	pubB, privB, _ := crypto.GenerateX25519KeyPair()

	srvA, err := NewServer(ctx, "127.0.0.1:0", pubA, privA)
	if err != nil {
		t.Fatalf("server A: %v", err)
	}
	defer srvA.Stop()

	srvB, err := NewServer(ctx, "127.0.0.1:0", pubB, privB)
	if err != nil {
		t.Fatalf("server B: %v", err)
	}
	defer srvB.Stop()

	addrB := srvB.ln.Addr().String()

	// Wrong expected ID (some other random node ID)
	wrongID := crypto.NodeID(pubA) // definitely not B's ID

	_, err = srvA.Connect(ctx, addrB, wrongID)
	if err == nil {
		t.Fatal("expected identity mismatch error, got nil")
	}
	t.Logf("identity mismatch correctly rejected: %v", err)
}

func TestFrameEncoding(t *testing.T) {
	// Test that frames round-trip through Encode/Decode.
	for _, payload := range [][]byte{
		[]byte("hello"),
		{}, // empty payload
		[]byte("a longer payload with more bytes inside it to test the length field"),
	} {
		want := EncodeFrameBare(MsgStore, payload)
		got, err := DecodeFrameBare(want)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if got.Type != MsgStore {
			t.Fatalf("type mismatch: %d", got.Type)
		}
		if fmt.Sprintf("%q", got.Payload) != fmt.Sprintf("%q", payload) {
			t.Fatalf("payload mismatch: %q != %q", got.Payload, payload)
		}
	}
}
