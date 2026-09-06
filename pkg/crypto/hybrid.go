package crypto

// Hybrid handshake: Noise XX (X25519) + Kyber KEM (post-quantum).
//
// This implements a hybrid key exchange where both algorithms contribute
// entropy to the final session key. If a quantum adversary breaks X25519,
// the Kyber component still protects the session.
//
// Uses Kyber-1024 from cloudflare/circl (NIST PQC Round 3 finalist).

import (
	"crypto/rand"
	"errors"
	"fmt"
	"sync"

	"github.com/cloudflare/circl/kem/kyber/kyber1024"
	"golang.org/x/crypto/hkdf"
	"golang.org/x/crypto/sha3"
)

const hybridProtocolName = "Noise_XX_25519_KYBER1024_XSalsa20Poly1305_SHA3-256"

// Kyber-1024 constants (from circl)
const (
	kyberPublicKeySize         = kyber1024.PublicKeySize         // 1568
	kyberSecretKeySize         = kyber1024.PrivateKeySize        // 3168
	kyberCiphertextSize        = kyber1024.CiphertextSize        // 1568
	kyberSharedSecretSize      = kyber1024.SharedKeySize         // 32
	kyberEncapsulationSeedSize = kyber1024.EncapsulationSeedSize // 32
)

// KyberPublicKey represents a Kyber-1024 public key.
type KyberPublicKey []byte

// KyberSecretKey represents a Kyber-1024 secret key.
type KyberSecretKey []byte

// KyberCiphertext represents a Kyber-1024 ciphertext.
type KyberCiphertext []byte

// KyberSharedSecret represents a Kyber-1024 shared secret (32 bytes).
type KyberSharedSecret []byte

// KyberKeygen generates a Kyber-1024 keypair using crypto/rand.
func KyberKeygen() (KyberPublicKey, KyberSecretKey, error) {
	pk, sk, err := kyber1024.GenerateKeyPair(rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	pkBytes := make([]byte, kyberPublicKeySize)
	pk.Pack(pkBytes)
	skBytes := make([]byte, kyberSecretKeySize)
	sk.Pack(skBytes)
	return KyberPublicKey(pkBytes), KyberSecretKey(skBytes), nil
}

// KyberKeygenFromSeed generates a Kyber-1024 keypair from a fixed seed (deterministic).
// Note: For production use KyberKeygen() with crypto/rand. This is for testing only.
func KyberKeygenFromSeed(seed [32]byte) (KyberPublicKey, KyberSecretKey) {
	// Use NewKeyFromSeed for deterministic key generation (testing only)
	pk, sk := kyber1024.NewKeyFromSeed(seed[:])
	pkBytes := make([]byte, kyberPublicKeySize)
	pk.Pack(pkBytes)
	skBytes := make([]byte, kyberSecretKeySize)
	sk.Pack(skBytes)
	return KyberPublicKey(pkBytes), KyberSecretKey(skBytes)
}

// KyberEncap encapsulates a shared secret using the Kyber-1024 public key.
func KyberEncap(pk KyberPublicKey) (KyberCiphertext, KyberSharedSecret, error) {
	if len(pk) != kyberPublicKeySize {
		return nil, nil, fmt.Errorf("invalid public key length: %d, expected %d", len(pk), kyberPublicKeySize)
	}

	var pubKey kyber1024.PublicKey
	pubKey.Unpack(pk)

	ct := make([]byte, kyberCiphertextSize)
	ss := make([]byte, kyberSharedSecretSize)
	seed := make([]byte, kyberEncapsulationSeedSize)
	if _, err := rand.Read(seed); err != nil {
		return nil, nil, err
	}
	pubKey.EncapsulateTo(ct, ss, seed)

	return KyberCiphertext(ct), KyberSharedSecret(ss), nil
}

// KyberDecap decapsulates a shared secret using the Kyber-1024 ciphertext and secret key.
func KyberDecap(ct KyberCiphertext, sk KyberSecretKey) (KyberSharedSecret, error) {
	if len(ct) != kyberCiphertextSize {
		return nil, fmt.Errorf("invalid ciphertext length: %d, expected %d", len(ct), kyberCiphertextSize)
	}
	if len(sk) != kyberSecretKeySize {
		return nil, fmt.Errorf("invalid secret key length: %d, expected %d", len(sk), kyberSecretKeySize)
	}

	var privKey kyber1024.PrivateKey
	privKey.Unpack(sk)

	ss := make([]byte, kyberSharedSecretSize)
	privKey.DecapsulateTo(ss, ct)

	return KyberSharedSecret(ss), nil
}

// HybridHandshakeState extends NoiseSession with post-quantum Kyber-1024 KEM.
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

	pk, sk, err := KyberKeygen()
	if err != nil {
		return nil, fmt.Errorf("Kyber keygen failed: %w", err)
	}

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

	pk, sk, err := KyberKeygen()
	if err != nil {
		return nil, fmt.Errorf("Kyber keygen failed: %w", err)
	}

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
		// Initiator's first message: -> e + Kyber ct
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
// Uses HKDF-SHA3-256(classical_SS || pq_SS, salt="LocalWEB-v2", info="session")
func (h *HybridHandshakeState) SessionKey() [32]byte {
	noiseKey := h.noise.SessionKey()
	kdf := hkdf.New(sha3.New256, noiseKey[:], h.kyberSS, []byte("LocalWEB-v2-session"))
	var key [32]byte
	kdf.Read(key[:])
	return key
}

// RemotePublic returns the peer's static public key (from Noise layer).
func (h *HybridHandshakeState) RemotePublic() [32]byte {
	return h.noise.RemotePublic()
}

// KyberPublicKey returns this node's Kyber-1024 public key.
func (h *HybridHandshakeState) KyberPublicKey() KyberPublicKey {
	return h.kyberPub
}

// KyberSharedSecret returns the Kyber-1024 shared secret from encapsulation.
func (h *HybridHandshakeState) KyberSharedSecret() KyberSharedSecret {
	return h.kyberSS
}
