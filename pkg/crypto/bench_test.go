package crypto

import (
	"testing"
)

func BenchmarkNoiseHandshake(b *testing.B) {
	initPub, initPriv, err := generateX25519Key()
	if err != nil {
		b.Fatal(err)
	}
	respPub, respPriv, err := generateX25519Key()
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		init, err := NewNoiseInitiator(initPub, initPriv)
		if err != nil {
			b.Fatal(err)
		}
		resp, err := NewNoiseResponder(respPub, respPriv)
		if err != nil {
			b.Fatal(err)
		}

		msg1, _, _, _ := init.WriteHandshake(nil)
		msg2, _, _, _ := resp.WriteHandshake(msg1)
		_, _, _, _ = init.WriteHandshake(msg2)
		_, _, _, _ = resp.WriteHandshake(nil)
	}
}

func BenchmarkNoiseEncrypt(b *testing.B) {
	initPub, initPriv, _ := generateX25519Key()
	respPub, respPriv, _ := generateX25519Key()

	init, _ := NewNoiseInitiator(initPub, initPriv)
	resp, _ := NewNoiseResponder(respPub, respPriv)

	msg1, _, _, _ := init.WriteHandshake(nil)
	msg2, _, _, _ := resp.WriteHandshake(msg1)
	_, _, _, _ = init.WriteHandshake(msg2)
	_, _, _, _ = resp.WriteHandshake(nil)

	plaintext := make([]byte, 1024)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = init.Encrypt(plaintext)
	}
}

func BenchmarkNoiseDecrypt(b *testing.B) {
	initPub, initPriv, _ := generateX25519Key()
	respPub, respPriv, _ := generateX25519Key()

	init, _ := NewNoiseInitiator(initPub, initPriv)
	resp, _ := NewNoiseResponder(respPub, respPriv)

	msg1, _, _, _ := init.WriteHandshake(nil)
	msg2, _, _, _ := resp.WriteHandshake(msg1)
	_, _, _, _ = init.WriteHandshake(msg2)
	_, _, _, _ = resp.WriteHandshake(nil)

	plaintext := make([]byte, 1024)
	ciphertext, _ := init.Encrypt(plaintext)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = resp.Decrypt(ciphertext)
	}
}

func BenchmarkKyberEncap(b *testing.B) {
	pk, _ := KyberKeygenFromSeed([32]byte{0x42})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = KyberEncap(pk)
	}
}

func BenchmarkKyberDecap(b *testing.B) {
	pk, sk := KyberKeygenFromSeed([32]byte{0x42})
	ct, _, _ := KyberEncap(pk)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = KyberDecap(ct, sk)
	}
}

func BenchmarkHybridHandshake(b *testing.B) {
	initPub, initPriv, _ := generateX25519Key()
	respPub, respPriv, _ := generateX25519Key()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		init, err := NewHybridInitiator(initPub, initPriv)
		if err != nil {
			b.Fatal(err)
		}
		resp, err := NewHybridResponder(respPub, respPriv)
		if err != nil {
			b.Fatal(err)
		}

		msg1, _, _, _ := init.WriteHandshake(nil)
		msg2, _, _, _ := resp.WriteHandshake(msg1)
		msg3, _, _, _ := init.WriteHandshake(msg2)
		_, _, _, _ = resp.WriteHandshake(msg3)
	}
}

func BenchmarkX25519Keygen(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = generateX25519Key()
	}
}
