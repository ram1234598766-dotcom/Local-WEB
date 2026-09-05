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
	closed    bool
	privKey   [32]byte // Ed25519 private key for signing signals
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
