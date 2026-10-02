package link

import (
	"context"
	"fmt"
	"net"
	"sort"
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
	// rrCursor advances on each round-robin write so consecutive writes take
	// different links. It is guarded by mu like the rest of the set.
	rrCursor uint64
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
	// Aggregation selects the strategy; nil means unset and falls back to
	// AggregationBandwidth. It is a pointer because AggregationFailover is 0,
	// so a plain field cannot tell "unset" from "failover".
	Aggregation *AggregationMode
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
	aggregation := AggregationBandwidth
	if cfg.Aggregation != nil {
		aggregation = *cfg.Aggregation
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
		aggregation: aggregation,
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
			// A sighting with no address cannot be dialled, so record the
			// connection with an empty address rather than indexing a slice that
			// may be empty.
			peerAddr := ""
			if len(evt.Peer.Addrs) > 0 {
				peerAddr = evt.Peer.Addrs[0]
			}
			existing.connections[linkMode] = &LinkConnection{
				Link:        m.linkForMode(linkMode),
				PeerAddr:    peerAddr,
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
			}
			pls.mu.Unlock()

			// updateStats takes pls.mu itself, so it must run outside the
			// critical section above.
			pls.updateStats()

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

	// A connection can exist without a Link behind it when a discovery event
	// names a mode no link was registered for, and such a connection cannot
	// carry traffic, so it is not a candidate for primary.
	candidates := make([]*LinkConnection, 0, len(conns))
	for _, c := range conns {
		if c.Link != nil {
			candidates = append(candidates, c)
		}
	}
	if len(candidates) == 0 {
		// ModeNone rather than the zero value, which is wifi-station.
		pls.primary = ModeNone
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
		best = candidates[0]

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
		if best == nil || best.Link == nil {
			best = candidates[0]
		}

	case AggregationBandwidth:
		// Highest bandwidth (estimated from latency)
		best = candidates[0]
		for _, c := range candidates[1:] {
			if c.Latency < best.Latency {
				best = c
			}
		}

	case AggregationLatency:
		// Lowest latency
		best = candidates[0]
		for _, c := range candidates[1:] {
			if c.Latency < best.Latency {
				best = c
			}
		}
	}

	// Round-robin rotates over every recorded mode, so the successor can still
	// be a connection without a Link behind it.
	if best != nil && best.Link != nil {
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
			if conn.Link == nil {
				// No Link is registered for this mode, so it cannot be dialled.
				log.Warn().Str("link", mode.String()).Msg("no link registered for this mode")
				continue
			}
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

// SendToPeer writes data to one peer over exactly one of its active links.
//
// It previously wrote the whole payload to the primary link and then again to
// every other active link for every mode except failover. That is not redundancy:
// the receiver ends up with the same stream once per link, so any protocol above
// this layer sees its data interleaved with copies of itself. A payload must
// therefore travel over exactly one link per call.
//
// Each mode now selects which single link carries the write:
//
//   - AggregationFailover uses the primary link, and on a write error tries the
//     remaining active links in order.
//   - AggregationRoundRobin rotates a per-peer cursor across active links.
//   - AggregationBandwidth prefers the link that has moved the most bytes per
//     second since it came up.
//   - AggregationLatency prefers the link with the lowest measured latency.
//
// These modes pick a link per write rather than striping one write across
// several. Byte-level striping would need per-segment sequence numbers and a
// reassembly step on the receiving side, and MultiPathManager has no receive path
// to reassemble into, so splitting a single write would corrupt the stream. A
// single copy per call is the most this layer can do correctly today.
func (m *MultiPathManager) SendToPeer(peerID [32]byte, data []byte) (int, error) {
	m.mu.RLock()
	pls, exists := m.peerLinks[peerID]
	m.mu.RUnlock()

	if !exists {
		return 0, fmt.Errorf("peer %x not connected", peerID[:8])
	}

	// Ordering candidate links requires the write lock because the round-robin
	// cursor advances on every call, and BytesSent is updated under it.
	pls.mu.Lock()
	defer pls.mu.Unlock()

	candidates := pls.selectLinks(m.aggregation)
	if len(candidates) == 0 {
		return 0, fmt.Errorf("no active connection to peer %x", peerID[:8])
	}

	var lastErr error
	for i, conn := range candidates {
		n, err := conn.ptr.Conn.Write(data)
		if err != nil {
			lastErr = fmt.Errorf("write over %v: %w", conn.mode, err)
			// Failover retries on the next link; the other modes report the
			// failure, because silently moving a stream between links mid-write
			// would leave the peer's reader with a gap.
			if m.aggregation != AggregationFailover {
				return n, lastErr
			}
			continue
		}
		conn.ptr.BytesSent += uint64(n)
		conn.ptr.LastUpdate = time.Now()

		if i == 0 {
			// The link that carried the write becomes the primary, so subsequent
			// calls start from where the traffic actually is.
			pls.primary = conn.mode
		}
		return n, nil
	}

	return 0, lastErr
}

// selectedLink is one candidate connection plus the mode it belongs to, so
// selectLinks can report which link carried a write.
type selectedLink struct {
	mode LinkMode
	ptr  *LinkConnection
}

// selectLinks orders the active, connected links according to the aggregation
// mode. The caller must hold pls.mu for writing.
//
// A connection with no Link behind it cannot carry traffic, and one with no
// net.Conn has not been dialled, so both are excluded rather than attempted.
func (pls *PeerLinkSet) selectLinks(mode AggregationMode) []selectedLink {
	conns := pls.activeConnections()
	usable := make([]selectedLink, 0, len(conns))
	for _, c := range conns {
		if c.Link == nil || c.Conn == nil {
			continue
		}
		usable = append(usable, selectedLink{mode: c.modeOf(pls), ptr: c})
	}
	if len(usable) == 0 {
		return nil
	}

	// A stable base order keeps the non-rotating modes deterministic: map
	// iteration order is randomised in Go, which would otherwise make link
	// selection vary run to run. The current primary leads the list, so a tie
	// between two links with no measurements yet keeps the one already known to
	// work instead of an arbitrary one.
	sort.SliceStable(usable, func(i, j int) bool {
		return usable[i].mode < usable[j].mode
	})
	usable = pls.primaryFirst(usable)

	switch mode {
	case AggregationRoundRobin:
		start := int(pls.rrCursor) % len(usable)
		out := make([]selectedLink, 0, len(usable))
		for i := 0; i < len(usable); i++ {
			out = append(out, usable[(start+i)%len(usable)])
		}
		pls.rrCursor++
		return out

	case AggregationBandwidth:
		sort.SliceStable(usable, func(i, j int) bool {
			return usable[i].ptr.throughput() > usable[j].ptr.throughput()
		})
		return usable

	case AggregationLatency:
		sort.SliceStable(usable, func(i, j int) bool {
			return usable[i].ptr.Latency < usable[j].ptr.Latency
		})
		return usable

	default: // AggregationFailover
		return usable
	}
}

// primaryFirst moves the current primary link to the front of the list.
func (pls *PeerLinkSet) primaryFirst(in []selectedLink) []selectedLink {
	for i, u := range in {
		if u.mode == pls.primary {
			if i == 0 {
				return in
			}
			out := make([]selectedLink, 0, len(in))
			out = append(out, u)
			out = append(out, in[:i]...)
			out = append(out, in[i+1:]...)
			return out
		}
	}
	return in
}

// modeOf reports the mode whose connection entry this is.
func (c *LinkConnection) modeOf(pls *PeerLinkSet) LinkMode {
	for mode, conn := range pls.connections {
		if conn == c {
			return mode
		}
	}
	return ModeNone
}

// throughput ranks links for the bandwidth-weighted aggregation mode.
//
// Dividing by the time since the connection came up is unstable for a young
// connection: a link established a millisecond ago has an elapsed time near zero,
// so the rate is dominated by the denominator and every fresh link measures as
// zero. Below one second of observation the absolute volume is used instead,
// which at least orders the links correctly, and once a link has been up long
// enough the true rate takes over.
func (c *LinkConnection) throughput() float64 {
	if c.Established.IsZero() {
		return 0
	}
	elapsed := time.Since(c.Established).Seconds()
	if elapsed < 1 {
		return float64(c.BytesSent)
	}
	return float64(c.BytesSent) / elapsed
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
