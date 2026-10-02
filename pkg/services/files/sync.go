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
	exchange   ExchangeProtocol
	lister     PeerLister
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
	// inFlight and bytesRecv give the UI something true to show. Without them
	// a transfer is invisible while it runs and indistinguishable from one that
	// never started.
	inFlight   map[cid.Cid]bool
	bytesRecv  uint64
	blocksRecv uint64
	failed     error
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

// SetExchange gives the engine a way to actually reach peers.
//
// Without it Sync could compute a diff and had no way to act on it, which is why
// the previous implementation computed the want list and returned: the only
// honest options were to move the blocks or to do nothing, and it did nothing
// without saying so.
func (s *syncEngine) SetExchange(e ExchangeProtocol) {
	s.mu.Lock()
	s.exchange = e
	s.mu.Unlock()
}

// SetPeerSource tells the engine who is connected. The daemon passes the
// transport server.
func (s *syncEngine) SetPeerSource(l PeerLister) {
	s.mu.Lock()
	s.lister = l
	s.mu.Unlock()
}

// recordPeerHave stores what a peer says it holds, so the next Sync can diff it.
func (s *syncEngine) recordPeerHave(peerID [32]byte, have []cid.Cid) {
	s.mu.Lock()
	defer s.mu.Unlock()
	peer, ok := s.peers[peerID]
	if !ok {
		peer = &peerSyncState{inFlight: make(map[cid.Cid]bool)}
		s.peers[peerID] = peer
	}
	peer.have = have
	peer.lastSync = time.Now()
}

// Progress reports what each peer is currently owed, what has arrived, and
// whether the exchange finished. It is the source for the GUI's Transfers
// panel, so every field has to be a real count.
func (s *syncEngine) Progress() []SyncProgress {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]SyncProgress, 0, len(s.peers))
	for pid, peer := range s.peers {
		inflight := make([]cid.Cid, 0, len(peer.inFlight))
		for c := range peer.inFlight {
			inflight = append(inflight, c)
		}
		sort.Slice(inflight, func(i, j int) bool { return inflight[i].KeyString() < inflight[j].KeyString() })

		// The total is measured from the local block store, so a peer cannot
		// inflate the progress bar by claiming a large size.
		var total int64
		for _, c := range peer.have {
			if sz, err := s.store.Size(context.Background(), c); err == nil {
				total += sz
			}
		}

		prog := SyncProgress{
			PeerID:     pid,
			Have:       peer.have,
			InFlight:   inflight,
			Complete:   len(inflight) == 0 && peer.failed == nil,
			BytesRecv:  peer.bytesRecv,
			TotalBytes: total,
		}
		if peer.failed != nil {
			prog.Err = peer.failed.Error()
		}
		out = append(out, prog)
	}
	sort.Slice(out, func(i, j int) bool {
		return fmt.Sprintf("%x", out[i].PeerID[:]) < fmt.Sprintf("%x", out[j].PeerID[:])
	})
	return out
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
			for _, peer := range s.connectedPeers() {
				// Advertise first: a peer cannot ask for what it does not know
				// this node holds, and it will not tell us what it holds until
				// it sees a have message.
				if err := s.advertise(ctx, peer); err != nil {
					continue
				}
				if err := s.Sync(ctx, peer); err != nil {
					// A peer that cannot be reached this tick may come back.
					continue
				}
			}
		}
	}
}

// connectedPeers is the union of peers recorded from advertisements and peers
// the transport currently holds open.
func (s *syncEngine) connectedPeers() [][32]byte {
	s.mu.RLock()
	lister := s.lister
	seen := make(map[[32]byte]bool, len(s.peers))
	for pid := range s.peers {
		seen[pid] = true
	}
	s.mu.RUnlock()

	if lister == nil {
		out := make([][32]byte, 0, len(seen))
		for pid := range seen {
			out = append(out, pid)
		}
		return out
	}
	for _, p := range lister.Peers() {
		if p.ID != s.selfID() {
			seen[p.ID] = true
		}
	}
	out := make([][32]byte, 0, len(seen))
	for pid := range seen {
		out = append(out, pid)
	}
	sort.Slice(out, func(i, j int) bool {
		return fmt.Sprintf("%x", out[i][:]) < fmt.Sprintf("%x", out[j][:])
	})
	return out
}

func (s *syncEngine) selfID() [32]byte {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.peerID
}

// advertise tells a peer what this node holds.
func (s *syncEngine) advertise(ctx context.Context, peer [32]byte) error {
	s.mu.RLock()
	exchange := s.exchange
	s.mu.RUnlock()
	if exchange == nil {
		return fmt.Errorf("sync engine has no exchange")
	}

	have, err := s.buildHaveList()
	if err != nil {
		return err
	}
	entries := make([]WantEntry, 0, len(have))
	for _, c := range have {
		entries = append(entries, WantEntry{CID: c, Type: WantHave, Priority: 1})
	}
	return exchange.SendHave(ctx, peer, entries)
}

// Sync diffs this node's store against a peer's advertisement and asks the peer
// for whatever is missing.
func (s *syncEngine) Sync(ctx context.Context, peerID [32]byte) error {
	s.mu.Lock()
	s.stats.TotalSyncs++
	exchange := s.exchange
	s.mu.Unlock()

	if exchange == nil {
		s.mu.Lock()
		s.stats.FailedSyncs++
		s.mu.Unlock()
		return fmt.Errorf("sync engine has no exchange, cannot reach peer %x", peerID[:8])
	}

	// buildHaveList also refreshes the engine's view of the local store, which
	// is what buildWantList diffs against.
	if _, err := s.buildHaveList(); err != nil {
		s.mu.Lock()
		s.stats.FailedSyncs++
		s.mu.Unlock()
		return fmt.Errorf("build have list: %w", err)
	}

	want, err := s.buildWantList(ctx, peerID)
	if err != nil {
		s.mu.Lock()
		s.stats.FailedSyncs++
		s.mu.Unlock()
		return fmt.Errorf("build want list: %w", err)
	}

	if len(want) == 0 {
		// Nothing to fetch. Leave the counters alone: this is a no-op, not a
		// completed transfer and not a failure.
		return nil
	}

	entries := make([]WantEntry, 0, len(want))
	for _, c := range want {
		entries = append(entries, WantEntry{CID: c, Type: WantWant, Priority: 1})
	}

	s.mu.Lock()
	peer, ok := s.peers[peerID]
	if !ok {
		peer = &peerSyncState{inFlight: make(map[cid.Cid]bool)}
		s.peers[peerID] = peer
	}
	peer.lastSync = time.Now()
	peer.failed = nil
	for _, c := range want {
		s.want[c] = true
		s.inFlight[c] = time.Now()
		peer.inFlight[c] = true
	}
	s.stats.ActiveSyncs++
	s.mu.Unlock()

	if err := exchange.SendWant(ctx, peerID, entries); err != nil {
		s.mu.Lock()
		for _, c := range want {
			delete(s.want, c)
			delete(s.inFlight, c)
			delete(peer.inFlight, c)
		}
		peer.failed = err
		s.stats.ActiveSyncs--
		s.stats.FailedSyncs++
		s.mu.Unlock()
		return fmt.Errorf("send want: %w", err)
	}

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
	_, wasWanted := s.want[block.CID]
	delete(s.want, block.CID)
	delete(s.inFlight, block.CID)
	s.stats.BlocksRecv++
	s.stats.BytesRecv += uint64(len(block.Data))
	if wasWanted && s.stats.ActiveSyncs > 0 {
		s.stats.ActiveSyncs--
	}
	s.mu.Unlock()

	return nil
}

// HandleBlock adapts a block arriving on the exchange to ReceivedBlock and
// attributes the progress to the peer that served it. The daemon installs it
// with exchange.SetHandler, which is what turns an answered want into a stored
// block and a moving progress bar.
//
// The same handler takes MsgHave, which is how a peer learns what this node
// holds and therefore what there is to ask for.
func (s *syncEngine) HandleBlock(ctx context.Context, peer [32]byte, msg *ExchangeMessage) error {
	if msg == nil {
		return fmt.Errorf("message is nil")
	}
	switch msg.Type {
	case MsgHave:
		s.RecordPeerHave(peer, msg.CIDs)
		return nil
	case MsgBlock:
	default:
		return nil
	}
	block := &Block{CID: msg.CID, Data: msg.Data}
	// The store recomputes the CID on Put, so a peer cannot hand us content
	// under someone else's name; that check is the boundary here.
	if err := s.ReceivedBlock(ctx, block); err != nil {
		return err
	}

	s.mu.Lock()
	if p, ok := s.peers[peer]; ok {
		if p.inFlight == nil {
			p.inFlight = make(map[cid.Cid]bool)
		}
		delete(p.inFlight, block.CID)
		p.blocksRecv++
		p.bytesRecv += uint64(len(block.Data))
	}
	s.mu.Unlock()
	return nil
}

// RecordPeerHave stores a have advertisement received from a peer.
func (s *syncEngine) RecordPeerHave(peerID [32]byte, have []cid.Cid) {
	s.recordPeerHave(peerID, have)
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

// buildWantList returns the peer's blocks that this node does not have yet.
//
// The previous version returned this node's entire inventory when the peer's
// advertisement was unknown, which is exactly backwards: the want list is what
// to ask the peer for, and an unknown peer inventory means ask for nothing.
func (s *syncEngine) buildWantList(ctx context.Context, peerID [32]byte) ([]cid.Cid, error) {
	// buildHaveList refreshes s.have from the local store, which is the map
	// this diffs against.
	if _, err := s.buildHaveList(); err != nil {
		return nil, err
	}

	s.mu.RLock()
	peerHave := s.peers[peerID]
	s.mu.RUnlock()

	if peerHave == nil || len(peerHave.have) == 0 {
		return nil, nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	// have is a slice, so it cannot be indexed by CID; the lookup has to go
	// through the map the engine maintains.
	out := make([]cid.Cid, 0, len(peerHave.have))
	for _, c := range peerHave.have {
		if s.have[c] {
			continue
		}
		if _, inflight := s.inFlight[c]; inflight {
			continue
		}
		out = append(out, c)
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
