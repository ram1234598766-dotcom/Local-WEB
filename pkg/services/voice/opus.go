package voice

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"math"
	"sync"

	"github.com/pion/opus"
	"github.com/pion/webrtc/v4"
)

// Opus constants for the media path.
//
// The voice package previously declared CodecOpus as a constant and had no codec
// behind it: tracks carried metadata and no audio bytes ever moved. What exists
// now is a real Opus *decoder* from github.com/pion/opus, a pure-Go libopus port.
//
// There is deliberately no encoder here. The pure-Go port is decoder-only, and
// the encoder needs cgo against libopus, which would break the CLI build
// (CGO_ENABLED=0) and every cgo-free cross-compile in CI. Emitting raw PCM under
// an Opus payload type would be worse than useless: a browser would decode it as
// Opus and produce noise. SendPCM therefore reports that no encoder is
// available instead of sending something wrong.
//
// A node in a call mainly receives: the browser on the other end is what encodes.
// Encoding from this node needs a libopus build, tracked as an open item.
const (
	OpusSampleRate = 48000
	OpusChannels   = 2
	OpusFrameMs    = 20
	OpusFrameSize  = OpusSampleRate * OpusFrameMs / 1000 // 960 samples
	OpusMaxPacket  = 1275                                // libopus per-packet ceiling
	OpusPayloadTyp = 111                                 // RFC 7587 Opus payload type
)

// ErrNoOpusEncoder is returned by SendPCM. It is a specific error so a caller
// can tell "no encoder linked" apart from a transport failure.
var ErrNoOpusEncoder = fmt.Errorf("no Opus encoder is linked into this build")

// defaultOpusBitrate is what the encoder is opened at. 32 kbps is a reasonable
// voice bitrate in opus' own recommendation range (6-510 kbps) for VOIP.
const defaultOpusBitrate = 32000

// newSSRC picks a synchronisation source for an outgoing track. The value only
// has to be distinct within an RTP session, not unpredictable.
func newSSRC() webrtc.SSRC {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return 1
	}
	ssrc := webrtc.SSRC(binary.BigEndian.Uint32(b[:]))
	if ssrc == 0 {
		return 1
	}
	return ssrc
}

// OpusDecoder decodes Opus packets to PCM.
//
// It is safe for concurrent use: libopus decoder state is not reentrant, so
// calls are serialised.
type OpusDecoder struct {
	mu     sync.Mutex
	dec    opus.Decoder
	closed bool
}

// NewOpusDecoder creates a decoder producing 48 kHz stereo PCM.
func NewOpusDecoder() (*OpusDecoder, error) {
	dec := opus.NewDecoder()
	if err := dec.Init(OpusSampleRate, OpusChannels); err != nil {
		return nil, fmt.Errorf("opus decoder init: %w", err)
	}
	return &OpusDecoder{dec: dec}, nil
}

// DecodeToPCM decompresses one Opus packet into interleaved stereo samples.
//
// The returned slice is exactly one frame for a normal 20 ms packet; a packet
// that decodes to fewer samples is returned as decoded rather than padded,
// because padding would misreport the caller's buffer alignment.
func (d *OpusDecoder) DecodeToPCM(packet []byte) ([]int16, error) {
	if len(packet) == 0 {
		return nil, fmt.Errorf("empty opus packet")
	}
	if len(packet) > OpusMaxPacket {
		return nil, fmt.Errorf("opus packet of %d bytes exceeds the %d byte limit", len(packet), OpusMaxPacket)
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return nil, fmt.Errorf("opus decoder is closed")
	}

	// Two frames of headroom: a packet may legitimately decode to more than one
	// frame's worth at some bandwidths.
	out := make([]int16, OpusFrameSize*OpusChannels*2)
	n, err := d.dec.DecodeToInt16(packet, out)
	if err != nil {
		return nil, fmt.Errorf("opus decode: %w", err)
	}
	return out[:n], nil
}

// Close releases the decoder.
func (d *OpusDecoder) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.closed = true
	return nil
}

// FrameCount reports how many 20 ms frames a sample buffer represents.
func FrameCount(samples []int16) int {
	return len(samples) / (OpusFrameSize * OpusChannels)
}

// TonePCM builds a deterministic stereo sine wave, so a codec result can be
// checked for carrying signal rather than merely for not erroring.
//
// frames counts 20 ms frames, so TonePCM(1, 440) returns exactly one frame's
// worth of interleaved samples - which is what the Opus encoder requires.
//
// This used to allocate frames*OpusChannels samples, so TonePCM(1, 440) returned
// two samples rather than one frame, and the sine never swung: the phase was
// sampled once per stereo pair. The decoder test passed OpusFrameSize as the
// frame count to compensate, which hid it. The encoder rejects any frame that
// is not exactly OpusFrameSize*OpusChannels samples, so the two cancelled.
func TonePCM(frames, freq int) []int16 {
	total := frames * OpusFrameSize * OpusChannels
	out := make([]int16, total)
	for i := 0; i < frames*OpusFrameSize; i++ {
		v := int16(12000 * math.Sin(2*math.Pi*float64(freq*i)/float64(OpusSampleRate)))
		out[i*2] = v
		out[i*2+1] = v
	}
	return out
}

// EnergyRMS returns the root-mean-square amplitude of a PCM buffer, used to
// assert that a decoded frame carries signal rather than silence.
func EnergyRMS(pcm []int16) float64 {
	if len(pcm) == 0 {
		return 0
	}
	var sum float64
	for _, s := range pcm {
		f := float64(s)
		sum += f * f
	}
	return math.Sqrt(sum / float64(len(pcm)))
}

// opusFrameDuration is the RTP timestamp increment for one 20 ms frame, in the
// 90 kHz clock units RFC 3550 specifies for audio.
const opusFrameDuration = OpusSampleRate / 1000 * OpusFrameMs // 960

// RTPTimebase converts a frame count to an RTP timestamp.
func RTPTimebase(frames uint32) uint32 { return frames * opusFrameDuration }
