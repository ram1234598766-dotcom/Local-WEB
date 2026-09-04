package voice

import (
	"testing"

	"github.com/mrityunjay/LocalWEB/pkg/crypto"
	"github.com/stretchr/testify/require"
)

func TestNewCallInitialState(t *testing.T) {
	pub, _, _ := crypto.GenerateX25519KeyPair()
	caller := crypto.NodeID(pub)
	callee := crypto.NodeID([32]byte{})
	for i := range callee {
		callee[i] = byte(i)
	}

	call := NewCall(CallConfig{
		Caller:    caller,
		Callee:    callee,
		ChannelID: "ch-test",
	})

	require.Equal(t, CallStateIdle, call.State())
	require.Equal(t, "ch-test", call.ChannelID())
}

func TestCallStateMachineValidTransitions(t *testing.T) {
	pub, _, _ := crypto.GenerateX25519KeyPair()
	caller := crypto.NodeID(pub)
	callee := crypto.NodeID([32]byte{})
	for i := range callee {
		callee[i] = byte(i + 1)
	}

	call := NewCall(CallConfig{Caller: caller, Callee: callee, ChannelID: "ch"})

	require.NoError(t, call.Call())
	require.Equal(t, CallStateCalling, call.State())

	require.NoError(t, call.Ringing())
	require.Equal(t, CallStateRinging, call.State())

	require.NoError(t, call.Accept())
	require.Equal(t, CallStateConnected, call.State())

	require.NoError(t, call.End())
	require.Equal(t, CallStateEnded, call.State())
}

func TestCallStateMachineInvalidTransitions(t *testing.T) {
	pub, _, _ := crypto.GenerateX25519KeyPair()
	caller := crypto.NodeID(pub)
	callee := crypto.NodeID([32]byte{})
	for i := range callee {
		callee[i] = byte(i + 2)
	}

	call := NewCall(CallConfig{Caller: caller, Callee: callee, ChannelID: "ch"})

	require.Error(t, call.Accept())
	require.Equal(t, CallStateIdle, call.State())

	require.NoError(t, call.Call())
	require.Error(t, call.Call())
	require.Equal(t, CallStateCalling, call.State())
}

func TestCallManagerCreateAndEnd(t *testing.T) {
	mgr := NewCallManager()
	pub, _, _ := crypto.GenerateX25519KeyPair()
	caller := crypto.NodeID(pub)
	callee := crypto.NodeID([32]byte{})
	for i := range callee {
		callee[i] = byte(i + 3)
	}

	call := mgr.Create(CallConfig{Caller: caller, Callee: callee, ChannelID: "ch-mgr"})
	require.NotNil(t, call)
	require.Equal(t, CallStateCalling, call.State())

	require.NoError(t, mgr.Accept(call.ID()))
	require.Equal(t, CallStateConnected, call.State())

	require.NoError(t, mgr.End(call.ID()))
	require.Equal(t, CallStateEnded, call.State())
}

func TestCallManagerGetNotFound(t *testing.T) {
	mgr := NewCallManager()
	_, err := mgr.Get(CallID{})
	require.ErrorIs(t, err, ErrCallNotFound)
}

func TestCallManagerGetByPeerNotFound(t *testing.T) {
	mgr := NewCallManager()
	pub, _, _ := crypto.GenerateX25519KeyPair()
	peer := crypto.NodeID(pub)
	_, err := mgr.GetByPeer(peer)
	require.ErrorIs(t, err, ErrCallNotFound)
}

func TestCallAddTrack(t *testing.T) {
	pub, _, _ := crypto.GenerateX25519KeyPair()
	caller := crypto.NodeID(pub)
	callee := crypto.NodeID([32]byte{})
	for i := range callee {
		callee[i] = byte(i + 4)
	}

	call := NewCall(CallConfig{Caller: caller, Callee: callee, ChannelID: "ch-track"})
	require.NoError(t, call.Call())
	require.NoError(t, call.AddTrack(TrackInfo{ID: "t1", Kind: TrackKindAudio, Direction: TrackDirectionSendRecv, Codec: CodecOpus, MimeType: "audio/opus"}))
	require.Len(t, call.Tracks(), 1)

	// Cannot add track after end.
	require.NoError(t, call.End())
	require.ErrorIs(t, call.AddTrack(TrackInfo{ID: "t2", Kind: TrackKindAudio, Direction: TrackDirectionSendRecv, Codec: CodecOpus, MimeType: "audio/opus"}), ErrCallEnded)
}

func TestCallAddCandidate(t *testing.T) {
	pub, _, _ := crypto.GenerateX25519KeyPair()
	caller := crypto.NodeID(pub)
	callee := crypto.NodeID([32]byte{})
	for i := range callee {
		callee[i] = byte(i + 5)
	}

	call := NewCall(CallConfig{Caller: caller, Callee: callee, ChannelID: "ch-cand"})
	call.AddCandidate(ICECandidate{ID: "c1", Type: CandidateHost, Address: "10.0.0.1", Port: 5000, Protocol: "udp"})
	require.Len(t, call.Candidates(), 1)
}

func TestCallManagerActiveCalls(t *testing.T) {
	mgr := NewCallManager()
	pub, _, _ := crypto.GenerateX25519KeyPair()
	caller := crypto.NodeID(pub)
	callee := crypto.NodeID([32]byte{})
	for i := range callee {
		callee[i] = byte(i + 6)
	}

	c1 := mgr.Create(CallConfig{Caller: caller, Callee: callee, ChannelID: "ch-a"})
	mgr.Create(CallConfig{Caller: callee, Callee: caller, ChannelID: "ch-b"})
	require.Len(t, mgr.ActiveCalls(), 2)

	require.NoError(t, mgr.End(c1.ID()))
	require.Len(t, mgr.ActiveCalls(), 1)
}

func TestCallStateString(t *testing.T) {
	require.Equal(t, "idle", CallStateIdle.String())
	require.Equal(t, "calling", CallStateCalling.String())
	require.Equal(t, "ringing", CallStateRinging.String())
	require.Equal(t, "connected", CallStateConnected.String())
	require.Equal(t, "ended", CallStateEnded.String())
}
