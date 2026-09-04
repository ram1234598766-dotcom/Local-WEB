package registry

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"time"

	"github.com/mrityunjay/LocalWEB/pkg/crypto"
)

// Manifest describes a LocalWEB package.
type Manifest struct {
	Name        string            `yaml:"name"`
	Version     string            `yaml:"version"`
	Description string            `yaml:"description"`
	Author      string            `yaml:"author"`
	Homepage    string            `yaml:"homepage,omitempty"`
	License     string            `yaml:"license,omitempty"`
	Entry       string            `yaml:"entry"`
	Platform    []string          `yaml:"platform,omitempty"`
	Dependencies []string         `yaml:"dependencies,omitempty"`
	Checksums   map[string]string `yaml:"checksums"`
	Size        int64             `yaml:"size"`
	Created     time.Time         `yaml:"created"`
}

// PackageMeta is the searchable metadata stored in the registry and DHT.
type PackageMeta struct {
	ID          string
	Name        string
	Version     string
	Description string
	Author      string
	Platform    []string
	Entry       string
	Published   time.Time
	Updated     time.Time
	Downloads   int64
	Verified    bool
	PublisherID [32]byte
}

// SearchQuery filters package listings.
type SearchQuery struct {
	Query      string
	Platform   string
	Author     string
	Verified   bool
	Limit      int
	Offset     int
}

// SearchResult is the response for a search operation.
type SearchResult struct {
	Packages []PackageMeta
	Total    int
	Limit    int
	Offset   int
}

// Registry is the central interface for package management.
type Registry interface {
	Publish(pkg *LWPKG, authorPubKey [32]byte, privKey [32]byte) (string, error)
	Install(packageID string) (*LWPKG, error)
	List() ([]PackageMeta, error)
	Search(q SearchQuery) (*SearchResult, error)
	Get(packageID string) (*PackageMeta, error)
	Delete(packageID string) error
}

// DHTDistributor publishes and retrieves metadata via the DHT.
type DHTDistributor interface {
	PublishMeta(ctx context.Context, meta *PackageMeta) error
	SearchMeta(ctx context.Context, query string) ([]PackageMeta, error)
	ResolveMeta(ctx context.Context, packageID string) (*PackageMeta, error)
	Local() []PackageMeta
	AddLocal(meta *PackageMeta)
	RemoveLocal(id string)
}

// Signer wraps Ed25519 signing using the project crypto package.
type Signer interface {
	Sign(priv [32]byte, msg []byte) ([]byte, error)
	Verify(pub [32]byte, msg, sig []byte) bool
}

// CryptoSigner adapts pkg/crypto to the Signer interface.
type CryptoSigner struct{}

func (s *CryptoSigner) Sign(priv [32]byte, msg []byte) ([]byte, error) {
	return crypto.Sign(priv, msg)
}

func (s *CryptoSigner) Verify(pub [32]byte, msg, sig []byte) bool {
	return crypto.Verify(pub, msg, sig)
}

// ErrPackageNotFound is returned when a package does not exist.
var ErrPackageNotFound = fmt.Errorf("package not found")

// ErrInvalidSignature is returned when signature verification fails.
var ErrInvalidSignature = fmt.Errorf("invalid signature")

// ErrManifestInvalid is returned when manifest validation fails.
var ErrManifestInvalid = fmt.Errorf("invalid manifest")

// ErrPackageExists is returned when publishing a duplicate package ID.
var ErrPackageExists = fmt.Errorf("package already exists")

// Ed25519PublicKeySize is the byte length of an Ed25519 public key.
const Ed25519PublicKeySize = ed25519.PublicKeySize

// Ed25519SignatureSize is the byte length of an Ed25519 signature.
const Ed25519SignatureSize = ed25519.SignatureSize

// DefaultSearchLimit is the default maximum results for search.
const DefaultSearchLimit = 50

// MaxSearchLimit is the maximum allowed results for search.
const MaxSearchLimit = 200
