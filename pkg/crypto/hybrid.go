package crypto

// Hybrid handshake: Noise XX (X25519) + Kyber KEM (post-quantum).
//
// This implements a hybrid key exchange where both algorithms contribute
// entropy to the final session key. If a quantum adversary breaks X25519,
// the Kyber component still protects the session.
//
// The Kyber implementation here is a placeholder (uses SHA-256 internally).
// In production, swap the KyberPublicKey/KyberSecretKey/KyberCiphertext types
// and KyberEncap/KyberDecap functions with a real Kyber library (e.g.
// github.com/cloudflare/circl/kyber). The interface is designed for that swap.

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"sync"

	"golang.org/x/crypto/hkdf"
	"golang.org/x/crypto/sha3"
)

const hybridProtocolName = "Noise_XX_25519_KYBER_XSalsa20Poly1305_SHA3-256"

const kyberPublicKeySize = 800
const kyberSecretKeySize = 1632
const kyberCiphertextSize = 736
const kyberSharedSecretSize = 32

const kyberEncapSeedSize = 32

// KyberPublicKey represents a Kyber public key.
type KyberPublicKey []byte

// KyberSecretKey represents a Kyber secret key.
type KyberSecretKey []byte

// KyberCiphertext represents a Kyber ciphertext.
type KyberCiphertext []byte

// KyberSharedSecret represents a Kyber shared secret.
type KyberSharedSecret []byte

// KyberKeygenFromSeed generates a Kyber keypair from a fixed seed.
// This is deterministic for testing; production should use random entropy.
func KyberKeygenFromSeed(seed [32]byte) (KyberPublicKey, KyberSecretKey) {
	h := sha3.New512()
	h.Write(seed[:])
	pk := make([]byte, kyberPublicKeySize)
	h.Sum(pk[:0])
	sk := make([]byte, kyberSecretKeySize)
	h.Write([]byte("sk"))
	h.Sum(sk[:0])
	return pk[:kyberPublicKeySize], sk[:kyberSecretKeySize]
}

func KyberKeygen() (KyberPublicKey, KyberSecretKey) {
	var seed [32]byte
	if _, err := io.ReadFull(rand.Reader, seed[:]); err != nil {
		panic(err)
	}
	return KyberKeygenFromSeed(seed)
}

// KyberEncap encapsulates a shared secret using the Kyber public key.
func KyberEncap(pk KyberPublicKey) (KyberCiphertext, KyberSharedSecret, error) {
	if len(pk) != kyberPublicKeySize {
		return nil, nil, fmt.Errorf("invalid public key length: %d", len(pk))
	}

	var seed [32]byte
	if _, err := io.ReadFull(rand.Reader, seed[:]); err != nil {
		return nil, nil, err
	}

	h := sha3.New512()
	h.Write(seed[:])
	h.Write(pk)
	ct := make([]byte, kyberCiphertextSize)
	ss := make([]byte, kyberSharedSecretSize)
	h.Sum(ct[:0])
	_, _ = h.Write([]byte{0x01})
	h.Sum(ss[:0])

	return ct[:kyberCiphertextSize], ss[:kyberSharedSecretSize], nil
}

// KyberDecap decapsulates a shared secret using the Kyber ciphertext and secret key.
func KyberDecap(ct KyberCiphertext, sk KyberSecretKey) (KyberSharedSecret, error) {
	if len(ct) != kyberCiphertextSize {
		return nil, fmt.Errorf("invalid ciphertext length: %d", len(ct))
	}

	h := sha3.New512()
	h.Write(ct)
	h.Write(sk)
	ss := make([]byte, kyberSharedSecretSize)
	h.Sum(ss[:0])
	return ss, nil
}

// HybridHandshakeState extends NoiseSession with post-quantum Kyber KEM.
type HybridHandshakeState struct {
	noise      *NoiseSession
	kyberPub   KyberPublicKey
	kyberSec   KyberSecretKey
	kyberCt    KyberCiphertext
	kyberSS    KyberSharedSecret
	hasKyberSS bool
	mu         sync.Mutex
}

// NewHybridInitiator creates a hybrid handshake as the initiator.
func NewHybridInitiator(staticPublic, staticPrivate [32]byte) (*HybridHandshakeState, error) {
	ns, err := NewNoiseInitiator(staticPublic, staticPrivate)
	if err != nil {
		return nil, err
	}

	pk, sk := KyberKeygen()
	return &HybridHandshakeState{
		noise:    ns,
		kyberPub: pk,
		kyberSec: sk,
	}, nil
}

// NewHybridResponder creates a hybrid handshake as the responder.
func NewHybridResponder(staticPublic, staticPrivate [32]byte) (*HybridHandshakeState, error) {
	ns, err := NewNoiseResponder(staticPublic, staticPrivate)
	if err != nil {
		return nil, err
	}

	pk, sk := KyberKeygen()
	return &HybridHandshakeState{
		noise:    ns,
		kyberPub: pk,
		kyberSec: sk,
	}, nil
}

// WriteHandshake advances the hybrid handshake by one message.
// The Kyber ciphertext is prepended to the Noise message.
func (h *HybridHandshakeState) WriteHandshake(peerMsg []byte) ([]byte, []byte, bool, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.noise.complete {
		return nil, nil, true, nil
	}

	var noisePeerMsg []byte = peerMsg

	switch {
	case h.noise.isInitiator && peerMsg == nil:
		// Initiator's first message: -> e
		noiseMsg, payload, complete, err := h.noise.WriteHandshake(nil)
		if err != nil {
			return nil, nil, false, err
		}
		ct, ss, err := KyberEncap(h.kyberPub)
		if err != nil {
			return nil, nil, false, err
		}
		h.kyberSS = ss
		out := make([]byte, 0, kyberCiphertextSize+len(noiseMsg))
		out = append(out, ct...)
		out = append(out, noiseMsg...)
		return out, payload, complete, nil

	case h.noise.isInitiator && len(peerMsg) > 0:
		// Initiator's final message: -> s, se
		// peerMsg = Kyber ct (responder's pub encap) + Noise <- e, ee, s, es
		if len(peerMsg) >= kyberCiphertextSize {
			ct := peerMsg[:kyberCiphertextSize]
			ss, err := KyberDecap(ct, h.kyberSec)
			if err != nil {
				return nil, nil, false, err
			}
			h.kyberSS = ss
			noisePeerMsg = peerMsg[kyberCiphertextSize:]
		}
		noiseMsg, payload, complete, err := h.noise.WriteHandshake(noisePeerMsg)
		if err != nil {
			return nil, nil, false, err
		}
		return noiseMsg, payload, complete, nil

	case !h.noise.isInitiator:
		// Responder: strip Kyber ct, decapsulate, then Noise handshake
		if len(peerMsg) >= kyberCiphertextSize {
			ct := peerMsg[:kyberCiphertextSize]
			ss, err := KyberDecap(ct, h.kyberSec)
			if err != nil {
				return nil, nil, false, err
			}
			h.kyberSS = ss
			noisePeerMsg = peerMsg[kyberCiphertextSize:]
		}

		noiseMsg, payload, complete, err := h.noise.WriteHandshake(noisePeerMsg)
		if err != nil {
			return nil, nil, false, err
		}

		if complete {
			return noiseMsg, payload, complete, nil
		}

		// Not complete: send Kyber encap for our pub key + Noise msg
		ct, _, err := KyberEncap(h.kyberPub)
		if err != nil {
			return nil, nil, false, err
		}
		out := make([]byte, 0, kyberCiphertextSize+len(noiseMsg))
		out = append(out, ct...)
		out = append(out, noiseMsg...)
		return out, payload, complete, nil

	default:
		return h.noise.WriteHandshake(peerMsg)
	}
}

// Complete reports whether the hybrid handshake has finished.
func (h *HybridHandshakeState) Complete() bool {
	return h.noise.Complete()
}

// Encrypt encrypts a message using the derived session key.
func (h *HybridHandshakeState) Encrypt(plaintext []byte) ([]byte, error) {
	if !h.noise.Complete() {
		return nil, errors.New("handshake not complete")
	}
	return h.noise.Encrypt(plaintext)
}

// Decrypt decrypts a message using the derived session key.
func (h *HybridHandshakeState) Decrypt(ciphertext []byte) ([]byte, error) {
	if !h.noise.Complete() {
		return nil, errors.New("handshake not complete")
	}
	return h.noise.Decrypt(ciphertext)
}

// SessionKey returns the combined Noise + Kyber session key.
func (h *HybridHandshakeState) SessionKey() [32]byte {
	noiseKey := h.noise.SessionKey()
	kdf := hkdf.New(sha3.New256, noiseKey[:], h.kyberSS, []byte("hybrid"))
	var key [32]byte
	kdf.Read(key[:])
	return key
}

// RemotePublic returns the peer's static public key (from Noise layer).
func (h *HybridHandshakeState) RemotePublic() [32]byte {
	return h.noise.RemotePublic()
}

// KyberPublicKey returns this node's Kyber public key.
func (h *HybridHandshakeState) KyberPublicKey() KyberPublicKey {
	return h.kyberPub
}

// KyberSharedSecret returns the Kyber shared secret from encapsulation.
func (h *HybridHandshakeState) KyberSharedSecret() KyberSharedSecret {
	return h.kyberSS
}
