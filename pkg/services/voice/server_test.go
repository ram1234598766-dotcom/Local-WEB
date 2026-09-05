package voice

import (
	"context"
	"errors"
	"testing"

	"github.com/ram1234598766-dotcom/Local-WEB/pkg/crypto"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/transport"
	"github.com/stretchr/testify/require"
)

// mockTransportServer is a minimal fake for testing.
type mockTransportServer struct {
	handlers        map[transport.ServiceID]transport.StreamHandler
	registerHandler func(transport.ServiceID, transport.StreamHandler)
}

func (m *mockTransportServer) RegisterHandler(sid transport.ServiceID, h transport.StreamHandler) {
	if m.registerHandler != nil {
		m.registerHandler(sid, h)
	}
}

// mockStream is a minimal stream fake.
type mockStream struct {
	readBuf  []byte
	readIdx  int
	writeBuf []byte
	closed   bool
}

func newMockStream(data []byte) *mockStream {
	return &mockStream{readBuf: data, writeBuf: make([]byte, 0)}
}

func (m *mockStream) Read(p []byte) (int, error) {
	if m.closed {
		return 0, errors.New("closed")
	}
	if m.readIdx >= len(m.readBuf) {
		return 0, errors.New("EOF")
	}
	n := copy(p, m.readBuf[m.readIdx:])
	m.readIdx += n
	return n, nil
}

func (m *mockStream) Write(p []byte) (int, error) {
	if m.closed {
		return 0, errors.New("closed")
	}
	m.writeBuf = append(m.writeBuf, p...)
	return len(p), nil
}

func (m *mockStream) Close() error {
	m.closed = true
	return nil
}

func (m *mockStream) ServiceID() transport.ServiceID {
	return transport.ServiceVoice
}

func (m *mockStream) ID() uint64 {
	return 1
}

func (m *mockStream) PeerID() [32]byte {
	return [32]byte{}
}

func TestNewVoiceServer(t *testing.T) {
	ctx := context.Background()
	pubA, privA, _ := crypto.GenerateX25519KeyPair()
	edPub, edPriv, _ := crypto.GenerateKeyPair()

	srv, err := transport.NewServer(ctx, "127.0.0.1:0", pubA, privA)
	require.NoError(t, err)
	defer srv.Stop()

	v := NewVoiceServer(srv, true, edPriv)
	require.NotNil(t, v)
	require.True(t, v.hubRole)
	_ = edPub
}

func TestStartCall(t *testing.T) {
	ctx := context.Background()
	pubA, privA, _ := crypto.GenerateX25519KeyPair()
	pubB, _, _ := crypto.GenerateX25519KeyPair()
	edPub, edPriv, _ := crypto.GenerateKeyPair()

	srvA, err := transport.NewServer(ctx, "127.0.0.1:0", pubA, privA)
	require.NoError(t, err)
	defer srvA.Stop()

	v := NewVoiceServer(srvA, false, edPriv)
	ch := newMockSignalingChannel()
	sig, err := v.StartCall(ctx, CallConfig{
		Caller:    [32]byte(edPub),
		Callee:    [32]byte(pubB),
		ChannelID: "ch-voice",
	}, ch)
	require.NoError(t, err)
	require.NotNil(t, sig)
	require.Len(t, v.ActiveCalls(), 1)
}

func TestAcceptCall(t *testing.T) {
	ctx := context.Background()
	pubA, privA, _ := crypto.GenerateX25519KeyPair()
	edPub, edPriv, _ := crypto.GenerateKeyPair()

	srvA, err := transport.NewServer(ctx, "127.0.0.1:0", pubA, privA)
	require.NoError(t, err)
	defer srvA.Stop()

	v := NewVoiceServer(srvA, false, edPriv)
	ch := newMockSignalingChannel()
	caller := [32]byte(edPub)
	callee := [32]byte{}
	for i := range callee {
		callee[i] = byte(i + 1)
	}
	sig, err := v.StartCall(ctx, CallConfig{
		Caller:    caller,
		Callee:    callee,
		ChannelID: "ch-voice-accept",
	}, ch)
	require.NoError(t, err)

	callID := sig.CallID()
	_ = sig

	require.NoError(t, v.AcceptCall(ctx, callID, ch, []TrackInfo{}))
}

func TestEndCall(t *testing.T) {
	ctx := context.Background()
	pubA, privA, _ := crypto.GenerateX25519KeyPair()
	edPub, edPriv, _ := crypto.GenerateKeyPair()

	srvA, err := transport.NewServer(ctx, "127.0.0.1:0", pubA, privA)
	require.NoError(t, err)
	defer srvA.Stop()

	v := NewVoiceServer(srvA, false, edPriv)
	ch := newMockSignalingChannel()
	caller := [32]byte(edPub)
	callee := [32]byte{}
	for i := range callee {
		callee[i] = byte(i + 1)
	}
	sig, err := v.StartCall(ctx, CallConfig{
		Caller:    caller,
		Callee:    callee,
		ChannelID: "ch-voice-end",
	}, ch)
	require.NoError(t, err)

	callID := sig.CallID()
	_ = sig

	require.NoError(t, v.EndCall(ctx, callID, ch))
	require.Empty(t, v.ActiveCalls())
}

func TestMarshalUnmarshalFrame(t *testing.T) {
	callID := CallID{1: 42}
	trackID := "audio-1"
	payload := []byte("hello media")
	frame := MarshalFrame(callID, trackID, 7, true, payload)

	header, p, err := UnmarshalFrame(frame)
	require.NoError(t, err)
	require.Equal(t, callID, header.CallID)
	require.Equal(t, trackID, header.TrackID)
	require.Equal(t, uint32(7), header.Sequence)
	require.True(t, header.IsKey)
	require.Equal(t, payload, p)
}

func TestTrackManagerPerPeer(t *testing.T) {
	ctx := context.Background()
	pubA, privA, _ := crypto.GenerateX25519KeyPair()
	_, edPriv, _ := crypto.GenerateKeyPair()

	srvA, err := transport.NewServer(ctx, "127.0.0.1:0", pubA, privA)
	require.NoError(t, err)
	defer srvA.Stop()

	v := NewVoiceServer(srvA, false, edPriv)
	peer := [32]byte{}
	for i := range peer {
		peer[i] = byte(i + 1)
	}
	tm := v.TrackManager(peer)
	require.NotNil(t, tm)

	require.NoError(t, tm.Add(TrackInfo{ID: "t1", Kind: TrackKindAudio, Direction: TrackDirectionSendRecv, Codec: CodecOpus, MimeType: "audio/opus"}))

	// Same peer should return the same manager.
	tm2 := v.TrackManager(peer)
	require.Equal(t, tm, tm2)
}

func TestVoiceServerClose(t *testing.T) {
	ctx := context.Background()
	pubA, privA, _ := crypto.GenerateX25519KeyPair()
	edPub, edPriv, _ := crypto.GenerateKeyPair()

	srvA, err := transport.NewServer(ctx, "127.0.0.1:0", pubA, privA)
	require.NoError(t, err)
	defer srvA.Stop()

	v := NewVoiceServer(srvA, false, edPriv)
	v.Close()

	ch := newMockSignalingChannel()
	caller := [32]byte(edPub)
	callee := [32]byte{}
	for i := range callee {
		callee[i] = byte(i + 1)
	}
	_, err = v.StartCall(ctx, CallConfig{
		Caller:    caller,
		Callee:    callee,
		ChannelID: "ch-voice-close",
	}, ch)
	require.ErrorIs(t, err, ErrServerClosed)
}

func TestRegisterPeerHandler(t *testing.T) {
	ctx := context.Background()
	pubA, privA, _ := crypto.GenerateX25519KeyPair()
	_, edPriv, _ := crypto.GenerateKeyPair()

	srvA, err := transport.NewServer(ctx, "127.0.0.1:0", pubA, privA)
	require.NoError(t, err)
	defer srvA.Stop()

	v := NewVoiceServer(srvA, false, edPriv)
	called := false
	v.RegisterPeerHandler("track-1", func(_ context.Context, _ PeerID, _ string, _ []byte) {
		called = true
	})
	v.mu.RLock()
	_, ok := v.handlers["track-1"]
	v.mu.RUnlock()
	require.True(t, ok)
	_ = called
}
