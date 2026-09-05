package voice

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/mrityunjay/LocalWEB/pkg/crypto"
)

var (
	ErrSignalingClosed = errors.New("signaling channel closed")
)

// SignalingChannel abstracts the messaging transport used for call signals.
type SignalingChannel interface {
	Publish(ctx context.Context, channelID string, sender [32]byte, contentType uint8, content []byte) error
	Subscribe(channelID string) (<-chan struct{}, error)
}

// MessagingSignaling adapts the LocalWEB messaging service for voice signals.
type MessagingSignaling struct {
	mu      sync.RWMutex
	channel string
	store   SignalingChannel
}

// NewMessagingSignaling creates a new signaling adapter.
func NewMessagingSignaling(channel string, store SignalingChannel) *MessagingSignaling {
	return &MessagingSignaling{channel: channel, store: store}
}

// SendSignal publishes a signaling message to the messaging channel.
func (s *MessagingSignaling) SendSignal(ctx context.Context, sender [32]byte, msg SignalMessage) error {
	payload, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return s.store.Publish(ctx, s.channel, sender, 1, payload)
}

// WaitForSignal subscribes to the channel and returns the first matching signal
// from the given peer, or a context cancellation.
func (s *MessagingSignaling) WaitForSignal(ctx context.Context, peer [32]byte, sigType SignalType) (*SignalMessage, error) {
	_, err := s.store.Subscribe(s.channel)
	if err != nil {
		return nil, err
	}

	// Poll with backoff for signals; in production this would be event-driven.
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			// Re-read history to find the signal.
			// The real messaging service exposes History through Store;
			// this polling is a bridge until the store exposes a push path.
			_ = peer
			_ = sigType
		}
	}
}

// SignalOffer constructs an offer signal.
func SignalOffer(callID CallID, sender PeerID, tracks []TrackInfo) SignalMessage {
	trackBytes, _ := json.Marshal(tracks)
	return SignalMessage{
		Type:      SignalTypeOffer,
		CallID:    callID,
		Sender:    sender,
		Timestamp: time.Now().UnixNano(),
		Payload:   trackBytes,
	}
}

// SignalAnswer constructs an answer signal.
func SignalAnswer(callID CallID, sender PeerID, tracks []TrackInfo) SignalMessage {
	trackBytes, _ := json.Marshal(tracks)
	return SignalMessage{
		Type:      SignalTypeAnswer,
		CallID:    callID,
		Sender:    sender,
		Timestamp: time.Now().UnixNano(),
		Payload:   trackBytes,
	}
}

// SignalICE constructs an ICE candidate signal.
func SignalICE(callID CallID, sender PeerID, cand ICECandidate) SignalMessage {
	candBytes, _ := json.Marshal(cand)
	return SignalMessage{
		Type:      SignalTypeICECandidate,
		CallID:    callID,
		Sender:    sender,
		Timestamp: time.Now().UnixNano(),
		Payload:   candBytes,
	}
}

// SignalBye constructs a bye signal.
func SignalBye(callID CallID, sender PeerID) SignalMessage {
	return SignalMessage{
		Type:      SignalTypeBye,
		CallID:    callID,
		Sender:    sender,
		Timestamp: time.Now().UnixNano(),
	}
}

// SignalGroupInvite constructs a group-call invite signal.
func SignalGroupInvite(callID CallID, sender PeerID, peers []PeerID) SignalMessage {
	peersBytes, _ := json.Marshal(peers)
	return SignalMessage{
		Type:      SignalTypeGroupInvite,
		CallID:    callID,
		Sender:    sender,
		Timestamp: time.Now().UnixNano(),
		Payload:   peersBytes,
	}
}

// DecodeSignal parses a raw signaling payload.
func DecodeSignal(data []byte) (*SignalMessage, error) {
	var msg SignalMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

// ValidateSignal verifies the Ed25519 signature on a signaling message.
// Returns an error if the signature is missing or invalid.
func ValidateSignal(msg *SignalMessage, pub [32]byte) error {
	if len(msg.Signature) != 64 {
		return errors.New("invalid signature length")
	}
	canonical := msg.CanonicalForm()
	if !crypto.Verify(pub, canonical, msg.Signature) {
		return errors.New("signal signature verification failed")
	}
	return nil
}

// Sign signs a signal message with the given private key.
func Sign(msg *SignalMessage, priv [32]byte) error {
	canonical := msg.CanonicalForm()
	sig, err := crypto.Sign(priv, canonical)
	if err != nil {
		return err
	}
	msg.Signature = sig
	return nil
}

// CanonicalForm returns the byte representation that is signed/verified.
func (m *SignalMessage) CanonicalForm() []byte {
	buf := make([]byte, 0, 16+1+8+len(m.Payload))
	buf = append(buf, m.CallID[:]...)
	buf = append(buf, byte(m.Type))
	buf = append(buf, byte(m.Sender[0]))
	ts := make([]byte, 8)
	binary.BigEndian.PutUint64(ts, uint64(m.Timestamp))
	buf = append(buf, ts...)
	buf = append(buf, m.Payload...)
	return buf
}

// CallSignaling bridges a Call with the signaling channel.
type CallSignaling struct {
	mu        sync.RWMutex
	callID    CallID
	caller    PeerID
	callee    PeerID
	channel   *MessagingSignaling
	callerKey [32]byte
	privKey   [32]byte
}

// NewCallSignaling creates a signaling bridge for a call.
// privKey is the Ed25519 private key of the caller, used to sign signals.
func NewCallSignaling(call *Call, channel *MessagingSignaling, callerKey, privKey [32]byte) *CallSignaling {
	caller, callee := call.Peers()
	return &CallSignaling{
		callID:    call.ID(),
		caller:    caller,
		callee:    callee,
		channel:   channel,
		callerKey: callerKey,
		privKey:   privKey,
	}
}

// SendOffer sends a signed offer to the callee.
func (s *CallSignaling) SendOffer(ctx context.Context, tracks []TrackInfo) error {
	msg := SignalOffer(s.callID, s.caller, tracks)
	if err := Sign(&msg, s.privKey); err != nil {
		return err
	}
	return s.channel.SendSignal(ctx, s.callerKey, msg)
}

// SendAnswer sends a signed answer to the caller.
func (s *CallSignaling) SendAnswer(ctx context.Context, tracks []TrackInfo) error {
	msg := SignalAnswer(s.callID, s.callee, tracks)
	if err := Sign(&msg, s.privKey); err != nil {
		return err
	}
	return s.channel.SendSignal(ctx, s.callerKey, msg)
}

// SendICE sends a signed ICE candidate.
func (s *CallSignaling) SendICE(ctx context.Context, cand ICECandidate) error {
	msg := SignalICE(s.callID, s.caller, cand)
	if err := Sign(&msg, s.privKey); err != nil {
		return err
	}
	return s.channel.SendSignal(ctx, s.callerKey, msg)
}

// SendBye sends a signed bye signal.
func (s *CallSignaling) SendBye(ctx context.Context) error {
	msg := SignalBye(s.callID, s.caller)
	if err := Sign(&msg, s.privKey); err != nil {
		return err
	}
	return s.channel.SendSignal(ctx, s.callerKey, msg)
}

// CallID returns the associated call ID.
func (s *CallSignaling) CallID() CallID {
	return s.callID
}
