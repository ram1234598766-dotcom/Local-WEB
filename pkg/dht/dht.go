package dht

import (
	"context"
	"crypto/sha3"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"sort"
	"sync"
	"time"

	"github.com/mrityunjay/LocalWEB/pkg/crypto"
	"github.com/mrityunjay/LocalWEB/pkg/discovery"
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

func (id NodeID) PrefixLen() int {
	xor := id.Xor(id)
	for i := range xor {
		if xor[i] != 0 {
			for j := 7; j >= 0; j-- {
				if (xor[i]>>j)&1 != 0 {
					return i*8 + (7 - j)
				}
			}
			break
		}
	}
	return 0
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
		return xorDist(peers[i].Info.ID, target) < xorDist(peers[j].Info.ID, target)
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

func (rt *RoutingTable) FindClosest(target NodeID, n int) []*Peer {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	var result []*Peer
	for _, b := range rt.buckets {
		result = append(result, b.GetClosest(n, target)...)
	}
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

func (rt *RoutingTable) bucket(id NodeID) *KBucket {
	prefixLen := id.PrefixLen()
	if prefixLen > 255 {
		prefixLen = 0
	}
	return rt.buckets[prefixLen]
}

func xorDist(a, b NodeID) uint64 {
	xor := a.Xor(b)
	n := new(big.Int).SetBytes(xor[:])
	return n.Uint64()
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

func (d *DHT) Lookup(ctx context.Context, target NodeID) ([]PeerInfo, error) {
	d.mu.RLock()
	if !d.running {
		d.mu.RUnlock()
		return nil, ErrNotRunning
	}
	d.mu.RUnlock()

	client := NewRPCClient(d.node.transport.Dial)
	peers := d.node.table.FindClosest(target, Alpha)
	if len(peers) == 0 {
		return nil, ErrNoPeers
	}

	queried := make(map[NodeID]bool)
	var closest []*Peer
	hops := 0

	for hops < MaxHops {
		closest = nil
		for _, p := range peers {
			if queried[p.Info.ID] {
				continue
			}
			queried[p.Info.ID] = true
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
			if resp.Type == MsgFoundNode {
				found, err := decodePeerList(resp.Payload)
				if err == nil {
					for _, fp := range found {
						d.storePeer(fp)
						closest = append(closest, &Peer{Info: fp})
					}
				}
			}
		}
		if len(closest) == 0 {
			break
		}
		peers = append(peers, closest...)
		hops++
	}

	out := make([]PeerInfo, 0, len(peers))
	for _, p := range peers {
		out = append(out, p.Info)
	}
	return out, nil
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

func (d *DHT) RegisterNode(ctx context.Context, pubKey [32]byte, name string, addrs []string, difficulty int) error {
	nonce, err := SolvePoW(append(pubKey[:], []byte(name)...), difficulty)
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
	d.storePeer(pi)
	_ = Message{
		Type:    MsgRegisterNode,
		Src:     d.localID,
		Dst:     d.localID,
		Payload: encodeRegister(pi, nonce, difficulty),
	}
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

func SolvePoW(data []byte, difficulty int) ([]byte, error) {
	if difficulty < 1 || difficulty > 255 {
		return nil, errors.New("invalid difficulty")
	}
	target := big.NewInt(1)
	target.Lsh(target, uint(256-difficulty))
	var nonce uint64
	nonceBytes := make([]byte, 8)
	h := sha3.New256()
	for {
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
		if nonce == 0 {
			return nil, errors.New("pow overflow")
		}
	}
}

func VerifyPoW(data []byte, nonce []byte, difficulty int) bool {
	if len(nonce) != 8 || difficulty < 1 || difficulty > 255 {
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

func encodeRegister(pi PeerInfo, nonce []byte, difficulty int) []byte {
	buf := make([]byte, 32+8+1)
	copy(buf[:32], pi.PublicKey[:])
	copy(buf[32:40], nonce)
	buf[40] = byte(difficulty)
	return buf
}

func decodeRegister(data []byte) (pubKey [32]byte, nonce []byte, difficulty int) {
	if len(data) < 41 {
		return
	}
	copy(pubKey[:], data[:32])
	nonce = append([]byte{}, data[32:40]...)
	difficulty = int(data[40])
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
