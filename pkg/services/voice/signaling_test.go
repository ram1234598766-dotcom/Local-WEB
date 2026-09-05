package voice

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/mrityunjay/LocalWEB/pkg/crypto"
	"github.com/stretchr/testify/require"
)

// mockSignalingChannel implements SignalingChannel for tests.
type mockSignalingChannel struct {
	mu       sync.RWMutex
	messages []SignalMessage
	closed   bool
}

func newMockSignalingChannel() *mockSignalingChannel {
	return &mockSignalingChannel{messages: make([]SignalMessage, 0)}
}

func (m *mockSignalingChannel) Publish(_ context.Context, _ string, _ [32]byte, _ uint8, _ []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return ErrSignalingClosed
	}
	return nil
}

func (m *mockSignalingChannel) Subscribe(_ string) (<-chan struct{}, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, ErrSignalingClosed
	}
	ch := make(chan struct{}, 1)
	return ch, nil
}

func TestSignalMessageRoundTrip(t *testing.T) {
	pub, _, _ := crypto.GenerateX25519KeyPair()
	caller := crypto.NodeID(pub)
	callee := crypto.NodeID([32]byte{})
	for i := range callee {
		callee[i] = byte(i + 1)
	}

	call := NewCall(CallConfig{Caller: caller, Callee: callee, ChannelID: "ch"})
	ch := newMockSignalingChannel()
	sig := NewCallSignaling(call, NewMessagingSignaling("ch", ch), pub)

	tracks := []TrackInfo{
		{ID: "audio-1", Kind: TrackKindAudio, Direction: TrackDirectionSendRecv, Codec: CodecOpus, MimeType: "audio/opus", SampleRate: 48000, Bitrate: 64},
	}

	ctx := context.Background()
	require.NoError(t, sig.SendOffer(ctx, tracks))
	require.NoError(t, sig.SendAnswer(ctx, tracks))
	require.NoError(t, sig.SendICE(ctx, ICECandidate{ID: "c1", Type: CandidateHost, Address: "10.0.0.1", Port: 5000, Protocol: "udp"}))
	require.NoError(t, sig.SendBye(ctx))
}

func TestDecodeSignal(t *testing.T) {
	msg := SignalMessage{Type: SignalTypeOffer, CallID: CallID{1: 42}, Sender: [32]byte{1: 1}}
	data, err := json.Marshal(msg)
	require.NoError(t, err)

	decoded, err := DecodeSignal(data)
	require.NoError(t, err)
	require.Equal(t, SignalTypeOffer, decoded.Type)
	require.Equal(t, CallID{1: 42}, decoded.CallID)
}

func TestSignalBye(t *testing.T) {
	callID := CallID{1: 7}
	msg := SignalBye(callID, [32]byte{2: 3})
	require.Equal(t, SignalTypeBye, msg.Type)
	require.Equal(t, callID, msg.CallID)
}

func TestSignalICE(t *testing.T) {
	callID := CallID{1: 8}
	cand := ICECandidate{ID: "host-1", Type: CandidateHost, Address: "192.168.1.5", Port: 3478, Protocol: "udp"}
	msg := SignalICE(callID, [32]byte{3: 4}, cand)
	require.Equal(t, SignalTypeICECandidate, msg.Type)
	require.Equal(t, callID, msg.CallID)
}

func TestSignalGroupInvite(t *testing.T) {
	pub, _, _ := crypto.GenerateX25519KeyPair()
	hub := [32]byte(pub)
	p1 := [32]byte{}
	p2 := [32]byte{}
	p1[0] = 1
	p2[0] = 2

	callID := CallID{1: 9}
	msg := SignalGroupInvite(callID, hub, []PeerID{p1, p2})
	require.Equal(t, SignalTypeGroupInvite, msg.Type)
	require.NotEmpty(t, msg.Payload)
}

func TestMessagingSignalingClosed(t *testing.T) {
	mock := newMockSignalingChannel()
	mock.mu.Lock()
	mock.closed = true
	mock.mu.Unlock()

	ctx := context.Background()
	ch := NewMessagingSignaling("ch", mock)
	// We can't easily construct a CallSignaling with nil call since it calls Peers().
	// Instead verify that SendSignal on the closed channel fails directly.
	require.ErrorIs(t, ch.SendSignal(ctx, [32]byte{}, SignalMessage{Type: SignalTypeBye, CallID: CallID{}}), ErrSignalingClosed)
}
