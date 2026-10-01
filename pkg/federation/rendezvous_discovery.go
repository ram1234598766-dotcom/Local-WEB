package federation

import (
	"context"
	"fmt"
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

// discoveryLoop periodically registers self and looks for peers.
//
// The loop previously re-registered on every tick and then only logged "polling
// for peers", so this mode could never surface a single peer: nothing was ever
// read from the server and no PeerEvent was ever emitted. It now enumerates the
// server's peer list and emits an event for each peer it has not yet reported.
func (m *RendezvousDiscoveryMode) discoveryLoop(peer discovery.PeerInfo) {
	ticker := time.NewTicker(m.config.PollInterval)
	defer ticker.Stop()

	// seen holds the ids already emitted so a stable peer produces one event,
	// not one per tick. lastSeen tracks when each was last advertised, so a
	// peer that disappears is reported as gone rather than lingering forever.
	seen := make(map[[32]byte]peerState)

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			// Re-register self so the server keeps our entry alive.
			if m.config.RegisterSelf && len(m.localAddrs) > 0 {
				if err := m.client.Register(m.ctx, peer); err != nil {
					log.Printf("rendezvous: register failed: %v", err)
				}
			}

			if err := m.pollOnce(peer, seen); err != nil {
				log.Printf("rendezvous: peer lookup failed: %v", err)
			}
		}
	}
}

// pollExpiry is how long a peer may go unadvertised before we report it gone.
// It is deliberately longer than one poll interval so a single missed tick does
// not flap the peer.
const pollExpiry = 3 * time.Minute

// peerState tracks what we last saw for one peer.
//
// Two times are needed and they are not interchangeable: advertisedLastSeen is
// the timestamp the peer itself published, which is the only thing that tells us
// whether its record changed, while observedAt is when we last saw it listed,
// which is what expiry is measured from.
type peerState struct {
	advertisedLastSeen time.Time
	observedAt         time.Time
}

// pollOnce performs a single discovery pass and emits events. It is separate
// from the ticker loop so it can be tested without waiting on a timer.
func (m *RendezvousDiscoveryMode) pollOnce(self discovery.PeerInfo, seen map[[32]byte]peerState) error {
	peers, err := m.client.ListPeers(m.ctx)
	if err != nil {
		return err
	}

	now := time.Now()
	present := make(map[[32]byte]struct{}, len(peers))

	for _, p := range peers {
		// Never report ourselves back to ourselves.
		if p.ID == self.ID {
			continue
		}
		// The register handler rejects peers with no addresses; skip anything
		// that arrived without them rather than emitting an undialable peer.
		if len(p.Addrs) == 0 {
			continue
		}
		present[p.ID] = struct{}{}

		previous, known := seen[p.ID]
		seen[p.ID] = peerState{advertisedLastSeen: p.LastSeen, observedAt: now}

		// An update is only worth an event when the peer's own record changed.
		// Comparing against when we happened to look would report every peer as
		// updated on every single tick.
		if known && previous.advertisedLastSeen.Equal(p.LastSeen) {
			continue
		}
		if !m.emit(discovery.PeerEvent{
			Type: eventTypeFor(known),
			Peer: p,
			Time: now,
		}) {
			return fmt.Errorf("discovery event channel closed")
		}
		log.Printf("rendezvous: %s peer %x (%s) via %s",
			eventVerb(known), p.ID[:8], p.Name, firstAddr(p.Addrs))
	}

	// Anything we knew about but the server no longer lists has expired.
	for id, st := range seen {
		if _, still := present[id]; still {
			continue
		}
		if now.Sub(st.observedAt) < pollExpiry {
			continue
		}
		delete(seen, id)
		m.emit(discovery.PeerEvent{
			Type: discovery.PeerLost,
			Peer: discovery.PeerInfo{ID: id},
			Time: now,
		})
		log.Printf("rendezvous: peer %x expired", id[:8])
	}
	return nil
}

func eventTypeFor(known bool) discovery.EventType {
	if known {
		return discovery.PeerUpdated
	}
	return discovery.PeerFound
}

func eventVerb(known bool) string {
	if known {
		return "updated"
	}
	return "found"
}

// emit sends an event without blocking the poll loop forever. A full channel
// means the consumer is not keeping up; dropping the event is correct because
// the next tick will report the same peer again.
func (m *RendezvousDiscoveryMode) emit(evt discovery.PeerEvent) bool {
	select {
	case <-m.ctx.Done():
		return false
	case m.events <- evt:
		return true
	default:
		return false
	}
}

func firstAddr(addrs []string) string {
	if len(addrs) == 0 {
		return "(no address)"
	}
	return addrs[0]
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
