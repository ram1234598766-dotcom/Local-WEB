package store

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mrityunjay/LocalWEB/pkg/crypto"
	"github.com/mrityunjay/LocalWEB/pkg/discovery"
)

func TestPeerStorePutGet(t *testing.T) {
	store := OpenInMemory()
	defer store.Close()

	ps := NewPeerStore(store)
	ctx := context.Background()

	pub, _, err := crypto.GenerateKeyPair()
	require.NoError(t, err)

	peer := discovery.PeerInfo{
		ID:        crypto.NodeID(pub),
		PublicKey: pub,
		Name:      "test-peer",
		Addrs:     []string{"192.168.1.1:8080"},
		Source:    "test",
		Score:     1.0,
		LastSeen:  time.Now(),
		FirstSeen: time.Now(),
		Version:   "1.0",
	}

	err = ps.PutPeer(ctx, peer)
	require.NoError(t, err)

	got, err := ps.GetPeer(ctx, peer.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, peer.Name, got.Name)
	require.Equal(t, peer.Score, got.Score)
	require.Len(t, got.Addrs, 1)
	require.Equal(t, "192.168.1.1:8080", got.Addrs[0])
}

func TestPeerStoreUpdate(t *testing.T) {
	store := OpenInMemory()
	defer store.Close()

	ps := NewPeerStore(store)
	ctx := context.Background()

	pub, _, err := crypto.GenerateKeyPair()
	require.NoError(t, err)
	id := crypto.NodeID(pub)

	peer1 := discovery.PeerInfo{
		ID:        id,
		PublicKey: pub,
		Name:      "v1",
		Score:     1.0,
		Addrs:    []string{"addr1"},
		LastSeen: time.Now(),
	}
	err = ps.PutPeer(ctx, peer1)
	require.NoError(t, err)

	peer2 := discovery.PeerInfo{
		ID:        id,
		PublicKey: pub,
		Name:      "v2",
		Score:     2.0,
		Addrs:    []string{"addr2"},
		LastSeen: time.Now(),
	}
	err = ps.PutPeer(ctx, peer2)
	require.NoError(t, err)

	got, err := ps.GetPeer(ctx, id)
	require.NoError(t, err)
	require.Equal(t, "v2", got.Name)
	require.Equal(t, 2.0, got.Score)
	require.Len(t, got.Addrs, 1)
	require.Equal(t, "addr2", got.Addrs[0])
}

func TestPeerStoreDelete(t *testing.T) {
	store := OpenInMemory()
	defer store.Close()

	ps := NewPeerStore(store)
	ctx := context.Background()

	pub, _, err := crypto.GenerateKeyPair()
	require.NoError(t, err)
	id := crypto.NodeID(pub)

	peer := discovery.PeerInfo{ID: id, PublicKey: pub, Name: "del"}
	err = ps.PutPeer(ctx, peer)
	require.NoError(t, err)

	err = ps.DeletePeer(ctx, id)
	require.NoError(t, err)

	_, err = ps.GetPeer(ctx, id)
	require.Error(t, err)
}

func TestPeerStoreListCount(t *testing.T) {
	store := OpenInMemory()
	defer store.Close()

	ps := NewPeerStore(store)
	ctx := context.Background()

	var peers []discovery.PeerInfo
	for i := 0; i < 5; i++ {
		pub, _, err := crypto.GenerateKeyPair()
		require.NoError(t, err)
		peers = append(peers, discovery.PeerInfo{
			ID:        crypto.NodeID(pub),
			PublicKey: pub,
			Name:      string(rune('a' + i)),
		})
	}

	for _, p := range peers {
		err := ps.PutPeer(ctx, p)
		require.NoError(t, err)
	}

	count, err := ps.CountPeers(ctx)
	require.NoError(t, err)
	require.Equal(t, 5, count)

	list, err := ps.ListPeers(ctx)
	require.NoError(t, err)
	require.Len(t, list, 5)
}

func TestPeerStoreMissing(t *testing.T) {
	store := OpenInMemory()
	defer store.Close()

	ps := NewPeerStore(store)
	ctx := context.Background()

	var id [32]byte
	_, err := ps.GetPeer(ctx, id)
	require.Error(t, err)
}
