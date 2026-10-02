package voice

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ram1234598766-dotcom/Local-WEB/pkg/services/messaging"
)

func newTestChannel() (*StoreChannel, messaging.Store) {
	store := messaging.NewMemoryStore()
	return NewStoreChannel(store), store
}

// TestSignallingRoundTrip is the test for call signalling. The handshake code
// (offer, answer, ICE, bye, all signed) was complete, but WaitForSignal was an
// endless loop that ignored its arguments and could only return ctx.Err(), and
// nothing implemented the SignalingChannel interface it needed. So a call could
// never be answered.
func TestSignallingRoundTrip(t *testing.T) {
	ch, _ := newTestChannel()
	caller := [32]byte{1}
	callee := [32]byte{2}

	sig := NewMessagingSignaling("call:"+hexID(callee), ch)

	// The caller offers and the callee waits. These run concurrently, as they
	// would in a real call: the waiter is already blocked when the offer lands.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	type result struct {
		msg *SignalMessage
		err error
	}
	got := make(chan result, 1)
	go func() {
		m, err := sig.WaitForSignal(ctx, caller, SignalTypeOffer)
		got <- result{m, err}
	}()

	// Give the waiter time to take its cursor before the offer is published.
	// Otherwise the offer could land first and be skipped as pre-existing, which
	// would make the test pass for the wrong reason on a slow machine.
	time.Sleep(100 * time.Millisecond)

	offer := SignalOffer(CallID{9}, caller, []TrackInfo{{
		ID: "audio-1", Kind: TrackKindAudio, Direction: TrackDirectionSendRecv, Codec: CodecOpus,
	}})
	require.NoError(t, sig.SendSignal(ctx, caller, offer))

	select {
	case r := <-got:
		require.NoError(t, r.err, "WaitForSignal must return the offer, not time out")
		require.NotNil(t, r.msg)
		require.Equal(t, SignalTypeOffer, r.msg.Type)
		require.Equal(t, CallID{9}, r.msg.CallID)
		require.Equal(t, caller, r.msg.Sender)

		var tracks []TrackInfo
		require.NoError(t, json.Unmarshal(r.msg.Payload, &tracks))
		require.Len(t, tracks, 1)
		require.Equal(t, "audio-1", tracks[0].ID)
		require.Equal(t, CodecOpus, tracks[0].Codec)

	case <-ctx.Done():
		t.Fatal("WaitForSignal never returned the offer")
	}
}

// TestWaitForSignalAnswersTheCaller'sQuestion is the shape of a real handshake:
// the callee waits for an answer from the caller and ignores the offer it did not
// ask about.
func TestWaitForSignalAnswersTheCallersQuestion(t *testing.T) {
	ch, _ := newTestChannel()
	peer := [32]byte{7}
	sig := NewMessagingSignaling("call:peer", ch)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	got := make(chan *SignalMessage, 1)
	go func() {
		m, err := sig.WaitForSignal(ctx, peer, SignalTypeAnswer)
		if err == nil {
			got <- m
		}
	}()

	time.Sleep(100 * time.Millisecond)

	// Wrong type and wrong sender must both be ignored rather than returned.
	require.NoError(t, sig.SendSignal(ctx, peer, SignalOffer(CallID{1}, peer, nil)))
	require.NoError(t, sig.SendSignal(ctx, [32]byte{99}, SignalAnswer(CallID{1}, [32]byte{99}, nil)))

	// The signal actually being waited for.
	require.NoError(t, sig.SendSignal(ctx, peer, SignalAnswer(CallID{2}, peer, nil)))

	select {
	case m := <-got:
		require.Equal(t, SignalTypeAnswer, m.Type)
		require.Equal(t, CallID{2}, m.CallID, "it must return the answer, not the earlier offer")
	case <-ctx.Done():
		t.Fatal("WaitForSignal never returned the answer")
	}
}

// TestWaitForSignalResumesFromCursor checks a reconnecting receiver neither
// re-answers a signal it has already seen nor misses one it has not.
func TestWaitForSignalResumesFromCursor(t *testing.T) {
	ch, _ := newTestChannel()
	peer := [32]byte{3}
	sig := NewMessagingSignaling("call:cursor", ch)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Publish an ICE candidate and note where the cursor lands.
	require.NoError(t, sig.SendSignal(ctx, peer, SignalICE(CallID{5}, peer,
		ICECandidate{ID: "a", Address: "10.0.0.1", Port: 4000, Protocol: "udp"})))

	batch, err := ch.History("call:cursor", "", 64)
	require.NoError(t, err)
	require.NotEmpty(t, batch)
	cursor := batch[len(batch)-1].ID

	// Resuming from that cursor must not return the already-seen candidate.
	lateCtx, lateCancel := context.WithTimeout(ctx, 400*time.Millisecond)
	defer lateCancel()
	_, _, err = sig.WaitForSignalFrom(lateCtx, peer, SignalTypeICECandidate, cursor)
	require.Error(t, err, "a signal before the cursor must not be returned again")

	// A signal published after the cursor must be found.
	go func() {
		time.Sleep(100 * time.Millisecond)
		_ = sig.SendSignal(ctx, peer, SignalICE(CallID{6}, peer, ICECandidate{ID: "b", Address: "10.0.0.2", Port: 4001, Protocol: "udp"}))
	}()

	msg, _, err := sig.WaitForSignalFrom(ctx, peer, SignalTypeICECandidate, cursor)
	require.NoError(t, err)
	require.Equal(t, CallID{6}, msg.CallID)
}

// TestWaitForSignalStopsOnContext is the case that used to be the only outcome:
// without a matching signal, it must return the context error rather than spin.
func TestWaitForSignalStopsOnContext(t *testing.T) {
	ch, _ := newTestChannel()
	sig := NewMessagingSignaling("call:none", ch)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := sig.WaitForSignal(ctx, [32]byte{1}, SignalTypeOffer)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Less(t, time.Since(start), 3*time.Second, "it must stop when the context ends, not hang")
}

// TestStoreChannelRejectsOversizedPayload keeps a hostile or buggy publisher from
// making a handshake read allocate without bound.
func TestStoreChannelRejectsOversizedPayload(t *testing.T) {
	ch, _ := newTestChannel()
	err := ch.Publish(context.Background(), "call:big", [32]byte{1}, SignaledContentType,
		make([]byte, maxSignaledPayload+1))
	require.ErrorIs(t, err, ErrSignalingPayloadTooLarge)
}

// TestStoreChannelGivesIdenticalSignalsDistinctIDs covers two offers that are
// byte-identical, which is possible in a retry. The messaging package derives IDs
// from SHA3(sender, content), so without a sequence number these would collide and
// a cursor-based reader would skip the second one.
func TestStoreChannelGivesIdenticalSignalsDistinctIDs(t *testing.T) {
	ch, _ := newTestChannel()
	sig := NewMessagingSignaling("call:dup", ch)
	ctx := context.Background()

	offer := SignalOffer(CallID{4}, [32]byte{5}, nil)
	for i := 0; i < 3; i++ {
		require.NoError(t, sig.SendSignal(ctx, [32]byte{5}, offer))
	}

	batch, err := ch.History("call:dup", "", 64)
	require.NoError(t, err)
	require.Len(t, batch, 3)

	seen := map[string]bool{}
	for _, s := range batch {
		require.Falsef(t, seen[s.ID], "duplicate message ID %s", s.ID)
		seen[s.ID] = true
	}
}

// TestStoreChannelSurvivesMalformedEntry checks a truncated record does not break
// every later read of the channel.
func TestStoreChannelSurvivesMalformedEntry(t *testing.T) {
	store := messaging.NewMemoryStore()
	ch := NewStoreChannel(store)
	ctx := context.Background()

	require.NoError(t, ch.Publish(ctx, "call:bad", [32]byte{1}, SignaledContentType, []byte(`{"type":"offer"}`)))

	// A record too short to hold a sender and a content type.
	require.NoError(t, store.Append(channelFor("call:bad"), messaging.Message{
		ID: "truncated", Content: []byte{1, 2, 3}, Type: SignaledContentType,
	}))

	require.NoError(t, ch.Publish(ctx, "call:bad", [32]byte{1}, SignaledContentType, []byte(`{"type":"answer"}`)))

	batch, err := ch.History("call:bad", "", 64)
	require.NoError(t, err)
	require.Len(t, batch, 2, "the malformed record must be skipped, not returned")
}

func hexID(id [32]byte) string {
	const hexdigits = "0123456789abcdef"
	out := make([]byte, 0, len(id)*2)
	for _, b := range id {
		out = append(out, hexdigits[b>>4], hexdigits[b&0x0f])
	}
	return string(out)
}
