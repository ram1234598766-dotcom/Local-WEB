package main

import (
	"net"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestDefaultGUIAddrIsLoopback holds the dashboard's default to loopback.
//
// The dashboard has no authentication and is not read-only: it uploads files into
// the local store, creates and saves documents, and restores backups from
// /api/onboarding/restore. Its default used to be 0.0.0.0:8080 while the README
// called it "read-only, localhost only", so every installation exposed an
// unauthenticated write path on every interface.
func TestDefaultGUIAddrIsLoopback(t *testing.T) {
	host, port, err := net.SplitHostPort(defaultGUIAddr)
	require.NoError(t, err, "defaultGUIAddr %q is not a host:port", defaultGUIAddr)
	require.NotEmpty(t, port)

	require.True(t, net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback() ||
		host == "localhost",
		"the dashboard must default to a loopback address, got %q. Exposing an "+
			"unauthenticated dashboard is a deliberate choice, not a default", host)
}
