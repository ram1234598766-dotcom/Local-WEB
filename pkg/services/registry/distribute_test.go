package registry

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/ram1234598766-dotcom/Local-WEB/pkg/crypto"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/dht"
	"github.com/stretchr/testify/require"
)

// noopTransport implements dht.QUICTransport with no-op methods.
type noopTransport struct{}

func (noopTransport) Dial(ctx context.Context, addr string) (net.Conn, error) {
	return &noopConn{}, nil
}
func (noopTransport) Listen(addr string) (net.Listener, error) {
	return &noopListener{}, nil
}

type noopConn struct{}

func (c noopConn) Read(p []byte) (int, error)         { return 0, contextDone() }
func (c noopConn) Write(p []byte) (int, error)        { return 0, contextDone() }
func (c noopConn) Close() error                       { return nil }
func (c noopConn) LocalAddr() net.Addr                { return &noopAddr{} }
func (c noopConn) RemoteAddr() net.Addr               { return &noopAddr{} }
func (c noopConn) SetDeadline(t time.Time) error      { return nil }
func (c noopConn) SetReadDeadline(t time.Time) error  { return nil }
func (c noopConn) SetWriteDeadline(t time.Time) error { return nil }

type noopListener struct{}

func (l noopListener) Accept() (net.Conn, error) { return &noopConn{}, contextDone() }
func (l noopListener) Close() error              { return nil }
func (l noopListener) Addr() net.Addr            { return &noopAddr{} }

type noopAddr struct{}

func (a noopAddr) Network() string { return "test" }
func (a noopAddr) String() string  { return "127.0.0.1:0" }

func newTestDHT(t *testing.T) (*dht.DHT, [32]byte) {
	t.Helper()
	pub, _, err := crypto.GenerateKeyPair()
	require.NoError(t, err)
	nodeID := dht.NodeIDFromPub(pub)
	d := dht.NewDHT(nodeID, pub, "test-node", noopTransport{})
	return d, pub
}

func TestNewDHTDistributor(t *testing.T) {
	d, pub := newTestDHT(t)
	dist := NewDHTDistributor(d, pub)
	require.NotNil(t, dist)
	require.Empty(t, dist.Local())
}

func TestDHTDistributorPublishMeta(t *testing.T) {
	d, pub := newTestDHT(t)
	dist := NewDHTDistributor(d, pub)

	meta := &PackageMeta{
		ID:        "pkg-1",
		Name:      "test-pkg",
		Version:   "1.0.0",
		Author:    "author",
		Published: timeNow(),
		Updated:   timeNow(),
	}
	require.NoError(t, dist.PublishMeta(context.Background(), meta))

	local := dist.Local()
	require.Len(t, local, 1)
	require.Equal(t, "test-pkg", local[0].Name)
}

func TestDHTDistributorSearchMeta(t *testing.T) {
	d, pub := newTestDHT(t)
	dist := NewDHTDistributor(d, pub)

	_ = dist.PublishMeta(context.Background(), &PackageMeta{
		ID: "p1", Name: "alpha-app", Version: "1.0", Author: "alice", Published: timeNow(), Updated: timeNow(),
	})
	_ = dist.PublishMeta(context.Background(), &PackageMeta{
		ID: "p2", Name: "beta-app", Version: "1.0", Author: "bob", Published: timeNow(), Updated: timeNow(),
	})

	results, err := dist.SearchMeta(context.Background(), "alpha")
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "alpha-app", results[0].Name)

	results, err = dist.SearchMeta(context.Background(), "bob")
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "beta-app", results[0].Name)

	results, err = dist.SearchMeta(context.Background(), "nonexistent")
	require.NoError(t, err)
	require.Empty(t, results)

	results, err = dist.SearchMeta(context.Background(), "")
	require.NoError(t, err)
	require.Len(t, results, 2)
}

func TestDHTDistributorResolveMeta(t *testing.T) {
	d, pub := newTestDHT(t)
	dist := NewDHTDistributor(d, pub)

	meta := &PackageMeta{ID: "resolve-pkg", Name: "r-app", Version: "1.0", Author: "a", Published: timeNow(), Updated: timeNow()}
	_ = dist.PublishMeta(context.Background(), meta)

	got, err := dist.ResolveMeta(context.Background(), "resolve-pkg")
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, "r-app", got.Name)

	_, err = dist.ResolveMeta(context.Background(), "missing")
	require.Error(t, err)
	require.Equal(t, ErrPackageNotFound, err)
}

func TestDHTDistributorAddLocal(t *testing.T) {
	d, pub := newTestDHT(t)
	dist := NewDHTDistributor(d, pub)

	dist.AddLocal(&PackageMeta{ID: "local-1", Name: "local-app", Version: "1.0", Author: "a", Published: timeNow(), Updated: timeNow()})
	require.Len(t, dist.Local(), 1)
	require.Equal(t, "local-app", dist.Local()[0].Name)
}

func TestDHTDistributorRemoveLocal(t *testing.T) {
	d, pub := newTestDHT(t)
	dist := NewDHTDistributor(d, pub)

	_ = dist.PublishMeta(context.Background(), &PackageMeta{ID: "rm-1", Name: "rm-app", Version: "1.0", Author: "a", Published: timeNow(), Updated: timeNow()})
	dist.RemoveLocal("rm-1")
	require.Empty(t, dist.Local())
}

func TestDHTDistributorMemorySync(t *testing.T) {
	d, pub := newTestDHT(t)
	dist := NewDHTDistributor(d, pub)

	_ = dist.PublishMeta(context.Background(), &PackageMeta{ID: "sync-1", Name: "sync-app", Version: "1.0", Author: "a", Published: timeNow(), Updated: timeNow()})

	err := (&dhtMetaStore{}).memorySync(context.Background())
	require.NoError(t, err)
}

func TestDHTDistributorPackageMetaRoundTrip(t *testing.T) {
	now := time.Now()
	meta := &PackageMeta{
		ID:          "roundtrip",
		Name:        "rt-app",
		Version:     "1.0",
		Description: "desc",
		Author:      "author",
		Platform:    []string{"linux"},
		Entry:       "main",
		Published:   now,
		Updated:     now,
		Downloads:   42,
		Verified:    true,
		PublisherID: [32]byte{1, 2, 3},
	}

	data, err := marshalPackageMeta(meta)
	require.NoError(t, err)

	got, err := unmarshalPackageMeta(data)
	require.NoError(t, err)
	require.Equal(t, meta.ID, got.ID)
	require.Equal(t, meta.Name, got.Name)
	require.Equal(t, meta.Version, got.Version)
	require.Equal(t, meta.Description, got.Description)
	require.Equal(t, meta.Author, got.Author)
	require.Equal(t, meta.Platform, got.Platform)
	require.Equal(t, meta.Entry, got.Entry)
	require.Equal(t, meta.Published.UnixNano(), got.Published.UnixNano())
	require.Equal(t, meta.Updated.UnixNano(), got.Updated.UnixNano())
	require.Equal(t, meta.Downloads, got.Downloads)
	require.Equal(t, meta.Verified, got.Verified)
	require.Equal(t, meta.PublisherID, got.PublisherID)
}

func TestPackageMetaKey(t *testing.T) {
	key := PackageMetaKey("test-pkg-123")
	require.NotEmpty(t, key)
	require.Len(t, key, 16)
}

func contextDone() error {
	return context.Background().Err()
}
