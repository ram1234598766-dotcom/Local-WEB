package dht

import (
	"context"
	"net"
)

// TCPTransport is a real QUICTransport over plain TCP.
//
// dht.Server.Start already listens with net.Listen("tcp", ...), and Server
// handles connections with net.Conn, so TCP is what this side of the DHT
// actually speaks. Nothing implemented the interface for real before: the only
// implementations were the noop transport in tests, which is why no DHT path in
// the registry had ever been exercised over a socket.
//
// Note this is a separate plane from the QUIC service transport: a peer address
// handed to Dial here is a TCP endpoint, not a Local-WEB QUIC address.
type TCPTransport struct{}

// Dial opens a TCP connection to a DHT peer.
func (TCPTransport) Dial(ctx context.Context, addr string) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, "tcp", addr)
}

// Listen binds a TCP listener for the DHT server.
func (TCPTransport) Listen(addr string) (net.Listener, error) {
	return net.Listen("tcp", addr)
}

var _ QUICTransport = TCPTransport{}
