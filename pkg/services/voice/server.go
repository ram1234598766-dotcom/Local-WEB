package voice

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"

	"github.com/ram1234598766-dotcom/Local-WEB/pkg/transport"
	"github.com/rs/zerolog/log"
)

const (
	voiceReadBufferSize = 4096
)

var (
	ErrServerClosed = errors.New("voice server closed")
	ErrNoHandler    = errors.New("no handler registered")
)

// PeerHandler is called when a media frame is received from a peer.
type PeerHandler func(ctx context.Context, peer PeerID, trackID string, payload []byte)

// VoiceServer coordinates voice/video sessions over QUIC streams.
type VoiceServer struct {
	mu        sync.RWMutex
	server    *transport.Server // QUIC transport server
	messaging transport.StreamHandler
	calls     *CallManager
	trackers  map[PeerID]*TrackManager // per-peer track state
	hubRole   bool                     // true if this node is the group-call hub
	handlers  map[string]PeerHandler   // trackID -> handler
	// channel is the signalling transport calls are placed and awaited on.
	channel SignalingChannel
	closed  bool
	privKey [32]byte // Ed25519 private key for signing signals
}

// NewVoiceServer wires the voice service into an existing QUIC server.
// privKey is the node's Ed25519 private key used to sign signaling messages.
func NewVoiceServer(srv *transport.Server, hubRole bool, privKey [32]byte) *VoiceServer {
	v := &VoiceServer{
		server:   srv,
		calls:    NewCallManager(),
		trackers: make(map[PeerID]*TrackManager),
		hubRole:  hubRole,
		handlers: make(map[string]PeerHandler),
		privKey:  privKey,
	}
	srv.RegisterHandler(transport.ServiceVoice, v.handleStream)
	return v
}

// SetSignalingChannel supplies the transport the service signals over.
//
// The server took a SignalingChannel per call, which meant nothing could call
// StartCall except a test: every real caller would have had to construct a
// signalling transport of its own. Owning it here is what lets a caller ask the
// service to place a call instead of wiring the pieces itself.
func (v *VoiceServer) SetSignalingChannel(ch SignalingChannel) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.channel = ch
}

// SignalingChannel returns the configured signalling transport, or nil.
func (v *VoiceServer) SignalingChannel() SignalingChannel {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.channel
}

// PlaceCall creates a call and publishes a signed offer for it.
//
// This is the caller-facing half of a handshake: it registers the call, signs an
// offer describing localTracks, and publishes it on the call's signalling channel.
// The peer reads it with AwaitSignal and answers with AcceptCall.
//
// Media does not flow yet. Nothing turns a live WebRTCSession's SDP or ICE
// candidates into signals, so a placed call reaches "offer sent", not "connected".
func (v *VoiceServer) PlaceCall(ctx context.Context, cfg CallConfig, localTracks []TrackInfo) (*CallSignaling, error) {
	v.mu.RLock()
	ch := v.channel
	closed := v.closed
	v.mu.RUnlock()

	if closed {
		return nil, ErrServerClosed
	}
	if ch == nil {
		return nil, ErrNoSignalingChannel
	}

	sig, err := v.StartCall(ctx, cfg, ch)
	if err != nil {
		return nil, err
	}
	if err := sig.SendOffer(ctx, localTracks); err != nil {
		return nil, fmt.Errorf("publish offer: %w", err)
	}
	return sig, nil
}

// AwaitSignal waits for a signal of the given type from a peer on a channel.
//
// It is the receiving half of PlaceCall, and the reason WaitForSignal had a read
// path to begin with: a receiver has to get the payload out, not just be told
// something arrived.
func (v *VoiceServer) AwaitSignal(ctx context.Context, channelID string, peer PeerID, sigType SignalType) (*SignalMessage, error) {
	v.mu.RLock()
	ch := v.channel
	v.mu.RUnlock()

	if ch == nil {
		return nil, ErrNoSignalingChannel
	}
	return NewMessagingSignaling(channelID, ch).WaitForSignal(ctx, peer, sigType)
}

// ErrNoSignalingChannel is returned when a call is placed before a signalling
// transport has been configured. It is distinct from ErrServerClosed because the
// remedies differ: this one needs SetSignalingChannel.
var ErrNoSignalingChannel = errors.New("the voice service has no signalling channel configured")

// handleStream dispatches incoming voice/video streams.
func (v *VoiceServer) handleStream(ctx context.Context, stm transport.Stream) {
	defer stm.Close()

	buf := make([]byte, voiceReadBufferSize)
	n, err := stm.Read(buf)
	if n == 0 || err != nil {
		return
	}

	// Wire format: [call_id:16][track_id_len:2][track_id:N][payload...]
	if n < 18 {
		return
	}

	var callID CallID
	copy(callID[:], buf[:16])
	trackIDLen := int(binary.BigEndian.Uint16(buf[16:18]))
	if n < 18+trackIDLen {
		return
	}
	trackID := string(buf[18 : 18+trackIDLen])
	payload := buf[18+trackIDLen : n]

	call, err := v.calls.Get(callID)
	if err != nil {
		log.Warn().Err(err).Str("call_id", fmt.Sprintf("%x", callID[:8])).Msg("stream for unknown call")
		return
	}

	_, callee := call.Peers()
	peerID := callee // in practice we'd derive from stream metadata; use callee for now
	v.mu.RLock()
	handler, ok := v.handlers[trackID]
	v.mu.RUnlock()
	if ok {
		handler(ctx, peerID, trackID, payload)
	}
}

// RegisterPeerHandler attaches a handler for incoming media on a track.
func (v *VoiceServer) RegisterPeerHandler(trackID string, h PeerHandler) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.handlers[trackID] = h
}

// TrackManager returns the track manager for a peer, creating it if needed.
func (v *VoiceServer) TrackManager(peer PeerID) *TrackManager {
	v.mu.Lock()
	defer v.mu.Unlock()
	if tm, ok := v.trackers[peer]; ok {
		return tm
	}
	tm := NewTrackManager()
	v.trackers[peer] = tm
	return tm
}

// StartCall initiates a new call and returns the signaling bridge.
func (v *VoiceServer) StartCall(ctx context.Context, cfg CallConfig, channel SignalingChannel) (*CallSignaling, error) {
	v.mu.RLock()
	if v.closed {
		v.mu.RUnlock()
		return nil, ErrServerClosed
	}
	v.mu.RUnlock()

	call := v.calls.Create(cfg)
	if call == nil {
		return nil, errors.New("failed to create call")
	}

	privKey := v.privKey
	if cfg.PrivKey != nil {
		privKey = *cfg.PrivKey
	}
	sig := NewCallSignaling(call, NewMessagingSignaling(cfg.ChannelID, channel), cfg.Caller, privKey)
	return sig, nil
}

// AcceptCall answers an incoming call.
func (v *VoiceServer) AcceptCall(ctx context.Context, callID CallID, channel SignalingChannel, localTracks []TrackInfo) error {
	v.mu.RLock()
	if v.closed {
		v.mu.RUnlock()
		return ErrServerClosed
	}
	v.mu.RUnlock()

	if err := v.calls.Accept(callID); err != nil {
		return err
	}
	call, err := v.calls.Get(callID)
	if err != nil {
		return err
	}
	sig := NewCallSignaling(call, NewMessagingSignaling(call.ChannelID(), channel), call.Callee(), v.privKey)
	return sig.SendAnswer(ctx, localTracks)
}

// EndCall terminates a call.
func (v *VoiceServer) EndCall(ctx context.Context, callID CallID, channel SignalingChannel) error {
	v.mu.RLock()
	if v.closed {
		v.mu.RUnlock()
		return ErrServerClosed
	}
	v.mu.RUnlock()

	call, err := v.calls.Get(callID)
	if err != nil {
		return err
	}
	sig := NewCallSignaling(call, NewMessagingSignaling(call.ChannelID(), channel), call.Callee(), v.privKey)
	_ = sig.SendBye(ctx)
	return v.calls.End(callID)
}

// ActiveCalls returns the active calls.
func (v *VoiceServer) ActiveCalls() []*Call {
	return v.calls.ActiveCalls()
}

// Close shuts down the voice server.
func (v *VoiceServer) Close() {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.closed = true
	v.calls.Stop()
}

// MediaFrameHeader is the 4-byte header on each media frame.
type MediaFrameHeader struct {
	CallID   CallID
	TrackID  string
	Sequence uint32
	IsKey    bool
}

// MarshalFrame packs a media frame with a simple header.
func MarshalFrame(callID CallID, trackID string, seq uint32, isKey bool, payload []byte) []byte {
	buf := make([]byte, 16+2+len(trackID)+4+1+len(payload))
	copy(buf[:16], callID[:])
	binary.BigEndian.PutUint16(buf[16:18], uint16(len(trackID)))
	copy(buf[18:18+len(trackID)], trackID)
	binary.BigEndian.PutUint32(buf[18+len(trackID):22+len(trackID)], seq)
	if isKey {
		buf[22+len(trackID)] = 1
	}
	copy(buf[23+len(trackID):], payload)
	return buf
}

// UnmarshalFrame unpacks a media frame.
func UnmarshalFrame(data []byte) (*MediaFrameHeader, []byte, error) {
	if len(data) < 18 {
		return nil, nil, errors.New("frame too short")
	}
	var callID CallID
	copy(callID[:], data[:16])
	trackIDLen := int(binary.BigEndian.Uint16(data[16:18]))
	if len(data) < 18+trackIDLen+5 {
		return nil, nil, errors.New("frame incomplete")
	}
	trackID := string(data[18 : 18+trackIDLen])
	seq := binary.BigEndian.Uint32(data[18+trackIDLen : 22+trackIDLen])
	isKey := data[22+trackIDLen] == 1
	payload := data[23+trackIDLen:]
	return &MediaFrameHeader{CallID: callID, TrackID: trackID, Sequence: seq, IsKey: isKey}, payload, nil
}
