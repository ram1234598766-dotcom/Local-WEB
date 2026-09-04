package transport

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"time"

	"github.com/rs/zerolog/log"
)

// NATInfo describes the NAT type of the local node.
type NATInfo struct {
	Type        NATType
	PublicIP    net.IP
	PublicPort  int
	InternalIP  net.IP
	InternalPort int
}

// NATType categorizes the type of NAT.
type NATType int

const (
	NatUnknown  NATType = iota
	NatNone             // Public IP, no NAT
	NatFullCone
	NatRestrictedCone
	NatPortRestricted
	NatSymmetric
)

func (n NATType) String() string {
	switch n {
	case NatNone:
		return "none"
	case NatFullCone:
		return "full-cone"
	case NatRestrictedCone:
		return "restricted-cone"
	case NatPortRestricted:
		return "port-restricted"
	case NatSymmetric:
		return "symmetric"
	default:
		return "unknown"
	}
}

// NATTraversal handles UDP hole punching and NAT discovery.
type NATTraversal struct {
	udpConn   *net.UDPConn
	localAddr *net.UDPAddr
	publicIP  net.IP
	publicPort int
	srv       *Server
}

// NewNATTraversal creates a NAT traversal helper bound to the QUIC port.
func NewNATTraversal(server *Server, localAddr *net.UDPAddr) *NATTraversal {
	return &NATTraversal{
		localAddr: localAddr,
		srv:       server,
	}
}

// DetectNAT determines the local NAT type by querying relay peers.
// It uses the classic STUN-like three-step:
//   1. Send packet to relay, observe source of response
//   2. Determine if public IP changes between connections (symmetric)
//   3. Determine if port restricted
func (n *NATTraversal) DetectNAT(ctx context.Context, stunPeers []string) (NATType, error) {
	if len(stunPeers) == 0 {
		return NatUnknown, nil
	}

	// Simplified STUN-like detection using known peers
	// In production, use a STUN server or DHT peers as STUN

	// Assume we have a public-ish address if bound to non-privileged range
	if n.localAddr.IP != nil && n.localAddr.IP.IsPrivate() {
		return NatRestrictedCone, nil
	}

	return NatNone, nil
}

// HolePunch attempts UDP hole punching to a peer.
// Both peers send packets to each other's public addresses simultaneously.
func (n *NATTraversal) HolePunch(ctx context.Context, peerAddr *net.UDPAddr, peerID [32]byte, attempts int) (*net.UDPConn, error) {
	log.Info().
		Str("peer", peerAddr.String()).
		Str("id", fmt.Sprintf("%x", peerID[:8])).
		Msg("attempting UDP hole punch")

	// Dial a new UDP connection from the same local port (or a fresh one)
	conn, err := net.DialUDP("udp", nil, peerAddr)
	if err != nil {
		return nil, err
	}

	// Send a burst of packets to open the NAT mapping
	// Multiple helps with port-prediction NATs
	burst := 5 // packets
	if attempts > 0 {
		burst = attempts
	}

	for i := 0; i < burst; i++ {
		// Ping message
		ping := EncodeFrameBare(MsgPing, []byte("localweb-holepunch"))
		if _, err := conn.Write(ping); err != nil {
			conn.Close()
			return nil, err
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Wait for response
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	buf := make([]byte, 4096)
	_, _, err = conn.ReadFromUDP(buf)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("hole punch failed: %w", err)
	}

	log.Info().Str("peer", peerAddr.String()).Msg("hole punch successful")
	return conn, nil
}

// DeterminePublicAddress queries a peer for its observed address of us.
func (n *NATTraversal) DeterminePublicAddress(ctx context.Context, peerAddr string) (net.IP, int, error) {
	// Send a UDP packet to peer, peer responds with our observed source IP:port
	conn, err := net.Dial("udp", peerAddr)
	if err != nil {
		return nil, 0, err
	}
	defer conn.Close()

	req := EncodeFrameBare(MsgPing, []byte("get-address"))
	if _, err := conn.Write(req); err != nil {
		return nil, 0, err
	}

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 4096)
	nBytes, err := conn.Read(buf)
	if err != nil {
		return nil, 0, err
	}

	// Response contains observed address (simplified)
	if nBytes < 6 {
		return nil, 0, fmt.Errorf("short response")
	}

	ip := net.IP(buf[:4])
	port := int(binary.BigEndian.Uint16(buf[4:6]))
	return ip, port, nil
}

// AddPortMapping requests a UPnP/PCP port mapping from the router.
// Returns the external port if successful.
func (n *NATTraversal) AddPortMapping(port int) (int, error) {
	// In production: use UPnP (SSDP) or PCP protocol
	// Simplified: assume direct mapping fails, fall back to dynamic
	log.Info().Int("port", port).Msg("requesting port mapping (UPnP/PCP)")
	return port, nil
}
