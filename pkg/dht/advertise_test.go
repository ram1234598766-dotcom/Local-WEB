package dht

import (
	"net"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestServerAdvertisedAddr checks that the address published to peers is not
// confused with the address bound.
//
// These are different questions. A node normally binds a wildcard so it listens
// on every interface, but a remote peer cannot dial 0.0.0.0, so publishing the
// bound address makes the node look reachable while nothing can reach it.
func TestServerAdvertisedAddr(t *testing.T) {
	t.Run("defaults to the bound address", func(t *testing.T) {
		n := NewDHT(NodeIDFromPub([32]byte{1}), [32]byte{1}, "test", TCPTransport{})
		srv := NewServer(n.Node())
		defer n.Stop()

		require.NoError(t, srv.Start("127.0.0.1:0"))
		defer srv.Stop()

		require.Equal(t, srv.Addr(), srv.AdvertisedAddr(),
			"with no override the published address is the bound one")

		// A loopback bind is dialable by a peer on the same host, which is what
		// the existing tests rely on, so it must survive unchanged.
		host, _, err := net.SplitHostPort(srv.AdvertisedAddr())
		require.NoError(t, err)
		require.Equal(t, "127.0.0.1", host)
	})

	t.Run("override replaces the bound address", func(t *testing.T) {
		n := NewDHT(NodeIDFromPub([32]byte{2}), [32]byte{2}, "test", TCPTransport{})
		srv := NewServer(n.Node())
		defer n.Stop()

		require.NoError(t, srv.Start("0.0.0.0:0"))
		defer srv.Stop()

		srv.SetAdvertisedAddr("203.0.113.7:9094")
		require.Equal(t, "203.0.113.7:9094", srv.AdvertisedAddr())

		// The bound address is still what the listener really has, including the
		// port the kernel chose, so an override cannot lose it.
		require.NotEmpty(t, srv.Addr())
		require.NotEqual(t, srv.Addr(), srv.AdvertisedAddr())
	})

	t.Run("empty override falls back to the bound address", func(t *testing.T) {
		n := NewDHT(NodeIDFromPub([32]byte{3}), [32]byte{3}, "test", TCPTransport{})
		srv := NewServer(n.Node())
		defer n.Stop()

		require.NoError(t, srv.Start("127.0.0.1:0"))
		defer srv.Stop()

		srv.SetAdvertisedAddr("203.0.113.7:9094")
		srv.SetAdvertisedAddr("")
		require.Equal(t, srv.Addr(), srv.AdvertisedAddr())
	})

	t.Run("no listener means no address", func(t *testing.T) {
		n := NewDHT(NodeIDFromPub([32]byte{4}), [32]byte{4}, "test", TCPTransport{})
		srv := NewServer(n.Node())
		defer n.Stop()

		require.Empty(t, srv.Addr())
		require.Empty(t, srv.AdvertisedAddr(),
			"an unstarted server must not publish an address")
	})
}
