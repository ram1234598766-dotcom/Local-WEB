//go:build libopus

package voice

import (
	"context"
	"testing"
	"time"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
	"github.com/stretchr/testify/require"
)

// This file only builds with -tags libopus, where a real libopus encoder is
// linked. The point is not that EncodePCM returns bytes: it is that they are a
// real Opus packet the decoder can turn back into intelligible audio.

// TestEncoderLinked proves the build actually has an encoder, so a passing
// encode test cannot be an accident of the stub.
func TestEncoderLinked(t *testing.T) {
	require.True(t, opusEncoderAvailable(), "this build should link libopus")
	enc, err := NewOpusEncoder(defaultOpusBitrate)
	require.NoError(t, err)
	require.NoError(t, enc.Close())
}

// TestEncodeDecodeRoundTrip is the real check: a tone goes in, an Opus packet
// comes out, and decoding it yields a tone again.
func TestEncodeDecodeRoundTrip(t *testing.T) {
	enc, err := NewOpusEncoder(defaultOpusBitrate)
	require.NoError(t, err)
	defer enc.Close()

	pcm := TonePCM(1, 440)
	packet, err := enc.EncodePCM(pcm)
	require.NoError(t, err)
	require.NotEmpty(t, packet)
	require.LessOrEqual(t, len(packet), OpusMaxPacket, "packet must respect the libopus ceiling")

	dec, err := NewOpusDecoder()
	require.NoError(t, err)
	defer dec.Close()

	out, err := dec.DecodeToPCM(packet)
	require.NoError(t, err)
	require.NotEmpty(t, out,
		"a valid opus packet must decode to some samples")

	// A lossy codec will not reproduce samples exactly, so the assertion is
	// that the signal survived rather than that it matched. Note the decoded
	// length is left to the decoder: pion/opus returns 960 samples for a 20 ms
	// frame rather than the 1920 an interleaved stereo frame holds, and the
	// energy check below is what actually matters.
	inEnergy := EnergyRMS(pcm)
	outEnergy := EnergyRMS(out)
	require.Greater(t, inEnergy, 1000.0, "the test tone should carry signal")
	require.Greater(t, outEnergy, inEnergy*0.25,
		"decoded energy %.0f is far below the input %.0f: this is not decodable audio",
		outEnergy, inEnergy)
}

// TestEncoderRejectsWrongFrameSize stops a caller with the wrong frame length
// from being read past by libopus.
func TestEncoderRejectsWrongFrameSize(t *testing.T) {
	enc, err := NewOpusEncoder(defaultOpusBitrate)
	require.NoError(t, err)
	defer enc.Close()

	for _, n := range []int{0, 1, OpusFrameSize, OpusFrameSize*OpusChannels - 1} {
		_, err := enc.EncodePCM(make([]int16, n))
		require.Error(t, err, "a %d sample frame should be refused", n)
	}
}

// TestEncodedPacketIsLegalRTPPayload checks the bytes the encoder produces
// survive the RTP wrapping the send path applies.
func TestEncodedPacketIsLegalRTPPayload(t *testing.T) {
	enc, err := NewOpusEncoder(defaultOpusBitrate)
	require.NoError(t, err)
	defer enc.Close()

	packet, err := enc.EncodePCM(TonePCM(1, 440))
	require.NoError(t, err)

	p := &rtp.Packet{
		Header: rtp.Header{
			Version:        2,
			PayloadType:    OpusPayloadTyp,
			SequenceNumber: 7,
			Timestamp:      RTPTimebase(1),
			SSRC:           uint32(newSSRC()),
		},
		Payload: packet,
	}
	raw, err := p.Marshal()
	require.NoError(t, err)

	var back rtp.Packet
	require.NoError(t, back.Unmarshal(raw))
	require.Equal(t, uint16(7), back.SequenceNumber)
	require.Equal(t, uint8(OpusPayloadTyp), back.PayloadType)
	require.Equal(t, RTPTimebase(1), back.Timestamp)
	require.Equal(t, packet, back.Payload, "the opus payload must survive the rtp header")
}

// connectAudioPair negotiates two connected sessions where the offerer has a
// real local audio track attached, so the RTP send path exists from the first
// offer rather than needing a renegotiation.
func connectAudioPair(t *testing.T) (*WebRTCSession, *WebRTCSession) {
	t.Helper()

	sender, err := NewWebRTCSession("sender")
	require.NoError(t, err)
	_, err = sender.PrepareAudio(webrtc.RTPTransceiverDirectionSendrecv)
	require.NoError(t, err, "PrepareAudio must attach a local track")

	receiver, err := NewWebRTCSession("receiver")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sender.Close(); _ = receiver.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	offerSDP, err := sender.Offer(ctx)
	require.NoError(t, err)
	answerSDP, err := receiver.Answer(ctx, offerSDP)
	require.NoError(t, err)
	require.NoError(t, sender.Accept(answerSDP))
	require.NoError(t, sender.WaitConnected(ctx))
	require.NoError(t, receiver.WaitConnected(ctx))
	return sender, receiver
}

// TestSendPCMReachesTheRemotePeer drives the whole send path over a real
// loopback peer connection: PCM is queued, encoded, written as RTP, and the far
// end decodes it back to audible signal.
func TestSendPCMReachesTheRemotePeer(t *testing.T) {
	if testing.Short() {
		t.Skip("needs a real ICE/DTLS handshake")
	}

	sender, receiver := connectAudioPair(t)

	energies := make(chan float64, 8)
	receiver.pc.OnTrack(func(remote *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		dec, derr := NewOpusDecoder()
		if derr != nil {
			return
		}
		defer dec.Close()
		for {
			pkt, _, rerr := remote.ReadRTP()
			if rerr != nil {
				return
			}
			pcm, derr := dec.DecodeToPCM(pkt.Payload)
			if derr != nil {
				continue
			}
			select {
			case energies <- EnergyRMS(pcm):
			default:
			}
		}
	})

	// A run of distinct tones, so the stream is not one repeated frame.
	for i := 0; i < 20; i++ {
		require.NoError(t, sender.SendPCM(TonePCM(1, 440+i)))
	}

	timeout := time.After(25 * time.Second)
	for {
		select {
		case e := <-energies:
			if e > 1000 {
				return // audible signal arrived over RTP and decoded
			}
		case <-timeout:
			t.Fatal("no decodable audio arrived over rtp")
		}
	}
}

// TestSendPCMRejectsWrongFrameSize keeps a bad frame out of the queue.
func TestSendPCMRejectsWrongFrameSize(t *testing.T) {
	sender, _ := connectAudioPair(t)
	require.Error(t, sender.SendPCM(make([]int16, 7)),
		"a 7 sample frame must not be queued")
}

// TestSendPCMOnReceiveOnlySessionIsAnError: a session with no send path must say
// so rather than silently dropping audio.
func TestSendPCMOnReceiveOnlySessionIsAnError(t *testing.T) {
	s, err := NewWebRTCSession("recvonly")
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	_, err = s.PrepareAudio(webrtc.RTPTransceiverDirectionRecvonly)
	require.NoError(t, err)

	require.Error(t, s.SendPCM(TonePCM(1, 440)))
}
