package link

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// Manager orchestrates all link types and selects the best available one.
type Manager struct {
	mu           sync.RWMutex
	links        []Link
	active       Link
	peers        map[[32]byte]*PeerInfo // nodeID → peer info
	configs      map[LinkMode]LinkConfig
	preferences  []LinkMode
	events       chan PeerEvent
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup
	onPeer       func(PeerEvent)
	autoEscalate bool
}

// ManagerConfig holds configuration for the link manager.
type ManagerConfig struct {
	Links        []Link
	Preferences  []LinkMode
	Configs      map[LinkMode]LinkConfig
	AutoEscalate bool // BLE → WiFi Direct automatically
	OnPeer       func(PeerEvent)
}

// NewManager creates a new adaptive link manager.
func NewManager(cfg ManagerConfig) *Manager {
	ctx, cancel := context.WithCancel(context.Background())

	if cfg.Preferences == nil {
		cfg.Preferences = defaultPreferences()
	}
	if cfg.Configs == nil {
		cfg.Configs = DefaultLinkConfigs()
	}

	m := &Manager{
		links:        cfg.Links,
		peers:        make(map[[32]byte]*PeerInfo),
		configs:      cfg.Configs,
		preferences:  cfg.Preferences,
		events:       make(chan PeerEvent, 64),
		ctx:          ctx,
		cancel:       cancel,
		onPeer:       cfg.OnPeer,
		autoEscalate: cfg.AutoEscalate,
	}

	return m
}

// Run starts the link manager. Discovers peers on all available links.
func (m *Manager) Run() error {
	log.Info().Msg("adaptive link manager starting")

	// Start discovery on all available links
	for _, link := range m.links {
		cfg := m.configs[link.Mode()]
		if !cfg.Enabled {
			continue
		}
		if !link.IsAvailable(m.ctx) {
			log.Info().Str("link", link.Name()).Msg("link not available, skipping")
			continue
		}

		m.wg.Add(1)
		go m.runLinkDiscovery(link, cfg)
	}

	// Start event processor
	m.wg.Add(1)
	go m.processEvents()

	log.Info().Int("links", len(m.links)).Msg("adaptive link manager running")
	return nil
}

// runLinkDiscovery starts discovery on a single link type.
func (m *Manager) runLinkDiscovery(link Link, cfg LinkConfig) {
	defer m.wg.Done()

	log.Info().Str("link", link.Name()).Msg("starting discovery")

	events, err := link.Discover(m.ctx)
	if err != nil {
		log.Error().Err(err).Str("link", link.Name()).Msg("discovery failed")
		return
	}

	for {
		select {
		case <-m.ctx.Done():
			return
		case evt, ok := <-events:
			if !ok {
				return
			}
			m.events <- evt
		}
	}
}

// processEvents handles peer events from all links.
func (m *Manager) processEvents() {
	defer m.wg.Done()

	for {
		select {
		case <-m.ctx.Done():
			return
		case evt := <-m.events:
			m.handleEvent(evt)
		}
	}
}

// handleEvent processes a single peer event.
func (m *Manager) handleEvent(evt PeerEvent) {
	m.mu.Lock()
	defer m.mu.Unlock()

	switch evt.Type {
	case PeerDiscovered, PeerUpdated:
		existing, exists := m.peers[evt.Peer.ID]
		if exists {
			// Update: keep best address, merge services
			evt.Peer.Score = computeScore(existing, &evt.Peer)
			if evt.Peer.Score > existing.Score {
				m.peers[evt.Peer.ID] = &evt.Peer
			}
		} else {
			evt.Peer.Score = 0.5 // Initial score
			m.peers[evt.Peer.ID] = &evt.Peer
		}
		log.Info().
			Str("peer", fmt.Sprintf("%x", evt.Peer.ID[:8])).
			Str("link", evt.Peer.LinkMode.String()).
			Strs("addrs", evt.Peer.Addrs).
			Msg("peer discovered")

	case PeerLost:
		if _, exists := m.peers[evt.Peer.ID]; exists {
			delete(m.peers, evt.Peer.ID)
			log.Info().
				Str("peer", fmt.Sprintf("%x", evt.Peer.ID[:8])).
				Msg("peer lost")
		}
	}

	// Notify handler
	if m.onPeer != nil {
		m.onPeer(evt)
	}

	// Auto-escalation: BLE peer → try WiFi Direct
	if m.autoEscalate && evt.Peer.LinkMode == ModeBLE {
		m.tryEscalate(&evt.Peer)
	}
}

// tryEscalate attempts to upgrade a BLE connection to WiFi Direct.
func (m *Manager) tryEscalate(peer *PeerInfo) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, link := range m.links {
		if link.Mode() == ModeWiFiDirect && link.IsAvailable(m.ctx) {
			log.Info().
				Str("peer", fmt.Sprintf("%x", peer.ID[:8])).
				Str("from", "ble").
				Str("to", "wifi-direct").
				Msg("auto-escalating connection")
			// In real implementation: exchange WiFi Direct credentials via BLE
			// then connect via WiFi Direct
			return
		}
	}
}

// BestLink returns the highest-priority available link.
func (m *Manager) BestLink() Link {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, mode := range m.preferences {
		for _, link := range m.links {
			if link.Mode() == mode && link.IsAvailable(m.ctx) {
				return link
			}
		}
	}
	return nil
}

// ConnectToPeer connects to a peer using the best available link.
func (m *Manager) ConnectToPeer(ctx context.Context, peer *PeerInfo) (net.Conn, error) {
	// Try addresses on the peer's discovery link first
	if link := m.linkForMode(peer.LinkMode); link != nil {
		for _, addr := range peer.Addrs {
			conn, err := link.Connect(ctx, addr)
			if err == nil {
				return conn, nil
			}
		}
	}

	// Fallback: try all links
	for _, link := range m.links {
		if !link.IsAvailable(ctx) {
			continue
		}
		for _, addr := range peer.Addrs {
			conn, err := link.Connect(ctx, addr)
			if err == nil {
				return conn, nil
			}
		}
	}

	return nil, fmt.Errorf("no available link to reach peer %x", peer.ID[:8])
}

// Peers returns a snapshot of all known peers.
func (m *Manager) Peers() []*PeerInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make([]*PeerInfo, 0, len(m.peers))
	for _, p := range m.peers {
		out = append(out, p)
	}
	return out
}

// PeerCount returns the number of known peers.
func (m *Manager) PeerCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.peers)
}

// ActiveLink returns the currently active (best) link.
func (m *Manager) ActiveLink() Link {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.active
}

// Stop halts all link operations.
func (m *Manager) Stop() {
	m.cancel()
	m.wg.Wait()

	for _, link := range m.links {
		if err := link.Stop(); err != nil {
			log.Error().Err(err).Str("link", link.Name()).Msg("error stopping link")
		}
	}

	close(m.events)
	log.Info().Msg("adaptive link manager stopped")
}

func (m *Manager) linkForMode(mode LinkMode) Link {
	for _, link := range m.links {
		if link.Mode() == mode {
			return link
		}
	}
	return nil
}

func computeScore(old, new *PeerInfo) float64 {
	score := 0.5 // Base score for existing peer

	// Boost if seen recently
	if time.Since(new.LastSeen) < 30*time.Second {
		score += 0.2
	}

	// Boost for low latency
	if new.Latency < 10*time.Millisecond {
		score += 0.1
	} else if new.Latency < 50*time.Millisecond {
		score += 0.05
	}

	// Boost for strong signal (BLE)
	if new.RSSI > -60 {
		score += 0.1
	} else if new.RSSI > -80 {
		score += 0.05
	}

	// Cap at 1.0
	if score > 1.0 {
		score = 1.0
	}

	return score
}

func defaultPreferences() []LinkMode {
	return []LinkMode{
		ModeWiFiStation,
		ModeWiFiDirect,
		ModeAdHocWiFi,
		ModeUSBTether,
		ModeBLE,
	}
}
