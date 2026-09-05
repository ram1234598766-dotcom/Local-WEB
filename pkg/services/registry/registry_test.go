package registry

import (
	"testing"

	"github.com/ram1234598766-dotcom/Local-WEB/pkg/crypto"
	"github.com/stretchr/testify/require"
)

func TestMemoryRegistryPublish(t *testing.T) {
	r := NewMemoryRegistry()
	priv, pub, _ := crypto.GenerateKeyPair()
	pkg := testPackage("mem-pub", "1.0.0", "pub-author")

	id, err := r.Publish(pkg, pub, priv)
	require.NoError(t, err)
	require.NotEmpty(t, id)
}

func TestMemoryRegistryPublishDuplicate(t *testing.T) {
	r := NewMemoryRegistry()
	priv, pub, _ := crypto.GenerateKeyPair()
	pkg := testPackage("dup-app", "1.0.0", "author")

	id1, err := r.Publish(pkg, pub, priv)
	require.NoError(t, err)
	_, err = r.Publish(pkg, pub, priv)
	require.Error(t, err)
	require.Equal(t, ErrPackageExists, err)
	_ = id1
}

func TestMemoryRegistryPublishInvalidManifest(t *testing.T) {
	r := NewMemoryRegistry()
	_, pub, _ := crypto.GenerateKeyPair()
	pkg := &LWPKG{Manifest: nil}
	_, err := r.Publish(pkg, pub, [32]byte{})
	require.Error(t, err)
	require.Equal(t, ErrManifestInvalid, err)
}

func TestMemoryRegistryList(t *testing.T) {
	r := NewMemoryRegistry()
	priv, pub, _ := crypto.GenerateKeyPair()
	_, _ = r.Publish(testPackage("list-a", "1.0.0", "a1"), pub, priv)
	_, _ = r.Publish(testPackage("list-b", "1.0.0", "a2"), pub, priv)

	pkgs, err := r.List()
	require.NoError(t, err)
	require.Len(t, pkgs, 2)
}

func TestMemoryRegistryListEmpty(t *testing.T) {
	r := NewMemoryRegistry()
	pkgs, err := r.List()
	require.NoError(t, err)
	require.Empty(t, pkgs)
}

func TestMemoryRegistryGet(t *testing.T) {
	r := NewMemoryRegistry()
	priv, pub, _ := crypto.GenerateKeyPair()
	pkg := testPackage("get-test", "1.0.0", "author")
	id, _ := r.Publish(pkg, pub, priv)

	meta, err := r.Get(id)
	require.NoError(t, err)
	require.NotNil(t, meta)
	require.Equal(t, "get-test", meta.Name)
	require.Equal(t, "1.0.0", meta.Version)
}

func TestMemoryRegistryGetNotFound(t *testing.T) {
	r := NewMemoryRegistry()
	_, err := r.Get("nonexistent")
	require.Error(t, err)
	require.Equal(t, ErrPackageNotFound, err)
}

func TestMemoryRegistryDelete(t *testing.T) {
	r := NewMemoryRegistry()
	priv, pub, _ := crypto.GenerateKeyPair()
	pkg := testPackage("del-test", "1.0.0", "author")
	id, _ := r.Publish(pkg, pub, priv)

	err := r.Delete(id)
	require.NoError(t, err)

	_, err = r.Get(id)
	require.Error(t, err)
	require.Equal(t, ErrPackageNotFound, err)
}

func TestMemoryRegistryDeleteNotFound(t *testing.T) {
	r := NewMemoryRegistry()
	err := r.Delete("nonexistent")
	require.Error(t, err)
	require.Equal(t, ErrPackageNotFound, err)
}

func TestMemoryRegistrySearch(t *testing.T) {
	r := NewMemoryRegistry()
	priv, pub, _ := crypto.GenerateKeyPair()
	_, _ = r.Publish(testPackage("search-alpha", "1.0.0", "alice"), pub, priv)
	_, _ = r.Publish(testPackage("search-beta", "1.0.1", "bob"), pub, priv)
	_, _ = r.Publish(testPackage("search-gamma", "1.0.2", "charlie"), pub, priv)

	result, err := r.Search(SearchQuery{Query: "alpha"})
	require.NoError(t, err)
	require.Equal(t, 1, result.Total)
	require.Len(t, result.Packages, 1)
	require.Equal(t, "search-alpha", result.Packages[0].Name)
}

func TestMemoryRegistrySearchEmpty(t *testing.T) {
	r := NewMemoryRegistry()
	priv, pub, _ := crypto.GenerateKeyPair()
	_, _ = r.Publish(testPackage("search-only", "1.0.0", "author"), pub, priv)

	result, err := r.Search(SearchQuery{Query: "nonexistent"})
	require.NoError(t, err)
	require.Equal(t, 0, result.Total)
}

func TestMemoryRegistrySearchAll(t *testing.T) {
	r := NewMemoryRegistry()
	priv, pub, _ := crypto.GenerateKeyPair()
	for i := 0; i < 5; i++ {
		_, _ = r.Publish(testPackage(string(rune('a'+i)), "1.0.0", "author"), pub, priv)
	}

	result, err := r.Search(SearchQuery{Query: "", Limit: 10})
	require.NoError(t, err)
	require.Equal(t, 5, result.Total)
	require.Len(t, result.Packages, 5)
}

func TestMemoryRegistrySearchByPlatform(t *testing.T) {
	r := NewMemoryRegistry()
	priv, pub, _ := crypto.GenerateKeyPair()
	pkg := testPackage("search-plat", "1.0.0", "author")
	pkg.Manifest.Platform = []string{"linux", "darwin"}
	_, _ = r.Publish(pkg, pub, priv)

	result, err := r.Search(SearchQuery{Platform: "linux"})
	require.NoError(t, err)
	require.Equal(t, 1, result.Total)

	result, err = r.Search(SearchQuery{Platform: "windows"})
	require.NoError(t, err)
	require.Equal(t, 0, result.Total)
}

func TestMemoryRegistrySearchByAuthor(t *testing.T) {
	r := NewMemoryRegistry()
	priv, pub, _ := crypto.GenerateKeyPair()
	_, _ = r.Publish(testPackage("sa1", "1.0.0", "alice"), pub, priv)
	_, _ = r.Publish(testPackage("sa2", "1.0.1", "bob"), pub, priv)

	result, err := r.Search(SearchQuery{Author: "alice"})
	require.NoError(t, err)
	require.Equal(t, 1, result.Total)
	require.Equal(t, "alice", result.Packages[0].Author)
}

func TestMemoryRegistrySearchVerified(t *testing.T) {
	r := NewMemoryRegistry()
	priv, pub, _ := crypto.GenerateKeyPair()
	pkg1 := testPackage("sv1", "1.0.0", "author")
	_, _ = r.Publish(pkg1, pub, priv)
	pkg2 := testPackage("sv2", "1.0.0", "author")
	pkg2.Signature = make([]byte, Ed25519SignatureSize)
	_, _ = r.Publish(pkg2, pub, priv)

	result, err := r.Search(SearchQuery{Verified: true})
	require.NoError(t, err)
	require.Equal(t, 1, result.Total)
	require.Equal(t, "sv2", result.Packages[0].Name)
}

func TestMemoryRegistryPublishSetsVerified(t *testing.T) {
	r := NewMemoryRegistry()
	priv, pub, _ := crypto.GenerateKeyPair()
	pkg := testPackage("sv-check", "1.0.0", "author")
	pkg.Signature = make([]byte, Ed25519SignatureSize)

	id, err := r.Publish(pkg, pub, priv)
	require.NoError(t, err)

	meta, err := r.Get(id)
	require.NoError(t, err)
	require.True(t, meta.Verified)
}

func TestMemoryRegistryPublishSetsTimestamps(t *testing.T) {
	r := NewMemoryRegistry()
	priv, pub, _ := crypto.GenerateKeyPair()
	pkg := testPackage("st-app", "1.0.0", "author")

	id, err := r.Publish(pkg, pub, priv)
	require.NoError(t, err)

	meta, err := r.Get(id)
	require.NoError(t, err)
	require.False(t, meta.Published.IsZero())
	require.False(t, meta.Updated.IsZero())
}
