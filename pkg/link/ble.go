package link

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// LocalWEB BLE Service UUID (128-bit custom UUID).
var LocalWEBServiceUUID = [16]byte{
	0x12, 0x34, 0x56, 0x78,
	0x9a, 0xbc, 0xde, 0xf0,
	0x12, 0x34, 0x56, 0x78,
	0x9a, 0xbc, 0xde, 0xf0,
}

// BLE Characteristic UUIDs.
var (
	BLEIdentityUUID  = [16]byte{0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x10, 0x00, 0x80, 0x00, 0x00, 0x80, 0x5f, 0x9b, 0x34, 0xfb}
	BLEMessagingUUID = [16]byte{0x02, 0x00, 0x00, 0x00, 0x00, 0x00, 0x10, 0x00, 0x80, 0x00, 0x00, 0x80, 0x5f, 0x9b, 0x34, 0xfb}
	BLETransferUUID  = [16]byte{0x03, 0x00, 0x00, 0x00, 0x00, 0x00, 0x10, 0x00, 0x80, 0x00, 0x00, 0x80, 0x5f, 0x9b, 0x34, 0xfb}
)

// BLEIdentity is the data exchanged via the Identity characteristic.
type BLEIdentity struct {
	NodeID       [32]byte // SHA3-256 of public key
	PublicKey    [32]byte // Ed25519 public key
	Name         [32]byte // Node name (null-padded)
	Capabilities uint32   // Bitmask of services
	Version      uint16   // Protocol version
}

// BLECapabilities bitmask.
const (
	CapDNS      uint32 = 1 << 0
	CapHTTP     uint32 = 1 << 1
	CapSMTP     uint32 = 1 << 2
	CapIMAP     uint32 = 1 << 3
	CapMsg      uint32 = 1 << 4
	CapFiles    uint32 = 1 << 5
	CapVoice    uint32 = 1 << 6
	CapVPN      uint32 = 1 << 7
	CapDocs     uint32 = 1 << 8
	CapRegistry uint32 = 1 << 9
)

// BLE implements Link for Bluetooth Low Energy (no WiFi needed).
type BLE struct {
	mu           sync.Mutex
	nodeID       [32]byte
	publicKey    [32]byte
	name         string
	capabilities uint32
	peers        map[[32]byte]*BLEPeer
	adapter      *bleAdapter
	ctx          context.Context
	cancel       context.CancelFunc
}

// BLEPeer holds BLE peer information.
type BLEPeer struct {
	ID           [32]byte
	PublicKey    [32]byte
	Name         string
	Capabilities uint32
	RSSI         int32
	Address      string // BLE MAC address
	LastSeen     time.Time
}

// NewBLE creates a BLE link.
func NewBLE(nodeID, publicKey [32]byte, name string, caps uint32) (*BLE, error) {
	ctx, cancel := context.WithCancel(context.Background())

	adapter, err := newBLEAdapter()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("BLE adapter: %w", err)
	}

	return &BLE{
		nodeID:       nodeID,
		publicKey:    publicKey,
		name:         name,
		capabilities: caps,
		peers:        make(map[[32]byte]*BLEPeer),
		adapter:      adapter,
		ctx:          ctx,
		cancel:       cancel,
	}, nil
}

func (b *BLE) Name() string         { return "ble" }
func (b *BLE) Mode() LinkMode       { return ModeBLE }
func (b *BLE) RequiresWiFi() bool   { return false }
func (b *BLE) RequiresRouter() bool { return false }
func (b *BLE) Bandwidth() int       { return 1 } // ~1 Mbps
func (b *BLE) MaxPeers() int        { return 10 }

func (b *BLE) IsAvailable(ctx context.Context) bool {
	return b.adapter != nil && b.adapter.IsPowered()
}

func (b *BLE) Discover(ctx context.Context) (<-chan PeerEvent, error) {
	events := make(chan PeerEvent, 16)

	// Start advertising
	if err := b.startAdvertising(); err != nil {
		return nil, fmt.Errorf("BLE advertise: %w", err)
	}

	// Start scanning
	go b.scanLoop(ctx, events)

	// Start heartbeats
	go b.heartbeatLoop(ctx)

	return events, nil
}

func (b *BLE) Connect(ctx context.Context, addr string) (net.Conn, error) {
	// BLE doesn't provide direct TCP connections
	// Instead, we use BLE to exchange addresses for higher-bandwidth links
	// Or use BLE GATT for small data transfer

	// For now, return a BLE-based connection wrapper
	return b.adapter.Connect(addr)
}

func (b *BLE) Advertise(info PeerInfo) error {
	return b.startAdvertising()
}

func (b *BLE) Stop() error {
	b.cancel()
	b.stopAdvertising()
	b.stopScanning()
	return nil
}

// startAdvertising makes this node visible to BLE scanners.
func (b *BLE) startAdvertising() error {
	// Construct advertisement data
	advData := b.buildAdvertData()

	// Start GATT server with LocalWEB service
	if err := b.adapter.StartGATTServer(b.serviceDefinition()); err != nil {
		return fmt.Errorf("GATT server: %w", err)
	}

	// Start advertising
	return b.adapter.StartAdvertising(advData)
}

// stopAdvertising stops BLE advertising.
func (b *BLE) stopAdvertising() {
	b.adapter.StopAdvertising()
}

// stopScanning stops BLE scanning.
func (b *BLE) stopScanning() {
	b.adapter.StopScanning()
}

// scanLoop continuously scans for BLE peers.
func (b *BLE) scanLoop(ctx context.Context, events chan<- PeerEvent) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			peers := b.scanOnce()
			for _, peer := range peers {
				b.mu.Lock()
				_, exists := b.peers[peer.ID]
				b.peers[peer.ID] = peer
				b.mu.Unlock()

				evtType := PeerDiscovered
				if exists {
					evtType = PeerUpdated
				}

				events <- PeerEvent{
					Type: evtType,
					Peer: PeerInfo{
						ID:        peer.ID,
						PublicKey: peer.PublicKey,
						Name:      peer.Name,
						LinkMode:  ModeBLE,
						RSSI:      peer.RSSI,
						LastSeen:  peer.LastSeen,
					},
					Time: time.Now(),
				}
			}
		}
	}
}

// scanOnce performs a single BLE scan.
func (b *BLE) scanOnce() []*BLEPeer {
	results := b.adapter.Scan(LocalWEBServiceUUID[:])
	var peers []*BLEPeer

	for _, r := range results {
		identity := parseBLEIdentity(r.Data)
		if identity == nil {
			continue
		}

		peer := &BLEPeer{
			ID:           identity.NodeID,
			PublicKey:    identity.PublicKey,
			Name:         string(identity.Name[:]),
			Capabilities: identity.Capabilities,
			RSSI:         r.RSSI,
			Address:      r.Address,
			LastSeen:     time.Now(),
		}

		// Skip self
		if peer.ID == b.nodeID {
			continue
		}

		peers = append(peers, peer)
	}

	return peers
}

// heartbeatLoop periodically re-advertises to stay visible.
func (b *BLE) heartbeatLoop(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			b.startAdvertising()
		}
	}
}

// buildAdvertData constructs the BLE advertisement packet.
func (b *BLE) buildAdvertData() []byte {
	// Format: [length][type][data...]
	// Service UUID (128-bit)
	// Local Name
	// TX Power Level
	var data []byte

	// Flags
	data = append(data, 0x02, 0x01, 0x06) // LE General Discoverable + BR/EDR Not Supported

	// Service UUID (128-bit)
	svcUUID := make([]byte, 17)
	svcUUID[0] = 16   // length
	svcUUID[1] = 0x07 // Complete List of 128-bit Service UUIDs
	copy(svcUUID[2:], LocalWEBServiceUUID[:])
	data = append(data, svcUUID...)

	// Local Name (shortened)
	nameBytes := []byte(b.name)
	if len(nameBytes) > 20 {
		nameBytes = nameBytes[:20]
	}
	nameField := make([]byte, 2+len(nameBytes))
	nameField[0] = byte(1 + len(nameBytes))
	nameField[1] = 0x08 // Shortened Local Name
	copy(nameField[2:], nameBytes)
	data = append(data, nameField...)

	return data
}

// serviceDefinition returns the GATT service definition.
func (b *BLE) serviceDefinition() []GATTCharacteristic {
	return []GATTCharacteristic{
		{
			UUID:  BLEIdentityUUID,
			Props: GATTRead | GATTNotify,
			Value: b.encodeIdentity(),
		},
		{
			UUID:  BLEMessagingUUID,
			Props: GATTWrite | GATTNotify,
		},
		{
			UUID:  BLETransferUUID,
			Props: GATTWriteWithoutResponse,
		},
	}
}

// encodeIdentity encodes this node's identity for BLE advertisement.
func (b *BLE) encodeIdentity() []byte {
	var buf [102]byte // 32 + 32 + 32 + 4 + 2 = 102 bytes
	copy(buf[0:32], b.nodeID[:])
	copy(buf[32:64], b.publicKey[:])
	copy(buf[64:96], []byte(b.name))
	binary.LittleEndian.PutUint32(buf[96:100], b.capabilities)
	binary.LittleEndian.PutUint16(buf[100:102], 1) // version
	return buf[:102]
}

// parseBLEIdentity parses a BLE advertisement into a BLEIdentity.
func parseBLEIdentity(data []byte) *BLEIdentity {
	if len(data) < 102 {
		return nil
	}

	var id BLEIdentity
	copy(id.NodeID[:], data[0:32])
	copy(id.PublicKey[:], data[32:64])
	copy(id.Name[:], data[64:96])
	id.Capabilities = binary.LittleEndian.Uint32(data[96:100])
	id.Version = binary.LittleEndian.Uint16(data[100:102])

	return &id
}

// --- BLE Adapter Abstraction ---

// bleAdapter abstracts platform-specific BLE operations.
type bleAdapter struct {
	powered bool
}

// BLEScanResult holds a single BLE scan result.
type BLEScanResult struct {
	Address string
	RSSI    int32
	Data    []byte
}

// GATTCharacteristic describes a GATT characteristic.
type GATTCharacteristic struct {
	UUID  [16]byte
	Props uint8
	Value []byte
}

// GATT property flags.
const (
	GATTRead                 uint8 = 0x02
	GATTWrite                uint8 = 0x08
	GATTWriteWithoutResponse uint8 = 0x04
	GATTNotify               uint8 = 0x10
	GATTIndicate             uint8 = 0x20
)

func newBLEAdapter() (*bleAdapter, error) {
	// Check if BLE is available on this system
	// Linux: check /sys/class/bluetooth/
	// macOS: check IOBluetoothFramework
	// Windows: check Bluetooth API

	return &bleAdapter{powered: true}, nil
}

func (a *bleAdapter) IsPowered() bool {
	return a.powered
}

func (a *bleAdapter) StartGATTServer(chars []GATTCharacteristic) error {
	log.Debug().Int("chars", len(chars)).Msg("GATT server started")
	return nil
}

func (a *bleAdapter) StartAdvertising(data []byte) error {
	log.Debug().Int("bytes", len(data)).Msg("BLE advertising started")
	return nil
}

func (a *bleAdapter) StopAdvertising() {
	log.Debug().Msg("BLE advertising stopped")
}

func (a *bleAdapter) Scan(serviceUUID []byte) []BLEScanResult {
	// Platform-specific BLE scan
	// Linux: bluetoothctl or BlueZ D-Bus API
	// macOS: CoreBluetooth
	// Windows: Windows.Devices.Bluetooth
	return nil
}

func (a *bleAdapter) StopScanning() {
	log.Debug().Msg("BLE scanning stopped")
}

func (a *bleAdapter) Connect(addr string) (net.Conn, error) {
	// BLE GATT connection for data transfer
	return nil, fmt.Errorf("BLE connect not yet implemented")
}
