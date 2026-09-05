package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ipfs/go-cid"
	"github.com/mrityunjay/LocalWEB/pkg/services/files"
	mh "github.com/multiformats/go-multihash"
)

func computeTestCID(data []byte) cid.Cid {
	prefix := cid.NewPrefixV1(cid.Raw, mh.SHA2_256)
	c, _ := prefix.Sum(data)
	return c
}

func TestBadgerBlockStorePutGet(t *testing.T) {
	store := OpenInMemory()
	defer store.Close()

	bs := NewBadgerBlockStore(store)
	ctx := context.Background()

	data := []byte("hello block world")
	c := computeTestCID(data)
	block := &files.Block{CID: c, Data: data}

	err := bs.Put(ctx, block)
	require.NoError(t, err)

	got, err := bs.Get(ctx, c)
	require.NoError(t, err)
	require.Equal(t, data, got.Data)
	require.Equal(t, c, got.CID)
}

func TestBadgerBlockStoreCIDMismatch(t *testing.T) {
	store := OpenInMemory()
	defer store.Close()

	bs := NewBadgerBlockStore(store)
	ctx := context.Background()

	block := &files.Block{CID: computeTestCID([]byte("other")), Data: []byte("hello")}
	err := bs.Put(ctx, block)
	require.Error(t, err)
	require.Contains(t, err.Error(), "CID mismatch")
}

func TestBadgerBlockStoreHas(t *testing.T) {
	store := OpenInMemory()
	defer store.Close()

	bs := NewBadgerBlockStore(store)
	ctx := context.Background()

	c := computeTestCID([]byte("data"))
	require.False(t, bs.Has(ctx, c))

	err := bs.Put(ctx, &files.Block{CID: c, Data: []byte("data")})
	require.NoError(t, err)

	require.True(t, bs.Has(ctx, c))
	require.False(t, bs.Has(ctx, computeTestCID([]byte("nope"))))
}

func TestBadgerBlockStoreDelete(t *testing.T) {
	store := OpenInMemory()
	defer store.Close()

	bs := NewBadgerBlockStore(store)
	ctx := context.Background()

	c := computeTestCID([]byte("data"))
	err := bs.Put(ctx, &files.Block{CID: c, Data: []byte("data")})
	require.NoError(t, err)

	err = bs.Delete(ctx, c)
	require.NoError(t, err)

	require.False(t, bs.Has(ctx, c))
}

func TestBadgerBlockStoreList(t *testing.T) {
	store := OpenInMemory()
	defer store.Close()

	bs := NewBadgerBlockStore(store)
	ctx := context.Background()

	data1 := []byte("block1")
	data2 := []byte("block2")
	data3 := []byte("block3")
	c1 := computeTestCID(data1)
	c2 := computeTestCID(data2)
	c3 := computeTestCID(data3)

	for _, entry := range []struct {
		c    cid.Cid
		data []byte
	}{{c1, data1}, {c2, data2}, {c3, data3}} {
		err := bs.Put(ctx, &files.Block{CID: entry.c, Data: entry.data})
		require.NoError(t, err)
	}

	list, err := bs.List(ctx)
	require.NoError(t, err)
	require.Len(t, list, 3)
}

func TestBadgerBlockStoreSize(t *testing.T) {
	store := OpenInMemory()
	defer store.Close()

	bs := NewBadgerBlockStore(store)
	ctx := context.Background()

	data := []byte("sized block")
	c := computeTestCID(data)
	err := bs.Put(ctx, &files.Block{CID: c, Data: data})
	require.NoError(t, err)

	size, err := bs.Size(ctx, c)
	require.NoError(t, err)
	require.Equal(t, int64(len(data)), size)
}

func TestBadgerBlockStoreEmptyReject(t *testing.T) {
	store := OpenInMemory()
	defer store.Close()

	bs := NewBadgerBlockStore(store)
	ctx := context.Background()

	err := bs.Put(ctx, &files.Block{CID: cid.Undef, Data: nil})
	require.Error(t, err)
	require.Contains(t, err.Error(), "empty")
}

func TestBadgerBlockStoreOverwrite(t *testing.T) {
	store := OpenInMemory()
	defer store.Close()

	bs := NewBadgerBlockStore(store)
	ctx := context.Background()

	c := computeTestCID([]byte("data"))
	err := bs.Put(ctx, &files.Block{CID: c, Data: []byte("data")})
	require.NoError(t, err)

	// Put same CID again should be idempotent
	err = bs.Put(ctx, &files.Block{CID: c, Data: []byte("data")})
	require.NoError(t, err)

	got, err := bs.Get(ctx, c)
	require.NoError(t, err)
	require.Equal(t, []byte("data"), got.Data)
}
