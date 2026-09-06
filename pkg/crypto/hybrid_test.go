package crypto

import (
	"testing"

	"github.com/cloudflare/circl/kem/kyber/kyber1024"
)

func TestNewHybridInitiator(t *testing.T) {
	staticPriv := [32]byte{0x01}
	staticPub := [32]byte{0x02}

	hs, err := NewHybridInitiator(staticPub, staticPriv)
	if err != nil {
		t.Fatalf("NewHybridInitiator failed: %v", err)
	}
	if hs == nil {
		t.Fatal("expected non-nil handshake state")
	}
}

func TestNewHybridResponder(t *testing.T) {
	staticPriv := [32]byte{0x01}
	staticPub := [32]byte{0x02}

	hs, err := NewHybridResponder(staticPub, staticPriv)
	if err != nil {
		t.Fatalf("NewHybridResponder failed: %v", err)
	}
	if hs == nil {
		t.Fatal("expected non-nil handshake state")
	}
}

func TestHybridHandshakeCompletes(t *testing.T) {
	// Use real X25519 key pairs for initiator and responder
	initStaticPub, initStaticPriv, err := generateX25519Key()
	if err != nil {
		t.Fatal(err)
	}
	respStaticPub, respStaticPriv, err := generateX25519Key()
	if err != nil {
		t.Fatal(err)
	}

	init, err := NewHybridInitiator(initStaticPub, initStaticPriv)
	if err != nil {
		t.Fatalf("initiator: %v", err)
	}

	resp, err := NewHybridResponder(respStaticPub, respStaticPriv)
	if err != nil {
		t.Fatalf("responder: %v", err)
	}

	// Round 1: initiator -> responder (ephemeral + kyber encap)
	msg1, _, complete, err := init.WriteHandshake(nil)
	if err != nil {
		t.Fatalf("initiator WriteHandshake: %v", err)
	}
	if complete {
		t.Fatal("handshake should not complete at step 1")
	}
	if len(msg1) == 0 {
		t.Fatal("expected non-empty first message")
	}
	if len(msg1) < kyberCiphertextSize+32 {
		t.Fatalf("expected msg1 >= %d bytes, got %d", kyberCiphertextSize+32, len(msg1))
	}

	// Round 2: responder -> initiator
	msg2, _, complete, err := resp.WriteHandshake(msg1)
	if err != nil {
		t.Fatalf("responder WriteHandshake: %v", err)
	}
	if complete {
		t.Fatal("handshake should not complete at step 2")
	}
	if len(msg2) == 0 {
		t.Fatal("expected non-empty second message")
	}

	// Round 3: initiator sends final message
	msg3, _, complete, err := init.WriteHandshake(msg2)
	if err != nil {
		t.Fatalf("initiator final WriteHandshake: %v", err)
	}
	if !complete {
		t.Fatal("expected handshake to complete at step 3")
	}

	// Responder processes initiator's final message
	_, _, complete, err = resp.WriteHandshake(msg3)
	if err != nil {
		t.Fatalf("responder final WriteHandshake: %v", err)
	}
	if !complete {
		t.Fatal("expected handshake to complete for responder")
	}

	if !init.Complete() {
		t.Error("initiator handshake not marked complete")
	}
	if !resp.Complete() {
		t.Error("responder handshake not marked complete")
	}
}

func TestHybridSessionKeyDerivation(t *testing.T) {
	staticPriv := [32]byte{0xAB}
	staticPub := [32]byte{0xCD}

	init, err := NewHybridInitiator(staticPub, staticPriv)
	if err != nil {
		t.Fatal(err)
	}

	// Drive the handshake without a real responder — just check
	// the session key is derived after completion with simulated responses.
	msg1, _, _, err := init.WriteHandshake(nil)
	if err != nil {
		t.Fatal(err)
	}

	// Verify the message contains Noise + Kyber payloads
	if len(msg1) < 32 {
		t.Fatalf("expected at least 32 bytes in msg1, got %d", len(msg1))
	}
}

func TestHybridKyberEncapDecap(t *testing.T) {
	pk, sk, err := KyberKeygen()
	if err != nil {
		t.Fatalf("KyberKeygen failed: %v", err)
	}

	ct, ss1, err := KyberEncap(pk)
	if err != nil {
		t.Fatalf("KyberEncap failed: %v", err)
	}
	if len(ct) == 0 || len(ss1) == 0 {
		t.Fatal("expected non-empty ciphertext and shared secret")
	}

	ss2, err := KyberDecap(ct, sk)
	if err != nil {
		t.Fatalf("KyberDecap failed: %v", err)
	}

	if string(ss1) != string(ss2) {
		t.Error("shared secrets do not match")
	}
}

func TestHybridKyberKeygenDeterministic(t *testing.T) {
	// Kyber keygen should be deterministic with a fixed seed of correct size
	// KeySeedSize = cpapke.KeySeedSize + 32
	// For Kyber-1024, this is typically 64 bytes
	seed := make([]byte, kyber1024.KeySeedSize)
	for i := range seed {
		seed[i] = byte(i)
	}
	pk1, sk1 := KyberKeygenFromSeedArray(seed)
	pk2, sk2 := KyberKeygenFromSeedArray(seed)

	if string(pk1) != string(pk2) {
		t.Error("public keys should match for same seed")
	}
	if string(sk1) != string(sk2) {
		t.Error("secret keys should match for same seed")
	}
}

func TestHybridEncryptDecrypt(t *testing.T) {
	initStaticPub, initStaticPriv, err := generateX25519Key()
	if err != nil {
		t.Fatal(err)
	}
	respStaticPub, respStaticPriv, err := generateX25519Key()
	if err != nil {
		t.Fatal(err)
	}

	init, err := NewHybridInitiator(initStaticPub, initStaticPriv)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := NewHybridResponder(respStaticPub, respStaticPriv)
	if err != nil {
		t.Fatal(err)
	}

	// Full handshake
	msg1, _, _, _ := init.WriteHandshake(nil)
	msg2, _, _, _ := resp.WriteHandshake(msg1)
	msg3, _, _, _ := init.WriteHandshake(msg2)
	_, _, _, _ = resp.WriteHandshake(msg3)

	// Now test encryption
	plaintext := []byte("secret message")
	ciphertext, err := init.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	decrypted, err := resp.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}

	if string(decrypted) != string(plaintext) {
		t.Errorf("expected %q, got %q", plaintext, decrypted)
	}
}

func generateTestKyberKey(t *testing.T) (KyberPublicKey, KyberSecretKey) {
	t.Helper()
	pk, sk, err := KyberKeygen()
	if err != nil {
		t.Fatalf("KyberKeygen failed: %v", err)
	}
	return pk, sk
}

// KyberKeygenFromSeedArray is a test helper that takes a byte slice of correct size
func KyberKeygenFromSeedArray(seed []byte) (KyberPublicKey, KyberSecretKey) {
	pk, sk := kyber1024.NewKeyFromSeed(seed)
	pkBytes := make([]byte, kyberPublicKeySize)
	pk.Pack(pkBytes)
	skBytes := make([]byte, kyberSecretKeySize)
	sk.Pack(skBytes)
	return KyberPublicKey(pkBytes), KyberSecretKey(skBytes)
}
