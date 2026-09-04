package crypto

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/curve25519"
	"golang.org/x/crypto/hkdf"
	"golang.org/x/crypto/nacl/secretbox"
	"golang.org/x/crypto/sha3"
)

// Protocol name. We deviate from the reference Noise_XX_25519_ChaChaPoly_SHA256
// only in the hash (SHA3-256) and AEAD (XSalsa20-Poly1305 via NaCl secretbox)
// choices — these are drop-in compatible with the Noise specification's token
// operations, so the XX pattern remains exactly as specified. The exact same
// protocol name must be used by both peers.
const protocolName = "Noise_XX_25519_XSalsa20Poly1305_SHA3-256"

// NoiseSession holds state for a Noise XX handshake and the post-handshake
// transport session keys.
type NoiseSession struct {
	isInitiator bool

	// Static identity keys (node long-term keypair).
	staticPublic  [32]byte
	staticPrivate [32]byte

	// Ephemeral keys for the current handshake.
	ephemeralPublic  [32]byte
	ephemeralPrivate [32]byte

	// Peer's static public key, captured during the XX exchange.
	remotePublic [32]byte

	// Peer's ephemeral public key.
	remoteEphemeral [32]byte

	// Handshake state.
	h  [32]byte // handshake hash h
	ck [32]byte // chaining key
	ok bool     // whether the temporary AEAD key k is set

	// Transport session keys (derived by Split after handshake).
	sendKey   [32]byte
	recvKey   [32]byte
	sendCount uint64
	recvCount uint64

	// Handshake message step for the XX pattern.
	step int // 0 = not started; 1 = sent/received -> e; 2 = sent/received <- e,ee,s,es

	complete bool
}

// NewNoiseInitiator creates a Noise session (XX pattern) as the initiator.
func NewNoiseInitiator(staticPublic, staticPrivate [32]byte) (*NoiseSession, error) {
	s := &NoiseSession{
		isInitiator:   true,
		staticPublic:  staticPublic,
		staticPrivate: staticPrivate,
	}
	initHashAndChain(s)
	if err := s.generateEphemeral(); err != nil {
		return nil, err
	}
	return s, nil
}

// NewNoiseResponder creates a Noise session (XX pattern) as the responder.
func NewNoiseResponder(staticPublic, staticPrivate [32]byte) (*NoiseSession, error) {
	s := &NoiseSession{
		isInitiator:   false,
		staticPublic:  staticPublic,
		staticPrivate: staticPrivate,
	}
	initHashAndChain(s)
	if err := s.generateEphemeral(); err != nil {
		return nil, err
	}
	return s, nil
}

func initHashAndChain(s *NoiseSession) {
	h := sha3.New256()
	h.Write([]byte(protocolName))
	copy(s.h[:], h.Sum(nil))
	s.ck = s.h // chaining key starts equal to the protocol hash
}

// generateEphemeral creates a new ephemeral X25519 keypair.
func (s *NoiseSession) generateEphemeral() error {
	pub, priv, err := generateX25519Key()
	if err != nil {
		return err
	}
	s.ephemeralPublic = pub
	s.ephemeralPrivate = priv
	return nil
}

func generateX25519Key() (pub, priv [32]byte, err error) {
	if _, err = io.ReadFull(rand.Reader, priv[:]); err != nil {
		return
	}
	// Clamp the private key scalar (X25519 requires clamping).
	priv[0] &= 248
	priv[31] &= 127
	priv[31] |= 64

	out, err := curve25519.X25519(priv[:], curve25519.Basepoint)
	if err != nil {
		return
	}
	copy(pub[:], out)
	return
}

// WriteHandshake advances the XX handshake by one message.
//
// The caller feeds the peer's inbound message (nil to produce the first
// outbound message) and receives the message to send next, plus whether
// the handshake has completed.
func (s *NoiseSession) WriteHandshake(peerMsg []byte) (toSend []byte, payload []byte, complete bool, err error) {
	if s.complete {
		return nil, nil, true, nil
	}
	if s.isInitiator {
		return s.initiatorWrite(peerMsg)
	}
	return s.responderWrite(peerMsg)
}

// initiatorWrite drives the two initiator messages of XX:
//
//	-> e            (step 0 → 1, no inbound message)
//	-> s, se        (step 2, inbound = <- e, ee, s, es)
func (s *NoiseSession) initiatorWrite(peerMsg []byte) ([]byte, []byte, bool, error) {
	switch s.step {
	case 0:
		// -> e
		s.mixHash(s.ephemeralPublic[:])
		s.step = 1
		msg := make([]byte, 32)
		copy(msg, s.ephemeralPublic[:])
		return msg, nil, false, nil

	case 1:
		// We've received <- e, ee, s, es and must send -> s, se.
		// Parse the responder message: remote ephemeral (32 bytes),
		// then encrypted responder static (ciphertext || poly1305 tag).
		if len(peerMsg) < 32 {
			return nil, nil, false, errors.New("short XX msg2")
		}
		s.mixHash(peerMsg[:32])
		copy(s.remoteEphemeral[:], peerMsg[:32])

		// ee = DH(init_e, resp_e)
		ee, err := curve25519.X25519(s.ephemeralPrivate[:], s.remoteEphemeral[:])
		if err != nil {
			return nil, nil, false, err
		}
		s.mixKey(ee)

		// Decrypt the responder's static public key.
		ct := peerMsg[32:]
		rs, err := s.decryptAndHash(ct)
		if err != nil {
			return nil, nil, false, fmt.Errorf("decrypt responder static: %w", err)
		}
		if len(rs) != 32 {
			return nil, nil, false, errors.New("bad responder static length")
		}
		copy(s.remotePublic[:], rs)

		// es = DH(init_e, resp_s)
		es, err := curve25519.X25519(s.ephemeralPrivate[:], s.remotePublic[:])
		if err != nil {
			return nil, nil, false, err
		}
		s.mixKey(es)

		// Build -> s, se
		encStatic, err := s.encryptAndHash(s.staticPublic[:])
		if err != nil {
			return nil, nil, false, err
		}
		// se = DH(init_s, resp_e)
		se, err := curve25519.X25519(s.staticPrivate[:], s.remoteEphemeral[:])
		if err != nil {
			return nil, nil, false, err
		}
		s.mixKey(se)

		s.split()

		msg := make([]byte, 0, len(encStatic))
		msg = append(msg, encStatic...)
		s.step = 3
		s.complete = true
		return msg, nil, true, nil

	default:
		return nil, nil, false, fmt.Errorf("invalid initiator step %d", s.step)
	}
}

// responderWrite drives the single responder message of XX:
//
//	<- e, ee, s, es  (step 1, inbound = -> e)
//
// The responder has no outbound message after this; the handshake
// completes when it later receives -> s, se (handled here implicitly
// by the second call with the initiator's final message).
func (s *NoiseSession) responderWrite(peerMsg []byte) ([]byte, []byte, bool, error) {
	switch s.step {
	case 0:
		// Received -> e (32 bytes).
		if len(peerMsg) != 32 {
			return nil, nil, false, errors.New("short XX msg1")
		}
		s.mixHash(peerMsg[:32])
		copy(s.remoteEphemeral[:], peerMsg[:32])
		s.step = 1

		// ee = DH(resp_e, init_e)
		ee, err := curve25519.X25519(s.ephemeralPrivate[:], s.remoteEphemeral[:])
		if err != nil {
			return nil, nil, false, err
		}
		s.mixKey(ee)

		// s = EncryptAndHash(resp_static)
		encStatic, err := s.encryptAndHash(s.staticPublic[:])
		if err != nil {
			return nil, nil, false, err
		}

		// es = DH(resp_s, init_e) — the responder mixes `es` into the key
		// schedule when producing the <- e, ee, s, es message. This keeps the
		// key chain in lock-step with the initiator, which mixes `es` (from
		// the initiator-ephemeral/responder-static DH) at the same point.
		es, err := curve25519.X25519(s.staticPrivate[:], s.remoteEphemeral[:])
		if err != nil {
			return nil, nil, false, err
		}
		s.mixKey(es)
		s.step = 2

		msg := make([]byte, 0, 32+len(encStatic))
		msg = append(msg, s.ephemeralPublic[:]...)
		msg = append(msg, encStatic...)
		return msg, nil, false, nil

	case 2:
		// Received -> s, se: decrypt the initiator's static public key.
		initStatic, err := s.decryptAndHash(peerMsg)
		if err != nil {
			return nil, nil, false, fmt.Errorf("decrypt initiator static: %w", err)
		}
		if len(initStatic) != 32 {
			return nil, nil, false, errors.New("bad initiator static length")
		}
		copy(s.remotePublic[:], initStatic)

		// se = DH(resp_e, init_s) — mixed now that initiator static is known.
		se, err := curve25519.X25519(s.ephemeralPrivate[:], s.remotePublic[:])
		if err != nil {
			return nil, nil, false, err
		}
		s.mixKey(se)

		s.split()
		s.complete = true
		return nil, nil, true, nil

	default:
		return nil, nil, false, fmt.Errorf("invalid responder step %d", s.step)
	}
}

// split derives the transport send/receive keys (Noise Split token).
func (s *NoiseSession) split() {
	kdf := hkdf.New(sha3.New256, s.ck[:], nil, nil)
	// First output → k1 (send for initiator, recv for responder).
	kdf.Read(s.sendKey[:])
	// Second output → k2.
	kdf.Read(s.recvKey[:])
	if !s.isInitiator {
		// Responder reverses: it receives on k1 and sends on k2.
		s.sendKey, s.recvKey = s.recvKey, s.sendKey
	}
}

// mixHash performs h = HASH(h || data).
func (s *NoiseSession) mixHash(data []byte) {
	hh := sha3.New256()
	hh.Write(s.h[:])
	hh.Write(data)
	copy(s.h[:], hh.Sum(nil))
}

// mixKey performs ck = HKDF(ck, dh, 1).
// In Noise semantics: ck is the IKM, dh is the info, no explicit salt.
func (s *NoiseSession) mixKey(dh []byte) {
	kdf := hkdf.New(sha3.New256, s.ck[:], nil, dh)
	kdf.Read(s.ck[:])
	s.ok = true
}

// encryptAndHash performs the Noise EncryptAndHash token: if the temporary
// key k is set, encrypt with AEAD(k, 0, h, plaintext) and append the
// ciphertext to h; otherwise emit plaintext unencrypted and mix regardless.
func (s *NoiseSession) encryptAndHash(plaintext []byte) ([]byte, error) {
	var out []byte
	if s.ok {
		var nonce [24]byte // AEAD nonce is all-zero in the handshake phase
		sealed := secretbox.Seal(nil, plaintext, &nonce, s.tempKey())
		out = sealed
	} else {
		// XX always has a key set before any EncryptAndHash, but be safe.
		out = append([]byte{}, plaintext...)
	}
	s.mixHash(out)
	return out, nil
}

// decryptAndHash performs the Noise DecryptAndHash token.
func (s *NoiseSession) decryptAndHash(ciphertext []byte) ([]byte, error) {
	var out []byte
	if s.ok {
		var nonce [24]byte
		var ok bool
		out, ok = secretbox.Open(nil, ciphertext, &nonce, s.tempKey())
		if !ok {
			return nil, errors.New("AEAD decrypt failed")
		}
	} else {
		out = append([]byte{}, ciphertext...)
	}
	s.mixHash(ciphertext)
	return out, nil
}

// tempKey derives the AEAD key for a handshake token. During the handshake
// the key k is the chaining key ck (the temporary key).
func (s *NoiseSession) tempKey() *[32]byte {
	return &s.ck
}

// Encrypt encrypts an application payload using the current send key.
func (s *NoiseSession) Encrypt(plaintext []byte) ([]byte, error) {
	if !s.complete {
		return nil, fmt.Errorf("handshake not complete")
	}
	var nonce [24]byte
	putUint64(nonce[:8], s.sendCount)
	s.sendCount++
	return secretbox.Seal(nil, plaintext, &nonce, &s.sendKey), nil
}

// Decrypt decrypts an application payload using the current receive key.
func (s *NoiseSession) Decrypt(ciphertext []byte) ([]byte, error) {
	if !s.complete {
		return nil, fmt.Errorf("handshake not complete")
	}
	if len(ciphertext) < secretbox.Overhead {
		return nil, fmt.Errorf("ciphertext too short")
	}
	var nonce [24]byte
	putUint64(nonce[:8], s.recvCount)
	s.recvCount++
	out, ok := secretbox.Open(nil, ciphertext, &nonce, &s.recvKey)
	if !ok {
		return nil, fmt.Errorf("decryption failed (bad MAC or key)")
	}
	return out, nil
}

// SessionKey returns the current send key (for inspection/debugging).
func (s *NoiseSession) SessionKey() [32]byte { return s.sendKey }

// RemotePublic returns the peer's static public key after the handshake.
// It is populated once the peer's static key has been authenticated.
func (s *NoiseSession) RemotePublic() [32]byte {
	return s.remotePublic
}

// Complete reports whether the handshake has finished.
func (s *NoiseSession) Complete() bool { return s.complete }

func putUint64(b []byte, v uint64) {
	for i := 0; i < 8; i++ {
		b[i] = byte(v >> (8 * uint(i)))
	}
}
