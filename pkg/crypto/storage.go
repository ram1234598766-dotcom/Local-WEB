package crypto

import (
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/crypto/sha3"
)

const (
	keyFileMode = 0600
	dirMode     = 0700
)

// Identity represents a node's Ed25519 keypair persisted to disk.
type Identity struct {
	Public  string `json:"public"`  // hex-encoded 32 bytes
	Private string `json:"private"` // hex-encoded 32 bytes (seed)
}

// LoadOrGenerateIdentity loads the node identity from dir, or generates
// a new one and writes it to dir/identity.json if it does not exist.
// The private key is never printed to stdout; it is stored in a file
// with 0600 permissions.
func LoadOrGenerateIdentity(dir string) (pub, priv [32]byte, err error) {
	keyFile := filepath.Join(dir, "identity.json")

	// Try to load existing identity
	if data, readErr := os.ReadFile(keyFile); readErr == nil {
		var id Identity
		if jsonErr := json.Unmarshal(data, &id); jsonErr == nil {
			pubBytes, decErr := hex.DecodeString(id.Public)
			privBytes, decPrivErr := hex.DecodeString(id.Private)
			if decErr == nil && decPrivErr == nil && len(pubBytes) == 32 && len(privBytes) == 32 {
				copy(pub[:], pubBytes)
				copy(priv[:], privBytes)
				return pub, priv, nil
			}
		}
	}

	// Generate new keypair
	pub, priv, err = GenerateKeyPair()
	if err != nil {
		return pub, priv, fmt.Errorf("generate keypair: %w", err)
	}

	// Persist to disk
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return pub, priv, fmt.Errorf("create key dir: %w", err)
	}

	id := Identity{
		Public:  hex.EncodeToString(pub[:]),
		Private: hex.EncodeToString(priv[:]),
	}
	data, err := json.MarshalIndent(id, "", "  ")
	if err != nil {
		return pub, priv, fmt.Errorf("marshal identity: %w", err)
	}
	if err := os.WriteFile(keyFile, data, keyFileMode); err != nil {
		return pub, priv, fmt.Errorf("write identity: %w", err)
	}

	return pub, priv, nil
}

// DeriveStorageKey derives a 32-byte AES storage key from the node's
// private key seed using SHA3-256. This binds store encryption to node
// identity so that the database cannot be read without the node keypair.
func DeriveStorageKey(priv [32]byte) [32]byte {
	// Use SHA3-256 of the private seed as the encryption key
	var hash [32]byte
	h := sha3.New256()
	h.Write(priv[:])
	copy(hash[:], h.Sum(nil))
	return hash
}

// NodeIDFromPub computes the node ID from a public key.
// This is exported for use by the store package.
func NodeIDFromPub(pub [32]byte) [32]byte {
	return SHA3Hash(pub[:])
}

// SeedToEd25519 expands a 32-byte Ed25519 seed into a full 64-byte
// private key (seed || public). Used internally by Sign.
func SeedToEd25519(seed [32]byte) []byte {
	h := sha512.New()
	h.Write(seed[:])
	digest := h.Sum(nil)
	pubKey, _ := computePubFromSeed(seed)
	full := make([]byte, 64)
	copy(full[:32], digest[:32])
	copy(full[32:], pubKey)
	return full
}
