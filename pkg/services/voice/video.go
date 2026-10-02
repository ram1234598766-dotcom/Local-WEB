package voice

import (
	"errors"
	"fmt"
	"strings"
)

// VP8 and VP9 RTP payload types. VP8 is RFC 7741; VP9 is the payload
// specification draft. Both are static payload types in the WebRTC registry
// range, so they are safe to hard-code.
const (
	VP8PayloadTyp  = 96
	VP9PayloadTyp  = 98
	VP8MimeType    = "video/vp8"
	VP9MimeType    = "video/vp9"
	VP8DefaultRate = 30
)

// VP8DefaultBitrate is the default VP8 bitrate in kbps.
const VP8DefaultBitrate = 2500

// ErrNoVPXEncoder is returned when a frame is handed to a build with no VP8 or
// VP9 encoder linked. It is a specific error so a caller can tell "no encoder
// linked" apart from a transport failure.
var ErrNoVPXEncoder = errors.New("no VP8/VP9 encoder is linked into this build")

// VideoPacket is one encoded video frame, before RTP packetisation.
//
// This is the video counterpart of an Opus payload: the encoder produces frames
// and a packetiser turns a frame into the RTP packets that actually go on the
// wire. Keeping the two steps separate is what lets the packetiser be tested
// without a native encoder and the encoder without a network.
type VideoPacket struct {
	Codec CodecID
	// Data is the compressed bitstream as libvpx produced it.
	Data     []byte
	Keyframe bool
	Width    uint32
	Height   uint32
	// Timestamp is in 90kHz units, the RTP video clock.
	Timestamp uint32
	// SSID is the synchronisation source for this packet.
	SSID uint32
}

// PayloadType returns the static RTP payload type for the packet's codec.
func (p *VideoPacket) PayloadType() uint8 {
	if p.Codec == CodecVP9 {
		return VP9PayloadTyp
	}
	return VP8PayloadTyp
}

// MimeType returns the SDP mime type for the packet's codec.
func (p *VideoPacket) MimeType() string {
	if p.Codec == CodecVP9 {
		return VP9MimeType
	}
	return VP8MimeType
}

// OpusEncoderLinked reports whether this build can encode Opus.
//
// A default build links the pure-Go decoder only, so it can take part in a call as
// a receiver but cannot send audio. Callers that report capability to an operator
// or a UI need to distinguish "no encoder" from "no microphone", because only the
// first is fixable by rebuilding.
func OpusEncoderLinked() bool { return opusEncoderAvailable() }

// VPXEncoderLinked reports whether this build can encode VP8 and VP9 video.
func VPXEncoderLinked() bool { return vpxEncoderAvailable() }

// String implements fmt.Stringer.
func (p *VideoPacket) String() string {
	var b strings.Builder
	b.WriteString(p.Codec.String())
	fmt.Fprintf(&b, " %dx%d keyframe=%v %d bytes ts=%d", p.Width, p.Height, p.Keyframe, len(p.Data), p.Timestamp)
	return b.String()
}
