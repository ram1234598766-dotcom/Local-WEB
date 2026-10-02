package main

import (
	"net"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestDHTAdvertisedAddr covers the bug this fixes: the DHT used to be bound to
// 127.0.0.1:0, so only a node on the same machine could ever reach it, while
// every log line said the node was "serving".
func TestDHTAdvertisedAddr(t *testing.T) {
	t.Run("explicit override always wins", func(t *testing.T) {
		// A node behind NAT knows its public address and nothing local can work
		// it out, so the flag must never be second-guessed.
		got := dhtAdvertisedAddr("203.0.113.7:9094", "0.0.0.0:12345")
		require.Equal(t, "203.0.113.7:9094", got)
	})

	t.Run("explicit override wins even for a specific bind", func(t *testing.T) {
		got := dhtAdvertisedAddr("198.51.100.9:1", "10.0.0.5:9094")
		require.Equal(t, "198.51.100.9:1", got)
	})

	t.Run("specific non-loopback bind is already dialable", func(t *testing.T) {
		// No NAT rewrite is needed, so the bound address is correct as-is. This
		// must not be rewritten to the outbound address, which would break a node
		// deliberately bound to one interface.
		got := dhtAdvertisedAddr("", "10.0.0.5:9094")
		require.Equal(t, "10.0.0.5:9094", got)
	})

	t.Run("wildcard bind never publishes the wildcard", func(t *testing.T) {
		// 0.0.0.0 is not dialable from another machine. Publishing it is exactly
		// the lie this replaces.
		got := dhtAdvertisedAddr("", "0.0.0.0:9094")
		require.NotEqual(t, "0.0.0.0:9094", got)

		got = dhtAdvertisedAddr("", "[::]:9094")
		require.NotEqual(t, "[::]:9094", got)

		got = dhtAdvertisedAddr("", ":9094")
		require.NotEqual(t, ":9094", got)
	})

	t.Run("wildcard bind keeps the ephemeral port", func(t *testing.T) {
		// The port comes from the bound listener, not from the flag, so a node
		// bound to :0 still publishes the port it actually got.
		got := dhtAdvertisedAddr("", "0.0.0.0:54321")
		if got == "" {
			t.Skip("no routable interface in this environment")
		}
		require.Equal(t, ":54321", got[len(got)-6:])
	})

	t.Run("empty is honest when nothing is dialable", func(t *testing.T) {
		// "": the caller then skips advertising instead of handing peers an
		// address they cannot connect to.
		if outboundIPv4() != "" {
			t.Skip("this host has a routable interface, so the fallback is reachable")
		}
		require.Equal(t, "", dhtAdvertisedAddr("", "0.0.0.0:9094"))
	})

	t.Run("malformed bound address is returned unchanged", func(t *testing.T) {
		// Better to pass an odd string through for the caller to log than to
		// invent an address for it.
		got := dhtAdvertisedAddr("", "not-an-address")
		require.Equal(t, "not-an-address", got)
	})
}

func TestIsWildcardHost(t *testing.T) {
	require.True(t, isWildcardHost("0.0.0.0"))
	require.True(t, isWildcardHost("::"))
	require.False(t, isWildcardHost("10.0.0.5"))
	require.False(t, isWildcardHost("localhost"))
	require.False(t, isWildcardHost(""))

	// The bracketed form is never seen: dhtAdvertisedAddr splits the bound
	// address first, and net.SplitHostPort("[::]:9094") yields host "::". Feeding
	// the bracketed string in would parse as nil and be reported as not
	// wildcard, which is why the split happens before this check.
	host, port, err := net.SplitHostPort("[::]:9094")
	require.NoError(t, err)
	require.Equal(t, "::", host)
	require.Equal(t, "9094", port)
	require.True(t, isWildcardHost(host))
}

// TestOutboundIPv4IsNotLoopback guards the trick used to find a public address:
// a connected UDP socket must not report a loopback or unspecified address, or
// the node would publish 127.0.0.1 as if it were reachable.
func TestOutboundIPv4IsNotLoopback(t *testing.T) {
	ip := outboundIPv4()
	if ip == "" {
		t.Skip("no routable IPv4 interface in this environment")
	}
	require.NotEqual(t, "127.0.0.1", ip)
	require.NotEqual(t, "0.0.0.0", ip)
	require.NotNil(t, net.ParseIP(ip), "outbound address %q is not a valid IP", ip)
}
