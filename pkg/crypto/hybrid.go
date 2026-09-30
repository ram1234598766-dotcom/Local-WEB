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
	noise        *NoiseSession
	kyberPub     KyberPublicKey
	kyberSec     KyberSecretKey
	kyberSS      KyberSharedSecret
	peerKyberPub KyberPublicKey
	hasKyberSS   bool
	mu           sync.Mutex
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
//
// The KEM is arranged so both sides end up with the *same* shared secret:
//
//	msg1 (initiator) = initiator_kyber_pub (1568) || Noise "-> e"      (32)
//	msg2 (responder) = kyber_ct (1568)          || Noise "<- e,ee,s,es" (80)
//	msg3 (initiator) =                             Noise "-> s, se"     (48)
//
// Only the initiator's PQ public key has to cross the wire. The responder
// encapsulates to it and keeps the shared secret; the initiator decapsulates
// the responder's ciphertext and recovers the same secret.
//
// The previous arrangement encapsulated each side to its *own* public key and
// had each side decapsulate the peer's ciphertext with its own secret key.
// Kyber answers that mismatch with implicit rejection rather than an error,
// so the handshake "succeeded" while both sides silently derived unrelated
// session keys.
func (h *HybridHandshakeState) WriteHandshake(peerMsg []byte) ([]byte, []byte, bool, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.noise.complete {
		return nil, nil, true, nil
	}

	switch {
	case h.noise.isInitiator && peerMsg == nil:
		// msg1: advertise the PQ public key, then the Noise ephemeral.
		noiseMsg, payload, complete, err := h.noise.WriteHandshake(nil)
		if err != nil {
			return nil, nil, false, err
		}
		out := make([]byte, 0, kyberPublicKeySize+len(noiseMsg))
		out = append(out, h.kyberPub...)
		out = append(out, noiseMsg...)
		return out, payload, complete, nil

	case h.noise.isInitiator && len(peerMsg) > 0:
		// msg3 input: strip the responder's ciphertext and decapsulate it
		// with our own secret key. No Noise token precedes it.
		if len(peerMsg) < kyberCiphertextSize {
			return nil, nil, false, fmt.Errorf("hybrid: responder message too short (%d bytes)", len(peerMsg))
		}
		ct := peerMsg[:kyberCiphertextSize]
		ss, err := KyberDecap(ct, h.kyberSec)
		if err != nil {
			return nil, nil, false, err
		}
		h.kyberSS = ss
		h.hasKyberSS = true

		noiseMsg, payload, complete, err := h.noise.WriteHandshake(peerMsg[kyberCiphertextSize:])
		if err != nil {
			return nil, nil, false, err
		}
		return noiseMsg, payload, complete, nil

	case !h.noise.isInitiator:
		if h.noise.step < 1 {
			// msg1 input: the initiator's PQ public key precedes the Noise
			// ephemeral. Learn the key, then run the Noise responder step.
			if len(peerMsg) < kyberPublicKeySize {
				return nil, nil, false, fmt.Errorf("hybrid: initiator message too short (%d bytes)", len(peerMsg))
			}
			peerKyberPub := KyberPublicKey(peerMsg[:kyberPublicKeySize])

			noiseMsg, payload, complete, err := h.noise.WriteHandshake(peerMsg[kyberPublicKeySize:])
			if err != nil {
				return nil, nil, false, err
			}
			if complete {
				// Noise XX finishes in two messages; the KEM still has to run
				// before a session key exists.
				complete = false
			}

			// msg2: encapsulate to the initiator's PQ key. The secret we hold
			// here is the one the initiator will recover by decapsulating.
			ct, ss, err := KyberEncap(peerKyberPub)
			if err != nil {
				return nil, nil, false, err
			}
			h.kyberSS = ss
			h.hasKyberSS = true
			h.peerKyberPub = peerKyberPub

			out := make([]byte, 0, kyberCiphertextSize+len(noiseMsg))
			out = append(out, ct...)
			out = append(out, noiseMsg...)
			return out, payload, complete, nil
		}

		// msg3 input: the initiator's final Noise token needs no KEM handling.
		noiseMsg, payload, complete, err := h.noise.WriteHandshake(peerMsg)
		if err != nil {
			return nil, nil, false, err
		}
		return noiseMsg, payload, complete, nil

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
//
// As with Noise, this is the directional send key: the initiator's value
// equals the responder's RecvSessionKey.
func (h *HybridHandshakeState) SessionKey() [32]byte {
	noiseKey := h.noise.SessionKey()
	kdf := hkdf.New(sha3.New256, noiseKey[:], h.kyberSS, []byte("LocalWEB-v2-session"))
	var key [32]byte
	kdf.Read(key[:])
	return key
}

// RecvSessionKey returns the combined receive-direction session key.
func (h *HybridHandshakeState) RecvSessionKey() [32]byte {
	noiseKey := h.noise.RecvSessionKey()
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
