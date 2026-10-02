package voice

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/ram1234598766-dotcom/Local-WEB/pkg/crypto"
)

var (
	ErrSignalingClosed = errors.New("signaling channel closed")
)

// Signaled is one message read back out of a signaling channel.
type Signaled struct {
	// ID is the store's cursor for this message. Passing it back as the "after"
	// argument resumes immediately after it, which is what stops a poller from
	// re-reading the same signal forever.
	ID          string
	Sender      [32]byte
	ContentType uint8
	Content     []byte
}

// SignalingChannel abstracts the messaging transport used for call signals.
//
// It has a read side because a receiver has to get the signal's content out, not
// just be told that something arrived. Subscribe's notification channel cannot
// carry a payload, so without History a receiver can only poll blindly.
type SignalingChannel interface {
	Publish(ctx context.Context, channelID string, sender [32]byte, contentType uint8, content []byte) error
	Subscribe(channelID string) (<-chan struct{}, error)
	// History returns messages published to a channel strictly after the message
	// ID afterID, oldest first. An empty afterID means the start of the channel.
	History(channelID, afterID string, limit int) ([]Signaled, error)
}

// signalingPollInterval is how often WaitForSignal re-reads the channel.
//
// The store exposes a cursor read rather than a push of decoded signals, so this
// is a poll. The interval is short because a call handshake is latency-sensitive,
// and each poll advances a cursor rather than rescanning, so a missed tick costs a
// little latency and nothing else.
const signalingPollInterval = 25 * time.Millisecond

// signalingHistoryLimit bounds one poll's read. A handshake is a handful of
// messages, so a small limit is generous; the cap exists so a channel that has
// been used for something else cannot make one poll unbounded.
const signalingHistoryLimit = 64

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

// WaitForSignal returns the next signal of sigType sent by peer.
//
// This used to be an endless loop: it subscribed, threw the notification channel
// away, ignored peer and sigType, and re-read nothing, so it could only ever
// return ctx.Err(). No call could be answered, and nothing called it.
//
// It waits for signals published from now on rather than replaying the channel,
// because a handshake answers a live offer: answering one that was published
// minutes ago would try to negotiate with a caller that has given up. A caller
// that needs to resume mid-handshake uses WaitForSignalFrom.
func (s *MessagingSignaling) WaitForSignal(ctx context.Context, peer [32]byte, sigType SignalType) (*SignalMessage, error) {
	msg, _, err := s.WaitForSignalFrom(ctx, peer, sigType, "")
	return msg, err
}

// WaitForSignalFrom is WaitForSignal with an explicit cursor, returning the
// cursor it stopped at so the caller can resume after reconnecting.
//
// It returns ctx.Err() only when the context ends without a matching signal.
func (s *MessagingSignaling) WaitForSignalFrom(ctx context.Context, peer [32]byte, sigType SignalType, afterID string) (*SignalMessage, string, error) {
	// An empty cursor means "from the start of the channel". Taking the tail first
	// is what makes WaitForSignal wait for new signals rather than replaying.
	if afterID == "" {
		existing, err := s.store.History(s.channel, "", signalingHistoryLimit)
		if err != nil {
			return nil, "", err
		}
		if n := len(existing); n > 0 {
			afterID = existing[n-1].ID
		}
	}

	if _, err := s.store.Subscribe(s.channel); err != nil {
		return nil, afterID, err
	}

	ticker := time.NewTicker(signalingPollInterval)
	defer ticker.Stop()

	cursor := afterID
	for {
		select {
		case <-ctx.Done():
			return nil, cursor, ctx.Err()
		case <-ticker.C:
		}

		batch, err := s.store.History(s.channel, cursor, signalingHistoryLimit)
		if err != nil {
			return nil, cursor, err
		}
		for _, raw := range batch {
			// The cursor advances past every message examined, including ones
			// filtered out, so a poller never re-reads the same history.
			cursor = raw.ID

			if raw.Sender != peer {
				continue
			}
			var msg SignalMessage
			if err := json.Unmarshal(raw.Content, &msg); err != nil {
				// A malformed payload must not stall the handshake: skip it and keep
				// going rather than failing every future wait on this channel.
				continue
			}
			if msg.Type != sigType {
				continue
			}
			return &msg, cursor, nil
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
