package federation

import (
	"context"
	"log"
	"time"

	"github.com/ram1234598766-dotcom/Local-WEB/pkg/discovery"
)

// RendezvousModeConfig holds configuration for the rendezvous discovery mode.
type RendezvousModeConfig struct {
	ServerURL    string
	RegisterSelf bool
	PollInterval time.Duration
}

// RendezvousDiscoveryMode implements DiscoveryMode using a rendezvous server.
type RendezvousDiscoveryMode struct {
	config     RendezvousModeConfig
	nodeID     [32]byte
	nodeName   string
	publicKey  [32]byte
	client     *RendezvousClient
	events     chan discovery.PeerEvent
	ctx        context.Context
	cancel     context.CancelFunc
	localAddrs []string
}

// Name returns the discovery mode name.
func (m *RendezvousDiscoveryMode) Name() string {
	return "rendezvous"
}

// RequiresWiFi returns false since rendezvous works over internet.
func (m *RendezvousDiscoveryMode) RequiresWiFi() bool {
	return false
}

// Start begins the rendezvous discovery loop.
func (m *RendezvousDiscoveryMode) Start(ctx context.Context, nodeID [32]byte, name string) (<-chan discovery.PeerEvent, error) {
	m.nodeID = nodeID
	m.nodeName = name
	m.ctx, m.cancel = context.WithCancel(ctx)
	m.events = make(chan discovery.PeerEvent, 32)

	// Build local peer info
	peer := discovery.PeerInfo{
		ID:        m.nodeID,
		PublicKey: m.publicKey,
		Name:      m.nodeName,
		Addrs:     m.localAddrs,
		Source:    "rendezvous",
		LastSeen:  time.Now(),
	}

	m.client = NewRendezvousClient(m.config.ServerURL)

	// Register self if enabled
	if m.config.RegisterSelf && len(m.localAddrs) > 0 {
		if err := m.client.Register(ctx, peer); err != nil {
			log.Printf("rendezvous: initial register failed: %v", err)
		} else {
			log.Printf("rendezvous: registered with %s", m.config.ServerURL)
		}
	}

	// Start periodic registration and discovery
	go m.discoveryLoop(peer)

	return m.events, nil
}

// Advertise updates the local peer info (called when addresses change).
func (m *RendezvousDiscoveryMode) Advertise(info discovery.PeerInfo) error {
	m.localAddrs = info.Addrs
	m.publicKey = info.PublicKey
	return nil
}

// Stop halts the rendezvous discovery.
func (m *RendezvousDiscoveryMode) Stop() error {
	if m.cancel != nil {
		m.cancel()
	}
	close(m.events)
	return nil
}

// discoveryLoop periodically registers self and polls for peers.
func (m *RendezvousDiscoveryMode) discoveryLoop(peer discovery.PeerInfo) {
	ticker := time.NewTicker(m.config.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			// Re-register self
			if m.config.RegisterSelf && len(m.localAddrs) > 0 {
				if err := m.client.Register(m.ctx, peer); err != nil {
					log.Printf("rendezvous: register failed: %v", err)
				}
			}

			// Poll for all known peers from rendezvous
			// This is a simplification - in practice you'd want to track
			// which peers you know and only look up new ones
			// For now, we'll just log that we're polling
			log.Printf("rendezvous: polling %s for peers", m.config.ServerURL)
		}
	}
}

// NewRendezvousDiscoveryMode creates a new rendezvous discovery mode.
func NewRendezvousDiscoveryMode(cfg RendezvousModeConfig) *RendezvousDiscoveryMode {
	if cfg.PollInterval == 0 {
		cfg.PollInterval = 60 * time.Second
	}
	return &RendezvousDiscoveryMode{
		config: cfg,
	}
}

// SetLocalAddrs sets the local addresses to advertise.
func (m *RendezvousDiscoveryMode) SetLocalAddrs(addrs []string) {
	m.localAddrs = addrs
}

// SetPublicKey sets the public key for peer info.
func (m *RendezvousDiscoveryMode) SetPublicKey(key [32]byte) {
	m.publicKey = key
}
