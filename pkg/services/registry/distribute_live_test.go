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

// tcpTransport is a real transport for the DHT. The noopTransport in
// distribute_test.go lets a DHT be constructed but never spoken to, which is
// exactly why the DHT paths in this package went untested: every resolve there
// was answered from the local cache.
type tcpTransport struct{}

func (tcpTransport) Dial(ctx context.Context, addr string) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, "tcp", addr)
}

func (tcpTransport) Listen(addr string) (net.Listener, error) {
	return net.Listen("tcp", addr)
}

// liveDHTNode is one DHT node that actually listens and answers requests.
type liveDHTNode struct {
	dht  *dht.DHT
	srv  *dht.Server
	pub  [32]byte
	addr string
	dist DHTDistributor
}

func newLiveDHTNode(t *testing.T, name string) *liveDHTNode {
	t.Helper()

	pub, _, err := crypto.GenerateKeyPair()
	require.NoError(t, err)
	id := dht.NodeIDFromPub(pub)

	d := dht.NewDHT(id, pub, name, tcpTransport{})
	srv := dht.NewServer(d.Node())
	require.NoError(t, srv.Start("127.0.0.1:0"))
	t.Cleanup(func() { _ = srv.Stop() })

	// Bootstrap is what marks a node running; Store and Get refuse to act
	// otherwise. No bootstrap addresses are needed because the nodes learn each
	// other explicitly below.
	require.NoError(t, d.Bootstrap(context.Background(), nil))

	return &liveDHTNode{
		dht:  d,
		srv:  srv,
		pub:  pub,
		addr: srv.Addr(),
		dist: NewDHTDistributor(d, pub),
	}
}

// learn records the other node in this node's routing table, so it can be
// dialled. RegisterNode is the public way in, and it also exercises the
// anti-Sybil gate.
func (n *liveDHTNode) learn(t *testing.T, other *liveDHTNode) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	// Difficulty 8 is the minimum the DHT accepts and costs ~256 hashes, so the
	// anti-Sybil gate is genuinely exercised without slowing the test down.
	require.NoError(t, n.dht.RegisterNode(ctx, other.pub, "peer", []string{other.addr}, 8))
}

// learnPair makes two nodes know each other.
//
// Both directions are needed. Store sends to the closest peers in the *sender's*
// own table, so a publisher that knows nobody writes its metadata into the void;
// likewise a consumer that knows nobody has nobody to ask. In a running network
// this comes from bootstrap gossip, and in a test it has to be said explicitly.
func learnPair(t *testing.T, a, b *liveDHTNode) {
	t.Helper()
	a.learn(t, b)
	b.learn(t, a)
}

// TestResolveMetaAcrossTwoNodes is the item 7.6 test: a package published on one
// node is resolved from a different node over the DHT.
//
// ResolveMeta used to run a lookup and then return ErrPackageNotFound whatever
// the lookup said. The old test passed only because it published to the same
// node, which short-circuits on the local cache.
// TestPublishThroughRegistryReachesTheOtherNode closes item 7.6 end to end.
//
// The path from a real Publish call to another node's resolve went through
// three stubs: RegisterDistributor threw its argument away, Publish never
// propagated, and ResolveMeta discarded the lookup it had just done.
func TestPublishThroughRegistryReachesTheOtherNode(t *testing.T) {
	publisher := newLiveDHTNode(t, "publisher")
	consumer := newLiveDHTNode(t, "consumer")
	learnPair(t, consumer, publisher)

	// The consumer is given the registry so it can resolve the way a node does.
	consumerReg := NewMemoryRegistry()
	consumerReg.RegisterDistributor(consumer.dist)

	// The publisher stores a package through a registry wired to its own DHT.
	publisherReg := NewMemoryRegistry()
	publisherReg.RegisterDistributor(publisher.dist)

	pkg := &LWPKG{Manifest: &Manifest{
		Name:        "round-trip-app",
		Version:     "0.3.1",
		Author:      "publisher",
		Description: "published through the registry, resolved on another node",
		Platform:    []string{"linux/amd64"},
		Entry:       "run",
		Checksums:   map[string]string{"run": "0000000000000000000000000000000000000000000000000000000000000000"},
	}}
	id, err := publisherReg.Publish(pkg, publisher.pub, [32]byte{})
	require.NoError(t, err)

	// Give the store a moment to reach the closest peers, which is what the
	// consumer will ask.
	resolved := waitForResolve(t, consumer.dist, id)
	require.Equal(t, "round-trip-app", resolved.Name)
	require.Equal(t, "0.3.1", resolved.Version)
}

func waitForResolve(t *testing.T, dist DHTDistributor, id string) *PackageMeta {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		got, err := dist.ResolveMeta(context.Background(), id)
		if err == nil {
			return got
		}
		lastErr = err
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("could not resolve %s from the other node: %v", id, lastErr)
	return nil
}

func TestResolveMetaAcrossTwoNodes(t *testing.T) {
	publisher := newLiveDHTNode(t, "publisher")
	consumer := newLiveDHTNode(t, "consumer")
	learnPair(t, consumer, publisher)

	meta := &PackageMeta{
		ID:          "cross-node-pkg",
		Name:        "cross-node-app",
		Version:     "2.1.0",
		Description: "published on one node, resolved on another",
		Author:      "someone",
		Platform:    []string{"linux/amd64"},
		Entry:       "app",
		Published:   time.Now(),
		Updated:     time.Now(),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	require.NoError(t, publisher.dist.PublishMeta(ctx, meta))

	// The consumer has never seen this package.
	require.NotNil(t, consumer.dist)
	got, err := consumer.dist.ResolveMeta(ctx, "cross-node-pkg")
	require.NoError(t, err, "resolving from another node must work")
	require.NotNil(t, got)
	require.Equal(t, "cross-node-app", got.Name)
	require.Equal(t, "2.1.0", got.Version)
	require.Equal(t, meta.ID, got.ID)

	// A second resolve is served from the local cache without another round
	// trip, which is what makes the cache worth having.
	again, err := consumer.dist.ResolveMeta(ctx, "cross-node-pkg")
	require.NoError(t, err)
	require.Equal(t, got.Name, again.Name)
}

func TestResolveMetaMissingAcrossNodes(t *testing.T) {
	publisher := newLiveDHTNode(t, "publisher")
	consumer := newLiveDHTNode(t, "consumer")
	learnPair(t, consumer, publisher)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	_, err := consumer.dist.ResolveMeta(ctx, "nobody-published-this")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrPackageNotFound)
}

// TestResolveMetaRejectsMismatchedID proves a peer cannot answer a request for
// one package with another's metadata.
func TestResolveMetaRejectsMismatchedID(t *testing.T) {
	node := newLiveDHTNode(t, "liar")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// Store a value under the key for "wanted" whose payload says it is
	// "something-else" entirely.
	wrong := &PackageMeta{ID: "something-else", Name: "not-what-you-asked-for"}
	raw, err := marshalPackageMeta(wrong)
	require.NoError(t, err)
	require.NoError(t, node.dht.Store(ctx, "pkg:wanted", raw))

	_, err = node.dist.ResolveMeta(ctx, "wanted")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrPackageNotFound)
}
