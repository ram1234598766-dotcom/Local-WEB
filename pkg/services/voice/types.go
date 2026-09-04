package voice

import "time"

// CallID uniquely identifies a voice/video call.
type CallID [16]byte

// PeerID is a 32-byte node identity (matches transport/crypto).
type PeerID = [32]byte

// CodecID identifies a supported audio/video codec.
type CodecID uint8

const (
	CodecOpus CodecID = iota + 1 // Opus audio
	CodecVP9                      // VP9 video
)

// MediaType classifies a media payload.
type MediaType uint8

const (
	MediaTypeAudio MediaType = iota + 1
	MediaTypeVideo
	MediaTypeData
)

// CallState tracks the lifecycle of a call.
type CallState uint8

const (
	CallStateIdle CallState = iota
	CallStateCalling
	CallStateRinging
	CallStateConnected
	CallStateEnded
)

// String returns the human-readable name of the state.
func (s CallState) String() string {
	switch s {
	case CallStateIdle:
		return "idle"
	case CallStateCalling:
		return "calling"
	case CallStateRinging:
		return "ringing"
	case CallStateConnected:
		return "connected"
	case CallStateEnded:
		return "ended"
	}
	return "unknown"
}

// CandidateType classifies an ICE-like network candidate.
type CandidateType uint8

const (
	CandidateHost             CandidateType = iota // host
	CandidateServerReflexive                       // srflx
	CandidateRelay                                 // relay
)

// ICECandidate describes a network candidate for peer connectivity.
type ICECandidate struct {
	ID        string
	Type      CandidateType
	Address   string
	Port      uint16
	Protocol  string // "udp" | "tcp"
	Priority  uint32
	SDPFormat string
}

// TrackDirection indicates the flow direction of a media track.
type TrackDirection uint8

const (
	TrackDirectionSend    TrackDirection = iota + 1
	TrackDirectionRecv
	TrackDirectionSendRecv
)

// TrackKind identifies the kind of media carried by a track.
type TrackKind uint8

const (
	TrackKindAudio TrackKind = iota + 1
	TrackKindVideo
	TrackKindScreen
	TrackKindData
)

// TrackInfo describes a negotiated media track.
type TrackInfo struct {
	ID        string
	Kind      TrackKind
	Direction TrackDirection
	Codec     CodecID
	MimeType  string
	SampleRate uint32 // audio: Hz; video: ignored
	FrameRate  uint32 // video: fps; audio: ignored
	Width      uint32 // video only
	Height     uint32 // video only
	Bitrate    uint32 // kbps target
}

// IsAudio reports whether the track carries audio.
func (t TrackInfo) IsAudio() bool { return t.Kind == TrackKindAudio }

// IsVideo reports whether the track carries video (includes screen share).
func (t TrackInfo) IsVideo() bool { return t.Kind == TrackKindVideo || t.Kind == TrackKindScreen }

// IsScreen reports whether the track carries a screen share.
func (t TrackInfo) IsScreen() bool { return t.Kind == TrackKindScreen }

// IsData reports whether the track carries application data.
func (t TrackInfo) IsData() bool { return t.Kind == TrackKindData }

// CallConfig holds the desired parameters for a call.
type CallConfig struct {
	Caller      PeerID
	Callee      PeerID
	ChannelID   string // messaging channel for signaling
	AudioCodec  CodecID
	VideoCodec  CodecID
	EnableVideo bool
	EnableData  bool
}

// MediaStats carries per-track quality metrics.
type MediaStats struct {
	TrackID      string
	BytesSent    uint64
	BytesRecv    uint64
	PacketsLost  uint32
	Jitter       float64 // ms
	RTT          float64 // ms
	Bitrate      uint32  // kbps current
	LastUpdate   time.Time
}

// SignalType identifies a signaling message.
type SignalType uint8

const (
	SignalTypeOffer  SignalType = iota + 1
	SignalTypeAnswer
	SignalTypeICECandidate
	SignalTypeBye
	SignalTypeGroupInvite
)

// SignalMessage is a signaling payload exchanged via messaging.
type SignalMessage struct {
	Type      SignalType
	CallID    CallID
	Sender    PeerID
	Timestamp int64
	Payload   []byte
}

// GroupCallRole identifies the star-topology role of a peer.
type GroupCallRole uint8

const (
	GroupRoleHub   GroupCallRole = iota + 1 // hub
	GroupRolePeer                            // leaf
)

// GroupCallConfig describes a multi-party call.
type GroupCallConfig struct {
	ChannelID string
	Peers     []PeerID
	EnableVideo bool
	EnableData  bool
}
