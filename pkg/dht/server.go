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
)

type Server struct {
	node   *Node
	ln     net.Listener
	mu     sync.Mutex
	closed bool
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
		out := make([]PeerInfo, len(peers))
		for i, p := range peers {
			out[i] = p.Info
		}
		return Message{Type: MsgFoundNode, Src: n.id, Dst: msg.Src, Payload: encodePeerList(out)}
	case MsgStore:
		key, value := decodeStore(msg.Payload)
		if key != "" {
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
	default:
		return Message{Type: MsgPong, Src: n.id, Dst: msg.Src}
	}
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
