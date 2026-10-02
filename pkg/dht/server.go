package dht

import (
	"bufio"
	"bytes"
	"crypto/sha3"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"sync"
	"time"
)

type Server struct {
	node   *Node
	ln     net.Listener
	mu     sync.Mutex
	closed bool
	// advertised is the address published to peers when it differs from the bound
	// one. Guarded by mu.
	advertised string
}

func NewServer(node *Node) *Server {
	return &Server{node: node}
}

func (s *Server) Start(addr string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("server closed")
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	s.ln = ln
	// Tell the node where it is reachable, so it can hand that address out.
	s.node.setListenAddr(ln.Addr().String())
	go s.acceptLoop()
	return nil
}

func (s *Server) acceptLoop() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.handleConn(conn)
	}
}

func (s *Server) handleConn(conn net.Conn) {
	defer conn.Close()
	var hdr [65]byte
	if _, err := io.ReadFull(conn, hdr[:]); err != nil {
		return
	}
	msgType := MessageType(hdr[0])
	var src, dst NodeID
	copy(src[:], hdr[1:33])
	copy(dst[:], hdr[33:65])

	var lenBuf [4]byte
	if _, err := io.ReadFull(conn, lenBuf[:]); err != nil {
		return
	}
	plLen := binary.BigEndian.Uint32(lenBuf[:])
	if plLen > 1<<20 {
		return
	}
	pl := make([]byte, plLen)
	if _, err := io.ReadFull(conn, pl); err != nil {
		return
	}

	resp := s.node.handleMessage(Message{
		Type:    msgType,
		Src:     src,
		Dst:     dst,
		Payload: pl,
	})

	var out [1 + 32 + 32]byte
	out[0] = byte(resp.Type)
	copy(out[1:33], resp.Src[:])
	copy(out[33:65], resp.Dst[:])
	var respLen [4]byte
	binary.BigEndian.PutUint32(respLen[:], uint32(len(resp.Payload)))
	conn.Write(out[:])
	conn.Write(respLen[:])
	conn.Write(resp.Payload)
}

// Addr reports the address the server is listening on.
//
// Start accepts a port of 0, so without this a node cannot tell a peer where to
// reach it and every bootstrap or registration has to guess.
func (s *Server) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ln == nil {
		return ""
	}
	return s.ln.Addr().String()
}

// SetAdvertisedAddr sets the address peers should be told to dial.
//
// This is separate from Addr because binding and being reachable are different
// questions. A node commonly binds a wildcard ("0.0.0.0:9094" or ":0"), and that
// address is useless to publish: a remote peer cannot dial 0.0.0.0. Without this
// the only way to be reachable is to bind a specific address, which rules out
// listening on every interface.
//
// An empty value is allowed and means "publish the bound address", which is the
// correct behaviour only when the bound address is one a peer can actually dial.
func (s *Server) SetAdvertisedAddr(addr string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.advertised = addr
}

// AdvertisedAddr returns the address to publish to peers: the override if one was
// set, otherwise the bound address.
func (s *Server) AdvertisedAddr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.advertised != "" {
		return s.advertised
	}
	if s.ln == nil {
		return ""
	}
	return s.ln.Addr().String()
}

func (s *Server) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	if s.ln != nil {
		return s.ln.Close()
	}
	return nil
}

func (n *Node) handleMessage(msg Message) Message {
	n.mu.Lock()
	defer n.mu.Unlock()
	switch msg.Type {
	case MsgPing:
		return Message{Type: MsgPong, Src: n.id, Dst: msg.Src}
	case MsgFindNode:
		target := NodeID{}
		copy(target[:], msg.Payload)
		peers := n.table.FindClosest(target, KBucketSize)
		// Include ourselves. The response used to list only peers we already
		// knew, so a node bootstrapping from an empty seed learned nothing at
		// all - not even the seed it had just dialled. Bootstrap was therefore
		// a no-op on a fresh network and a node could never join one.
		out := make([]PeerInfo, 0, len(peers)+1)
		out = append(out, n.selfInfo())
		for _, p := range peers {
			// Detach before handing the value out: the table's peers are
			// replaced when their details change, and the caller must not share
			// memory with them.
			out = append(out, copyPeerInfo(p.Info))
		}
		return Message{Type: MsgFoundNode, Src: n.id, Dst: msg.Src, Payload: encodePeerList(out)}
	case MsgStore:
		key, value := decodeStore(msg.Payload)
		if key != "" {
			// n.store is already protected: handleMessage holds n.mu for its
			// whole body, so taking it again here would self-deadlock.
			if n.store == nil {
				n.store = make(map[string][]byte)
			}
			n.store[key] = value
		}
		return Message{Type: MsgPong, Src: n.id, Dst: msg.Src}
	case MsgFindValue:
		key, _ := decodeStore(msg.Payload)
		val, ok := n.store[key]
		if ok {
			return Message{Type: MsgFoundValue, Src: n.id, Dst: msg.Src, Payload: encodeStore(key, val)}
		}
		return Message{Type: MsgFoundNode, Src: n.id, Dst: msg.Src, Payload: encodePeerList(nil)}
	case MsgRegisterNode:
		// This is the anti-Sybil gate. Previously the message type was not
		// handled at all and fell through to default, so a node could solve
		// the challenge and be ignored: the proof of work was decorative.
		pubKey, nonce, difficulty, name, addrs, ok := decodeRegister(msg.Payload)
		if !ok {
			return Message{Type: MsgPong, Src: n.id, Dst: msg.Src}
		}
		// The challenge binds the advertised addresses, so an address cannot be
		// swapped in after the fact.
		if !VerifyPoW(registrationChallenge(pubKey, name, addrs), nonce, difficulty) {
			// Do not admit the peer. A failed challenge is the same shape as
			// a pong so the responder does not confirm the endpoint is a DHT
			// registration oracle.
			return Message{Type: MsgPong, Src: n.id, Dst: msg.Src}
		}
		// The announced identity must match the peer that solved the work,
		// otherwise anyone could reuse another node's published nonce.
		if msg.Src != NodeIDFromPub(pubKey) {
			return Message{Type: MsgPong, Src: n.id, Dst: msg.Src}
		}
		pi := PeerInfo{
			ID:        msg.Src,
			PublicKey: pubKey,
			Name:      name,
			Addrs:     addrs,
			Score:     0.5,
			FirstSeen: time.Now(),
		}
		if n.peers == nil {
			n.peers = make(map[NodeID]*Peer)
		}
		if existing, ok := n.peers[msg.Src]; ok {
			existing.Info = pi
		} else {
			peer := &Peer{Info: pi}
			n.peers[msg.Src] = peer
			n.table.Add(peer)
		}
		return Message{Type: MsgPong, Src: n.id, Dst: msg.Src}
	default:
		return Message{Type: MsgPong, Src: n.id, Dst: msg.Src}
	}
}

// PruneStale drops peers that have not been seen within ttl.
//
// Without this, a bucket that fills up with departed nodes rejects every new
// peer (KBucket.Add returns false once the bucket is full) and the routing
// table silently stops learning about the network.
func (rt *RoutingTable) PruneStale(ttl time.Duration) int {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	cutoff := time.Now().Add(-ttl)
	removed := 0
	for _, b := range rt.buckets {
		for _, p := range b.All() {
			if p.Info.LastSeen.Before(cutoff) {
				b.Remove(p.Info.ID)
				removed++
			}
		}
	}
	return removed
}

type WireProtocol struct {
	node *Node
}

func NewWireProtocol(node *Node) *WireProtocol {
	return &WireProtocol{node: node}
}

func (w *WireProtocol) Handle(conn net.Conn) {
	defer conn.Close()
	reader := bufio.NewReader(conn)
	var hdr [65]byte
	if _, err := io.ReadFull(reader, hdr[:]); err != nil {
		return
	}
	msgType := MessageType(hdr[0])
	var src, dst NodeID
	copy(src[:], hdr[1:33])
	copy(dst[:], hdr[33:65])

	var lenBuf [4]byte
	if _, err := io.ReadFull(reader, lenBuf[:]); err != nil {
		return
	}
	plLen := binary.BigEndian.Uint32(lenBuf[:])
	if plLen > 1<<20 {
		return
	}
	pl := make([]byte, plLen)
	if _, err := io.ReadFull(reader, pl); err != nil {
		return
	}

	msg := Message{Type: msgType, Src: src, Dst: dst, Payload: pl}
	resp := w.node.handleMessage(msg)

	var out [1 + 32 + 32]byte
	out[0] = byte(resp.Type)
	copy(out[1:33], resp.Src[:])
	copy(out[33:65], resp.Dst[:])
	var respLen [4]byte
	binary.BigEndian.PutUint32(respLen[:], uint32(len(resp.Payload)))
	conn.Write(out[:])
	conn.Write(respLen[:])
	conn.Write(resp.Payload)
}

type MerkleProof struct {
	Root   [32]byte
	Branch [][32]byte
	Leaf   []byte
}

func ComputeMerkleRoot(data []byte) [32]byte {
	h := sha3.New256()
	h.Write(data)
	var root [32]byte
	h.Sum(root[:0])
	return root
}

func ComputeMerkleRoots(entries []string) [][32]byte {
	roots := make([][32]byte, len(entries))
	for i, e := range entries {
		roots[i] = ComputeMerkleRoot([]byte(e))
	}
	return roots
}

func VerifyMerkleProof(root [32]byte, proof MerkleProof) bool {
	current := ComputeMerkleRoot(proof.Leaf)
	for _, sibling := range proof.Branch {
		h := sha3.New256()
		if bytes.Compare(current[:], sibling[:]) < 0 {
			h.Write(current[:])
			h.Write(sibling[:])
		} else {
			h.Write(sibling[:])
			h.Write(current[:])
		}
		var next [32]byte
		h.Sum(next[:0])
		current = next
	}
	return current == root
}
