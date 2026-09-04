package crypto

import (
	"bytes"
	"testing"

	"golang.org/x/crypto/nacl/secretbox"
)

func TestNoiseXXKeySchedule(t *testing.T) {
	pubA, privA, err := generateX25519Key()
	if err != nil {
		t.Fatal(err)
	}
	pubB, privB, err := generateX25519Key()
	if err != nil {
		t.Fatal(err)
	}

	initiator, err := NewNoiseInitiator(pubA, privA)
	if err != nil {
		t.Fatal(err)
	}
	responder, err := NewNoiseResponder(pubB, privB)
	if err != nil {
		t.Fatal(err)
	}

	// Step 1: initiator -> responder
	msg1, _, _, err := initiator.WriteHandshake(nil)
	if err != nil {
		t.Fatalf("initiator step1: %v", err)
	}
	t.Logf("msg1: %d bytes", len(msg1))

	// Responder processes msg1, sends msg2
	msg2, _, _, err := responder.WriteHandshake(msg1)
	if err != nil {
		t.Fatalf("responder step2: %v", err)
	}
	t.Logf("msg2: %d bytes", len(msg2))

	// Initiator processes msg2, sends msg3
	msg3, _, complete, err := initiator.WriteHandshake(msg2)
	if err != nil {
		t.Fatalf("initiator step3: %v", err)
	}
	t.Logf("msg3: %d bytes, complete=%v", len(msg3), complete)

	// Responder processes msg3
	msg4, _, completeR, err := responder.WriteHandshake(msg3)
	if err != nil {
		t.Fatalf("responder step4: %v", err)
	}
	t.Logf("msg4: %d bytes, complete=%v", len(msg4), completeR)

	// Identity verification
	rpI := initiator.RemotePublic()
	rpR := responder.RemotePublic()
	if !bytes.Equal(rpI[:], responder.staticPublic[:]) {
		t.Fatalf("identity mismatch: initiator sees %x, responder pub is %x",
			rpI[:8], responder.staticPublic[:8])
	}
	if !bytes.Equal(rpR[:], initiator.staticPublic[:]) {
		t.Fatalf("identity mismatch: responder sees %x, initiator pub is %x",
			rpR[:8], initiator.staticPublic[:8])
	}

	// Application data round-trip
	payload := []byte("hello secure world")
	encrypted, err := initiator.Encrypt(payload)
	if err != nil {
		t.Fatalf("initiator encrypt: %v", err)
	}

	decrypted, err := responder.Decrypt(encrypted)
	if err != nil {
		t.Fatalf("responder decrypt: %v", err)
	}
	if !bytes.Equal(decrypted, payload) {
		t.Fatalf("payload mismatch: got %q, want %q", decrypted, payload)
	}
}

func TestSecretboxRoundTrip(t *testing.T) {
	var key [32]byte
	var nonce [24]byte
	plaintext := []byte("hello world")

	enc := secretbox.Seal(nil, plaintext, &nonce, &key)
	dec, ok := secretbox.Open(nil, enc, &nonce, &key)
	if !ok {
		t.Fatal("secretbox decrypt failed")
	}
	if !bytes.Equal(dec, plaintext) {
		t.Fatal("round trip mismatch")
	}
}
