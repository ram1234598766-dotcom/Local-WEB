package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStoreOpenClose(t *testing.T) {
	store := OpenInMemory()
	defer store.Close()

	err := store.checkClosed()
	require.NoError(t, err)
}

func TestStoreClosed(t *testing.T) {
	store := OpenInMemory()
	store.Close()

	_, err := store.Get(context.Background(), []byte("key"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "closed")
}

func TestStorePutGet(t *testing.T) {
	store := OpenInMemory()
	defer store.Close()

	ctx := context.Background()
	err := store.Put(ctx, []byte("hello"), []byte("world"))
	require.NoError(t, err)

	val, err := store.Get(ctx, []byte("hello"))
	require.NoError(t, err)
	require.Equal(t, []byte("world"), val)
}

func TestStoreGetMissing(t *testing.T) {
	store := OpenInMemory()
	defer store.Close()

	_, err := store.Get(context.Background(), []byte("missing"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestStoreDelete(t *testing.T) {
	store := OpenInMemory()
	defer store.Close()

	ctx := context.Background()
	err := store.Put(ctx, []byte("key"), []byte("val"))
	require.NoError(t, err)

	err = store.Delete(ctx, []byte("key"))
	require.NoError(t, err)

	_, err = store.Get(ctx, []byte("key"))
	require.Error(t, err)
}

func TestStoreHas(t *testing.T) {
	store := OpenInMemory()
	defer store.Close()

	ctx := context.Background()
	exists, err := store.Has(ctx, []byte("key"))
	require.NoError(t, err)
	require.False(t, exists)

	err = store.Put(ctx, []byte("key"), []byte("val"))
	require.NoError(t, err)

	exists, err = store.Has(ctx, []byte("key"))
	require.NoError(t, err)
	require.True(t, exists)
}

func TestStoreBatch(t *testing.T) {
	store := OpenInMemory()
	defer store.Close()

	ctx := context.Background()
	entries := []Entry{
		{Key: []byte("a"), Value: []byte("1")},
		{Key: []byte("b"), Value: []byte("2")},
		{Key: []byte("c"), Value: []byte("3")},
	}

	err := store.Batch(ctx, entries)
	require.NoError(t, err)

	for _, e := range entries {
		val, err := store.Get(ctx, e.Key)
		require.NoError(t, err)
		require.Equal(t, e.Value, val)
	}
}

func TestStoreBatchDelete(t *testing.T) {
	store := OpenInMemory()
	defer store.Close()

	ctx := context.Background()
	store.Put(ctx, []byte("a"), []byte("1"))
	store.Put(ctx, []byte("b"), []byte("2"))

	entries := []Entry{
		{Key: []byte("a"), Value: nil},
		{Key: []byte("b"), Value: nil},
	}
	err := store.Batch(ctx, entries)
	require.NoError(t, err)

	for _, k := range [][]byte{{'a'}, {'b'}} {
		_, err := store.Get(ctx, k)
		require.Error(t, err)
	}
}

func TestStoreIterator(t *testing.T) {
	store := OpenInMemory()
	defer store.Close()

	ctx := context.Background()
	entries := []Entry{
		{Key: []byte("user:1"), Value: []byte("alice")},
		{Key: []byte("user:2"), Value: []byte("bob")},
		{Key: []byte("post:1"), Value: []byte("hello")},
	}

	err := store.Batch(ctx, entries)
	require.NoError(t, err)

	var userCount int
	userValues := make(map[string]bool)
	err = store.Iterator(ctx, []byte("user:"), func(k, v []byte) error {
		userCount++
		userValues[string(v)] = true
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, 2, userCount)
	require.True(t, userValues["alice"])
	require.True(t, userValues["bob"])
}

func TestStoreCount(t *testing.T) {
	store := OpenInMemory()
	defer store.Close()

	ctx := context.Background()
	entries := []Entry{
		{Key: []byte("item:1"), Value: []byte("a")},
		{Key: []byte("item:2"), Value: []byte("b")},
		{Key: []byte("other:1"), Value: []byte("c")},
	}

	err := store.Batch(ctx, entries)
	require.NoError(t, err)

	count, err := store.Count(ctx, []byte("item:"))
	require.NoError(t, err)
	require.Equal(t, 2, count)
}

func TestStoreOverwrite(t *testing.T) {
	store := OpenInMemory()
	defer store.Close()

	ctx := context.Background()
	err := store.Put(ctx, []byte("key"), []byte("old"))
	require.NoError(t, err)

	err = store.Put(ctx, []byte("key"), []byte("new"))
	require.NoError(t, err)

	val, err := store.Get(ctx, []byte("key"))
	require.NoError(t, err)
	require.Equal(t, []byte("new"), val)
}
