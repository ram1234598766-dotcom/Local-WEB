package discovery

import (
	"sync"
	"time"
)

// EventType represents a discovery event type.
type EventType int

const (
	PeerFound EventType = iota
	PeerLost
	PeerUpdated
)

// PeerInfo holds comprehensive peer information.
type PeerInfo struct {
	ID        [32]byte
	PublicKey [32]byte
	Name      string
	Addrs     []string
	Services  []ServiceInfo
	Source    string // Which discovery method found this peer
	RSSI      int32
	Latency   time.Duration
	Score     float64
	LastSeen  time.Time
	FirstSeen time.Time
	Version   string
}

// ServiceInfo describes a service offered by a peer.
type ServiceInfo struct {
	Name string
	Port int
	TXT  map[string]string
}

// PeerEvent represents a discovery event.
type PeerEvent struct {
	Type EventType
	Peer PeerInfo
	Time time.Time
}

// PeerDatabase stores discovered peers with deduplication and scoring.
type PeerDatabase struct {
	mu    sync.RWMutex
	peers map[[32]byte]*PeerInfo
}

// NewPeerDatabase creates a new peer database.
func NewPeerDatabase() *PeerDatabase {
	return &PeerDatabase{
		peers: make(map[[32]byte]*PeerInfo),
	}
}

// Add adds or updates a peer in the database.
func (db *PeerDatabase) Add(peer PeerInfo) {
	db.mu.Lock()
	defer db.mu.Unlock()

	if existing, ok := db.peers[peer.ID]; ok {
		// Update: keep higher score, merge addresses
		peer.FirstSeen = existing.FirstSeen
		if peer.Score < existing.Score {
			peer.Score = existing.Score
		}
		peer.Addrs = mergeAddrs(existing.Addrs, peer.Addrs)
	}

	peer.LastSeen = time.Now()
	if peer.FirstSeen.IsZero() {
		peer.FirstSeen = time.Now()
	}

	db.peers[peer.ID] = &peer
}

// Get returns a peer by ID.
func (db *PeerDatabase) Get(id [32]byte) (*PeerInfo, bool) {
	db.mu.RLock()
	defer db.mu.RUnlock()
	p, ok := db.peers[id]
	return p, ok
}

// Remove removes a peer from the database.
func (db *PeerDatabase) Remove(id [32]byte) {
	db.mu.Lock()
	defer db.mu.Unlock()
	delete(db.peers, id)
}

// All returns all known peers.
func (db *PeerDatabase) All() []*PeerInfo {
	db.mu.RLock()
	defer db.mu.RUnlock()
	out := make([]*PeerInfo, 0, len(db.peers))
	for _, p := range db.peers {
		out = append(out, p)
	}
	return out
}

// Count returns the number of known peers.
func (db *PeerDatabase) Count() int {
	db.mu.RLock()
	defer db.mu.RUnlock()
	return len(db.peers)
}

// GC removes peers not seen since timeout.
func (db *PeerDatabase) GC(timeout time.Duration) int {
	db.mu.Lock()
	defer db.mu.Unlock()
	before := len(db.peers)
	now := time.Now()
	for id, p := range db.peers {
		if now.Sub(p.LastSeen) > timeout {
			delete(db.peers, id)
		}
	}
	return before - len(db.peers)
}

// BestPeers returns the top N peers by score.
func (db *PeerDatabase) BestPeers(n int) []*PeerInfo {
	db.mu.RLock()
	defer db.mu.RUnlock()

	all := make([]*PeerInfo, 0, len(db.peers))
	for _, p := range db.peers {
		all = append(all, p)
	}

	// Simple selection sort for top N
	for i := 0; i < len(all) && i < n; i++ {
		maxIdx := i
		for j := i + 1; j < len(all); j++ {
			if all[j].Score > all[maxIdx].Score {
				maxIdx = j
			}
		}
		all[i], all[maxIdx] = all[maxIdx], all[i]
	}

	if len(all) > n {
		all = all[:n]
	}
	return all
}

func mergeAddrs(a, b []string) []string {
	seen := make(map[string]bool)
	for _, addr := range a {
		seen[addr] = true
	}
	var merged []string
	merged = append(merged, a...)
	for _, addr := range b {
		if !seen[addr] {
			merged = append(merged, addr)
			seen[addr] = true
		}
	}
	return merged
}
