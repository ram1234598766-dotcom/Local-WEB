package discovery

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/mrityunjay/LocalWEB/pkg/link"
	"github.com/rs/zerolog/log"
)

// Orchestrator manages all discovery modes and merges results.
type Orchestrator struct {
	mu          sync.RWMutex
	nodeID      [32]byte
	publicKey   [32]byte
	name        string
	db          *PeerDatabase
	linkManager *link.Manager
	modes       []DiscoveryMode
	events      chan PeerEvent
	handlers    []func(PeerEvent)
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
}

// DiscoveryMode defines a peer discovery mechanism.
type DiscoveryMode interface {
	Name() string
	RequiresWiFi() bool
	Start(ctx context.Context, nodeID [32]byte, name string) (<-chan PeerEvent, error)
	Advertise(info PeerInfo) error
	Stop() error
}

// OrchestratorConfig holds configuration for the discovery orchestrator.
type OrchestratorConfig struct {
	NodeID      [32]byte
	PublicKey   [32]byte
	Name        string
	Modes       []DiscoveryMode
	LinkManager *link.Manager
	OnPeer      func(PeerEvent)
}

// NewOrchestrator creates a new discovery orchestrator.
func NewOrchestrator(cfg OrchestratorConfig) *Orchestrator {
	ctx, cancel := context.WithCancel(context.Background())

	o := &Orchestrator{
		nodeID:      cfg.NodeID,
		publicKey:   cfg.PublicKey,
		name:        cfg.Name,
		db:          NewPeerDatabase(),
		linkManager: cfg.LinkManager,
		modes:       cfg.Modes,
		events:      make(chan PeerEvent, 128),
		ctx:         ctx,
		cancel:      cancel,
	}

	if cfg.OnPeer != nil {
		o.handlers = append(o.handlers, cfg.OnPeer)
	}

	return o
}

// Run starts all discovery modes.
func (o *Orchestrator) Run() error {
	log.Info().Int("modes", len(o.modes)).Msg("discovery orchestrator starting")

	// Determine which modes to run based on connectivity
	wifi := o.hasWiFi()

	for _, mode := range o.modes {
		if mode.RequiresWiFi() && !wifi {
			log.Info().Str("mode", mode.Name()).Msg("skipping (no WiFi)")
			continue
		}

		events, err := mode.Start(o.ctx, o.nodeID, o.name)
		if err != nil {
			log.Error().Err(err).Str("mode", mode.Name()).Msg("failed to start")
			continue
		}

		o.wg.Add(1)
		go o.consumeModeEvents(mode.Name(), events)
	}

	// Start event processor
	o.wg.Add(1)
	go o.processEvents()

	// Start periodic GC
	o.wg.Add(1)
	go o.gcLoop()

	log.Info().Msg("discovery orchestrator running")
	return nil
}

// consumeModeEvents reads events from a single discovery mode.
func (o *Orchestrator) consumeModeEvents(name string, events <-chan PeerEvent) {
	defer o.wg.Done()

	for {
		select {
		case <-o.ctx.Done():
			return
		case evt, ok := <-events:
			if !ok {
				return
			}
			evt.Peer.Source = name
			o.events <- evt
		}
	}
}

// processEvents handles all discovery events.
func (o *Orchestrator) processEvents() {
	defer o.wg.Done()

	for {
		select {
		case <-o.ctx.Done():
			return
		case evt := <-o.events:
			o.handleEvent(evt)
		}
	}
}

// HandleEvent processes a peer event directly. Exported for testing.
func (o *Orchestrator) HandleEvent(evt PeerEvent) {
	o.handleEvent(evt)
}

func (o *Orchestrator) handleEvent(evt PeerEvent) {
	switch evt.Type {
	case PeerFound, PeerUpdated:
		// Add to database
		existing, exists := o.db.Get(evt.Peer.ID)

		// Skip self
		if evt.Peer.ID == o.nodeID {
			return
		}

		if exists {
			// Update score based on freshness
			evt.Peer.Score = computeScore(existing, &evt.Peer)
			evt.Peer.FirstSeen = existing.FirstSeen
			evt.Peer.Addrs = mergeAddrs(existing.Addrs, evt.Peer.Addrs)
		} else {
			evt.Peer.Score = 0.5
			evt.Peer.FirstSeen = time.Now()
		}

		evt.Peer.LastSeen = time.Now()
		o.db.Add(evt.Peer)

		if !exists {
			log.Info().
				Str("id", fmt.Sprintf("%x", evt.Peer.ID[:8])).
				Str("name", evt.Peer.Name).
				Str("source", evt.Peer.Source).
				Strs("addrs", evt.Peer.Addrs).
				Msg("peer discovered")
		}

	case PeerLost:
		peer, exists := o.db.Get(evt.Peer.ID)
		if exists {
			// Don't remove immediately, just mark
			peer.Score *= 0.5 // Decay score
			if peer.Score < 0.1 {
				o.db.Remove(evt.Peer.ID)
				log.Info().
					Str("id", fmt.Sprintf("%x", evt.Peer.ID[:8])).
					Msg("peer lost")
			}
		}
	}

	// Notify handlers
	for _, h := range o.handlers {
		h(evt)
	}
}

// gcLoop periodically removes stale peers.
func (o *Orchestrator) gcLoop() {
	defer o.wg.Done()
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-o.ctx.Done():
			return
		case <-ticker.C:
			removed := o.db.GC(5 * time.Minute)
			if removed > 0 {
				log.Info().Int("removed", removed).Msg("GC removed stale peers")
			}
		}
	}
}

// Peers returns all known peers.
func (o *Orchestrator) Peers() []*PeerInfo {
	return o.db.All()
}

// PeerCount returns the number of known peers.
func (o *Orchestrator) PeerCount() int {
	return o.db.Count()
}

// BestPeers returns the top N peers by score.
func (o *Orchestrator) BestPeers(n int) []*PeerInfo {
	return o.db.BestPeers(n)
}

// GetPeer returns a specific peer by ID.
func (o *Orchestrator) GetPeer(id [32]byte) (*PeerInfo, bool) {
	return o.db.Get(id)
}

// OnPeer registers a handler for peer events.
func (o *Orchestrator) OnPeer(handler func(PeerEvent)) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.handlers = append(o.handlers, handler)
}

// Stop halts all discovery operations.
func (o *Orchestrator) Stop() {
	o.cancel()
	o.wg.Wait()

	for _, mode := range o.modes {
		if err := mode.Stop(); err != nil {
			log.Error().Err(err).Str("mode", mode.Name()).Msg("error stopping")
		}
	}

	close(o.events)
	log.Info().Msg("discovery orchestrator stopped")
}

func (o *Orchestrator) hasWiFi() bool {
	// Check if any WiFi interface is up
	if o.linkManager == nil {
		return true // Assume WiFi if no link manager
	}
	best := o.linkManager.BestLink()
	if best == nil {
		return false
	}
	return best.Mode() == link.ModeWiFiStation || best.Mode() == link.ModeWiFiDirect
}

func computeScore(old, new *PeerInfo) float64 {
	score := 0.5

	// Freshness boost
	if time.Since(new.LastSeen) < 30*time.Second {
		score += 0.2
	}

	// Latency boost
	if new.Latency < 10*time.Millisecond {
		score += 0.1
	} else if new.Latency < 50*time.Millisecond {
		score += 0.05
	}

	// RSSI boost (BLE)
	if new.RSSI > -60 {
		score += 0.1
	} else if new.RSSI > -80 {
		score += 0.05
	}

	if score > 1.0 {
		score = 1.0
	}
	return score
}
