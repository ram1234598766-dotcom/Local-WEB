//go:build !libopus

package voice

import "fmt"

// OpusEncoder is the no-encoder build of the Opus encoder.
//
// This file is what every default build gets. The pure-Go libopus port the
// decoder uses (github.com/pion/opus) is decoder-only, and the real encoder
// needs cgo against libopus, so a build without the `libopus` tag has no
// encoder at all.
//
// SendPCM reports ErrNoOpusEncoder rather than sending raw PCM under an Opus
// payload type: a browser would decode that as Opus and produce noise.
//
// To get a real encoder, build with the native codec:
//
//	go build -tags libopus ./...
//
// which needs libopus available to cgo (Debian/Ubuntu: libopus-dev, macOS:
// brew install opus, Windows: vcpkg install opus).
type OpusEncoder struct{}

// NewOpusEncoder always fails in this build.
func NewOpusEncoder(bitrate int) (*OpusEncoder, error) {
	return nil, fmt.Errorf("%w: rebuild with -tags libopus and libopus installed", ErrNoOpusEncoder)
}

// EncodePCM always fails in this build.
func (e *OpusEncoder) EncodePCM(pcm []int16) ([]byte, error) {
	return nil, fmt.Errorf("%w: rebuild with -tags libopus and libopus installed", ErrNoOpusEncoder)
}

// Close is a no-op in this build.
func (e *OpusEncoder) Close() error { return nil }

// opusEncoderAvailable reports whether this build linked an encoder.
func opusEncoderAvailable() bool { return false }
