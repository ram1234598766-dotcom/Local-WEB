package link

import (
	"context"
	"net"
	"time"
)

// LinkMode represents the connectivity mode.
type LinkMode int

const (
	ModeWiFiStation   LinkMode = iota // Connected to WiFi router
	ModeWiFiDirect                     // P2P, no router
	ModeAdHocWiFi                      // IBSS mode
	ModeUSBTether                      // USB cable
	ModeBLE                            // Bluetooth Low Energy
	ModeAcoustic                       // Audio coupling (emergency)
	ModeNone                           // No connectivity
)

func (m LinkMode) String() string {
	switch m {
	case ModeWiFiStation:
		return "wifi-station"
	case ModeWiFiDirect:
		return "wifi-direct"
	case ModeAdHocWiFi:
		return "ad-hoc-wifi"
	case ModeUSBTether:
		return "usb-tether"
	case ModeBLE:
		return "ble"
	case ModeAcoustic:
		return "acoustic"
	default:
		return "none"
	}
}

// Link defines a network transport mechanism.
type Link interface {
	// Name returns the human-readable link name.
	Name() string

	// Mode returns the link mode.
	Mode() LinkMode

	// RequiresWiFi reports if this link needs WiFi hardware.
	RequiresWiFi() bool

	// RequiresRouter reports if this link needs a WiFi router.
	RequiresRouter() bool

	// IsAvailable checks if this link can be used right now.
	IsAvailable(ctx context.Context) bool

	// Bandwidth returns estimated bandwidth in Mbps.
	Bandwidth() int

	// MaxPeers returns maximum concurrent peers supported.
	MaxPeers() int

	// Discover scans for nearby peers using this link.
	Discover(ctx context.Context) (<-chan PeerEvent, error)

	// Connect establishes a connection to a peer.
	Connect(ctx context.Context, addr string) (net.Conn, error)

	// Advertise makes this node visible to other peers.
	Advertise(info PeerInfo) error

	// Stop halts all operations on this link.
	Stop() error
}

// PeerEvent represents a discovery event.
type PeerEvent struct {
	Type  EventType
	Peer  PeerInfo
	Time  time.Time
}

// EventType is the kind of peer event.
type EventType int

const (
	PeerDiscovered EventType = iota
	PeerLost
	PeerUpdated
)

// PeerInfo holds information about a discovered peer.
type PeerInfo struct {
	ID           [32]byte          // SHA3-256 of public key
	PublicKey    [32]byte          // Ed25519 public key
	Name         string            // Human-readable name
	Addrs        []string          // Network addresses (ip:port)
	Services     []string          // Capabilities: dns, http, smtp, etc.
	LinkMode     LinkMode          // How we discovered this peer
	RSSI         int32             // Signal strength (BLE/WiFi)
	Latency      time.Duration     // Round-trip time
	Score        float64           // 0.0 → 1.0 reliability score
	LastSeen     time.Time
	Version      string            // Software version
}

// LinkConfig holds configuration for a specific link type.
type LinkConfig struct {
	Mode            LinkMode
	Enabled         bool
	AdvertiseInterval time.Duration
	ScanInterval    time.Duration
	MaxRetries      int
	Timeout         time.Duration
}

// DefaultLinkConfigs returns sensible defaults for all link types.
func DefaultLinkConfigs() map[LinkMode]LinkConfig {
	return map[LinkMode]LinkConfig{
		ModeWiFiStation: {
			Mode:              ModeWiFiStation,
			Enabled:           true,
			AdvertiseInterval: 30 * time.Second,
			ScanInterval:      10 * time.Second,
			MaxRetries:        3,
			Timeout:           5 * time.Second,
		},
		ModeWiFiDirect: {
			Mode:              ModeWiFiDirect,
			Enabled:           true,
			AdvertiseInterval: 5 * time.Second,
			ScanInterval:      2 * time.Second,
			MaxRetries:        3,
			Timeout:           10 * time.Second,
		},
		ModeAdHocWiFi: {
			Mode:              ModeAdHocWiFi,
			Enabled:           true,
			AdvertiseInterval: 10 * time.Second,
			ScanInterval:      5 * time.Second,
			MaxRetries:        3,
			Timeout:           5 * time.Second,
		},
		ModeUSBTether: {
			Mode:              ModeUSBTether,
			Enabled:           true,
			AdvertiseInterval: 5 * time.Second,
			ScanInterval:      2 * time.Second,
			MaxRetries:        1,
			Timeout:           3 * time.Second,
		},
		ModeBLE: {
			Mode:              ModeBLE,
			Enabled:           true,
			AdvertiseInterval: 1 * time.Second,
			ScanInterval:      1 * time.Second,
			MaxRetries:        5,
			Timeout:           5 * time.Second,
		},
	}
}
