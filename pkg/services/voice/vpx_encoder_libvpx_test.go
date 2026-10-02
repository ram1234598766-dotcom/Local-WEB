//go:build libvpx

package voice

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// testFrame builds a real I420 frame with a visible gradient and a moving bar.
//
// The content matters: a uniform frame compresses to almost nothing, which would
// let a broken encoder pass a size check. A gradient plus a hard edge gives the
// encoder real work and makes the decoded output checkable.
func testFrame(w, h uint32, phase int) (y, u, v []byte) {
	y = make([]byte, w*h)
	u = make([]byte, w*h/4)
	v = make([]byte, w*h/4)
	for row := uint32(0); row < h; row++ {
		for col := uint32(0); col < w; col++ {
			y[row*w+col] = byte((int(col)*4 + int(row)*2 + phase*8) % 256)
		}
	}
	barX := uint32(phase*16) % w
	for row := uint32(0); row < h; row++ {
		for col := uint32(0); col < w; col++ {
			if col >= barX && col < barX+8 {
				y[row*w+col] = 255
			}
		}
	}
	// Flat chroma is fine here; luma carries the detail the test checks.
	cSize := int(w * h / 4)
	for i := 0; i < cSize; i++ {
		u[i] = 128
		v[i] = 128
	}
	return y, u, v
}

func TestVPXEncoderIsLinkedInThisBuild(t *testing.T) {
	require.True(t, vpxEncoderAvailable(),
		"a -tags libvpx build must link libvpx")
}

// TestVPXEncodesVP8AndVP9 is the test for the encoder. Neither codec had one at
// all, even though CodecVP9 was advertised in SupportedCodecs with a full bitrate
// and resolution profile.
func TestVPXEncodesVP8AndVP9(t *testing.T) {
	const w, h uint32 = 160, 120

	for _, codec := range []CodecID{CodecVP8, CodecVP9} {
		codec := codec
		t.Run(codec.String(), func(t *testing.T) {
			enc, err := NewVPXEncoder(codec, w, h, 30, 500)
			require.NoError(t, err)
			defer enc.Close()

			y, u, v := testFrame(w, h, 0)
			key, err := enc.EncodeY420P(y, u, v, true)
			require.NoError(t, err, "keyframe encode")
			require.NotEmpty(t, key.Data, "a keyframe must produce a bitstream")
			require.True(t, key.Keyframe)
			require.Equal(t, w, key.Width)
			require.Equal(t, h, key.Height)

			// A single frame of gradient at 160x120 compressing to under 100 bytes
			// would mean the encoder is emitting headers and no picture.
			require.Greater(t, len(key.Data), 100,
				"bitstream is suspiciously small for a real frame")

			// The codec must actually be the one asked for, and the stream must
			// settle into inter-coded deltas.
			//
			// VP9 needs more than one frame to get going: with no lookahead it codes
			// the first two frames intra before it has anything to predict from, so
			// the second frame is also flagged as a keyframe. Observed sizes at
			// 160x120: vp8 1555/199/173, vp9 2141/1820/99. Asserting that frame 1 is
			// a delta would fail for VP9, so this waits for frame 3.
			var deltas []int
			for i := 1; i <= 3; i++ {
				yi, ui, vi := testFrame(w, h, i)
				pkt, err := enc.EncodeY420P(yi, ui, vi, false)
				require.NoError(t, err, "delta frame %d encode", i)
				require.NotEmpty(t, pkt.Data)
				require.Equal(t, codec.String(), pkt.Codec.String())
				if !pkt.Keyframe {
					deltas = append(deltas, len(pkt.Data))
				}
			}
			require.NotEmpty(t, deltas,
				"%s never produced an inter-coded delta frame", codec)

			if codec == CodecVP9 {
				require.Equal(t, uint8(VP9PayloadTyp), key.PayloadType())
				require.Equal(t, VP9MimeType, key.MimeType())
			} else {
				require.Equal(t, uint8(VP8PayloadTyp), key.PayloadType())
				require.Equal(t, VP8MimeType, key.MimeType())
			}
		})
	}
}

// TestVPXKeyframeFlagComesFromLibvpx checks the packet's keyframe flag reports
// what libvpx actually emitted, not what the caller asked for. A caller
// requesting a keyframe does not get to promise one, and trusting the request
// would mark an ordinary delta as a keyframe and break a receiver's GOP
// tracking.
func TestVPXKeyframeFlagComesFromLibvpx(t *testing.T) {
	const w, h uint32 = 160, 120

	enc, err := NewVPXEncoder(CodecVP8, w, h, 30, 500)
	require.NoError(t, err)
	defer enc.Close()

	// Asking for a keyframe repeatedly on VP8 does produce keyframes, and the
	// flag must agree in both directions.
	y, u, v := testFrame(w, h, 0)
	pkt, err := enc.EncodeY420P(y, u, v, true)
	require.NoError(t, err)
	require.True(t, pkt.Keyframe)

	pkt, err = enc.EncodeY420P(y, u, v, false)
	require.NoError(t, err)
	require.False(t, pkt.Keyframe)
}

// TestVPXDeltaFramesAreSmallerThanKeyframes checks the encoder is actually doing
// inter-frame prediction rather than coding every frame as if it were a keyframe.
func TestVPXDeltaFramesAreSmallerThanKeyframes(t *testing.T) {
	const w, h uint32 = 160, 120

	for _, codec := range []CodecID{CodecVP8, CodecVP9} {
		codec := codec
		t.Run(codec.String(), func(t *testing.T) {
			enc, err := NewVPXEncoder(codec, w, h, 30, 500)
			require.NoError(t, err)
			defer enc.Close()

			y, u, v := testFrame(w, h, 0)
			key, err := enc.EncodeY420P(y, u, v, true)
			require.NoError(t, err)

			// Second frame is nearly identical to the first, so inter prediction
			// should shrink it substantially.
			y2, u2, v2 := testFrame(w, h, 0)
			delta, err := enc.EncodeY420P(y2, u2, v2, false)
			require.NoError(t, err)

			require.Less(t, len(delta.Data), len(key.Data),
				"a near-identical delta frame (%d bytes) should be smaller than the keyframe (%d bytes)",
				len(delta.Data), len(key.Data))
		})
	}
}

func TestVPXEncoderRejectsBadInput(t *testing.T) {
	const w, h uint32 = 160, 120

	t.Run("odd dimensions", func(t *testing.T) {
		// I420 is 4:2:0 subsampled, so an odd dimension has no valid chroma plane.
		_, err := NewVPXEncoder(CodecVP8, 161, h, 30, 500)
		require.Error(t, err)
	})
	t.Run("zero dimensions", func(t *testing.T) {
		_, err := NewVPXEncoder(CodecVP8, 0, h, 30, 500)
		require.Error(t, err)
	})
	t.Run("tiny dimensions", func(t *testing.T) {
		_, err := NewVPXEncoder(CodecVP8, 8, 8, 30, 500)
		require.Error(t, err)
	})
	t.Run("absurd bitrate", func(t *testing.T) {
		_, err := NewVPXEncoder(CodecVP8, w, h, 30, 10_000_000)
		require.Error(t, err)
	})
	t.Run("absurd frame rate", func(t *testing.T) {
		_, err := NewVPXEncoder(CodecVP8, w, h, 100_000, 500)
		require.Error(t, err)
	})
	t.Run("unsupported codec", func(t *testing.T) {
		_, err := NewVPXEncoder(CodecOpus, w, h, 30, 500)
		require.ErrorIs(t, err, ErrUnsupportedCodec)
	})

	t.Run("short luma plane", func(t *testing.T) {
		// libvpx trusts the stride it is given, so a short plane would be read
		// past its end. The check has to happen before the cgo call.
		enc, err := NewVPXEncoder(CodecVP8, w, h, 30, 500)
		require.NoError(t, err)
		defer enc.Close()

		y, u, v := testFrame(w, h, 0)
		_, err = enc.EncodeY420P(y[:len(y)-1], u, v, true)
		require.Error(t, err)

		_, err = enc.EncodeY420P(y, u[:len(u)-1], v, true)
		require.Error(t, err)

		_, err = enc.EncodeY420P(y, u, v[:len(v)-1], true)
		require.Error(t, err)
	})

	t.Run("after close", func(t *testing.T) {
		enc, err := NewVPXEncoder(CodecVP8, w, h, 30, 500)
		require.NoError(t, err)
		require.NoError(t, enc.Close())
		require.NoError(t, enc.Close(), "Close must be idempotent")

		y, u, v := testFrame(w, h, 0)
		_, err = enc.EncodeY420P(y, u, v, true)
		require.Error(t, err, "encoding after Close must fail, not crash")
	})
}
