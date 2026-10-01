package transport

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ram1234598766-dotcom/Local-WEB/pkg/crypto"
)

// recordingShaper captures what SendTo hands to the shaper.
type recordingShaper struct {
	mu       sync.Mutex
	services []string
	peers    []string
	sizes    []int
	err      error
}

func (r *recordingShaper) Send(ctx context.Context, service, peerID string, data []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.services = append(r.services, service)
	r.peers = append(r.peers, peerID)
	r.sizes = append(r.sizes, len(data))
	return r.err
}

func (r *recordingShaper) counts() (int, []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.services), append([]string(nil), r.services...)
}

// Phase 6 item 6.6 claimed QoS was implemented. It was: pkg/qos had token
// buckets, priorities and an HTB hierarchy, and 12 passing tests - but nothing
// outside the package ever constructed a QoSManager, so no traffic was shaped.
// These tests pin the wiring rather than the policy maths, which pkg/qos already
// covers.
func TestSendToPassesFramesThroughShaper(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pubA, privA, err := crypto.GenerateX25519KeyPair()
	if err != nil {
		t.Fatal(err)
	}
	pubB, privB, err := crypto.GenerateX25519KeyPair()
	if err != nil {
		t.Fatal(err)
	}

	srvA, err := NewServer(ctx, "127.0.0.1:0", pubA, privA)
	if err != nil {
		t.Fatal(err)
	}
	defer srvA.Stop()

	shaper := &recordingShaper{}
	srvB, err := NewServer(ctx, "127.0.0.1:0", pubB, privB, WithTrafficShaper(shaper))
	if err != nil {
		t.Fatal(err)
	}
	defer srvB.Stop()

	addrA := srvA.ln.Addr().String()
	conn, err := srvB.Connect(ctx, addrA, crypto.NodeID(pubA))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close()

	payload := []byte("hello over the shaped path")
	if err := srvB.SendTo(ctx, crypto.NodeID(pubA), ServiceFS, 0, payload); err != nil {
		t.Fatalf("SendTo: %v", err)
	}

	n, services := shaper.counts()
	if n != 1 {
		t.Fatalf("shaper saw %d frames, want 1", n)
	}
	if services[0] != "files" {
		t.Errorf("shaper saw service %q, want %q", services[0], "files")
	}

	shaper.mu.Lock()
	size, peer := shaper.sizes[0], shaper.peers[0]
	shaper.mu.Unlock()

	if size == 0 {
		t.Error("shaper received a zero-length frame; the payload was not offered to it")
	}
	if size < len(payload) {
		t.Errorf("shaper saw %d bytes, less than the %d-byte payload; the frame should be encoded first", size, len(payload))
	}
	if peer == "" {
		t.Error("shaper received an empty peer identifier")
	}
}

// A shaper that rejects traffic must stop the frame reaching the wire, otherwise
// installing one would be decorative.
func TestSendToHonoursShaperRejection(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pubA, privA, err := crypto.GenerateX25519KeyPair()
	if err != nil {
		t.Fatal(err)
	}
	pubB, privB, err := crypto.GenerateX25519KeyPair()
	if err != nil {
		t.Fatal(err)
	}

	srvA, err := NewServer(ctx, "127.0.0.1:0", pubA, privA)
	if err != nil {
		t.Fatal(err)
	}
	defer srvA.Stop()

	rejected := errors.New("token bucket exhausted")
	shaper := &recordingShaper{err: rejected}
	srvB, err := NewServer(ctx, "127.0.0.1:0", pubB, privB, WithTrafficShaper(shaper))
	if err != nil {
		t.Fatal(err)
	}
	defer srvB.Stop()

	addrA := srvA.ln.Addr().String()
	conn, err := srvB.Connect(ctx, addrA, crypto.NodeID(pubA))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close()

	err = srvB.SendTo(ctx, crypto.NodeID(pubA), ServiceVoice, 0, []byte("x"))
	if err == nil {
		t.Fatal("expected SendTo to fail when the shaper rejects the frame")
	}
	if !strings.Contains(err.Error(), "token bucket exhausted") {
		t.Errorf("expected the shaper's error to be surfaced, got: %v", err)
	}
	if !strings.Contains(err.Error(), "voice") {
		t.Errorf("expected the error to name the service, got: %v", err)
	}
}

func TestServiceNameCoversEveryServiceID(t *testing.T) {
	cases := map[ServiceID]string{
		ServiceControl: "control",
		ServiceDNS:     "dns",
		ServiceHTTP:    "http",
		ServiceMsg:     "messaging",
		ServiceFS:      "files",
		ServiceRelay:   "relay",
		ServiceVoice:   "voice",
		ServiceVPN:     "vpn",
		ServiceDocs:    "docs",
		ServiceReg:     "registry",
	}
	for id, want := range cases {
		if got := serviceName(id); got != want {
			t.Errorf("serviceName(%q) = %q, want %q", string(byte(id)), got, want)
		}
	}
	// An unregistered ID must still produce a stable, non-empty label so QoS
	// classes cannot collide on the empty string.
	if got := serviceName(ServiceID('Z')); got != "svc-Z" {
		t.Errorf("serviceName(unknown) = %q, want %q", got, "svc-Z")
	}
}

// Without a shaper SendTo must behave exactly as before.
func TestSendToWithoutShaperStillWorks(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pubA, privA, err := crypto.GenerateX25519KeyPair()
	if err != nil {
		t.Fatal(err)
	}
	pubB, privB, err := crypto.GenerateX25519KeyPair()
	if err != nil {
		t.Fatal(err)
	}

	srvA, err := NewServer(ctx, "127.0.0.1:0", pubA, privA)
	if err != nil {
		t.Fatal(err)
	}
	defer srvA.Stop()

	srvB, err := NewServer(ctx, "127.0.0.1:0", pubB, privB)
	if err != nil {
		t.Fatal(err)
	}
	defer srvB.Stop()

	addrA := srvA.ln.Addr().String()
	conn, err := srvB.Connect(ctx, addrA, crypto.NodeID(pubA))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close()

	if err := srvB.SendTo(ctx, crypto.NodeID(pubA), ServiceDNS, 0, []byte("payload")); err != nil {
		t.Fatalf("SendTo without a shaper should succeed: %v", err)
	}
}
