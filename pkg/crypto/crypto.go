package crypto

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha512"
	"fmt"
	"io"
	"math/big"

	"github.com/cloudflare/circl/sign/ed448"
	"github.com/cloudflare/circl/sign/eddilithium3"
	"github.com/cloudflare/circl/sign/mldsa/mldsa65"
	"golang.org/x/crypto/curve25519"
	"golang.org/x/crypto/sha3"
)

// KeySize is the byte length of LocalWEB public/private keys.
const KeySize = 32

// KeyType represents the type of identity key.
type KeyType uint8

const (
	KeyTypeEd25519 KeyType = iota
	KeyTypeEd448
	KeyTypeEd448Dilithium3
	KeyTypeMLDSA65
)

// Identity represents a node identity with key type.
type KeyIdentity struct {
	Type       KeyType
	PublicKey  []byte
	PrivateKey []byte
}

// GenerateKeyPair creates a new Ed25519 keypair.
// The public key is returned as a 32-byte array (the Ed25519 public key
// is the first 32 bytes of the 64-byte public key material, which is the
// compressed encoding of the point).
func GenerateKeyPair() (pub, priv [32]byte, err error) {
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return pub, priv, fmt.Errorf("generate keypair: %w", err)
	}

	copy(pub[:], pubKey[:32])
	copy(priv[:], privKey[:32])
	return pub, priv, nil
}

// GenerateEd448KeyPair creates a new Ed448 keypair.
func GenerateEd448KeyPair() (pub, priv [57]byte, err error) {
	pubKey, privKey, err := ed448.GenerateKey(rand.Reader)
	if err != nil {
		return pub, priv, fmt.Errorf("generate Ed448 keypair: %w", err)
	}

	copy(pub[:], pubKey[:57])
	copy(priv[:], privKey[:57])
	return pub, priv, nil
}

// GenerateEd448Dilithium3KeyPair creates a new Ed448-Dilithium3 hybrid keypair.
// This provides post-quantum security with Ed448 classical fallback.
func GenerateEd448Dilithium3KeyPair() (pub, priv []byte, err error) {
	pk, sk, err := eddilithium3.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generate Ed448-Dilithium3 keypair: %w", err)
	}

	pubBytes, err := pk.MarshalBinary()
	if err != nil {
		return nil, nil, fmt.Errorf("marshal Ed448-Dilithium3 pub: %w", err)
	}

	privBytes, err := sk.MarshalBinary()
	if err != nil {
		return nil, nil, fmt.Errorf("marshal Ed448-Dilithium3 priv: %w", err)
	}

	return pubBytes, privBytes, nil
}

// GenerateMLDSA65KeyPair creates a new ML-DSA-65 (Dilithium3 equivalent) keypair.
func GenerateMLDSA65KeyPair() (pub, priv []byte, err error) {
	pk, sk, err := mldsa65.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generate ML-DSA-65 keypair: %w", err)
	}

	pubBytes, err := pk.MarshalBinary()
	if err != nil {
		return nil, nil, fmt.Errorf("marshal ML-DSA-65 pub: %w", err)
	}

	privBytes, err := sk.MarshalBinary()
	if err != nil {
		return nil, nil, fmt.Errorf("marshal ML-DSA-65 priv: %w", err)
	}

	return pubBytes, privBytes, nil
}

// NodeID derives a node ID from a public key using SHA3-256.
// This matches the DHT's 256-bit Kademlia key space.
func NodeID(pub [32]byte) [32]byte {
	return SHA3Hash(pub[:])
}

// NodeIDFromBytes derives a node ID from a public key of any type using SHA3-256.
func NodeIDFromBytes(pub []byte) [32]byte {
	return SHA3Hash(pub)
}

// Sign signs a message with the given private key.
func Sign(priv [32]byte, msg []byte) ([]byte, error) {
	// Expand the 32-byte seed to a full Ed25519 private key (64 bytes).
	fullPriv := make([]byte, ed25519.PrivateKeySize)
	copy(fullPriv[:32], priv[:])
	// Derive the public key portion from the seed.
	pubKey, err := computePubFromSeed(priv)
	if err != nil {
		return nil, err
	}
	copy(fullPriv[32:], pubKey)

	return ed25519.Sign(ed25519.PrivateKey(fullPriv), msg), nil
}

// SignEd448 signs a message with an Ed448 private key.
func SignEd448(priv [57]byte, msg []byte) ([]byte, error) {
	fullPriv := make([]byte, ed448.PrivateKeySize)
	copy(fullPriv[:57], priv[:])
	// Derive public key
	pubKey, err := computeEd448PubFromSeed(priv)
	if err != nil {
		return nil, err
	}
	copy(fullPriv[57:], pubKey)

	return ed448.Sign(ed448.PrivateKey(fullPriv), msg, ""), nil
}

// SignPQ signs a message with a post-quantum private key (Ed448-Dilithium3 or ML-DSA-65).
func SignPQ(priv []byte, msg []byte) ([]byte, error) {
	// Try Ed448-Dilithium3 first
	var sk eddilithium3.PrivateKey
	if err := sk.UnmarshalBinary(priv); err == nil {
		sig := make([]byte, eddilithium3.SignatureSize)
		eddilithium3.SignTo(&sk, msg, sig)
		return sig, nil
	}

	// Try ML-DSA-65
	var sk2 mldsa65.PrivateKey
	if err := sk2.UnmarshalBinary(priv); err == nil {
		sig := make([]byte, mldsa65.SignatureSize)
		err := mldsa65.SignTo(&sk2, msg, nil, false, sig)
		if err != nil {
			return nil, err
		}
		return sig, nil
	}

	return nil, fmt.Errorf("unknown PQ private key format")
}

// Verify checks a signature against a public key and message.
func Verify(pub [32]byte, msg, sig []byte) bool {
	return ed25519.Verify(ed25519.PublicKey(pub[:]), msg, sig)
}

// VerifyEd448 checks an Ed448 signature.
func VerifyEd448(pub [57]byte, msg, sig []byte) bool {
	return ed448.Verify(ed448.PublicKey(pub[:]), msg, sig, "")
}

// VerifyPQ verifies a post-quantum signature (Ed448-Dilithium3 or ML-DSA-65).
func VerifyPQ(pub []byte, msg, sig []byte) bool {
	// Try Ed448-Dilithium3 first
	var pk eddilithium3.PublicKey
	if err := pk.UnmarshalBinary(pub); err == nil {
		return eddilithium3.Verify(&pk, msg, sig)
	}

	// Try ML-DSA-65
	var pk2 mldsa65.PublicKey
	if err := pk2.UnmarshalBinary(pub); err == nil {
		return mldsa65.Verify(&pk2, msg, nil, sig)
	}

	return false
}

// computePubFromSeed derives the Ed25519 public key from a 32-byte seed.
func computePubFromSeed(seed [32]byte) ([]byte, error) {
	priv := make([]byte, ed25519.PrivateKeySize)
	copy(priv[:32], seed[:])
	// ed25519.NewKeyFromSeed computes both halves.
	fullPriv := ed25519.NewKeyFromSeed(priv[:32])
	return fullPriv[32:], nil
}

// computeEd448PubFromSeed derives the Ed448 public key from a 57-byte seed.
func computeEd448PubFromSeed(seed [57]byte) ([]byte, error) {
	priv := make([]byte, ed448.PrivateKeySize)
	copy(priv[:57], seed[:])
	fullPriv := ed448.NewKeyFromSeed(priv[:57])
	return fullPriv[57:], nil
}

// SHA3Hash computes the SHA3-256 hash of data.
func SHA3Hash(data []byte) [32]byte {
	h := sha3.New256()
	h.Write(data)
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

// SeedToPrivateKey expands a 32-byte seed into a full 64-byte Ed25519
// private key (seed || public). Useful for x509 and TLS interop.
func SeedToPrivateKey(seed [32]byte) ([]byte, error) {
	full := ed25519.NewKeyFromSeed(seed[:])
	return []byte(full), nil
}

// GenerateX25519KeyPair creates a new X25519 keypair for Noise handshakes.
func GenerateX25519KeyPair() (pub, priv [32]byte, err error) {
	if _, err = io.ReadFull(rand.Reader, priv[:]); err != nil {
		return
	}
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

var (
	curve25519p *big.Int
	curve25519d *big.Int
)

func init() {
	curve25519p, _ = new(big.Int).SetString("57896044618658097711785492504343953926634992332820282019728792003956564819949", 10)
	curve25519d, _ = new(big.Int).SetString("37095705934669483943151133361467617231603323252428812480226727831072828823221", 10)
}

// Ed25519PublicToX25519 converts an Ed25519 public key to an X25519 public key
// for use as a Noise identity key in X25519-based handshakes.
func Ed25519PublicToX25519(pub [32]byte) ([32]byte, error) {
	// Decode Ed25519 compressed Edwards point.
	// The public key is in compressed form: first bit is x sign, rest is y.
	yBytes := make([]byte, 32)
	copy(yBytes, pub[:])
	yBytes[0] &= 0x7f

	y := new(big.Int).SetBytes(yBytes)
	one := big.NewInt(1)
	p := curve25519p

	// u = (1 + y) / (1 - y) mod p
	// u = (1 + y) * inv(1 - y) mod p
	denom := new(big.Int).Sub(p, y) // p - y
	inv := new(big.Int).ModInverse(denom, p)
	num := new(big.Int).Add(one, y)
	u := new(big.Int).Mod(new(big.Int).Mul(num, inv), p)

	var xpub [32]byte
	uBytes := u.Bytes()
	copy(xpub[32-len(uBytes):], uBytes)
	return xpub, nil
}

// Ed25519PrivateToX25519 converts an Ed25519 private key seed to an X25519
// private key for use as a Noise identity key in X25519-based handshakes.
func Ed25519PrivateToX25519(priv [32]byte) [32]byte {
	// Ed25519 expands the seed via SHA-512; first 32 bytes are the scalar.
	h := sha512.New()
	h.Write(priv[:])
	digest := h.Sum(nil)
	scalar := make([]byte, 32)
	copy(scalar, digest[:32])
	scalar[0] &= 248
	scalar[31] &= 127
	scalar[31] |= 64
	var xpriv [32]byte
	copy(xpriv[:], scalar)
	return xpriv
}
