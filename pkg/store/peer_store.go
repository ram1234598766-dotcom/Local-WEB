package store

import (
	"context"
	"fmt"
	"sync"

	"github.com/mrityunjay/LocalWEB/pkg/discovery"
)

// PeerStore persists discovered peers to the underlying BadgerDB store.
type PeerStore struct {
	mu    sync.RWMutex
	store *Store
}

// NewPeerStore creates a peer store backed by the given Store.
func NewPeerStore(store *Store) *PeerStore {
	return &PeerStore{store: store}
}

const peerPrefix = "peer"

// PutPeer adds or updates a peer in the store.
func (ps *PeerStore) PutPeer(ctx context.Context, peer discovery.PeerInfo) error {
	data, err := encodeGob(peer)
	if err != nil {
		return fmt.Errorf("encode peer: %w", err)
	}

	key := ps.store.nsKey(peerPrefix, fmt.Sprintf("%x", peer.ID))
	return ps.store.Put(ctx, key, data)
}

// GetPeer retrieves a peer by ID.
func (ps *PeerStore) GetPeer(ctx context.Context, id [32]byte) (*discovery.PeerInfo, error) {
	key := ps.store.nsKey(peerPrefix, fmt.Sprintf("%x", id))
	data, err := ps.store.Get(ctx, key)
	if err != nil {
		return nil, err
	}

	var peer discovery.PeerInfo
	if err := decodeGob(data, &peer); err != nil {
		return nil, fmt.Errorf("decode peer: %w", err)
	}
	return &peer, nil
}

// DeletePeer removes a peer from the store.
func (ps *PeerStore) DeletePeer(ctx context.Context, id [32]byte) error {
	key := ps.store.nsKey(peerPrefix, fmt.Sprintf("%x", id))
	return ps.store.Delete(ctx, key)
}

// ListPeers returns all stored peers.
func (ps *PeerStore) ListPeers(ctx context.Context) ([]*discovery.PeerInfo, error) {
	prefix := ps.store.nsKey(peerPrefix)
	var peers []*discovery.PeerInfo

	err := ps.store.Iterator(ctx, prefix, func(k, v []byte) error {
		var peer discovery.PeerInfo
		if err := decodeGob(v, &peer); err != nil {
			return fmt.Errorf("decode peer: %w", err)
		}
		peers = append(peers, &peer)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list peers: %w", err)
	}
	return peers, nil
}

// CountPeers returns the number of stored peers.
func (ps *PeerStore) CountPeers(ctx context.Context) (int, error) {
	prefix := ps.store.nsKey(peerPrefix)
	return ps.store.Count(ctx, prefix)
}
