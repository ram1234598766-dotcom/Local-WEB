//go:build libopus

package voice

/*
#cgo pkg-config: opus
#include <opus/opus.h>
#include <stdlib.h>

// OPUS_SET_BITRATE is a function-like macro, so cgo cannot refer to it as a
// value. The control request is declared as a plain struct in libopus's header
// (opus_defines.h), so a shim is the way to set it.
//
// This also keeps the create-and-configure sequence in one place: libopus
// requires the bitrate to be set before any frame is encoded, and doing it
// through one shim means a failure cannot leave a half-configured encoder
// behind.
static OpusEncoder *lw_opus_encoder_create(int rate, int channels, int application, int bitrate, int *err) {
	int code = OPUS_OK;
	OpusEncoder *enc = opus_encoder_create(rate, channels, application, &code);
	if (err != NULL) {
		*err = code;
	}
	if (enc == NULL || code != OPUS_OK) {
		return NULL;
	}
	if (bitrate > 0) {
		opus_int32 br = bitrate;
		int rc = opus_encoder_ctl(enc, OPUS_SET_BITRATE(br));
		if (rc != OPUS_OK) {
			opus_encoder_destroy(enc);
			if (err != NULL) {
				*err = rc;
			}
			return NULL;
		}
	}
	return enc;
}
*/
import "C"

import (
	"fmt"
	"sync"
	"unsafe"
)

// OpusEncoder encodes PCM to Opus using libopus.
//
// This file is behind the `libopus` build tag on purpose. The CI release matrix
// builds ./cmd/node with CGO_ENABLED=1 on ubuntu-latest and macos-latest, and
// neither runner has libopus installed, so gating on cgo alone would break both
// entries. The tag makes the native codec strictly opt-in:
//
//	go build -tags libopus ./...
//
// Everything else - the cgo-free CLI, the cross-compiled binaries, the default
// node build - keeps the pure-Go decoder and reports ErrNoOpusEncoder on send.
//
// The encoder state is not reentrant, so calls are serialised.
type OpusEncoder struct {
	mu     sync.Mutex
	enc    *C.OpusEncoder
	closed bool
}

// NewOpusEncoder creates a 48 kHz stereo encoder at the given bitrate in bits
// per second.
func NewOpusEncoder(bitrate int) (*OpusEncoder, error) {
	if bitrate < 500 || bitrate > 512000 {
		return nil, fmt.Errorf("opus bitrate %d is outside the 500..512000 range libopus accepts", bitrate)
	}

	var code C.int
	enc := C.lw_opus_encoder_create(
		C.int(OpusSampleRate),
		C.int(OpusChannels),
		C.OPUS_APPLICATION_VOIP,
		C.int(bitrate),
		&code,
	)
	if enc == nil {
		return nil, fmt.Errorf("opus encoder create: %s", C.GoString(C.opus_strerror(code)))
	}
	return &OpusEncoder{enc: enc}, nil
}

// EncodePCM encodes exactly one 20 ms frame of interleaved stereo samples.
//
// The frame length is checked rather than assumed: libopus would read past the
// end of a short buffer, and a caller that got the frame size wrong would get a
// corrupted stream rather than an error.
func (e *OpusEncoder) EncodePCM(pcm []int16) ([]byte, error) {
	want := OpusFrameSize * OpusChannels
	if len(pcm) != want {
		return nil, fmt.Errorf("expected %d samples for one %d ms frame, got %d", want, OpusFrameMs, len(pcm))
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return nil, fmt.Errorf("opus encoder is closed")
	}

	out := make([]byte, OpusMaxPacket)
	n := C.opus_encode(
		e.enc,
		(*C.opus_int16)(unsafe.Pointer(&pcm[0])),
		C.int(OpusFrameSize),
		(*C.uchar)(unsafe.Pointer(&out[0])),
		C.opus_int32(len(out)),
	)
	if n < 0 {
		return nil, fmt.Errorf("opus encode: %s", C.GoString(C.opus_strerror(C.int(n))))
	}
	return out[:n], nil
}

// Close destroys the encoder.
func (e *OpusEncoder) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return nil
	}
	e.closed = true
	C.opus_encoder_destroy(e.enc)
	e.enc = nil
	return nil
}

// opusEncoderAvailable reports whether this build linked an encoder.
func opusEncoderAvailable() bool { return true }
