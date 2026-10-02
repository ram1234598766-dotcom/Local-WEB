//go:build !libvpx

package voice

import "fmt"

// VPXEncoder is the no-encoder build of the VP8/VP9 encoder.
//
// This file is what every default build gets. There is no pure-Go VP8 or VP9
// encoder in the dependency set, and the real one needs cgo against libvpx, so a
// build without the `libvpx` tag has no video encoder at all.
//
// A caller that tries to encode gets ErrNoVPXEncoder rather than being handed
// empty packets, which a receiver would decode as a broken frame.
//
// To get a real encoder, build with the native codec:
//
//	go build -tags libvpx ./...
//
// which needs libvpx available to cgo (Debian/Ubuntu: libvpx-dev, macOS:
// brew install vpx, Windows: vcpkg install vpx).
type VPXEncoder struct{}

// NewVPXEncoder always fails in this build.
func NewVPXEncoder(codec CodecID, width, height uint32, fps, kbps int) (*VPXEncoder, error) {
	return nil, fmt.Errorf("%w: rebuild with -tags libvpx and libvpx installed", ErrNoVPXEncoder)
}

// EncodeY420P always fails in this build.
func (e *VPXEncoder) EncodeY420P(y, u, v []byte, keyframe bool) (*VideoPacket, error) {
	return nil, fmt.Errorf("%w: rebuild with -tags libvpx and libvpx installed", ErrNoVPXEncoder)
}

// Close is a no-op in this build.
func (e *VPXEncoder) Close() error { return nil }

// vpxEncoderAvailable reports whether this build linked a VP8/VP9 encoder.
func vpxEncoderAvailable() bool { return false }
