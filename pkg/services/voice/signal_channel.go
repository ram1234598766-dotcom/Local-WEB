package voice

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"encoding/binary"
	"encoding/hex"
	"golang.org/x/crypto/sha3"

	"github.com/ram1234598766-dotcom/Local-WEB/pkg/services/messaging"
)

// SignaledContentType is the content type carried by call signalling messages.
//
// It distinguishes a handshake payload from ordinary chat on the same channel, so
// a receiver can tell a call offer from a message that happens to be in JSON.
const SignaledContentType uint8 = 1

// maxSignaledPayload bounds a signalling payload. An offer is a small JSON
// document of track descriptions; anything larger is not a handshake and is
// rejected rather than buffered.
const maxSignaledPayload = 64 << 10

// ErrSignalingPayloadTooLarge is returned when a signal exceeds the size cap.
var ErrSignalingPayloadTooLarge = fmt.Errorf("signalling payload exceeds %d bytes", maxSignaledPayload)

// StoreChannel adapts the messaging service's store to the voice signalling
// interface.
//
// This is the piece that was missing. voice had a complete signalling
// implementation: offers, answers, ICE candidates and goodbyes, all signed, all
// waiting on a SignalingChannel. Nothing implemented that interface, so
// VoiceServer.StartCall had no argument it could be given by a real caller.
type StoreChannel struct {
	store messaging.Store

	mu        sync.Mutex
	notifiers map[string]chan struct{}
	// seq makes each message ID unique. The messaging package derives its IDs from
	// SHA3(sender, content), which means two identical offers to the same peer
	// would share an ID and the cursor in WaitForSignalFrom would skip the second
	// one. A call handshake can legitimately repeat, so uniqueness has to come
	// from here.
	seq uint64
}

// NewStoreChannel wraps a messaging store as a voice signalling channel.
func NewStoreChannel(store messaging.Store) *StoreChannel {
	return &StoreChannel{
		store:     store,
		notifiers: make(map[string]chan struct{}),
	}
}

// channelFor derives a channel ID from a signalling channel name.
//
// messaging.ChannelID is a [32]byte and the signalling API uses readable names
// like "call:<peer>", so the name is hashed. It is a deterministic mapping, which
// matters: two peers must land on the same channel for the same name without
// having agreed beforehand.
func channelFor(name string) messaging.ChannelID {
	return messaging.ChannelID(sha3.Sum256([]byte("localweb-signal:" + name)))
}

// nextMessageID returns a unique ID for one published signal.
func (c *StoreChannel) nextMessageID(sender [32]byte, content []byte) string {
	c.mu.Lock()
	c.seq++
	seq := c.seq
	c.mu.Unlock()

	h := sha3.New256()
	h.Write(sender[:])
	h.Write(content)
	var counter [16]byte
	binary.BigEndian.PutUint64(counter[:8], seq)
	binary.BigEndian.PutUint64(counter[8:], uint64(time.Now().UnixNano()))
	h.Write(counter[:])

	var out [32]byte
	h.Sum(out[:0])
	return hex.EncodeToString(out[:16])
}

// Publish appends a signalling message to a channel.
//
// The content is length-prefixed with the sender and content type so a reader can
// recover the complete envelope from the store's own copy rather than trusting
// the store's Message.Sender and Message.Type fields.
func (c *StoreChannel) Publish(ctx context.Context, channelID string, sender [32]byte, contentType uint8, content []byte) error {
	if len(content) > maxSignaledPayload {
		return ErrSignalingPayloadTooLarge
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	envelope := make([]byte, 0, 33+len(content))
	envelope = append(envelope, sender[:]...)
	envelope = append(envelope, contentType)
	envelope = append(envelope, content...)

	ch := channelFor(channelID)
	msg := messaging.Message{
		ID:        c.nextMessageID(sender, content),
		ChannelID: ch,
		Sender:    sender,
		Timestamp: time.Now().UnixNano(),
		Content:   envelope,
		Type:      contentType,
	}
	if err := c.store.Append(ch, msg); err != nil {
		return err
	}

	c.mu.Lock()
	notifiers := make([]chan struct{}, 0, len(c.notifiers))
	for _, n := range c.notifiers {
		notifiers = append(notifiers, n)
	}
	c.mu.Unlock()

	// Waking subscribers is best effort. A waiter that misses a wakeup still picks
	// the message up on its next poll, because the poll is cursor-based rather
	// than notification-based. Blocking here would let a slow reader stall a
	// publish, which is the opposite of what a signalling path needs.
	for _, n := range notifiers {
		select {
		case n <- struct{}{}:
		default:
		}
	}
	return nil
}

// Subscribe returns a channel that receives a wakeup when the signalling channel
// is published to.
//
// The channel carries no payload, so it cannot be used on its own to read a
// signal; History is the read path. Its purpose is to let a future push-based
// implementation replace the poll in WaitForSignalFrom without changing the
// interface.
func (c *StoreChannel) Subscribe(channelID string) (<-chan struct{}, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	ch, ok := c.notifiers[channelID]
	if !ok {
		// Buffered so a publish never blocks on an unread wakeup.
		ch = make(chan struct{}, 1)
		c.notifiers[channelID] = ch
	}
	return ch, nil
}

// Unsubscribe drops a subscription created by Subscribe.
func (c *StoreChannel) Unsubscribe(channelID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.notifiers, channelID)
}

// History returns signalling messages published after the cursor, unwrapping the
// sender and content type that Publish prefixed.
func (c *StoreChannel) History(channelID, afterID string, limit int) ([]Signaled, error) {
	msgs, err := c.store.History(channelFor(channelID), afterID, limit)
	if err != nil {
		return nil, err
	}

	out := make([]Signaled, 0, len(msgs))
	for _, m := range msgs {
		if len(m.Content) < 33 {
			// Too short to carry a sender and a content type. Skipping keeps one
			// malformed entry from failing every later read of the channel.
			continue
		}
		var sender [32]byte
		copy(sender[:], m.Content[:32])
		out = append(out, Signaled{
			ID:          m.ID,
			Sender:      sender,
			ContentType: m.Content[32],
			Content:     m.Content[33:],
		})
	}
	return out, nil
}

// Ping reports whether the channel can currently be used, which is what the
// daemon checks before offering to place a call.
func (c *StoreChannel) Ping() error {
	if c.store == nil {
		return errors.New("signalling store is nil")
	}
	return nil
}
