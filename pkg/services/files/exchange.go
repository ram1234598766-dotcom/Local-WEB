package files

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/mrityunjay/LocalWEB/pkg/transport"
)

// exchangeProtocol implements Bitswap-like block exchange over QUIC streams.
type exchangeProtocol struct {
	mu        sync.RWMutex
	server    *transport.Server
	store     BlockStore
	fileStore FileStore
	peerID    [32]byte
	handler   ExchangeHandler
	peers     map[[32]byte]*peerExchange
}

type peerExchange struct {
	have     []WantEntry
	lastSeen time.Time
	stream   transport.Stream
}

// ExchangeHandler processes incoming exchange messages.
type ExchangeHandler func(ctx context.Context, peerID [32]byte, msg *ExchangeMessage) error

// ExchangeMessage is a wire-level exchange message.
type ExchangeMessage struct {
	Type MessageType
	CID  cid.Cid
	Data []byte
}

// MessageType enumerates exchange message types.
type MessageType uint8

const (
	MsgWant   MessageType = 0x01
	MsgHave   MessageType = 0x02
	MsgBlock  MessageType = 0x03
	MsgCancel MessageType = 0x04
)

// NewExchangeProtocol creates a new exchange protocol handler.
func NewExchangeProtocol(server *transport.Server, store BlockStore, fileStore FileStore, peerID [32]byte) ExchangeProtocol {
	if server == nil {
		return &noopExchange{}
	}
	ep := &exchangeProtocol{
		server:    server,
		store:     store,
		fileStore: fileStore,
		peerID:    peerID,
		peers:     make(map[[32]byte]*peerExchange),
	}
	server.RegisterHandler(transport.ServiceFS, ep.handleStream)
	return ep
}

func (e *exchangeProtocol) OpenStream(ctx context.Context, peerID [32]byte) (ExchangeStream, error) {
	stream, err := e.server.OpenStream(ctx, peerID, transport.ServiceFS)
	if err != nil {
		return nil, fmt.Errorf("open stream: %w", err)
	}
	return &exchangeStream{stream: stream}, nil
}

func (e *exchangeProtocol) SendWant(ctx context.Context, peerID [32]byte, entries []WantEntry) error {
	return e.sendMessage(ctx, peerID, MsgWant, encodeWantEntries(entries))
}

func (e *exchangeProtocol) SendHave(ctx context.Context, peerID [32]byte, entries []WantEntry) error {
	return e.sendMessage(ctx, peerID, MsgHave, encodeWantEntries(entries))
}

func (e *exchangeProtocol) SendBlock(ctx context.Context, peerID [32]byte, block *Block) error {
	data, err := EncodeBlock(block)
	if err != nil {
		return fmt.Errorf("encode block: %w", err)
	}
	return e.sendMessage(ctx, peerID, MsgBlock, data)
}

func (e *exchangeProtocol) Close() error {
	return nil
}

func (e *exchangeProtocol) sendMessage(ctx context.Context, peerID [32]byte, msgType MessageType, payload []byte) error {
	stream, err := e.OpenStream(ctx, peerID)
	if err != nil {
		return err
	}
	defer stream.Close()

	buf := make([]byte, 1+4+len(payload))
	buf[0] = byte(msgType)
	binary.BigEndian.PutUint32(buf[1:5], uint32(len(payload)))
	copy(buf[5:], payload)

	_, err = stream.Write(buf)
	return err
}

func (e *exchangeProtocol) handleStream(ctx context.Context, stream transport.Stream) {
	defer stream.Close()

	header := make([]byte, 5)
	if _, err := io.ReadFull(stream, header); err != nil {
		return
	}

	msgType := MessageType(header[0])
	length := binary.BigEndian.Uint32(header[1:5])
	payload := make([]byte, length)
	if _, err := io.ReadFull(stream, payload); err != nil {
		return
	}

	peerID := extractPeerID(stream)

	switch msgType {
	case MsgWant:
		e.handleWant(ctx, peerID, payload)
	case MsgHave:
		e.handleHave(ctx, peerID, payload)
	case MsgBlock:
		e.handleBlock(ctx, peerID, payload)
	case MsgCancel:
		e.handleCancel(ctx, peerID, payload)
	}
}

func (e *exchangeProtocol) handleWant(ctx context.Context, peerID [32]byte, payload []byte) {
	entries, err := decodeWantEntries(payload)
	if err != nil {
		return
	}

	e.mu.Lock()
	peer, ok := e.peers[peerID]
	if !ok {
		peer = &peerExchange{lastSeen: time.Now()}
		e.peers[peerID] = peer
	}
	peer.lastSeen = time.Now()
	peer.have = append(peer.have, entries...)
	e.mu.Unlock()

	for _, entry := range entries {
		if entry.Type == WantWant {
			block, err := e.store.Get(ctx, entry.CID)
			if err == nil {
				go func(c cid.Cid, b *Block) {
					e.SendBlock(ctx, peerID, b)
				}(entry.CID, block)
			}
		}
	}
}

func (e *exchangeProtocol) handleHave(ctx context.Context, peerID [32]byte, payload []byte) {
	entries, err := decodeWantEntries(payload)
	if err != nil {
		return
	}

	e.mu.Lock()
	peer, ok := e.peers[peerID]
	if !ok {
		peer = &peerExchange{lastSeen: time.Now()}
		e.peers[peerID] = peer
	}
	peer.lastSeen = time.Now()
	peer.have = append(peer.have, entries...)
	e.mu.Unlock()
}

func (e *exchangeProtocol) handleBlock(ctx context.Context, peerID [32]byte, payload []byte) {
	block, err := DecodeBlock(payload)
	if err != nil {
		return
	}

	if e.handler != nil {
		go func() {
			e.handler(ctx, peerID, &ExchangeMessage{Type: MsgBlock, CID: block.CID, Data: block.Data})
		}()
	}
}

func (e *exchangeProtocol) handleCancel(ctx context.Context, peerID [32]byte, payload []byte) {
	if len(payload) < 32 {
		return
	}
	var c cid.Cid
	copy(c.Hash()[:], payload[:32])
}

func extractPeerID(stream transport.Stream) [32]byte {
	return stream.PeerID()
}

// encodeWantEntries serializes want entries.
func encodeWantEntries(entries []WantEntry) []byte {
	buf := make([]byte, 1+len(entries)*(32+1+1))
	buf[0] = byte(len(entries))
	offset := 1
	for _, e := range entries {
		copy(buf[offset:offset+32], e.CID.Hash())
		offset += 32
		buf[offset] = byte(e.Type)
		offset++
		buf[offset] = e.Priority
		offset++
	}
	return buf
}

// decodeWantEntries deserializes want entries.
func decodeWantEntries(data []byte) ([]WantEntry, error) {
	if len(data) < 1 {
		return nil, errors.New("data too short")
	}
	count := int(data[0])
	if len(data) < 1+count*34 {
		return nil, errors.New("data truncated")
	}
	entries := make([]WantEntry, count)
	offset := 1
	for i := 0; i < count; i++ {
		var c cid.Cid
		copy(c.Hash()[:], data[offset:offset+32])
		offset += 32
		entries[i] = WantEntry{
			CID:      c,
			Type:     WantType(data[offset]),
			Priority: data[offset+1],
		}
		offset += 2
	}
	return entries, nil
}

// exchangeStream wraps a transport.Stream for exchange.
type exchangeStream struct {
	stream transport.Stream
}

func (e *exchangeStream) Read(p []byte) (int, error) {
	return e.stream.Read(p)
}

func (e *exchangeStream) Write(p []byte) (int, error) {
	return e.stream.Write(p)
}

func (e *exchangeStream) Close() error {
	return e.stream.Close()
}
func (e *exchangeStream) PeerID() [32]byte {
	return e.stream.PeerID()
}

// noopExchange is a no-op exchange protocol for testing.
type noopExchange struct{}

func (n *noopExchange) OpenStream(ctx context.Context, peerID [32]byte) (ExchangeStream, error) {
	return nil, fmt.Errorf("noop exchange not implemented")
}

func (n *noopExchange) SendWant(ctx context.Context, peerID [32]byte, entries []WantEntry) error {
	return nil
}

func (n *noopExchange) SendHave(ctx context.Context, peerID [32]byte, entries []WantEntry) error {
	return nil
}

func (n *noopExchange) SendBlock(ctx context.Context, peerID [32]byte, block *Block) error {
	return nil
}

func (n *noopExchange) Close() error {
	return nil
}
