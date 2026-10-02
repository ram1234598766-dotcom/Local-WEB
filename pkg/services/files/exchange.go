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
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/transport"
	"github.com/rs/zerolog/log"
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
	// have is the peer's latest advertisement, want is what it last asked us
	// for. Both replace rather than accumulate.
	have     []WantEntry
	want     []WantEntry
	lastSeen time.Time
	stream   transport.Stream

	// counters are diagnostics: a reply that cannot be sent used to be
	// indistinguishable from a peer holding nothing.
	blocksServed   int
	blocksReceived int
	lastErr        error
}

// ExchangeHandler processes incoming exchange messages.
type ExchangeHandler func(ctx context.Context, peerID [32]byte, msg *ExchangeMessage) error

// ExchangeMessage is a wire-level exchange message.
type ExchangeMessage struct {
	Type MessageType
	CID  cid.Cid
	// CIDs carries a whole list, which is what a have advertisement is.
	CIDs []cid.Cid
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

// SetHandler installs the callback for blocks arriving from peers.
//
// Without this, handleBlock decoded a block and dropped it on the floor: the
// field was private and nothing could ever assign it, so a peer that served a
// block perfectly well still left the requester waiting forever.
func (e *exchangeProtocol) SetHandler(h ExchangeHandler) {
	e.mu.Lock()
	e.handler = h
	e.mu.Unlock()
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
	// The length comes from the peer, so it is untrusted input. Without this
	// cap a peer could ask for a 4 GiB allocation with a five-byte message.
	if length > maxExchangePayload {
		return
	}
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

// exchangeSendTimeout bounds a single outbound block send. A peer that stops
// reading must not pin a goroutine and a stream for ever.
const exchangeSendTimeout = 30 * time.Second

func (e *exchangeProtocol) handleWant(ctx context.Context, peerID [32]byte, payload []byte) {
	entries, err := decodeWantEntries(payload)
	if err != nil {
		return
	}

	wanted := make([]WantEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.Type != WantWant {
			continue
		}
		wanted = append(wanted, entry)
	}

	e.mu.Lock()
	peer, ok := e.peers[peerID]
	if !ok {
		peer = &peerExchange{lastSeen: time.Now()}
		e.peers[peerID] = peer
	}
	peer.lastSeen = time.Now()
	// Replace rather than append: a want list is a statement of what is wanted
	// now, and appending on every message grew this without bound.
	peer.want = wanted
	e.mu.Unlock()

	for _, entry := range wanted {
		block, err := e.store.Get(ctx, entry.CID)
		if err != nil {
			// Not an error worth reporting per block: we simply do not have it.
			continue
		}
		go func(c cid.Cid, b *Block) {
			sendCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), exchangeSendTimeout)
			defer cancel()
			// The error used to be discarded, which is why a reply that could
			// never be sent looked exactly like a peer that had nothing to give.
			err := e.SendBlock(sendCtx, peerID, b)
			e.mu.Lock()
			if cur := e.peers[peerID]; cur != nil {
				cur.lastErr = err
				if err == nil {
					cur.blocksServed++
				}
			}
			e.mu.Unlock()
			if err != nil {
				log.Warn().Err(err).
					Str("cid", c.String()).
					Str("peer", fmt.Sprintf("%x", peerID[:8])).
					Msg("could not send requested block")
			}
		}(entry.CID, block)
	}
}

func (e *exchangeProtocol) handleHave(ctx context.Context, peerID [32]byte, payload []byte) {
	entries, err := decodeWantEntries(payload)
	if err != nil {
		return
	}

	have := make([]WantEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.Type == WantHave {
			have = append(have, entry)
		}
	}

	cids := make([]cid.Cid, 0, len(have))
	for _, entry := range have {
		cids = append(cids, entry.CID)
	}

	e.mu.Lock()
	peer, ok := e.peers[peerID]
	if !ok {
		peer = &peerExchange{lastSeen: time.Now()}
		e.peers[peerID] = peer
	}
	peer.lastSeen = time.Now()
	// A have advertisement replaces the previous one for the same reason a
	// want list does: it describes now, and the store only grows.
	peer.have = have
	handler := e.handler
	e.mu.Unlock()

	// Pass it on. Without this the requester has no way to learn what a peer
	// holds, so it has nothing to ask for and sync never starts.
	if handler != nil {
		if err := handler(ctx, peerID, &ExchangeMessage{Type: MsgHave, CIDs: cids}); err != nil {
			log.Warn().Err(err).Str("peer", fmt.Sprintf("%x", peerID[:8])).Msg("have handler rejected an advertisement")
		}
	}
}

func (e *exchangeProtocol) handleBlock(ctx context.Context, peerID [32]byte, payload []byte) {
	block, err := DecodeBlock(payload)
	if err != nil {
		log.Warn().Err(err).Str("peer", fmt.Sprintf("%x", peerID[:8])).Msg("incoming block did not decode")
		return
	}

	e.mu.Lock()
	handler := e.handler
	if peer := e.peers[peerID]; peer != nil {
		peer.blocksReceived++
	}
	e.mu.Unlock()

	if handler == nil {
		return
	}
	if err := handler(ctx, peerID, &ExchangeMessage{Type: MsgBlock, CID: block.CID, Data: block.Data}); err != nil {
		log.Warn().Err(err).Str("cid", block.CID.String()).Msg("block handler rejected an incoming block")
	}
}

// handleCancel drops a cancelled want.
//
// The old body copied a peer's bytes into c.Hash(), which on a zero-value cid.Cid
// is a slice of a zero-length string: it panicked on every cancel message.
func (e *exchangeProtocol) handleCancel(ctx context.Context, peerID [32]byte, payload []byte) {
	entries, err := decodeWantEntries(payload)
	if err != nil {
		return
	}
	cancelled := make(map[string]bool, len(entries))
	for _, entry := range entries {
		cancelled[entry.CID.String()] = true
	}

	e.mu.Lock()
	peer, ok := e.peers[peerID]
	if !ok {
		e.mu.Unlock()
		return
	}
	kept := peer.want[:0]
	for _, entry := range peer.want {
		if !cancelled[entry.CID.String()] {
			kept = append(kept, entry)
		}
	}
	peer.want = kept
	e.mu.Unlock()
}

func extractPeerID(stream transport.Stream) [32]byte {
	return stream.PeerID()
}

// maxWantEntries bounds a single want/have message, and maxExchangePayload
// bounds a whole message. Both are attacker-controlled: a peer can send any
// length prefix it likes, and without a cap the receiver allocates whatever was
// asked for.
const (
	maxWantEntries     = 4096
	maxExchangePayload = 8 << 20
	maxCIDLen          = 64
)

// encodeWantEntries serialises want entries.
//
// A CIDv1 is a multibase string (for raw/sha2-256, "bafkrei..."), not a 32-byte
// hash, so it cannot be copied into a fixed 32-byte slot: a decode of the old
// fixed-width layout produced the single-byte CID "b" for every entry, which
// meant a peer answered wants for a block nobody asked for. Each entry therefore
// carries its own length-prefixed CID.
func encodeWantEntries(entries []WantEntry) []byte {
	buf := make([]byte, 0, 1+len(entries)*(2+maxCIDLen+2))
	buf = append(buf, byte(len(entries)))

	var scratch [2]byte
	for _, e := range entries {
		raw := e.CID.Bytes()
		if len(raw) > maxCIDLen {
			// A CID longer than any codec this project produces; skip it rather
			// than emit a frame the peer cannot parse.
			continue
		}
		binary.BigEndian.PutUint16(scratch[:], uint16(len(raw)))
		buf = append(buf, scratch[:]...)
		buf = append(buf, raw...)
		buf = append(buf, byte(e.Type), e.Priority)
	}
	return buf
}

// decodeWantEntries deserialises want entries.
func decodeWantEntries(data []byte) ([]WantEntry, error) {
	if len(data) < 1 {
		return nil, errors.New("data too short")
	}
	count := int(data[0])
	if count > maxWantEntries {
		return nil, fmt.Errorf("want list of %d entries exceeds the %d limit", count, maxWantEntries)
	}

	entries := make([]WantEntry, 0, count)
	offset := 1
	for i := 0; i < count; i++ {
		if offset+2 > len(data) {
			return nil, errors.New("data truncated at cid length")
		}
		n := int(binary.BigEndian.Uint16(data[offset : offset+2]))
		offset += 2
		if n > maxCIDLen {
			return nil, fmt.Errorf("entry %d claims a %d byte cid, over the %d limit", i, n, maxCIDLen)
		}
		if offset+n+2 > len(data) {
			return nil, errors.New("data truncated at cid body")
		}
		c, err := cid.Cast(data[offset : offset+n])
		if err != nil {
			return nil, fmt.Errorf("entry %d has a malformed cid: %w", i, err)
		}
		entries = append(entries, WantEntry{
			CID:      c,
			Type:     WantType(data[offset+n]),
			Priority: data[offset+n+1],
		})
		offset += n + 2
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

func (n *noopExchange) SetHandler(h ExchangeHandler) {}

func (n *noopExchange) Close() error {
	return nil
}
