package files

import (
	"context"
	"crypto/sha3"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/ipfs/go-cid"
	mh "github.com/multiformats/go-multihash"
)

// syncEngine implements SyncEngine using Merkle DAG diff.
type syncEngine struct {
	mu         sync.RWMutex
	peerID     [32]byte
	store      BlockStore
	fileStore  FileStore
	merkleRoot cid.Cid
	have       map[cid.Cid]bool
	want       map[cid.Cid]bool
	inFlight   map[cid.Cid]time.Time
	peers      map[[32]byte]*peerSyncState
	running    bool
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	interval   time.Duration
	stats      SyncStats
}

type peerSyncState struct {
	have     []cid.Cid
	lastSync time.Time
}

// NewSyncEngine creates a new synchronization engine.
func NewSyncEngine(store BlockStore, fileStore FileStore, peerID [32]byte, interval time.Duration) SyncEngine {
	if interval == 0 {
		interval = 5 * time.Second
	}
	return &syncEngine{
		store:     store,
		fileStore: fileStore,
		peerID:    peerID,
		have:      make(map[cid.Cid]bool),
		want:      make(map[cid.Cid]bool),
		inFlight:  make(map[cid.Cid]time.Time),
		peers:     make(map[[32]byte]*peerSyncState),
		interval:  interval,
	}
}

func (s *syncEngine) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return fmt.Errorf("sync engine already running")
	}
	s.running = true
	ctx, s.cancel = context.WithCancel(ctx)
	s.mu.Unlock()

	s.wg.Add(1)
	go s.tickLoop(ctx)
	return nil
}

func (s *syncEngine) Stop() error {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return nil
	}
	s.cancel()
	s.running = false
	s.mu.Unlock()

	s.wg.Wait()
	return nil
}

func (s *syncEngine) tickLoop(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	defer s.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.mu.RLock()
		peers := make([]PeerInfo, 0, len(s.peers))
		for pid := range s.peers {
			peers = append(peers, PeerInfo{ID: pid, State: "connected"})
		}
			s.mu.RUnlock()

			for _, peer := range peers {
				if err := s.Sync(ctx, peer.ID); err != nil {
					// Log but continue
				}
			}
		}
	}
}

func (s *syncEngine) Sync(ctx context.Context, peerID [32]byte) error {
	s.mu.Lock()
	s.stats.TotalSyncs++
	s.mu.Unlock()

	have, err := s.buildHaveList()
	if err != nil {
		return fmt.Errorf("build have list: %w", err)
	}

	want, err := s.buildWantList(ctx, peerID)
	if err != nil {
		return fmt.Errorf("build want list: %w", err)
	}

	onlyInWant := diffCIDs(want, have)
	if len(onlyInWant) == 0 {
		s.mu.Lock()
		s.stats.ActiveSyncs--
		s.mu.Unlock()
		return nil
	}

	s.mu.Lock()
	for _, c := range onlyInWant {
		s.want[c] = true
		s.inFlight[c] = time.Now()
	}
	s.mu.Unlock()

	// Trigger exchange with peer (handled externally via ReceivedBlock)
	s.mu.Lock()
	s.stats.ActiveSyncs++
	s.mu.Unlock()

	return nil
}

func (s *syncEngine) WantList(ctx context.Context, peerID [32]byte) ([]cid.Cid, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]cid.Cid, 0, len(s.want))
	for c := range s.want {
		out = append(out, c)
	}
	return out, nil
}

func (s *syncEngine) ReceivedBlock(ctx context.Context, block *Block) error {
	if block == nil {
		return fmt.Errorf("block is nil")
	}

	if err := s.store.Put(ctx, block); err != nil {
		return fmt.Errorf("store block: %w", err)
	}

	s.mu.Lock()
	s.have[block.CID] = true
	delete(s.want, block.CID)
	delete(s.inFlight, block.CID)
	s.stats.BlocksRecv++
	s.stats.BytesRecv += uint64(len(block.Data))
	s.mu.Unlock()

	return nil
}

func (s *syncEngine) Peers() []PeerInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]PeerInfo, 0, len(s.peers))
	for pid := range s.peers {
		out = append(out, PeerInfo{ID: pid, State: "connected"})
	}
	return out
}

func (s *syncEngine) Stats() SyncStats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.stats
}

func (s *syncEngine) buildHaveList() ([]cid.Cid, error) {
	cids, err := s.store.List(context.Background())
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, c := range cids {
		if !s.have[c] {
			s.have[c] = true
		}
	}

	out := make([]cid.Cid, 0, len(s.have))
	for c := range s.have {
		out = append(out, c)
	}
	return out, nil
}

func (s *syncEngine) buildWantList(ctx context.Context, peerID [32]byte) ([]cid.Cid, error) {
	have, err := s.buildHaveList()
	if err != nil {
		return nil, err
	}

	s.mu.RLock()
	peerHave := s.peers[peerID]
	s.mu.RUnlock()

	if peerHave == nil || len(peerHave.have) == 0 {
		return have, nil
	}

	peerSet := make(map[string]bool, len(peerHave.have))
	for _, c := range peerHave.have {
		peerSet[c.String()] = true
	}

	out := make([]cid.Cid, 0)
	for cidStr := range peerSet {
		c, err := cid.Decode(cidStr)
		if err != nil {
			continue
		}
		if !s.have[c] && s.want[c] {
			out = append(out, c)
		}
	}
	return out, nil
}

// computeFileCID computes the CID for a file's content.
func computeFileCID(data []byte) cid.Cid {
	prefix := cid.NewPrefixV1(cid.Raw, mh.SHA2_256)
	c, _ := prefix.Sum(data)
	return c
}

// computeMerkleRoot computes the root of a Merkle DAG from file CIDs.
func computeMerkleRoot(fileCIDs []cid.Cid) cid.Cid {
	if len(fileCIDs) == 0 {
		return cid.Cid{}
	}
	if len(fileCIDs) == 1 {
		return fileCIDs[0]
	}
	leaves := make([][32]byte, len(fileCIDs))
	for i, c := range fileCIDs {
		copy(leaves[i][:], c.Hash())
	}
	root := merkleRoot(leaves)
	prefix := cid.NewPrefixV1(cid.Raw, mh.SHA2_256)
	c, _ := prefix.Sum(root[:])
	return c
}

func merkleRoot(leaves [][32]byte) [32]byte {
	if len(leaves) == 0 {
		return [32]byte{}
	}
	if len(leaves) == 1 {
		return leaves[0]
	}
	sorted := make([][32]byte, len(leaves))
	copy(sorted, leaves)
	sort.Slice(sorted, func(i, j int) bool {
		return bytesCompare(sorted[i][:], sorted[j][:]) < 0
	})
	var next [][32]byte
	for i := 0; i < len(sorted); i += 2 {
		if i+1 < len(sorted) {
			next = append(next, hashPair(sorted[i], sorted[i+1]))
		} else {
			next = append(next, sorted[i])
		}
	}
	return merkleRoot(next)
}

func hashPair(a, b [32]byte) [32]byte {
	h := sha3.New256()
	if bytesCompare(a[:], b[:]) < 0 {
		h.Write(a[:])
		h.Write(b[:])
	} else {
		h.Write(b[:])
		h.Write(a[:])
	}
	var out [32]byte
	h.Sum(out[:0])
	return out
}

func bytesCompare(a, b []byte) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return int(a[i]) - int(b[i])
		}
	}
	return len(a) - len(b)
}

// diffCIDs returns CIDs in a that are not in b.
func diffCIDs(a, b []cid.Cid) []cid.Cid {
	set := make(map[cid.Cid]bool, len(b))
	for _, c := range b {
		set[c] = true
	}
	out := make([]cid.Cid, 0)
	for _, c := range a {
		if !set[c] {
			out = append(out, c)
		}
	}
	return out
}
