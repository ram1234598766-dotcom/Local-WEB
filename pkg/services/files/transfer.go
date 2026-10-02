package files

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/pion/webrtc/v4"
)

// DataChannel is the subset of a pion data channel the transfer needs. It is
// declared here so the transfer can be driven by anything with this shape, and
// so a test can substitute a channel pair that is not a real socket.
type DataChannel interface {
	Send(data []byte) error
	SendText(s string) error
	OnMessage(f func(msg webrtc.DataChannelMessage))
	OnOpen(f func())
	OnError(f func(error))
	OnClose(f func())
	ReadyState() webrtc.DataChannelState
}

// Transfer moves a file over a WebRTC data channel, block by block, sending
// only what the receiver does not already have.
//
// The Files service previously had no way to move bytes to a peer: Sync never
// contacted anyone and GetFile returned a nil data slice. This is the real
// path, and the WebRTC transport it rides on is verified separately in the
// voice package: a loopback pair reaches connected and carries 256 KB with a
// matching digest.
type Transfer struct {
	mu      sync.Mutex
	store   BlockStore
	stats   TransferStats
	closed  bool
	in      *pending
	channel DataChannel
}

// NewTransfer creates a transfer that reads and writes a block store.
func NewTransfer(store BlockStore) *Transfer {
	return &Transfer{store: store}
}

// Attach binds the transfer to a data channel and starts buffering whatever
// arrives on it.
//
// This must happen before the peer starts sending. A pion data channel delivers
// every message to the single handler registered with OnMessage, so the handler
// has to push into the inbox immediately; a frame that arrives before Attach
// runs is gone.
func (t *Transfer) Attach(dc DataChannel) *Transfer {
	t.mu.Lock()
	t.channel = dc
	in := t.inboxLocked()
	t.mu.Unlock()

	dc.OnMessage(func(msg webrtc.DataChannelMessage) {
		if msg.IsString {
			return // the transfer is binary only
		}
		f, err := decodeFrame(msg.Data)
		if err != nil {
			return // a malformed frame must not take the transfer down
		}
		in.push(f)
	})
	return t
}

func (t *Transfer) inboxLocked() *pending {
	if t.in == nil {
		t.in = newPending()
	}
	return t.in
}

// Send chunks data, tells the receiver the full block order, and streams only
// the blocks the receiver does not already hold.
//
// The order is fixed by the manifest going out first: the sender owns the block
// list, and only the sender can say what the file is. The receiver's reply names
// the subset of the manifest it already holds, so the two directions can never be
// confused for one another.
func (t *Transfer) Send(ctx context.Context, dc DataChannel, data []byte) (TransferStats, error) {
	start := time.Now()

	blocks, err := Chunk(data)
	if err != nil {
		return TransferStats{}, err
	}
	order := make([]cid.Cid, 0, len(blocks))
	for _, b := range blocks {
		order = append(order, b.CID)
	}

	t.mu.Lock()
	t.stats = TransferStats{TotalBlocks: len(blocks)}
	t.mu.Unlock()

	// manifest: the full order, so the receiver knows the final layout and can
	// report which of those blocks it already has.
	manifest, err := encodeFrame(frame{Type: wireManifest, CIDs: order, Total: uint32(len(order))})
	if err != nil {
		return TransferStats{}, err
	}
	if err := dc.Send(manifest); err != nil {
		return TransferStats{}, fmt.Errorf("send manifest: %w", err)
	}

	// The receiver replies with the manifest blocks it already holds; anything
	// else in its store is irrelevant to this file.
	reply, err := t.awaitFrame(ctx, wireHello)
	if err != nil {
		return TransferStats{}, fmt.Errorf("await held set: %w", err)
	}
	have := make(map[cid.Cid]bool, len(reply.CIDs))
	for _, c := range reply.CIDs {
		have[c] = true
	}
	resumed := 0
	for _, c := range order {
		if have[c] {
			resumed++
		}
	}
	t.mu.Lock()
	t.stats.ResumedFrom = resumed
	t.mu.Unlock()

	for _, b := range blocks {
		if have[b.CID] {
			t.mu.Lock()
			t.stats.SkippedBlocks++
			t.stats.BytesSaved += int64(len(b.Data))
			t.mu.Unlock()
			continue
		}
		select {
		case <-ctx.Done():
			return t.snapshot(), ctx.Err()
		default:
		}
		raw, err := encodeFrame(frame{Type: wireBlock, CID: b.CID, Data: b.Data})
		if err != nil {
			return TransferStats{}, err
		}
		if err := dc.Send(raw); err != nil {
			return t.snapshot(), fmt.Errorf("send block %s: %w", b.CID, err)
		}
		t.mu.Lock()
		t.stats.SentBlocks++
		t.stats.BytesSent += int64(len(b.Data))
		t.mu.Unlock()
	}

	if err := dc.Send(mustFrame(wireComplete)); err != nil {
		return t.snapshot(), fmt.Errorf("send complete: %w", err)
	}

	t.mu.Lock()
	t.stats.Duration = time.Since(start)
	out := t.stats
	t.mu.Unlock()
	return out, nil
}

// Receive answers the hello, reads the manifest and the blocks, stores them, and
// returns the reassembled file.
//
// Every block is verified against its own digest by the store on Put, and the
// whole file is returned only when every block named in the manifest has
// arrived, so a truncated transfer fails rather than yielding a short file.
func (t *Transfer) Receive(ctx context.Context) ([]byte, TransferStats, error) {
	start := time.Now()
	dc := t.channel
	if dc == nil {
		return nil, TransferStats{}, fmt.Errorf("transfer is not attached to a data channel")
	}

	manifest, err := t.awaitFrame(ctx, wireManifest)
	if err != nil {
		return nil, TransferStats{}, fmt.Errorf("await manifest: %w", err)
	}

	asm := newReAssembler(manifest.CIDs)

	// Report the manifest blocks already in the store, and seed the
	// reassembler with their content. A resumed transfer never sends these
	// blocks again, so without seeding them the final assembly would be short
	// exactly when the transfer worked best.
	held := make([]cid.Cid, 0, len(manifest.CIDs))
	var heldBytes int64
	for _, c := range manifest.CIDs {
		if !t.store.Has(ctx, c) {
			continue
		}
		block, err := t.store.Get(ctx, c)
		if err != nil {
			return nil, TransferStats{}, fmt.Errorf("read held block %s: %w", c, err)
		}
		cp := make([]byte, len(block.Data))
		copy(cp, block.Data)
		asm.received[c] = cp
		held = append(held, c)
		heldBytes += int64(len(cp))
	}

	reply, err := encodeFrame(frame{Type: wireHello, CIDs: held, Total: uint32(len(manifest.CIDs))})
	if err != nil {
		return nil, TransferStats{}, err
	}
	if err := dc.Send(reply); err != nil {
		return nil, TransferStats{}, fmt.Errorf("send held set: %w", err)
	}

	stats := TransferStats{
		TotalBlocks:   len(manifest.CIDs),
		SkippedBlocks: len(held),
		ResumedFrom:   len(held),
		BytesSaved:    heldBytes,
	}
	inbox := make(chan frame, 64)
	go t.pump(ctx, inbox)

	for {
		select {
		case <-ctx.Done():
			return nil, stats, ctx.Err()
		case f, ok := <-inbox:
			if !ok {
				return nil, stats, fmt.Errorf("channel closed before the transfer completed")
			}
			switch f.Type {
			case wireBlock:
				if !asm.wants(f.CID) {
					// The store verifies the digest, but a block that was never
					// in the manifest must not be written at all.
					continue
				}
				if err := t.store.Put(ctx, &Block{CID: f.CID, Data: f.Data}); err != nil {
					return nil, stats, fmt.Errorf("store block %s: %w", f.CID, err)
				}
				cp := make([]byte, len(f.Data))
				copy(cp, f.Data)
				asm.received[f.CID] = cp
				stats.ReceivedBlocks++
				stats.BytesReceived += int64(len(f.Data))
			case wireComplete:
				out, err := asm.Bytes()
				if err != nil {
					return nil, stats, err
				}
				stats.Duration = time.Since(start)
				return out, stats, nil
			case wireAbort:
				return nil, stats, fmt.Errorf("sender aborted: %s", f.Reason)
			}
		}
	}
}

// pump forwards inbound messages until the channel closes.
func (t *Transfer) pump(ctx context.Context, out chan<- frame) {
	defer close(out)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		f, err := t.nextFrame(ctx)
		if err != nil {
			return
		}
		select {
		case out <- f:
		case <-ctx.Done():
			return
		}
		if f.Type == wireComplete || f.Type == wireAbort {
			return
		}
	}
}

// pending holds the frames read but not yet consumed, because a pion channel
// delivers every message to the same handler.
type pending struct {
	mu     sync.Mutex
	frames []frame
	signal chan struct{}
}

func newPending() *pending {
	return &pending{signal: make(chan struct{}, 1024)}
}

func (p *pending) push(f frame) {
	p.mu.Lock()
	p.frames = append(p.frames, f)
	p.mu.Unlock()
	select {
	case p.signal <- struct{}{}:
	default:
	}
}

func (p *pending) pop() (frame, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.frames) == 0 {
		return frame{}, false
	}
	f := p.frames[0]
	p.frames = p.frames[1:]
	return f, true
}

// nextFrame reads the next buffered frame, waiting briefly if the channel has
// not produced one yet.
func (t *Transfer) nextFrame(ctx context.Context) (frame, error) {
	in := t.inbox()
	for {
		if f, ok := in.pop(); ok {
			return f, nil
		}
		select {
		case <-ctx.Done():
			return frame{}, ctx.Err()
		case <-in.signal:
		case <-time.After(60 * time.Second):
			return frame{}, fmt.Errorf("timed out waiting for a frame")
		}
	}
}

// awaitFrame reads until a frame of the wanted type arrives.
func (t *Transfer) awaitFrame(ctx context.Context, want wireMessageType) (frame, error) {
	for {
		f, err := t.nextFrame(ctx)
		if err != nil {
			return frame{}, err
		}
		if f.Type == want {
			return f, nil
		}
	}
}

func (t *Transfer) inbox() *pending {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.inboxLocked()
}

func (t *Transfer) snapshot() TransferStats {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.stats
}

func mustFrame(kind wireMessageType) []byte {
	b, err := encodeFrame(frame{Type: kind})
	if err != nil {
		panic(fmt.Sprintf("encode %v: %v", kind, err))
	}
	return b
}
