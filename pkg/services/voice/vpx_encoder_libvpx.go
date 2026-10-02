//go:build libvpx

package voice

/*
#cgo pkg-config: vpx
#include <stdlib.h>
#include <vpx/vpx_encoder.h>
#include <vpx/vpx_image.h>
#include <vpx/vp8cx.h>
#include <vpx/vp8dx.h>
#include <vpx/vpx_decoder.h>

// vpx_codec_enc_cfg_t and vpx_image_t are structs, so cgo cannot pass them
// across the boundary directly. These shims own the create-and-configure
// sequence instead: libvpx requires g_w/g_h/g_timebase and rc_target_bitrate to
// be set before the encoder is initialised, and doing it through one call means
// a failure cannot leave a half-configured encoder behind.

// lw_vpx_create initialises an encoder for the given codec interface.
static vpx_codec_ctx_t *lw_vpx_create(vpx_codec_iface_t *iface, unsigned w, unsigned h,
                                     unsigned fps, unsigned kbps, int threads, vpx_codec_err_t *err) {
	vpx_codec_ctx_t ctx;
	vpx_codec_enc_cfg_t cfg;
	vpx_codec_err_t rc = vpx_codec_enc_config_default(iface, &cfg, 0);
	if (rc != VPX_CODEC_OK) {
		if (err) *err = rc;
		return NULL;
	}
	cfg.g_w = w;
	cfg.g_h = h;
	cfg.g_timebase.num = 1;
	cfg.g_timebase.den = fps ? fps : 30;
	cfg.rc_target_bitrate = kbps;
	if (threads > 0) {
		cfg.g_threads = threads;
	} else {
		// This is an interactive send path, not a file transcode. The VP9
		// encoder defaults to a multi-frame lookahead so it can code a batch at
		// once; with the default it produces no output at all until the buffer
		// fills, which is why a one-frame-at-a-time call would hang waiting for a
		// packet that never comes. One thread and no lookahead is what VP8 and
		// VP9 both want here.
		cfg.g_threads = 1;
		cfg.g_lag_in_frames = 0;
		// A lost RTP packet must not corrupt every frame after it. This is
		// header for FEC/PLC and it costs a little compression.
		cfg.g_error_resilient = 1;
	}
	rc = vpx_codec_enc_init(&ctx, iface, &cfg, 0);
	if (err) *err = rc;
	if (rc != VPX_CODEC_OK) {
		return NULL;
	}
	vpx_codec_ctx_t *out = (vpx_codec_ctx_t *)malloc(sizeof(vpx_codec_ctx_t));
	if (out == NULL) {
		vpx_codec_destroy(&ctx);
		return NULL;
	}
	*out = ctx;
	return out;
}

// lw_vpx_encode encodes one I420 image. out_pts/out_kind/out_size describe the
// first available packet; the caller iterates for the rest.
// vpx_codec_pts_t is a typedef cgo cannot name, so the timestamp crosses the
// boundary as a plain long long and is cast here.
static vpx_codec_err_t lw_vpx_encode(vpx_codec_ctx_t *ctx, unsigned char *y,
                                     unsigned char *u, unsigned char *v,
                                     unsigned w, unsigned h, long long pts,
                                     unsigned int deadline) {
	vpx_image_t img;
	img.fmt = VPX_IMG_FMT_I420;
	img.planes[VPX_PLANE_Y] = y;
	img.planes[VPX_PLANE_U] = u;
	img.planes[VPX_PLANE_V] = v;
	img.stride[VPX_PLANE_Y] = (int)w;
	img.stride[VPX_PLANE_U] = (int)(w / 2);
	img.stride[VPX_PLANE_V] = (int)(w / 2);
	img.d_w = w;
	img.d_h = h;
	img.w = w;
	img.h = h;
	img.x_chroma_shift = 1;
	img.y_chroma_shift = 1;
	return vpx_codec_encode(ctx, &img, (vpx_codec_pts_t)pts, 1, 0, deadline);
}

// lw_vpx_next returns the next encoded packet.
//
// cgo cannot reach through vpx_codec_cx_pkt_t: the packet's data member is an
// anonymous union, and cgo refuses to project an anonymous struct out of it.
// The frame lives at data.frame, so the pointer and length are copied out here.
// The VPX_DL_* deadline constants are likewise a bare enum that cannot be named
// as a Go value, so they are read here too.
static int lw_vpx_next(vpx_codec_ctx_t *ctx, vpx_codec_iter_t *iter,
                       const unsigned char **buf, unsigned int *sz,
                       int *is_frame, int *is_key) {
	const vpx_codec_cx_pkt_t *pkt = vpx_codec_get_cx_data(ctx, iter);
	if (pkt == NULL) {
		return 0;
	}
	*buf = (const unsigned char *)pkt->data.frame.buf;
	*sz = (unsigned int)pkt->data.frame.sz;
	*is_frame = (pkt->kind == VPX_CODEC_CX_FRAME_PKT) ? 1 : 0;
	// libvpx decides for itself whether a frame is a keyframe, which is
	// authoritative: a caller asking for a keyframe does not guarantee one, since
	// the encoder may be forced to emit a keyframe at a different point.
	*is_key = (*is_frame && (pkt->data.frame.flags & VPX_FRAME_IS_KEY)) ? 1 : 0;
	return 1;
}

static unsigned int lw_vpx_dl_good_quality(void) { return VPX_DL_GOOD_QUALITY; }
static unsigned int lw_vpx_dl_realtime(void) { return VPX_DL_REALTIME; }
*/
import "C"

import (
	"errors"
	"fmt"
	"sync"
	"unsafe"
)

// ErrVPXEncodeFailed is returned when libvpx rejects a frame.
var ErrVPXEncodeFailed = errors.New("libvpx encode failed")

// VPXEncoder encodes I420 frames to VP8 or VP9 using libvpx.
//
// Like OpusEncoder, this file is behind an opt-in build tag. The CI release
// matrix builds with CGO_ENABLED=1 on ubuntu-latest and macos-latest, and
// neither runner has libvpx, so gating on cgo alone would break both entries:
//
//	go build -tags libvpx ./...
//
// The encoder state is not reentrant, so calls are serialised.
type VPXEncoder struct {
	mu     sync.Mutex
	ctx    *C.vpx_codec_ctx_t
	codec  CodecID
	width  uint32
	height uint32
	pts    C.longlong
	closed bool
}

// NewVPXEncoder creates a VP8 or VP9 encoder.
//
// Only dimensions that are even are accepted: I420 is 4:2:0 subsampled, so an
// odd width or height has no valid chroma plane and libvpx would either reject
// it or read past the end of the buffer.
func NewVPXEncoder(codec CodecID, width, height uint32, fps, kbps int) (*VPXEncoder, error) {
	if codec != CodecVP8 && codec != CodecVP9 {
		return nil, fmt.Errorf("%w: libvpx cannot encode codec id %d", ErrUnsupportedCodec, codec)
	}
	if width == 0 || height == 0 {
		return nil, fmt.Errorf("width and height must be non-zero, got %dx%d", width, height)
	}
	if width%2 != 0 || height%2 != 0 {
		return nil, fmt.Errorf("I420 requires even dimensions, got %dx%d", width, height)
	}
	// libvpx refuses extremely small inputs outright, and a 1x2 frame is not a
	// meaningful video frame either.
	if width < 16 || height < 16 {
		return nil, fmt.Errorf("width and height must be at least 16, got %dx%d", width, height)
	}
	if kbps <= 0 || kbps > 100_000 {
		return nil, fmt.Errorf("bitrate %d kbps is outside the 1..100000 range", kbps)
	}
	if fps <= 0 || fps > 240 {
		return nil, fmt.Errorf("frame rate %d is outside the 1..240 range", fps)
	}

	var iface *C.vpx_codec_iface_t
	if codec == CodecVP9 {
		iface = C.vpx_codec_vp9_cx()
	} else {
		iface = C.vpx_codec_vp8_cx()
	}

	var cerr C.vpx_codec_err_t
	ctx := C.lw_vpx_create(iface, C.uint(width), C.uint(height),
		C.uint(fps), C.uint(kbps), 0, &cerr)
	if ctx == nil {
		return nil, fmt.Errorf("%w: create: %s", ErrVPXEncodeFailed,
			C.GoString(C.vpx_codec_err_to_string(cerr)))
	}

	return &VPXEncoder{
		ctx:    ctx,
		codec:  codec,
		width:  width,
		height: height,
	}, nil
}

// EncodeY420P encodes one I420 frame and returns the resulting packet.
//
// The three planes are checked against the exact size the resolution implies.
// libvpx trusts the strides it is given, so a short plane would be read past its
// end and produce a corrupt frame rather than an error.
func (e *VPXEncoder) EncodeY420P(y, u, v []byte, keyframe bool) (*VideoPacket, error) {
	ySize := int(e.width * e.height)
	cSize := ySize / 4
	if len(y) != ySize {
		return nil, fmt.Errorf("luma plane must be %d bytes for %dx%d, got %d", ySize, e.width, e.height, len(y))
	}
	if len(u) != cSize || len(v) != cSize {
		return nil, fmt.Errorf("chroma planes must be %d bytes each for %dx%d, got %d and %d",
			cSize, e.width, e.height, len(u), len(v))
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return nil, fmt.Errorf("vpx encoder is closed")
	}

	pts := e.pts
	e.pts++

	deadline := uint(C.lw_vpx_dl_good_quality())
	if keyframe {
		deadline = uint(C.lw_vpx_dl_realtime())
	}
	if err := C.lw_vpx_encode(e.ctx,
		(*C.uchar)(unsafe.Pointer(&y[0])),
		(*C.uchar)(unsafe.Pointer(&u[0])),
		(*C.uchar)(unsafe.Pointer(&v[0])),
		C.uint(e.width), C.uint(e.height),
		pts, C.uint(deadline)); err != C.VPX_CODEC_OK {
		return nil, fmt.Errorf("%w: encode: %s", ErrVPXEncodeFailed,
			C.GoString(C.vpx_codec_err_to_string(err)))
	}

	var iter C.vpx_codec_iter_t
	for {
		var (
			buf     *C.uchar
			sz      C.uint
			isFrame C.int
			isKey   C.int
		)
		if C.lw_vpx_next(e.ctx, &iter, &buf, &sz, &isFrame, &isKey) == 0 {
			break
		}
		if isFrame == 0 || sz == 0 {
			continue
		}
		// The payload pointer is owned by the encoder and is invalidated by the
		// next encode call, so it is copied out here.
		data := C.GoBytes(unsafe.Pointer(buf), C.int(sz))
		return &VideoPacket{
			Codec:    e.codec,
			Data:     data,
			Keyframe: isKey != 0,
			Width:    e.width,
			Height:   e.height,
		}, nil
	}

	// With a one-frame deadline libvpx normally emits immediately, but a buffered
	// frame is a real possibility and silently dropping it would corrupt the
	// stream.
	return nil, fmt.Errorf("%w: libvpx produced no packet for pts %d", ErrVPXEncodeFailed, pts)
}

// Close destroys the encoder.
func (e *VPXEncoder) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return nil
	}
	e.closed = true
	C.vpx_codec_destroy(e.ctx)
	C.free(unsafe.Pointer(e.ctx))
	e.ctx = nil
	return nil
}

// vpxEncoderAvailable reports whether this build linked a VP8/VP9 encoder.
func vpxEncoderAvailable() bool { return true }
