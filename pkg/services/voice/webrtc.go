package voice

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/pion/webrtc/v4"
)

// WebRTCSession is a real WebRTC peer connection.
//
// The package previously had a "VoiceServer" that carried signalling over the
// messaging service and no transport at all: no ICE, no DTLS, no SRTP, and no
// codec. This is the transport, built on pion, which is pure Go and therefore
// keeps the cgo-free cross-compiles and the CLI build working.
//
// What is genuinely provided here, and is verified by test:
//   - ICE agent and candidate exchange, including trickle
//   - DTLS transport, so media and data are actually encrypted
//   - SRTP for media tracks
//   - SCTP data channels for application bytes (used by 7.2)
//
// What is not provided is an Opus encoder; see opus.go for why, and SendPCM for
// how that surfaces.
type WebRTCSession struct {
	mu        sync.Mutex
	pc        *webrtc.PeerConnection
	decoder   *OpusDecoder
	closed    bool
	role      string
	remoteSDP atomicString
	onTrack   func(*webrtc.TrackRemote)
	dataChans map[string]*webrtc.DataChannel
}

// atomicString is a tiny mutex-guarded string; the session keeps the last remote
// description so a re-offer can be answered without renegotiating from scratch.
type atomicString struct {
	mu sync.Mutex
	v  string
}

func (a *atomicString) set(s string) {
	a.mu.Lock()
	a.v = s
	a.mu.Unlock()
}

func (a *atomicString) get() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.v
}

// NewWebRTCSession creates a session with a media engine that offers Opus for
// audio.
//
// VP8/VP9 are not registered. The roadmap said "Opus/VP9", and Opus satisfies
// the actual requirement - a library-backed codec rather than raw passthrough -
// whereas VP8 and VP9 have no Go module and would need cgo against libvpx, which
// would break the cgo-free builds. Registering a codec that cannot be satisfied
// would be a lie in the SDP.
func NewWebRTCSession(role string) (*WebRTCSession, error) {
	m := &webrtc.MediaEngine{}
	if err := m.RegisterCodec(webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType:    webrtc.MimeTypeOpus,
			ClockRate:   OpusSampleRate,
			Channels:    OpusChannels,
			SDPFmtpLine: "minptime=10;useinbandfec=1",
		},
		PayloadType: OpusPayloadTyp,
	}, webrtc.RTPCodecTypeAudio); err != nil {
		return nil, fmt.Errorf("register opus: %w", err)
	}

	api := webrtc.NewAPI(webrtc.WithMediaEngine(m))
	pc, err := api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		return nil, fmt.Errorf("new peer connection: %w", err)
	}

	dec, err := NewOpusDecoder()
	if err != nil {
		_ = pc.Close()
		return nil, err
	}

	s := &WebRTCSession{
		pc:        pc,
		decoder:   dec,
		role:      role,
		dataChans: make(map[string]*webrtc.DataChannel),
	}

	pc.OnTrack(func(remote *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		s.mu.Lock()
		handler := s.onTrack
		s.mu.Unlock()
		if handler != nil {
			handler(remote)
		}
	})
	return s, nil
}

// OnTrack installs a handler for inbound media tracks.
func (s *WebRTCSession) OnTrack(h func(*webrtc.TrackRemote)) {
	s.mu.Lock()
	s.onTrack = h
	s.mu.Unlock()
}

// PeerConnection exposes the underlying connection for state inspection.
func (s *WebRTCSession) PeerConnection() *webrtc.PeerConnection { return s.pc }

// ICEState reports the current ICE connection state.
func (s *WebRTCSession) ICEState() webrtc.ICEConnectionState { return s.pc.ICEConnectionState() }

// ConnectionState reports the current peer connection state.
func (s *WebRTCSession) ConnectionState() webrtc.PeerConnectionState {
	return s.pc.ConnectionState()
}

// PrepareAudio adds an audio transceiver so the SDP carries an m-line.
//
// This is not optional. pion only gathers ICE candidates once there is
// something to gather for, so an offer built with no transceiver at all yields
// an SDP with no candidates and no ice-ufrag, and the peer can never connect -
// which is exactly the failure the first version of this code hit.
//
// A node is normally the receiver in a call, since the browser on the far end
// is what encodes, so RecvOnly is the sensible default.
func (s *WebRTCSession) PrepareAudio(direction webrtc.RTPTransceiverDirection) (*webrtc.RTPTransceiver, error) {
	tr, err := s.pc.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio,
		webrtc.RTPTransceiverInit{Direction: direction})
	if err != nil {
		return nil, fmt.Errorf("add audio transceiver: %w", err)
	}
	return tr, nil
}

// Offer creates an SDP offer and waits for ICE gathering to finish.
//
// Waiting matters: pion populates candidates asynchronously, so handing the
// description over before gathering completes produces an offer with no
// candidates and the peer can never connect. The gathered description is stored
// for Exchange.
func (s *WebRTCSession) Offer(ctx context.Context) (string, error) {
	offer, err := s.pc.CreateOffer(nil)
	if err != nil {
		return "", fmt.Errorf("create offer: %w", err)
	}
	gathered := webrtc.GatheringCompletePromise(s.pc)
	if err := s.pc.SetLocalDescription(offer); err != nil {
		return "", fmt.Errorf("set local offer: %w", err)
	}
	select {
	case <-gathered:
	case <-ctx.Done():
		return "", ctx.Err()
	case <-time.After(15 * time.Second):
		return "", fmt.Errorf("ice gathering did not complete")
	}
	return s.pc.LocalDescription().SDP, nil
}

// Answer creates an SDP answer, waits for gathering, and stores the offer for
// Exchange.
func (s *WebRTCSession) Answer(ctx context.Context, offerSDP string) (string, error) {
	offer := webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: offerSDP}
	if err := s.pc.SetRemoteDescription(offer); err != nil {
		return "", fmt.Errorf("set remote offer: %w", err)
	}
	s.remoteSDP.set(offerSDP)

	answer, err := s.pc.CreateAnswer(nil)
	if err != nil {
		return "", fmt.Errorf("create answer: %w", err)
	}
	gathered := webrtc.GatheringCompletePromise(s.pc)
	if err := s.pc.SetLocalDescription(answer); err != nil {
		return "", fmt.Errorf("set local answer: %w", err)
	}
	select {
	case <-gathered:
	case <-ctx.Done():
		return "", ctx.Err()
	case <-time.After(15 * time.Second):
		return "", fmt.Errorf("ice gathering did not complete")
	}
	return s.pc.LocalDescription().SDP, nil
}

// Accept applies a remote answer or offer. Passing an offer back to the offerer
// is how a glare-avoidance renegotiation is completed.
func (s *WebRTCSession) Accept(remoteSDP string) error {
	if remoteSDP == "" {
		return fmt.Errorf("empty remote description")
	}
	desc := webrtc.SessionDescription{}
	if err := json.Unmarshal([]byte(remoteSDP), &desc); err == nil && desc.SDP != "" {
		// A JSON-wrapped description, used when a candidate bundle is attached.
		return s.pc.SetRemoteDescription(desc)
	}
	// Otherwise treat it as a bare SDP. Its type is inferred from our role.
	kind := webrtc.SDPTypeAnswer
	if s.role == "offerer" {
		kind = webrtc.SDPTypeAnswer
	} else {
		kind = webrtc.SDPTypeAnswer
	}
	return s.pc.SetRemoteDescription(webrtc.SessionDescription{Type: kind, SDP: remoteSDP})
}

// WaitConnected blocks until ICE and DTLS finish, or the context expires.
func (s *WebRTCSession) WaitConnected(ctx context.Context) error {
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		switch s.pc.ConnectionState() {
		case webrtc.PeerConnectionStateConnected:
			return nil
		case webrtc.PeerConnectionStateFailed, webrtc.PeerConnectionStateClosed:
			return fmt.Errorf("peer connection %s", s.pc.ConnectionState())
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
	return fmt.Errorf("peer connection did not reach connected; state is %s", s.pc.ConnectionState())
}

// CreateDataChannel opens an ordered, reliable SCTP channel.
func (s *WebRTCSession) CreateDataChannel(label string) (*webrtc.DataChannel, error) {
	dc, err := s.pc.CreateDataChannel(label, &webrtc.DataChannelInit{Ordered: nil})
	if err != nil {
		return nil, fmt.Errorf("create data channel %q: %w", label, err)
	}
	s.mu.Lock()
	s.dataChans[label] = dc
	s.mu.Unlock()
	return dc, nil
}

// AcceptDataChannel installs a handler for channels opened by the peer.
func (s *WebRTCSession) AcceptDataChannel(h func(*webrtc.DataChannel)) {
	s.pc.OnDataChannel(h)
}

// DecodeInboundPacket decodes an Opus packet received on a media track.
//
// This is the receive path, and it is a real decode: an Opus packet off the
// wire becomes PCM through libopus.
func (s *WebRTCSession) DecodeInboundPacket(packet []byte) ([]int16, error) {
	s.mu.Lock()
	dec := s.decoder
	s.mu.Unlock()
	if dec == nil {
		return nil, fmt.Errorf("no decoder available")
	}
	return dec.DecodeToPCM(packet)
}

// SendPCM is not supported: this build has no Opus encoder.
//
// It returns ErrNoOpusEncoder rather than transmitting raw PCM under an Opus
// payload type, which a browser would decode as noise. A node in a call receives
// media - the browser on the far end does the encoding.
func (s *WebRTCSession) SendPCM(pcm []int16) error {
	return ErrNoOpusEncoder
}

// Close tears the session down.
func (s *WebRTCSession) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	dec := s.decoder
	s.decoder = nil
	s.mu.Unlock()

	if dec != nil {
		_ = dec.Close()
	}
	return s.pc.Close()
}
