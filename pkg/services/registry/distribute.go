package registry

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/mrityunjay/LocalWEB/pkg/crypto"
	"github.com/mrityunjay/LocalWEB/pkg/dht"
)

// dhtMetaStore is the concrete DHT-backed implementation of DHTDistributor.
type dhtMetaStore struct {
	mu       sync.Mutex
	dht      *dht.DHT
	nodeID   dht.NodeID
	pubKey   [32]byte
	local    map[string]*PackageMeta
}

// NewDHTDistributor creates a DHT-backed metadata distributor.
func NewDHTDistributor(d *dht.DHT, pubKey [32]byte) DHTDistributor {
	return &dhtMetaStore{
		dht:    d,
		nodeID: dht.NodeIDFromPub(pubKey),
		pubKey: pubKey,
		local:  make(map[string]*PackageMeta),
	}
}

// PublishMeta stores package metadata in the DHT.
// The local cache is always updated; DHT errors are non-fatal.
func (d *dhtMetaStore) PublishMeta(ctx context.Context, meta *PackageMeta) error {
	d.mu.Lock()
	d.local[meta.ID] = meta
	d.mu.Unlock()

	key := "pkg:" + meta.ID
	data, err := marshalPackageMeta(meta)
	if err != nil {
		return fmt.Errorf("marshal meta: %w", err)
	}
	_ = d.dht.Store(ctx, key, data)
	return nil
}

// SearchMeta searches locally cached metadata matching the query.
func (d *dhtMetaStore) SearchMeta(ctx context.Context, query string) ([]PackageMeta, error) {
	var results []PackageMeta

	d.mu.Lock()
	for _, meta := range d.local {
		if matchesQuery(meta, query) {
			cp := *meta
			results = append(results, cp)
		}
	}
	d.mu.Unlock()

	target := dht.NodeID(crypto.SHA3Hash([]byte("search:" + query)))
	_, _ = d.dht.Lookup(ctx, target)

	if results == nil {
		results = []PackageMeta{}
	}
	return results, nil
}

// ResolveMeta retrieves a specific package's metadata.
func (d *dhtMetaStore) ResolveMeta(ctx context.Context, packageID string) (*PackageMeta, error) {
	d.mu.Lock()
	meta, ok := d.local[packageID]
	d.mu.Unlock()
	if ok {
		cp := *meta
		return &cp, nil
	}

	target := dht.NodeID(crypto.SHA3Hash([]byte("pkg:" + packageID)))
	_, err := d.dht.Lookup(ctx, target)
	if err != nil {
		return nil, ErrPackageNotFound
	}
	return nil, ErrPackageNotFound
}

// Local returns a snapshot of locally cached package metadata.
func (d *dhtMetaStore) Local() []PackageMeta {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]PackageMeta, 0, len(d.local))
	for _, m := range d.local {
		cp := *m
		out = append(out, cp)
	}
	return out
}

// AddLocal adds a package meta to the local cache without DHT propagation.
func (d *dhtMetaStore) AddLocal(meta *PackageMeta) {
	d.mu.Lock()
	defer d.mu.Unlock()
	cp := *meta
	d.local[meta.ID] = &cp
}

// RemoveLocal removes a package meta from the local cache.
func (d *dhtMetaStore) RemoveLocal(id string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.local, id)
}

// marshalPackageMeta serializes PackageMeta to JSON bytes.
func marshalPackageMeta(m *PackageMeta) ([]byte, error) {
	type alias struct {
		ID          string    `json:"id"`
		Name        string    `json:"name"`
		Version     string    `json:"version"`
		Description string    `json:"description"`
		Author      string    `json:"author"`
		Platform    []string  `json:"platform"`
		Entry       string    `json:"entry"`
		Published   int64     `json:"published"`
		Updated     int64     `json:"updated"`
		Downloads   int64     `json:"downloads"`
		Verified    bool      `json:"verified"`
		PublisherID [32]byte  `json:"publisher_id"`
	}
	a := alias{
		ID:          m.ID,
		Name:        m.Name,
		Version:     m.Version,
		Description: m.Description,
		Author:      m.Author,
		Platform:    m.Platform,
		Entry:       m.Entry,
		Published:   m.Published.UnixNano(),
		Updated:     m.Updated.UnixNano(),
		Downloads:   m.Downloads,
		Verified:    m.Verified,
		PublisherID: m.PublisherID,
	}
	return json.Marshal(a)
}

// unmarshalPackageMeta deserializes bytes into PackageMeta.
func unmarshalPackageMeta(data []byte) (*PackageMeta, error) {
	type alias struct {
		ID          string    `json:"id"`
		Name        string    `json:"name"`
		Version     string    `json:"version"`
		Description string    `json:"description"`
		Author      string    `json:"author"`
		Platform    []string  `json:"platform"`
		Entry       string    `json:"entry"`
		Published   int64     `json:"published"`
		Updated     int64     `json:"updated"`
		Downloads   int64     `json:"downloads"`
		Verified    bool      `json:"verified"`
		PublisherID [32]byte  `json:"publisher_id"`
	}
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return nil, err
	}
	return &PackageMeta{
		ID:          a.ID,
		Name:        a.Name,
		Version:     a.Version,
		Description: a.Description,
		Author:      a.Author,
		Platform:    a.Platform,
		Entry:       a.Entry,
		Published:   time.Unix(0, a.Published),
		Updated:     time.Unix(0, a.Updated),
		Downloads:   a.Downloads,
		Verified:    a.Verified,
		PublisherID: a.PublisherID,
	}, nil
}

// matchesQuery returns true if the package meta matches the search query.
func matchesQuery(m *PackageMeta, query string) bool {
	if query == "" {
		return true
	}
	q := strings.ToLower(query)
	return strings.Contains(strings.ToLower(m.Name), q) ||
		strings.Contains(strings.ToLower(m.Description), q) ||
		strings.Contains(strings.ToLower(m.Author), q)
}

// memorySync pushes local metadata to connected DHT peers.
func (d *dhtMetaStore) memorySync(ctx context.Context) error {
	metas := d.Local()
	for _, m := range metas {
		if err := d.PublishMeta(ctx, &m); err != nil {
			return err
		}
	}
	return nil
}

// PackageMetaKey generates a deterministic DHT key for a package ID.
func PackageMetaKey(id string) string {
	h := sha256.Sum256([]byte("lwpkg:" + id))
	return fmt.Sprintf("%x", h[:8])
}
