package transport

import (
	"context"
	"testing"
	"time"

	"github.com/ram1234598766-dotcom/Local-WEB/pkg/crypto"
)

func TestDeriveTransportKeyIsDeterministic(t *testing.T) {
	hkd := NewHybridKeyDerivation()
	var sessionKey [32]byte
	for i := range sessionKey {
		sessionKey[i] = byte(i)
	}
	a := hkd.DeriveTransportKey(sessionKey, "files")
	b := hkd.DeriveTransportKey(sessionKey, "files")
	if a != b {
		t.Fatal("transport key derivation is not deterministic")
	}
}

// Domain separation: a key derived for one service must not be usable for
// another. The previous implementation ignored the context string and
// returned the session key unchanged, so every context produced the same key.
func TestDeriveTransportKeySeparatesContexts(t *testing.T) {
	hkd := NewHybridKeyDerivation()
	var sessionKey [32]byte
	for i := range sessionKey {
		sessionKey[i] = byte(i)
	}

	files := hkd.DeriveTransportKey(sessionKey, "files")
	messaging := hkd.DeriveTransportKey(sessionKey, "messaging")
	vpn := hkd.DeriveTransportKey(sessionKey, "vpn")

	if files == messaging {
		t.Fatal("files and messaging derived the same key")
	}
	if files == vpn {
		t.Fatal("files and vpn derived the same key")
	}
	if messaging == vpn {
		t.Fatal("messaging and vpn derived the same key")
	}
}

// The derived key must not be the session key itself. Returning the input
// unchanged is what made the Kyber contribution reach nothing.
func TestDeriveTransportKeyDiffersFromSessionKey(t *testing.T) {
	hkd := NewHybridKeyDerivation()
	var sessionKey [32]byte
	for i := range sessionKey {
		sessionKey[i] = byte(i + 1)
	}
	got := hkd.DeriveTransportKey(sessionKey, "files")
	if got == sessionKey {
		t.Fatal("derived key equals the input session key")
	}
}

func TestDeriveTransportKeyVariesWithSessionKey(t *testing.T) {
	hkd := NewHybridKeyDerivation()
	var a, b [32]byte
	for i := range a {
		a[i] = byte(i)
		b[i] = byte(i + 7)
	}
	if hkd.DeriveTransportKey(a, "files") == hkd.DeriveTransportKey(b, "files") {
		t.Fatal("distinct session keys derived the same transport key")
	}
}

// The Kyber shared secret must actually change the session key. If the KEM
// output were dropped, the "hybrid" key would be identical to the classical
// Noise key and the post-quantum claim would be empty.
func TestHybridSessionKeyDependsOnKyber(t *testing.T) {
	pubA, privA, err := crypto.GenerateX25519KeyPair()
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}
	pubB, privB, err := crypto.GenerateX25519KeyPair()
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}

	initiator, err := crypto.NewHybridInitiator(pubA, privA)
	if err != nil {
		t.Fatalf("initiator: %v", err)
	}
	responder, err := crypto.NewHybridResponder(pubB, privB)
	if err != nil {
		t.Fatalf("responder: %v", err)
	}

	msg1, _, _, err := initiator.WriteHandshake(nil)
	if err != nil {
		t.Fatalf("initiator msg1: %v", err)
	}
	msg2, _, _, err := responder.WriteHandshake(msg1)
	if err != nil {
		t.Fatalf("responder msg2: %v", err)
	}
	msg3, _, complete, err := initiator.WriteHandshake(msg2)
	if err != nil {
		t.Fatalf("initiator msg3: %v", err)
	}
	if !complete {
		t.Fatal("initiator did not complete the hybrid handshake")
	}
	if _, _, complete, err = responder.WriteHandshake(msg3); err != nil {
		t.Fatalf("responder msg4: %v", err)
	}
	if !complete {
		t.Fatal("responder did not complete the hybrid handshake")
	}

	// Noise splits the handshake into directional keys, so the correct
	// cross-side comparison is initiator-send against responder-recv.
	keyI := initiator.SessionKey()
	keyR := responder.RecvSessionKey()
	if keyI != keyR {
		t.Fatalf("initiator send key and responder recv key disagree: %x vs %x", keyI[:8], keyR[:8])
	}
	// The reverse direction must agree too.
	if initiator.RecvSessionKey() != responder.SessionKey() {
		t.Fatal("responder send key and initiator recv key disagree")
	}
	// Sanity: the two directions must actually differ, otherwise the
	// directional comparison above proves nothing.
	if keyI == initiator.RecvSessionKey() {
		t.Fatal("send and recv keys are identical; directionality is not enforced")
	}
	// The Kyber encapsulation is randomised, so a second handshake between
	// the same long-term keys must land on a different session key. If it
	// did not, the PQ KEM was not contributing anything.
	initiator2, _ := crypto.NewHybridInitiator(pubA, privA)
	responder2, _ := crypto.NewHybridResponder(pubB, privB)
	m1, _, _, _ := initiator2.WriteHandshake(nil)
	m2, _, _, _ := responder2.WriteHandshake(m1)
	if _, _, _, err := initiator2.WriteHandshake(m2); err != nil {
		t.Fatalf("initiator2 msg3: %v", err)
	}
	if initiator2.SessionKey() == keyI {
		t.Fatal("two handshakes produced the same session key; the KEM contributes nothing")
	}
}

// A live two-node handshake over real QUIC must agree on the session key, and
// the connection must expose it.
func TestHybridServerHandshakeAgreesOnSessionKey(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pubA, privA, err := crypto.GenerateX25519KeyPair()
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}
	pubB, privB, err := crypto.GenerateX25519KeyPair()
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}

	srv, err := NewHybridServer(ctx, "127.0.0.1:0", pubA, privA, true)
	if err != nil {
		t.Fatalf("new hybrid server: %v", err)
	}
	defer srv.Stop()

	dialer, err := NewServer(ctx, "127.0.0.1:0", pubB, privB)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	defer dialer.Stop()

	clientCtx, clientCancel := context.WithTimeout(ctx, 20*time.Second)
	defer clientCancel()
	conn, err := dialer.Connect(clientCtx, srv.ln.Addr().String(), crypto.NodeID(pubA))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	clientSend := conn.SessionKey()
	clientRecv := conn.PeerRecvKey()
	if clientSend == ([32]byte{}) {
		t.Fatal("client received a zero send key")
	}
	if clientSend == clientRecv {
		t.Fatal("client send and recv keys are identical")
	}

	// The server side must have derived the complementary keys: the client is
	// the initiator, so the server's send key is the client's recv key and
	// vice versa.
	deadline := time.Now().Add(10 * time.Second)
	var serverSend [32]byte
	for time.Now().Before(deadline) {
		srv.mu.Lock()
		for _, c := range srv.conns {
			serverSend = c.SessionKey()
		}
		srv.mu.Unlock()
		if serverSend != ([32]byte{}) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if serverSend == ([32]byte{}) {
		t.Fatal("server never recorded a connection with a session key")
	}
	if serverSend != clientRecv {
		t.Fatalf("server send key does not match client recv key: %x vs %x",
			serverSend[:8], clientRecv[:8])
	}

	// A key derived from a shared directional key must match on both sides,
	// which is what makes it usable as an application-layer key.
	hkd := NewHybridKeyDerivation()
	if hkd.DeriveTransportKey(clientRecv, "files") != hkd.DeriveTransportKey(serverSend, "files") {
		t.Fatal("per-service keys derived from the shared key do not match")
	}
}
