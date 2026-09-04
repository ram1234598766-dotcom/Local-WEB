package voice

import (
	"crypto/sha3"
	"errors"
	"fmt"
	"sync"
)

var (
	ErrTrackNotFound     = errors.New("track not found")
	ErrUnsupportedCodec  = errors.New("unsupported codec")
	ErrTrackAlreadyAdded = errors.New("track already added")
)

// OpusDefaultBitrate is the default Opus bitrate in kbps.
const OpusDefaultBitrate = 64

// VP9DefaultBitrate is the default VP9 bitrate in kbps.
const VP9DefaultBitrate = 1500

// VP9MinResolution is the minimum VP9 resolution.
var VP9MinResolution = struct{ Width, Height uint32 }{320, 240}

// VP9MaxResolution is the maximum VP9 resolution (720p).
var VP9MaxResolution = struct{ Width, Height uint32 }{1280, 720}

// CodecProfile describes the parameters for a supported codec.
type CodecProfile struct {
	ID         CodecID
	Name       string
	MimeType   string
	SampleRate uint32
	FrameRate  uint32
	Width      uint32
	Height     uint32
	Bitrate    uint32
	MinBitrate uint32
	MaxBitrate uint32
}

// SupportedCodecs returns the codecs this package can negotiate.
func SupportedCodecs() []CodecProfile {
	return []CodecProfile{
		{
			ID:         CodecOpus,
			Name:       "opus",
			MimeType:   "audio/opus",
			SampleRate: 48000,
			FrameRate:  0,
			Bitrate:    OpusDefaultBitrate,
			MinBitrate: 12,
			MaxBitrate: 510,
		},
		{
			ID:         CodecVP9,
			Name:       "vp9",
			MimeType:   "video/vp9",
			SampleRate: 0,
			FrameRate:  30,
			Width:      1280,
			Height:     720,
			Bitrate:    VP9DefaultBitrate,
			MinBitrate: 100,
			MaxBitrate: 10000,
		},
	}
}

// CodecProfileForID returns the profile for a given codec ID.
func CodecProfileForID(id CodecID) (CodecProfile, bool) {
	for _, p := range SupportedCodecs() {
		if p.ID == id {
			return p, true
		}
	}
	return CodecProfile{}, false
}

// NegotiateCodecs selects a mutually supported audio/video codec pair
// from the local and remote capability lists. Each list is a sequence of
// CodecID values in preference order. An empty local list means that media
// type is not supported; the returned CodecID is 0 in that case.
func NegotiateCodecs(localAudio, remoteAudio []CodecID, localVideo, remoteVideo []CodecID) (CodecID, CodecID, error) {
	audio, errA := pickFirst(localAudio, remoteAudio)
	video, errV := pickFirst(localVideo, remoteVideo)
	if errA != nil && errV != nil {
		return 0, 0, fmt.Errorf("audio and video codec negotiation failed: %w, %w", errA, errV)
	}
	if errA != nil {
		return 0, video, nil
	}
	if errV != nil {
		return audio, 0, nil
	}
	return audio, video, nil
}

func pickFirst(local, remote []CodecID) (CodecID, error) {
	localSet := make(map[CodecID]bool, len(local))
	for _, c := range local {
		localSet[c] = true
	}
	for _, c := range remote {
		if localSet[c] {
			return c, nil
		}
	}
	return 0, ErrUnsupportedCodec
}

// AdaptiveBitrate adjusts the bitrate target for a video track based on
// measured RTT and packet loss.
//
//	rttMs: round-trip time in milliseconds.
//	packetLoss: fraction of packets lost (0.0 – 1.0).
//	currentKbps: current operating bitrate.
//
// Returns the recommended bitrate in kbps, clamped to the codec limits.
func AdaptiveBitrate(rttMs float64, packetLoss float64, currentKbps uint32) uint32 {
	profile, ok := CodecProfileForID(CodecVP9)
	if !ok {
		return currentKbps
	}

	bitrate := float64(currentKbps)
	if rttMs > 200 || packetLoss > 0.05 {
		bitrate *= 0.7
	} else if rttMs < 50 && packetLoss < 0.01 && bitrate < float64(profile.MaxBitrate) {
		bitrate *= 1.15
	}
	if bitrate < float64(profile.MinBitrate) {
		bitrate = float64(profile.MinBitrate)
	}
	if bitrate > float64(profile.MaxBitrate) {
		bitrate = float64(profile.MaxBitrate)
	}
	return uint32(bitrate)
}

// Track represents a single media track (audio, video, screen, data).
type Track struct {
	mu       sync.RWMutex
	info     TrackInfo
	codec    CodecProfile
	active   bool
}

// NewTrack creates a track from a negotiated track info.
func NewTrack(info TrackInfo) (*Track, error) {
	profile, ok := CodecProfileForID(info.Codec)
	if !ok {
		return nil, fmt.Errorf("%w: %d", ErrUnsupportedCodec, info.Codec)
	}
	t := &Track{info: info, codec: profile, active: true}
	return t, nil
}

// Info returns a copy of the track info.
func (t *Track) Info() TrackInfo {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.info
}

// IsActive reports whether the track is currently active.
func (t *Track) IsActive() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.active
}

// Stop deactivates the track.
func (t *Track) Stop() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.active = false
}

// TrackManager manages all tracks for a single peer session.
type TrackManager struct {
	mu     sync.RWMutex
	tracks map[string]*Track
}

// NewTrackManager creates a new TrackManager.
func NewTrackManager() *TrackManager {
	return &TrackManager{tracks: make(map[string]*Track)}
}

// Add inserts a track. Returns ErrTrackAlreadyAdded if the ID exists.
func (m *TrackManager) Add(t TrackInfo) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.tracks[t.ID]; exists {
		return fmt.Errorf("%w: %s", ErrTrackAlreadyAdded, t.ID)
	}
	track, err := NewTrack(t)
	if err != nil {
		return err
	}
	m.tracks[t.ID] = track
	return nil
}

// Get retrieves a track by ID.
func (m *TrackManager) Get(id string) (*Track, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	t, ok := m.tracks[id]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrTrackNotFound, id)
	}
	return t, nil
}

// List returns all active tracks.
func (m *TrackManager) List() []TrackInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]TrackInfo, 0, len(m.tracks))
	for _, t := range m.tracks {
		if t.IsActive() {
			out = append(out, t.Info())
		}
	}
	return out
}

// StopAll deactivates every track.
func (m *TrackManager) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, t := range m.tracks {
		t.Stop()
	}
}

// Remove deletes a track by ID.
func (m *TrackManager) Remove(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.tracks[id]; !ok {
		return fmt.Errorf("%w: %s", ErrTrackNotFound, id)
	}
	delete(m.tracks, id)
	return nil
}

// TrackID generates a deterministic track ID from kind and peer.
func TrackID(kind TrackKind, peer PeerID) string {
	h := sha3.New256()
	h.Write([]byte{byte(kind)})
	h.Write(peer[:])
	var out [32]byte
	h.Sum(out[:0])
	return fmt.Sprintf("track-%x", out[:8])
}
