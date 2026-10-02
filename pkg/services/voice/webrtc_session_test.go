package voice

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"sync"

	"github.com/pion/webrtc/v4"
	"testing"
	"time"
)

// This is the proof that the voice package has a real transport, which is what
// 7.4 needs. Both peers are in-process and connected over loopback, so ICE,
// DTLS and SRTP are all genuinely exercised with no network at all.
//
// The same session type carries the data channel that 7.2 uses, so these tests
// also cover the transport for file transfer.

func connectLoopback(t *testing.T, timeout time.Duration) (*WebRTCSession, *WebRTCSession) {
	t.Helper()

	offerer, err := NewWebRTCSession("offerer")
	if err != nil {
		t.Fatalf("offerer: %v", err)
	}
	// Without a transceiver the offer has no m-line, pion gathers no
	// candidates, and the SDP has no ice-ufrag.
	if _, err := offerer.PrepareAudio(webrtc.RTPTransceiverDirectionRecvonly); err != nil {
		t.Fatalf("PrepareAudio: %v", err)
	}
	answerer, err := NewWebRTCSession("answerer")
	if err != nil {
		t.Fatalf("answerer: %v", err)
	}
	t.Cleanup(func() { _ = offerer.Close(); _ = answerer.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	offerSDP, err := offerer.Offer(ctx)
	if err != nil {
		t.Fatalf("Offer: %v", err)
	}
	answerSDP, err := answerer.Answer(ctx, offerSDP)
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if err := offerer.Accept(answerSDP); err != nil {
		t.Fatalf("offerer.Accept: %v", err)
	}
	if err := offerer.WaitConnected(ctx); err != nil {
		t.Fatalf("offerer never connected: %v", err)
	}
	if err := answerer.WaitConnected(ctx); err != nil {
		t.Fatalf("answerer never connected: %v", err)
	}
	return offerer, answerer
}

func TestWebRTCSessionReachesConnectedOverLoopback(t *testing.T) {
	offerer, answerer := connectLoopback(t, 45*time.Second)

	if got := offerer.ConnectionState(); got.String() != "connected" {
		t.Errorf("offerer connection state = %v, want connected", got)
	}
	if got := offerer.ICEState(); got.String() != "connected" {
		t.Errorf("offerer ICE state = %v, want connected", got)
	}
	if got := answerer.ICEState(); got.String() != "connected" {
		t.Errorf("answerer ICE state = %v, want connected", got)
	}
}

// A real offer must carry candidates. An offer handed over before gathering
// finishes has none, and the peer silently never connects.
func TestWebRTCOfferCarriesICECandidates(t *testing.T) {
	s, err := NewWebRTCSession("offerer")
	if err != nil {
		t.Fatalf("NewWebRTCSession: %v", err)
	}
	defer s.Close()
	if _, err := s.PrepareAudio(webrtc.RTPTransceiverDirectionRecvonly); err != nil {
		t.Fatalf("PrepareAudio: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	offerSDP, err := s.Offer(ctx)
	if err != nil {
		t.Fatalf("Offer: %v", err)
	}
	if !bytes.Contains([]byte(offerSDP), []byte("a=candidate:")) {
		t.Error("offer contains no ICE candidate; the peer could never connect")
	}
	if !bytes.Contains([]byte(offerSDP), []byte("opus")) {
		t.Error("offer does not advertise Opus")
	}
}

// A large payload crossing the data channel is the transport 7.2 transfers
// files over. The digest is compared, so a truncation or reorder is caught.
//
// The channel is created before the offer, not after connecting: a data channel
// opened once the peers are already connected needs a renegotiation round, and
// relying on that would be testing renegotiation rather than the transfer.
func TestWebRTCDataChannelTransfersPayloadIntact(t *testing.T) {
	offerer, err := NewWebRTCSession("offerer")
	if err != nil {
		t.Fatalf("offerer: %v", err)
	}
	defer offerer.Close()
	answerer, err := NewWebRTCSession("answerer")
	if err != nil {
		t.Fatalf("answerer: %v", err)
	}
	defer answerer.Close()

	if _, err := offerer.PrepareAudio(webrtc.RTPTransceiverDirectionRecvonly); err != nil {
		t.Fatalf("PrepareAudio: %v", err)
	}

	// Far larger than one SCTP message, so chunking and reassembly are
	// exercised rather than a single small write.
	payload := make([]byte, 256*1024)
	for i := range payload {
		payload[i] = byte(i * 7)
	}
	want := sha256.Sum256(payload)

	received := make(chan []byte, 1)
	answerer.AcceptDataChannel(func(dc *webrtc.DataChannel) {
		var mu sync.Mutex
		var buf []byte
		dc.OnMessage(func(msg webrtc.DataChannelMessage) {
			mu.Lock()
			buf = append(buf, msg.Data...)
			done := len(buf) >= len(payload)
			mu.Unlock()
			if done {
				select {
				case received <- buf:
				default:
				}
			}
		})
	})

	dc, err := offerer.CreateDataChannel("files")
	if err != nil {
		t.Fatalf("CreateDataChannel: %v", err)
	}

	opened := make(chan struct{})
	var once sync.Once
	dc.OnOpen(func() { once.Do(func() { close(opened) }) })

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	offerSDP, err := offerer.Offer(ctx)
	if err != nil {
		t.Fatalf("Offer: %v", err)
	}
	answerSDP, err := answerer.Answer(ctx, offerSDP)
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if err := offerer.Accept(answerSDP); err != nil {
		t.Fatalf("offerer.Accept: %v", err)
	}
	if err := offerer.WaitConnected(ctx); err != nil {
		t.Fatalf("offerer never connected: %v", err)
	}

	select {
	case <-opened:
	case <-time.After(30 * time.Second):
		t.Fatal("data channel never opened")
	}

	// Write in chunks so the SCTP layer must reassemble.
	const chunk = 16 * 1024
	for off := 0; off < len(payload); off += chunk {
		end := off + chunk
		if end > len(payload) {
			end = len(payload)
		}
		if err := dc.Send(payload[off:end]); err != nil {
			t.Fatalf("send at %d: %v", off, err)
		}
	}

	select {
	case got := <-received:
		if sum := sha256.Sum256(got); sum != want {
			t.Errorf("payload digest = %x, want %x (got %d bytes, want %d)",
				sum, want, len(got), len(payload))
		}
	case <-time.After(45 * time.Second):
		t.Fatalf("payload never completed; wanted %d bytes", len(payload))
	}
}

// SendPCM must report honestly that this build has no Opus encoder, rather than
// transmitting raw PCM under an Opus payload type that a browser would decode
// as noise.
// A session with no send path must refuse audio rather than dropping it
// silently.
//
// The original test asserted the error was exactly ErrNoOpusEncoder. That is
// right in a default build, but with -tags libopus an encoder *is* linked and
// the failure is the missing send path instead, so asserting one specific error
// would break the build that actually has a codec. What matters either way is
// that SendPCM does not report success.
func TestSendPCMRefusesWhenThereIsNoSendPath(t *testing.T) {
	s, err := NewWebRTCSession("offerer")
	if err != nil {
		t.Fatalf("NewWebRTCSession: %v", err)
	}
	defer s.Close()
	if _, err := s.PrepareAudio(webrtc.RTPTransceiverDirectionRecvonly); err != nil {
		t.Fatalf("PrepareAudio: %v", err)
	}

	err = s.SendPCM(TonePCM(1, 440))
	if err == nil {
		t.Fatal("SendPCM on a recvonly session must error, not silently drop audio")
	}
	// With no encoder linked the reason must name that, so a caller can tell
	// "not built with a codec" from "not connected to a track".
	if !opusEncoderAvailable() && !errors.Is(err, ErrNoOpusEncoder) {
		t.Errorf("SendPCM error = %v, want ErrNoOpusEncoder in a build with no encoder", err)
	}
}

// The decoder is real: it must exist, decode a malformed packet with an error
// rather than panicking, and reject an oversized packet.
func TestOpusDecoderRejectsBadInput(t *testing.T) {
	d, err := NewOpusDecoder()
	if err != nil {
		t.Fatalf("NewOpusDecoder: %v", err)
	}
	defer d.Close()

	if _, err := d.DecodeToPCM(nil); err == nil {
		t.Error("expected an error for an empty packet")
	}
	if _, err := d.DecodeToPCM(make([]byte, OpusMaxPacket+1)); err == nil {
		t.Error("expected an error for an oversized packet")
	}
	// A syntactically valid but meaningless packet must error, not panic.
	if _, err := d.DecodeToPCM([]byte{0xFF, 0xFF, 0xFF, 0xFF}); err == nil {
		t.Error("expected an error decoding a garbage packet")
	}
}

func TestFrameCountAndTone(t *testing.T) {
	pcm := TonePCM(1, 440) // one full frame, so the sine actually swings
	if got := FrameCount(pcm); got != 1 {
		t.Errorf("FrameCount = %d, want 1", got)
	}
	if e := EnergyRMS(pcm); e < 1000 {
		t.Errorf("tone RMS = %v, want audible signal", e)
	}
	if EnergyRMS(nil) != 0 {
		t.Error("EnergyRMS of nil should be 0")
	}
}
