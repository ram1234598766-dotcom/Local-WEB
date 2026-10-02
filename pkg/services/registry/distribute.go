package registry

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ram1234598766-dotcom/Local-WEB/pkg/crypto"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/dht"
	"github.com/rs/zerolog/log"
)

// dhtMetaStore is the concrete DHT-backed implementation of DHTDistributor.
type dhtMetaStore struct {
	mu     sync.Mutex
	dht    *dht.DHT
	nodeID dht.NodeID
	pubKey [32]byte
	local  map[string]*PackageMeta

	// addr is this node's DHT address, advertised in its registration so a peer
	// that learns about this node can add it to its own table. Without it a node
	// could be found but not contacted.
	addr string

	// peersSeen counts successful lookups, so the daemon can report whether the
	// node is actually part of a network or talking to nobody.
	peersSeen int
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

// NewDHTDistributorAt is NewDHTDistributor with the node's own DHT address, so
// it can advertise where peers should connect.
func NewDHTDistributorAt(d *dht.DHT, pubKey [32]byte, addr string) DHTDistributor {
	return &dhtMetaStore{
		dht:    d,
		nodeID: dht.NodeIDFromPub(pubKey),
		pubKey: pubKey,
		addr:   addr,
		local:  make(map[string]*PackageMeta),
	}
}

// Start begins the background work that makes the DHT reachable rather than
// merely present: periodically re-announcing this node, refreshing the routing
// table, and re-publishing local metadata.
//
// Without it a node registered itself once and then sat still. A peer that
// joined later had no route to it, because a value is only pushed to peers the
// publisher already knows, and nothing ever discovered anybody else.
func (d *dhtMetaStore) Start(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		d.announceAndRefresh(ctx)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				d.announceAndRefresh(ctx)
			}
		}
	}()
}

// announceAndRefresh does one round of discovery and re-publication.
func (d *dhtMetaStore) announceAndRefresh(ctx context.Context) {
	if d.addr != "" {
		// The registration carries our address and the anti-Sybil proof of work.
		if err := d.dht.RegisterNode(ctx, d.pubKey, "localweb", []string{d.addr}, dht.MinPoWDifficulty); err != nil {
			log.Debug().Err(err).Msg("dht: announce failed")
		}
	}

	// A lookup against a stable target walks the network and folds every peer it
	// meets into the routing table, which is how a node learns publishers it was
	// never told about.
	target := dht.NodeID(crypto.SHA3Hash([]byte("localweb-refresh")))
	if _, err := d.dht.Lookup(ctx, target); err != nil {
		log.Debug().Err(err).Msg("dht: refresh lookup found nothing")
	}
	// Counted from the routing table rather than from what the lookup returned:
	// the table is what decides whether anyone can be reached.
	d.mu.Lock()
	d.peersSeen = d.dht.PeerCount()
	d.mu.Unlock()

	// Re-publish so the value reaches peers discovered since the last round,
	// including nodes that joined after we first published.
	for _, m := range d.Local() {
		key := "pkg:" + m.ID
		data, err := marshalPackageMeta(&m)
		if err != nil {
			continue
		}
		if err := d.dht.Store(ctx, key, data); err != nil {
			log.Debug().Err(err).Str("package", m.ID).Msg("dht: store failed")
		}
	}
}

// PeersSeen reports how many peers the last refresh found, so a node that is
// bootstrapped with no addresses can be seen to be reaching nobody.
func (d *dhtMetaStore) PeersSeen() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.peersSeen
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
//
// The local cache answers first. On a miss the metadata is fetched from the DHT
// and cached, so a second resolve is local.
//
// This used to run a DHT lookup and then return ErrPackageNotFound regardless
// of the result, discarding the lookup entirely. The test passed only because
// it published to the same node, which short-circuits on the local cache: no
// test ever resolved a package that lived on a different node.
func (d *dhtMetaStore) ResolveMeta(ctx context.Context, packageID string) (*PackageMeta, error) {
	d.mu.Lock()
	meta, ok := d.local[packageID]
	d.mu.Unlock()
	if ok {
		cp := *meta
		return &cp, nil
	}

	key := "pkg:" + packageID
	raw, err := d.dht.Get(ctx, key)
	if err != nil {
		// The sentinel is returned bare so callers can compare it directly; the
		// underlying reason is logged rather than wrapped.
		log.Debug().Err(err).Str("package", packageID).Msg("dht has no metadata for this package")
		return nil, ErrPackageNotFound
	}

	fetched, err := unmarshalPackageMeta(raw)
	if err != nil {
		log.Warn().Err(err).Str("package", packageID).Msg("metadata from the dht did not decode")
		return nil, ErrPackageNotFound
	}
	// Trust the key we asked for, not one the value claims: a peer must not be
	// able to answer a request for one package with another's metadata.
	if fetched.ID != packageID {
		log.Warn().
			Str("asked_for", packageID).
			Str("returned", fetched.ID).
			Msg("peer answered a metadata request with a different package")
		return nil, ErrPackageNotFound
	}

	d.AddLocal(fetched)
	cp := *fetched
	return &cp, nil
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
		ID          string   `json:"id"`
		Name        string   `json:"name"`
		Version     string   `json:"version"`
		Description string   `json:"description"`
		Author      string   `json:"author"`
		Platform    []string `json:"platform"`
		Entry       string   `json:"entry"`
		Published   int64    `json:"published"`
		Updated     int64    `json:"updated"`
		Downloads   int64    `json:"downloads"`
		Verified    bool     `json:"verified"`
		PublisherID [32]byte `json:"publisher_id"`
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
		ID          string   `json:"id"`
		Name        string   `json:"name"`
		Version     string   `json:"version"`
		Description string   `json:"description"`
		Author      string   `json:"author"`
		Platform    []string `json:"platform"`
		Entry       string   `json:"entry"`
		Published   int64    `json:"published"`
		Updated     int64    `json:"updated"`
		Downloads   int64    `json:"downloads"`
		Verified    bool     `json:"verified"`
		PublisherID [32]byte `json:"publisher_id"`
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
