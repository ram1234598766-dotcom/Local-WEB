package link

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// MultiPathManager wraps multiple links and aggregates their bandwidth.
// It maintains concurrent connections over all available links to the same peer.
type MultiPathManager struct {
	mu          sync.RWMutex
	links       []Link
	peerLinks   map[[32]byte]*PeerLinkSet // nodeID -> active links
	preferences []LinkMode
	configs     map[LinkMode]LinkConfig
	events      chan PeerEvent
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	onPeer      func(PeerEvent)
	aggregation AggregationMode
}

// PeerLinkSet tracks all active links to a single peer.
type PeerLinkSet struct {
	mu          sync.RWMutex
	peerID      [32]byte
	connections map[LinkMode]*LinkConnection
	primary     LinkMode
	aggregated  AggregatedStats
}

// LinkConnection represents an active connection over a specific link.
type LinkConnection struct {
	Link        Link
	Conn        net.Conn
	PeerAddr    string
	Established time.Time
	LastUpdate  time.Time
	BytesSent   uint64
	BytesRecv   uint64
	Latency     time.Duration
	Active      bool
}

// AggregatedStats tracks aggregate bandwidth across all links.
type AggregatedStats struct {
	TotalSent   uint64
	TotalRecv   uint64
	TotalLinks  int
	PrimaryLink LinkMode
	AvgLatency  time.Duration
	LastUpdate  time.Time
}

// AggregationMode defines how traffic is distributed across links.
type AggregationMode int

const (
	// AggregationFailover - use primary link, failover on failure
	AggregationFailover AggregationMode = iota
	// AggregationRoundRobin - distribute packets round-robin
	AggregationRoundRobin
	// AggregationBandwidth - weight by link bandwidth
	AggregationBandwidth
	// AggregationLatency - weight by inverse latency
	AggregationLatency
)

// MultiPathConfig holds configuration for multi-path manager.
type MultiPathConfig struct {
	Links       []Link
	Preferences []LinkMode
	Configs     map[LinkMode]LinkConfig
	Aggregation AggregationMode
	OnPeer      func(PeerEvent)
	MaxLinks    int // Maximum concurrent links per peer
}

// NewMultiPathManager creates a new multi-path link manager.
func NewMultiPathManager(cfg MultiPathConfig) *MultiPathManager {
	ctx, cancel := context.WithCancel(context.Background())

	if cfg.Preferences == nil {
		cfg.Preferences = defaultPreferences()
	}
	if cfg.Configs == nil {
		cfg.Configs = DefaultLinkConfigs()
	}
	if cfg.Aggregation == 0 {
		cfg.Aggregation = AggregationBandwidth
	}
	if cfg.MaxLinks == 0 {
		cfg.MaxLinks = 3
	}

	m := &MultiPathManager{
		links:       cfg.Links,
		peerLinks:   make(map[[32]byte]*PeerLinkSet),
		preferences: cfg.Preferences,
		configs:     cfg.Configs,
		events:      make(chan PeerEvent, 128),
		ctx:         ctx,
		cancel:      cancel,
		onPeer:      cfg.OnPeer,
		aggregation: cfg.Aggregation,
	}

	return m
}

// Run starts discovery on all links and manages multi-path connections.
func (m *MultiPathManager) Run() error {
	log.Info().Str("aggregation", m.aggregation.String()).Msg("multi-path link manager starting")

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

	// Start stats aggregator
	m.wg.Add(1)
	go m.statsAggregator()

	log.Info().Int("links", len(m.links)).Msg("multi-path link manager running")
	return nil
}

// runLinkDiscovery starts discovery on a single link type.
func (m *MultiPathManager) runLinkDiscovery(link Link, cfg LinkConfig) {
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
func (m *MultiPathManager) processEvents() {
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
func (m *MultiPathManager) handleEvent(evt PeerEvent) {
	m.mu.Lock()
	defer m.mu.Unlock()

	switch evt.Type {
	case PeerDiscovered, PeerUpdated:
		existing, exists := m.peerLinks[evt.Peer.ID]
		if !exists {
			existing = &PeerLinkSet{
				peerID:      evt.Peer.ID,
				connections: make(map[LinkMode]*LinkConnection),
			}
			m.peerLinks[evt.Peer.ID] = existing
		}

		// Update or add connection for this link mode
		linkMode := evt.Peer.LinkMode
		if conn, ok := existing.connections[linkMode]; ok {
			conn.BytesSent += uint64(len(evt.Peer.Addrs)) // placeholder
			conn.BytesRecv += uint64(len(evt.Peer.Addrs))
			conn.Latency = evt.Peer.Latency
			conn.LastUpdate = time.Now()
		} else if len(existing.connections) < 3 { // MaxLinks
			existing.connections[linkMode] = &LinkConnection{
				Link:        m.linkForMode(linkMode),
				PeerAddr:    evt.Peer.Addrs[0],
				Established: time.Now(),
				Active:      true,
				Latency:     evt.Peer.Latency,
			}
			existing.updatePrimary(m.aggregation)
		}

		// Update aggregate stats
		existing.updateStats()

		log.Info().
			Str("peer", fmt.Sprintf("%x", evt.Peer.ID[:8])).
			Str("link", linkMode.String()).
			Str("aggregation", m.aggregation.String()).
			Msg("multi-path peer updated")

	case PeerLost:
		if pls, ok := m.peerLinks[evt.Peer.ID]; ok {
			pls.mu.Lock()
			if conn, ok := pls.connections[evt.Peer.LinkMode]; ok {
				conn.Active = false
				pls.updatePrimary(m.aggregation)
				pls.updateStats()
			}
			pls.mu.Unlock()

			// If no active connections, remove peer entirely
			if len(pls.activeConnections()) == 0 {
				delete(m.peerLinks, evt.Peer.ID)
			}
		}
	}

	// Notify handler
	if m.onPeer != nil {
		m.onPeer(evt)
	}
}

// statsAggregator periodically recalculates aggregate stats.
func (m *MultiPathManager) statsAggregator() {
	defer m.wg.Done()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.mu.RLock()
			for _, pls := range m.peerLinks {
				pls.updateStats()
			}
			m.mu.RUnlock()
		}
	}
}

// PeerLinkSet methods

func (pls *PeerLinkSet) activeConnections() []*LinkConnection {
	var conns []*LinkConnection
	for _, conn := range pls.connections {
		if conn.Active {
			conns = append(conns, conn)
		}
	}
	return conns
}

func (pls *PeerLinkSet) updatePrimary(mode AggregationMode) {
	conns := pls.activeConnections()
	if len(conns) == 0 {
		return
	}

	var best *LinkConnection
	switch mode {
	case AggregationFailover:
		// Keep existing primary if active, else first
		if pls.primary != 0 {
			if conn, ok := pls.connections[pls.primary]; ok && conn.Active {
				return
			}
		}
		best = conns[0]

	case AggregationRoundRobin:
		// Rotate primary
		if pls.primary != 0 {
			modes := make([]LinkMode, 0, len(pls.connections))
			for mode := range pls.connections {
				modes = append(modes, mode)
			}
			for i, m := range modes {
				if m == pls.primary && i+1 < len(modes) {
					best = pls.connections[modes[i+1]]
					break
				}
			}
		}
		if best == nil {
			best = conns[0]
		}

	case AggregationBandwidth:
		// Highest bandwidth (estimated from latency)
		best = conns[0]
		for _, c := range conns[1:] {
			if c.Latency < best.Latency {
				best = c
			}
		}

	case AggregationLatency:
		// Lowest latency
		best = conns[0]
		for _, c := range conns[1:] {
			if c.Latency < best.Latency {
				best = c
			}
		}
	}

	if best != nil {
		pls.primary = best.Link.Mode()
	}
}

func (pls *PeerLinkSet) updateStats() {
	pls.mu.Lock()
	defer pls.mu.Unlock()

	conns := pls.activeConnections()
	pls.aggregated.TotalLinks = len(conns)
	pls.aggregated.PrimaryLink = pls.primary
	pls.aggregated.LastUpdate = time.Now()

	var totalLatency time.Duration
	for _, conn := range conns {
		pls.aggregated.TotalSent += conn.BytesSent
		pls.aggregated.TotalRecv += conn.BytesRecv
		totalLatency += conn.Latency
	}
	if len(conns) > 0 {
		pls.aggregated.AvgLatency = totalLatency / time.Duration(len(conns))
	}
}

// MultiPathManager public API

// ConnectToPeer establishes connections over all available links to a peer.
func (m *MultiPathManager) ConnectToPeer(ctx context.Context, peer *PeerInfo) (map[LinkMode]net.Conn, error) {
	m.mu.RLock()
	pls, exists := m.peerLinks[peer.ID]
	m.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("peer %x not known", peer.ID[:8])
	}

	pls.mu.RLock()
	defer pls.mu.RUnlock()

	connections := make(map[LinkMode]net.Conn)
	for mode, conn := range pls.connections {
		if !conn.Active {
			continue
		}
		// Establish connection if not already connected
		if conn.Conn == nil {
			var err error
			conn.Conn, err = conn.Link.Connect(ctx, conn.PeerAddr)
			if err != nil {
				log.Error().Err(err).Str("link", mode.String()).Msg("failed to connect")
				continue
			}
			conn.Active = true
		}
		connections[mode] = conn.Conn
	}

	if len(connections) == 0 {
		return nil, fmt.Errorf("no active connections to peer %x", peer.ID[:8])
	}

	return connections, nil
}

// SendToPeer sends data over all active links (for redundancy) or primary link.
func (m *MultiPathManager) SendToPeer(peerID [32]byte, data []byte) (int, error) {
	m.mu.RLock()
	pls, exists := m.peerLinks[peerID]
	m.mu.RUnlock()

	if !exists {
		return 0, fmt.Errorf("peer %x not connected", peerID[:8])
	}

	pls.mu.RLock()
	defer pls.mu.RUnlock()

	// Send over primary link
	primaryConn := pls.connections[pls.primary]
	if primaryConn == nil || primaryConn.Conn == nil || !primaryConn.Active {
		return 0, fmt.Errorf("no active primary connection")
	}

	n, err := primaryConn.Conn.Write(data)
	if err != nil {
		return n, err
	}

	primaryConn.BytesSent += uint64(n)

	// For redundancy, also send over other links (optional)
	if m.aggregation == AggregationFailover {
		return n, err
	}

	// Also send over backup links
	for mode, conn := range pls.connections {
		if mode == pls.primary || conn.Conn == nil || !conn.Active {
			continue
		}
		if n, err := conn.Conn.Write(data); err == nil {
			conn.BytesSent += uint64(n)
		}
	}

	return n, err
}

// GetAggregatedStats returns aggregate stats for a peer.
func (m *MultiPathManager) GetAggregatedStats(peerID [32]byte) (AggregatedStats, bool) {
	m.mu.RLock()
	pls, exists := m.peerLinks[peerID]
	m.mu.RUnlock()

	if !exists {
		return AggregatedStats{}, false
	}

	pls.mu.RLock()
	defer pls.mu.RUnlock()
	return pls.aggregated, true
}

// GetPeerLinks returns active links for a peer.
func (m *MultiPathManager) GetPeerLinks(peerID [32]byte) (map[LinkMode]*LinkConnection, bool) {
	m.mu.RLock()
	pls, exists := m.peerLinks[peerID]
	m.mu.RUnlock()

	if !exists {
		return nil, false
	}

	pls.mu.RLock()
	defer pls.mu.RUnlock()

	result := make(map[LinkMode]*LinkConnection)
	for mode, conn := range pls.connections {
		if conn.Active {
			result[mode] = conn
		}
	}
	return result, true
}

// SetAggregationMode changes the aggregation strategy at runtime.
func (m *MultiPathManager) SetAggregationMode(mode AggregationMode) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.aggregation = mode

	// Re-evaluate primary for all peers
	for _, pls := range m.peerLinks {
		pls.updatePrimary(mode)
	}
}

// BestLink returns the primary link for a peer.
func (m *MultiPathManager) BestLink() Link {
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

// Peers returns all known peers.
func (m *MultiPathManager) Peers() []*PeerInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make([]*PeerInfo, 0, len(m.peerLinks))
	for _, pls := range m.peerLinks {
		if len(pls.activeConnections()) > 0 {
			// Return primary connection info
			primary := pls.connections[pls.primary]
			if primary != nil {
				out = append(out, &PeerInfo{
					ID:       pls.peerID,
					Addrs:    []string{primary.PeerAddr},
					LinkMode: pls.primary,
					Latency:  pls.aggregated.AvgLatency,
					Score:    float64(len(pls.activeConnections())) / 3.0, // heuristic
					LastSeen: pls.aggregated.LastUpdate,
				})
			}
		}
	}
	return out
}

// PeerCount returns the number of known peers.
func (m *MultiPathManager) PeerCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.peerLinks)
}

// Stop halts all link operations.
func (m *MultiPathManager) Stop() {
	m.cancel()
	m.wg.Wait()

	for _, link := range m.links {
		if err := link.Stop(); err != nil {
			log.Error().Err(err).Str("link", link.Name()).Msg("error stopping link")
		}
	}

	close(m.events)
	log.Info().Msg("multi-path link manager stopped")
}

func (m *MultiPathManager) linkForMode(mode LinkMode) Link {
	for _, link := range m.links {
		if link.Mode() == mode {
			return link
		}
	}
	return nil
}

func (a AggregationMode) String() string {
	switch a {
	case AggregationFailover:
		return "failover"
	case AggregationRoundRobin:
		return "round-robin"
	case AggregationBandwidth:
		return "bandwidth"
	case AggregationLatency:
		return "latency"
	default:
		return "unknown"
	}
}
