package transport

import (
	"context"
	"strings"
	"testing"

	"github.com/ram1234598766-dotcom/Local-WEB/pkg/crypto"
)

// Phase 8 item 8.6 asks for InsecureSkipVerify to require an explicit opt-out,
// which implies verification is on by default. That cannot be done yet, and this
// test is the evidence for why.
//
// The QUIC listener presents a self-signed certificate from
// GenerateSelfSignedCert: no CA, no chain, no pinning. A client that verifies
// certificates therefore rejects every peer, so flipping the default would break
// the entire mesh rather than harden it.
//
// Peer identity is authenticated by the Noise XX handshake beneath TLS, which is
// the control that actually prevents impersonation: Connect compares the
// authenticated NodeID against the expected one and refuses on mismatch
// (TestConnectIdentityMismatch). TLS here provides encryption and integrity.
//
// Closing 8.6 needs certificate pinning keyed to the node identity, which does
// not exist yet. Until then the daemon exposes -tls-verify so the choice is
// explicit rather than an accident of a zero value.
func TestClientCannotVerifySelfSignedServer(t *testing.T) {
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

	srvA, err := NewServer(ctx, "127.0.0.1:0", pubA, privA)
	if err != nil {
		t.Fatalf("server A: %v", err)
	}
	defer srvA.Stop()

	srvB, err := NewServer(ctx, "127.0.0.1:0", pubB, privB, WithEnforceTLSVerify(true))
	if err != nil {
		t.Fatalf("server B: %v", err)
	}
	defer srvB.Stop()

	addrA := srvA.ln.Addr().String()
	idA := crypto.NodeID(pubA)

	// B verifies certificates and dials A, which serves a self-signed cert.
	conn, err := srvB.Connect(ctx, addrA, idA)
	if err == nil {
		if conn != nil {
			conn.Close()
		}
		t.Fatal("expected certificate verification to fail against a self-signed server, but the dial succeeded")
	}

	// The failure must be about the certificate, not something incidental.
	msg := err.Error()
	if !strings.Contains(msg, "certificate") && !strings.Contains(msg, "x509") {
		t.Errorf("expected a certificate verification error, got: %v", err)
	}
}

// With verification off, the same dial succeeds. This is what makes the insecure
// default load-bearing rather than merely tolerated: turn it off and the mesh
// stops connecting.
func TestSelfSignedDialSucceedsWhenVerificationDisabled(t *testing.T) {
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

	addrA := srvA.ln.Addr().String()
	conn, err := srvB.Connect(ctx, addrA, crypto.NodeID(pubA))
	if err != nil {
		t.Fatalf("dial with verification disabled should succeed: %v", err)
	}
	defer conn.Close()
}

// NewHybridServer must forward ServerOption values, otherwise the daemon's
// -tls-verify flag would silently have no effect.
func TestHybridServerForwardsTLSOption(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pub, priv, err := crypto.GenerateX25519KeyPair()
	if err != nil {
		t.Fatalf("keypair: %v", err)
	}

	srv, err := NewHybridServer(ctx, "127.0.0.1:0", pub, priv, false, WithEnforceTLSVerify(true))
	if err != nil {
		t.Fatalf("NewHybridServer: %v", err)
	}
	defer srv.Stop()

	if !srv.enforceTLS {
		t.Error("NewHybridServer dropped the WithEnforceTLSVerify option; " +
			"the daemon's -tls-verify flag would be a no-op")
	}
}
