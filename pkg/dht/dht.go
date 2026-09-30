package dht

import (
	"bytes"
	"context"
	"crypto/sha3"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"net"
	"sort"
	"sync"
	"time"

	"github.com/ram1234598766-dotcom/Local-WEB/pkg/crypto"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/discovery"
	"github.com/rs/zerolog/log"
)

const (
	KBucketSize = 20
	Alpha       = 3
	MaxHops     = 15
)

var (
	ErrBucketFull  = errors.New("bucket full")
	ErrNoPeers     = errors.New("no peers found")
	ErrMaxHops     = errors.New("max hops exceeded")
	ErrPoWFailed   = errors.New("proof of work failed")
	ErrNodeRunning = errors.New("dht node already running")
	ErrNotRunning  = errors.New("dht node not running")
)

type NodeID [32]byte

func NodeIDFromPub(pub [32]byte) NodeID {
	h := sha3.New256()
	h.Write(pub[:])
	var out NodeID
	h.Sum(out[:0])
	return out
}

func (id NodeID) String() string {
	return fmt.Sprintf("%x", id[:8])
}

func (id NodeID) Xor(other NodeID) [32]byte {
	var out [32]byte
	for i := range id {
		out[i] = id[i] ^ other[i]
	}
	return out
}

// PrefixLen returns the number of leading zero bits shared between this ID and
// the all-zero ID, i.e. the index of the ID's first set bit.
//
// Kademlia indexes buckets by the common prefix length between the local ID
// and the remote ID. Comparing against zero is how the routing table maps a
// peer to a bucket when it wants a single fixed split axis.
func (id NodeID) PrefixLen() int {
	for i := range id {
		if id[i] == 0 {
			continue
		}
		for j := 7; j >= 0; j-- {
			if id[i]&(1<<uint(j)) != 0 {
				return i*8 + (7 - j)
			}
		}
	}
	// The all-zero ID shares the full 256-bit prefix.
	return 256
}

type PeerInfo struct {
	ID        NodeID
	PublicKey [32]byte
	Name      string
	Addrs     []string
	Services  []string
	Score     float64
	LastSeen  time.Time
	FirstSeen time.Time
	Version   string
}

type Peer struct {
	Info PeerInfo
}

type KBucket struct {
	peers []*Peer
	mu    sync.Mutex
}

func NewKBucket() *KBucket {
	return &KBucket{peers: make([]*Peer, 0, KBucketSize)}
}

func (b *KBucket) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.peers)
}

func (b *KBucket) Add(p *Peer) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, existing := range b.peers {
		if existing.Info.ID == p.Info.ID {
			existing.Info.LastSeen = time.Now()
			existing.Info.Score = p.Info.Score
			return false
		}
	}
	if len(b.peers) >= KBucketSize {
		return false
	}
	p.Info.LastSeen = time.Now()
	b.peers = append(b.peers, p)
	return true
}

func (b *KBucket) Remove(id NodeID) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i, p := range b.peers {
		if p.Info.ID == id {
			b.peers = append(b.peers[:i], b.peers[i+1:]...)
			return
		}
	}
}

func (b *KBucket) GetClosest(n int, target NodeID) []*Peer {
	b.mu.Lock()
	defer b.mu.Unlock()
	peers := make([]*Peer, len(b.peers))
	copy(peers, b.peers)
	sort.Slice(peers, func(i, j int) bool {
		return compareDist(xorDist(peers[i].Info.ID, target), xorDist(peers[j].Info.ID, target)) < 0
	})
	if len(peers) > n {
		peers = peers[:n]
	}
	return peers
}

func (b *KBucket) All() []*Peer {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]*Peer, len(b.peers))
	copy(out, b.peers)
	return out
}

type RoutingTable struct {
	localID NodeID
	buckets [256]*KBucket
	mu      sync.RWMutex
}

func NewRoutingTable(localID NodeID) *RoutingTable {
	rt := &RoutingTable{localID: localID}
	for i := range rt.buckets {
		rt.buckets[i] = NewKBucket()
	}
	return rt
}

func (rt *RoutingTable) Add(p *Peer) bool {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	b := rt.bucket(p.Info.ID)
	return b.Add(p)
}

func (rt *RoutingTable) Remove(id NodeID) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	b := rt.bucket(id)
	b.Remove(id)
}

// FindClosest returns the n peers closest to target across every bucket.
//
// The global sort matters: each bucket is searched independently, so
// concatenating per-bucket results in bucket order and truncating would keep
// whichever peers happen to sit in low-numbered buckets regardless of
// distance.
func (rt *RoutingTable) FindClosest(target NodeID, n int) []*Peer {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	var result []*Peer
	for _, b := range rt.buckets {
		result = append(result, b.All()...)
	}
	if len(result) == 0 {
		return nil
	}
	sort.SliceStable(result, func(i, j int) bool {
		return compareDist(xorDist(result[i].Info.ID, target), xorDist(result[j].Info.ID, target)) < 0
	})
	if len(result) > n {
		result = result[:n]
	}
	return result
}

func (rt *RoutingTable) AllPeers() []*Peer {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	var all []*Peer
	for _, b := range rt.buckets {
		all = append(all, b.All()...)
	}
	return all
}

// Len reports how many peers the table holds across all buckets.
func (rt *RoutingTable) Len() int {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	n := 0
	for _, b := range rt.buckets {
		n += b.Len()
	}
	return n
}

// BucketIndex reports which bucket a node ID maps to. Exported so the bucket
// layout can be asserted directly rather than only through Add/FindClosest.
func (rt *RoutingTable) BucketIndex(id NodeID) int {
	prefixLen := id.PrefixLen()
	if prefixLen > 255 {
		prefixLen = 0
	}
	return prefixLen
}

func (rt *RoutingTable) bucket(id NodeID) *KBucket {
	return rt.buckets[rt.BucketIndex(id)]
}

// xorDist returns the full 256-bit XOR distance between two node IDs.
//
// The previous implementation truncated this to the low 64 bits, which made
// every pair of IDs agreeing in the first 192 bits compare equal and ordered
// the routing table on the least significant bytes instead of the most
// significant ones. Kademlia closeness is defined by the first differing bit,
// so the whole value has to be compared.
func xorDist(a, b NodeID) [32]byte {
	return a.Xor(b)
}

// compareDist orders two 256-bit distances: -1, 0, or 1.
func compareDist(a, b [32]byte) int {
	return bytes.Compare(a[:], b[:])
}

type Node struct {
	mu         sync.Mutex
	id         NodeID
	pubKey     [32]byte
	name       string
	table      *RoutingTable
	peers      map[NodeID]*Peer
	store      map[string][]byte
	listenAddr string
	transport  QUICTransport
	bootstrap  []string
}

type QUICTransport interface {
	Dial(ctx context.Context, addr string) (net.Conn, error)
	Listen(addr string) (net.Listener, error)
}

type MessageType uint8

const (
	MsgPing MessageType = iota + 1
	MsgPong
	MsgFindNode
	MsgFoundNode
	MsgStore
	MsgFindValue
	MsgFoundValue
	MsgRegisterNode
)

type Message struct {
	Type    MessageType
	Src     NodeID
	Dst     NodeID
	Payload []byte
}

type RPCClient interface {
	Call(ctx context.Context, addr string, msg Message) (Message, error)
}

type rpcClient struct {
	dial func(ctx context.Context, addr string) (net.Conn, error)
}

func NewRPCClient(dial func(ctx context.Context, addr string) (net.Conn, error)) RPCClient {
	return &rpcClient{dial: dial}
}

func (c *rpcClient) Call(ctx context.Context, addr string, msg Message) (Message, error) {
	conn, err := c.dial(ctx, addr)
	if err != nil {
		return Message{}, err
	}
	defer conn.Close()

	if deadline, ok := ctx.Deadline(); ok {
		conn.SetDeadline(deadline)
	} else {
		conn.SetDeadline(time.Now().Add(5 * time.Second))
	}

	hdr := make([]byte, 1+32+32)
	hdr[0] = byte(msg.Type)
	copy(hdr[1:33], msg.Src[:])
	copy(hdr[33:65], msg.Dst[:])
	if _, err := conn.Write(hdr); err != nil {
		return Message{}, err
	}

	// Write payload length prefix and payload
	var payloadLenBuf [4]byte
	binary.BigEndian.PutUint32(payloadLenBuf[:], uint32(len(msg.Payload)))
	if _, err := conn.Write(payloadLenBuf[:]); err != nil {
		return Message{}, err
	}
	if len(msg.Payload) > 0 {
		if _, err := conn.Write(msg.Payload); err != nil {
			return Message{}, err
		}
	}

	var typeBuf [1]byte
	if _, err := io.ReadFull(conn, typeBuf[:]); err != nil {
		return Message{}, err
	}
	respType := MessageType(typeBuf[0])
	var src, dst NodeID
	if _, err := io.ReadFull(conn, src[:]); err != nil {
		return Message{}, err
	}
	if _, err := io.ReadFull(conn, dst[:]); err != nil {
		return Message{}, err
	}
	var lenBuf [4]byte
	if _, err := io.ReadFull(conn, lenBuf[:]); err != nil {
		return Message{}, err
	}
	plLen := binary.BigEndian.Uint32(lenBuf[:])
	pl := make([]byte, plLen)
	if _, err := io.ReadFull(conn, pl); err != nil {
		return Message{}, err
	}

	return Message{
		Type:    respType,
		Src:     src,
		Dst:     dst,
		Payload: pl,
	}, nil
}

type DHT struct {
	localID NodeID
	node    *Node
	peers   *discovery.PeerDatabase
	mu      sync.RWMutex
	running bool
	ctx     context.Context
	cancel  context.CancelFunc
}

func NewDHT(localID NodeID, pubKey [32]byte, name string, transport QUICTransport) *DHT {
	table := NewRoutingTable(localID)
	node := &Node{
		id:        localID,
		pubKey:    pubKey,
		name:      name,
		table:     table,
		peers:     make(map[NodeID]*Peer),
		store:     make(map[string][]byte),
		transport: transport,
	}
	return &DHT{
		localID: localID,
		node:    node,
		peers:   discovery.NewPeerDatabase(),
	}
}

func (d *DHT) Bootstrap(ctx context.Context, bootstrap []string) error {
	d.mu.Lock()
	if d.running {
		d.mu.Unlock()
		return ErrNodeRunning
	}
	d.running = true
	d.node.bootstrap = bootstrap
	d.mu.Unlock()

	client := NewRPCClient(d.node.transport.Dial)
	for _, addr := range bootstrap {
		msg := Message{
			Type:    MsgFindNode,
			Src:     d.localID,
			Dst:     d.localID,
			Payload: d.localID[:],
		}
		resp, err := client.Call(ctx, addr, msg)
		if err != nil {
			continue
		}
		if resp.Type == MsgFoundNode {
			peers, err := decodePeerList(resp.Payload)
			if err == nil {
				for _, p := range peers {
					d.storePeer(p)
				}
			}
		}
	}
	return nil
}

// Lookup performs an iterative Kademlia lookup for target.
//
// Each round queries the Alpha closest peers that have not been queried yet
// and folds the responses into a shortlist of the KBucketSize closest nodes
// seen so far. The shortlist is what bounds the lookup: the previous
// implementation appended every newly discovered peer to the frontier without
// re-sorting or pruning, so the query set grew on every hop and the lookup
// degenerated into a broadcast.
func (d *DHT) Lookup(ctx context.Context, target NodeID) ([]PeerInfo, error) {
	d.mu.RLock()
	if !d.running {
		d.mu.RUnlock()
		return nil, ErrNotRunning
	}
	d.mu.RUnlock()

	client := NewRPCClient(d.node.transport.Dial)
	seeds := d.node.table.FindClosest(target, KBucketSize)
	if len(seeds) == 0 {
		return nil, ErrNoPeers
	}

	// shortlist holds the closest nodes discovered, sorted ascending by
	// distance to target and capped at KBucketSize.
	shortlist := append([]*Peer(nil), seeds...)
	sortPeersByDistance(shortlist, target)

	queried := make(map[NodeID]bool)
	for hops := 0; hops < MaxHops; hops++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		// Pick the closest unqueried peers, up to Alpha per round.
		batch := make([]*Peer, 0, Alpha)
		for _, p := range shortlist {
			if len(batch) >= Alpha {
				break
			}
			if queried[p.Info.ID] {
				continue
			}
			if len(p.Info.Addrs) == 0 {
				// Nothing to dial; mark it so the round does not retry it.
				queried[p.Info.ID] = true
				continue
			}
			queried[p.Info.ID] = true
			batch = append(batch, p)
		}
		if len(batch) == 0 {
			break
		}

		progressed := false
		for _, p := range batch {
			msg := Message{
				Type:    MsgFindNode,
				Src:     d.localID,
				Dst:     p.Info.ID,
				Payload: target[:],
			}
			resp, err := client.Call(ctx, p.Info.Addrs[0], msg)
			if err != nil {
				continue
			}
			if resp.Type != MsgFoundNode {
				continue
			}
			found, err := decodePeerList(resp.Payload)
			if err != nil {
				continue
			}
			for _, fp := range found {
				if _, known := d.node.peers[fp.ID]; !known {
					progressed = true
				}
				d.storePeer(fp)
				shortlist = append(shortlist, &Peer{Info: fp})
			}
		}

		// Re-sort and prune. Duplicate IDs from overlapping responses are
		// collapsed before pruning so one node cannot occupy several slots.
		shortlist = dedupeAndPrune(shortlist, target, KBucketSize)
		if !progressed {
			break
		}
	}

	out := make([]PeerInfo, 0, len(shortlist))
	for _, p := range shortlist {
		out = append(out, p.Info)
	}
	return out, nil
}

func sortPeersByDistance(peers []*Peer, target NodeID) {
	sort.SliceStable(peers, func(i, j int) bool {
		return compareDist(xorDist(peers[i].Info.ID, target), xorDist(peers[j].Info.ID, target)) < 0
	})
}

func dedupeAndPrune(peers []*Peer, target NodeID, n int) []*Peer {
	sortPeersByDistance(peers, target)
	seen := make(map[NodeID]bool, len(peers))
	out := peers[:0]
	for _, p := range peers {
		if seen[p.Info.ID] {
			continue
		}
		seen[p.Info.ID] = true
		out = append(out, p)
		if len(out) == n {
			break
		}
	}
	return out
}

func (d *DHT) Store(ctx context.Context, key string, value []byte) error {
	d.mu.RLock()
	if !d.running {
		d.mu.RUnlock()
		return ErrNotRunning
	}
	d.mu.RUnlock()

	client := NewRPCClient(d.node.transport.Dial)
	target := NodeID(crypto.SHA3Hash([]byte(key)))
	peers := d.node.table.FindClosest(target, Alpha)
	for _, p := range peers {
		msg := Message{
			Type:    MsgStore,
			Src:     d.localID,
			Dst:     p.Info.ID,
			Payload: encodeStore(key, value),
		}
		_, _ = client.Call(ctx, p.Info.Addrs[0], msg)
	}
	return nil
}

// RegisterNode announces this node to the peers closest to it in the routing
// table, paying a proof-of-work cost per announcement.
//
// The cost is what makes Sybil registration expensive: every announcement
// costs the registerer 2^difficulty SHA3-256 hashes, and the receiving node
// verifies it before the peer enters the table.
func (d *DHT) RegisterNode(ctx context.Context, pubKey [32]byte, name string, addrs []string, difficulty int) error {
	nonce, err := SolvePoW(registrationChallenge(pubKey, name), difficulty)
	if err != nil {
		return ErrPoWFailed
	}
	pi := PeerInfo{
		ID:        NodeIDFromPub(pubKey),
		PublicKey: pubKey,
		Name:      name,
		Addrs:     addrs,
		Score:     0.5,
		FirstSeen: time.Now(),
	}
	msg := Message{
		Type:    MsgRegisterNode,
		Src:     d.localID,
		Dst:     d.localID,
		Payload: encodeRegister(pi, nonce, difficulty),
	}
	client := NewRPCClient(d.node.transport.Dial)
	target := NodeIDFromPub(pubKey)
	peers := d.node.table.FindClosest(target, Alpha)
	for _, p := range peers {
		if len(p.Info.Addrs) == 0 {
			continue
		}
		if _, err := client.Call(ctx, p.Info.Addrs[0], msg); err != nil {
			log.Warn().Err(err).Str("peer", p.Info.ID.String()).Msg("failed to register node with peer")
		}
	}
	d.storePeer(pi)
	return nil
}

func (d *DHT) storePeer(pi PeerInfo) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if p, ok := d.node.peers[pi.ID]; ok {
		p.Info = pi
	} else {
		peer := &Peer{Info: pi}
		d.node.peers[pi.ID] = peer
		d.node.table.Add(peer)
	}
}

func (d *DHT) Stop() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.running {
		return
	}
	d.running = false
	if d.cancel != nil {
		d.cancel()
	}
}

// DHT registration proof-of-work bounds.
//
// Unlike pkg/security's memory-hard challenge, registration work is a bare
// SHA3-256 search over (public key || name || nonce). That keeps the
// anti-Sybil cost small enough to pay on every node announcement without a
// 64 MiB allocation, at the cost of being GPU-friendly. Difficulty is
// therefore capped: an unbounded value would be unsolvable, and a very high
// one would be a free denial of service for anyone trying to join.
const (
	MinPoWDifficulty = 8
	MaxPoWDifficulty = 24
)

// registrationChallenge is the pre-image a registering node must hash.
func registrationChallenge(pubKey [32]byte, name string) []byte {
	buf := make([]byte, 0, 32+len(name))
	buf = append(buf, pubKey[:]...)
	buf = append(buf, name...)
	return buf
}

// SolvePoW finds an 8-byte nonce such that
// SHA3-256(data || nonce) is numerically below 2^(256-difficulty),
// i.e. it has at least `difficulty` leading zero bits.
func SolvePoW(data []byte, difficulty int) ([]byte, error) {
	if difficulty < MinPoWDifficulty || difficulty > MaxPoWDifficulty {
		return nil, fmt.Errorf("pow difficulty %d outside supported range [%d,%d]",
			difficulty, MinPoWDifficulty, MaxPoWDifficulty)
	}
	target := big.NewInt(1)
	target.Lsh(target, uint(256-difficulty))
	var nonce uint64
	nonceBytes := make([]byte, 8)
	h := sha3.New256()
	for nonce < powNonceCeiling {
		binary.BigEndian.PutUint64(nonceBytes, nonce)
		h.Reset()
		h.Write(data)
		h.Write(nonceBytes)
		var hash [32]byte
		h.Sum(hash[:0])
		num := new(big.Int).SetBytes(hash[:])
		if num.Cmp(target) < 0 {
			return append([]byte{}, nonceBytes...), nil
		}
		nonce++
	}
	return nil, errors.New("pow nonce space exhausted")
}

// powNonceCeiling bounds the search. At the maximum difficulty the expected
// work is 2^24 hashes; this ceiling is far above that, so hitting it means the
// hash is degenerate rather than that the work was merely unlucky.
const powNonceCeiling uint64 = 1 << 32

// VerifyPoW reports whether nonce solves the challenge at the given difficulty.
func VerifyPoW(data []byte, nonce []byte, difficulty int) bool {
	if len(nonce) != 8 {
		return false
	}
	if difficulty < MinPoWDifficulty || difficulty > MaxPoWDifficulty {
		return false
	}
	target := big.NewInt(1)
	target.Lsh(target, uint(256-difficulty))
	h := sha3.New256()
	h.Write(data)
	h.Write(nonce)
	var hash [32]byte
	h.Sum(hash[:0])
	num := new(big.Int).SetBytes(hash[:])
	return num.Cmp(target) < 0
}

func encodeStore(key string, value []byte) []byte {
	buf := make([]byte, 2+len(key)+len(value))
	binary.BigEndian.PutUint16(buf[:2], uint16(len(key)))
	copy(buf[2:2+len(key)], []byte(key))
	copy(buf[2+len(key):], value)
	return buf
}

func decodeStore(data []byte) (string, []byte) {
	if len(data) < 2 {
		return "", nil
	}
	kLen := int(binary.BigEndian.Uint16(data[:2]))
	if len(data) < 2+kLen {
		return "", nil
	}
	return string(data[2 : 2+kLen]), data[2+kLen:]
}

// encodeRegister serialises a registration announcement.
//
// The layout is: public key (32) || nonce (8) || difficulty (1) || name (2 +
// len). The name is part of the payload because it is part of the
// proof-of-work pre-image; omitting it made the server unable to recompute the
// challenge and therefore unable to verify the work at all.
func encodeRegister(pi PeerInfo, nonce []byte, difficulty int) []byte {
	name := []byte(pi.Name)
	if len(name) > math.MaxUint16 {
		name = name[:math.MaxUint16]
	}
	buf := make([]byte, 32+8+1+2+len(name))
	copy(buf[:32], pi.PublicKey[:])
	copy(buf[32:40], nonce)
	buf[40] = byte(difficulty)
	binary.BigEndian.PutUint16(buf[41:43], uint16(len(name)))
	copy(buf[43:], name)
	return buf
}

// decodeRegister parses a registration announcement. It reports false when the
// payload is malformed or truncated, so a hostile peer cannot drive the
// verifier into a panic with a short buffer.
func decodeRegister(data []byte) (pubKey [32]byte, nonce []byte, difficulty int, name string, ok bool) {
	const header = 32 + 8 + 1 + 2
	if len(data) < header {
		return
	}
	copy(pubKey[:], data[:32])
	nonce = append([]byte{}, data[32:40]...)
	difficulty = int(data[40])
	nameLen := int(binary.BigEndian.Uint16(data[41:43]))
	if len(data) < header+nameLen {
		return
	}
	name = string(data[header : header+nameLen])
	ok = true
	return
}

func encodePeerList(peers []PeerInfo) []byte {
	type simplePeer struct {
		ID        [32]byte
		PublicKey [32]byte
		Name      string
		Addrs     []string
		Services  []string
		Score     float64
		Version   string
	}
	tmp := make([]simplePeer, len(peers))
	for i, p := range peers {
		tmp[i] = simplePeer{
			ID:        p.ID,
			PublicKey: p.PublicKey,
			Name:      p.Name,
			Addrs:     p.Addrs,
			Services:  p.Services,
			Score:     p.Score,
			Version:   p.Version,
		}
	}
	out, _ := marshalGob(tmp)
	return out
}

func decodePeerList(data []byte) ([]PeerInfo, error) {
	var tmp []struct {
		ID        [32]byte
		PublicKey [32]byte
		Name      string
		Addrs     []string
		Services  []string
		Score     float64
		Version   string
	}
	if err := unmarshalGob(data, &tmp); err != nil {
		return nil, err
	}
	peers := make([]PeerInfo, len(tmp))
	for i, t := range tmp {
		peers[i] = PeerInfo{
			ID:        t.ID,
			PublicKey: t.PublicKey,
			Name:      t.Name,
			Addrs:     t.Addrs,
			Services:  t.Services,
			Score:     t.Score,
			Version:   t.Version,
			LastSeen:  time.Now(),
		}
	}
	return peers, nil
}
