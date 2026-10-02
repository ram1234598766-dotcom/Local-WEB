package voice

import (
	"testing"

	"github.com/ram1234598766-dotcom/Local-WEB/pkg/crypto"
	"github.com/stretchr/testify/require"
)

func TestCodecProfiles(t *testing.T) {
	profiles := SupportedCodecs()
	// Opus, VP8 and VP9 are all implemented, so all three are advertised. This
	// used to be 2 with only VP9 listed, which claimed support for a codec
	// nothing could encode.
	require.Len(t, profiles, 3)

	// The order is preference order, so it is still asserted. The parameters are
	// then looked up by ID rather than by index, so adding a codec cannot silently
	// repoint a check at the wrong entry.
	require.Equal(t, CodecOpus, profiles[0].ID)
	require.Equal(t, CodecVP8, profiles[1].ID)
	require.Equal(t, CodecVP9, profiles[2].ID)

	byID := func(id CodecID) CodecProfile {
		for _, p := range profiles {
			if p.ID == id {
				return p
			}
		}
		t.Fatalf("codec %s missing from SupportedCodecs", id)
		return CodecProfile{}
	}

	opus := byID(CodecOpus)
	require.Equal(t, uint32(48000), opus.SampleRate)
	require.Equal(t, "audio/opus", opus.MimeType)
	require.Equal(t, "opus", opus.Name)

	vp8 := byID(CodecVP8)
	require.Equal(t, VP8MimeType, vp8.MimeType)
	require.Equal(t, uint32(640), vp8.Width)
	require.Equal(t, uint32(480), vp8.Height)
	require.Equal(t, uint32(VP8DefaultBitrate), vp8.Bitrate)

	vp9 := byID(CodecVP9)
	require.Equal(t, VP9MimeType, vp9.MimeType)
	require.Equal(t, uint32(1280), vp9.Width)
	require.Equal(t, uint32(720), vp9.Height)
}

func TestCodecProfileForID(t *testing.T) {
	p, ok := CodecProfileForID(CodecOpus)
	require.True(t, ok)
	require.Equal(t, "opus", p.Name)

	_, ok = CodecProfileForID(CodecID(99))
	require.False(t, ok)
}

func TestNegotiateCodecsAudio(t *testing.T) {
	audio, video, err := NegotiateCodecs(
		[]CodecID{CodecOpus},
		[]CodecID{CodecOpus},
		[]CodecID{CodecVP9},
		[]CodecID{CodecVP9},
	)
	require.NoError(t, err)
	require.Equal(t, CodecOpus, audio)
	require.Equal(t, CodecVP9, video)
}

func TestNegotiateCodecsFallback(t *testing.T) {
	// Only audio on remote; video should be unavailable (not an error).
	audio, video, err := NegotiateCodecs(
		[]CodecID{CodecOpus, CodecVP9},
		[]CodecID{CodecOpus},
		[]CodecID{CodecVP9},
		[]CodecID{},
	)
	require.NoError(t, err)
	require.Equal(t, CodecOpus, audio)
	require.Equal(t, CodecID(0), video)
}

func TestNegotiateCodecsOrder(t *testing.T) {
	// Remote prefers VP9 but local only has Opus for audio.
	audio, _, err := NegotiateCodecs(
		[]CodecID{CodecOpus},
		[]CodecID{CodecOpus},
		[]CodecID{},
		[]CodecID{CodecVP9},
	)
	require.NoError(t, err)
	require.Equal(t, CodecOpus, audio)
}

func TestAdaptiveBitrateReducesOnLoss(t *testing.T) {
	current := uint32(VP9DefaultBitrate)
	reduced := AdaptiveBitrate(250, 0.06, current)
	require.Less(t, reduced, current)
	require.True(t, reduced >= 100, "bitrate should not drop below minimum")
}

func TestAdaptiveBitrateIncreasesOnGood(t *testing.T) {
	low := uint32(500)
	increased := AdaptiveBitrate(30, 0.005, low)
	require.Greater(t, increased, low)
}

func TestAdaptiveBitrateClamps(t *testing.T) {
	clamped := AdaptiveBitrate(30, 0.005, VP9DefaultBitrate)
	require.LessOrEqual(t, clamped, uint32(10000))
	require.GreaterOrEqual(t, clamped, uint32(100))
}

func TestTrackManagerAddRemove(t *testing.T) {
	mgr := NewTrackManager()
	require.NoError(t, mgr.Add(TrackInfo{ID: "t1", Kind: TrackKindAudio, Direction: TrackDirectionSendRecv, Codec: CodecOpus, MimeType: "audio/opus"}))

	tracks := mgr.List()
	require.Len(t, tracks, 1)
	require.Equal(t, "t1", tracks[0].ID)

	require.NoError(t, mgr.Remove("t1"))
	require.Empty(t, mgr.List())
}

func TestTrackManagerAddDuplicate(t *testing.T) {
	mgr := NewTrackManager()
	require.NoError(t, mgr.Add(TrackInfo{ID: "t1", Kind: TrackKindAudio, Direction: TrackDirectionSendRecv, Codec: CodecOpus, MimeType: "audio/opus"}))
	require.Error(t, mgr.Add(TrackInfo{ID: "t1", Kind: TrackKindAudio, Direction: TrackDirectionSendRecv, Codec: CodecOpus, MimeType: "audio/opus"}))
}

func TestTrackManagerGetNotFound(t *testing.T) {
	mgr := NewTrackManager()
	_, err := mgr.Get("missing")
	require.Error(t, err)
}

func TestTrackStop(t *testing.T) {
	mgr := NewTrackManager()
	require.NoError(t, mgr.Add(TrackInfo{ID: "t1", Kind: TrackKindAudio, Direction: TrackDirectionSendRecv, Codec: CodecOpus, MimeType: "audio/opus"}))
	track, err := mgr.Get("t1")
	require.NoError(t, err)
	require.True(t, track.IsActive())

	mgr.StopAll()
	track, _ = mgr.Get("t1")
	require.False(t, track.IsActive())
}

func TestTrackInfoHelpers(t *testing.T) {
	audio := TrackInfo{Kind: TrackKindAudio}
	require.True(t, audio.IsAudio())
	require.False(t, audio.IsVideo())
	require.False(t, audio.IsData())

	video := TrackInfo{Kind: TrackKindVideo}
	require.False(t, video.IsAudio())
	require.True(t, video.IsVideo())
	require.False(t, video.IsData())

	screen := TrackInfo{Kind: TrackKindScreen}
	require.True(t, screen.IsVideo())
	require.True(t, screen.IsScreen())
	require.False(t, screen.IsAudio())

	data := TrackInfo{Kind: TrackKindData}
	require.True(t, data.IsData())
}

func TestTrackIDDeterministic(t *testing.T) {
	pub, _, _ := crypto.GenerateX25519KeyPair()
	peer := crypto.NodeID(pub)
	id1 := TrackID(TrackKindAudio, peer)
	id2 := TrackID(TrackKindAudio, peer)
	require.Equal(t, id1, id2)
}
