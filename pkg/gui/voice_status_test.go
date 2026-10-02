package gui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ram1234598766-dotcom/Local-WEB/pkg/crypto"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/services/voice"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/transport"
	"github.com/stretchr/testify/require"
)

// TestVoiceStatusWithoutService is the honest-unavailable case: the daemon has
// not constructed the voice service, so nothing can handle a voice stream and the
// panel must say that rather than claiming a call is possible.
func TestVoiceStatusWithoutService(t *testing.T) {
	api := NewAPI([32]byte{1})
	h := NewHandler(api)

	rec := httptest.NewRecorder()
	h.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/voice/status", nil))
	require.Equal(t, http.StatusOK, rec.Code)

	var got VoiceStatusResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.False(t, got.ServiceLive)
	require.NotEmpty(t, got.Reason, "an unavailable service must explain itself")
	require.NotNil(t, got.Calls, "calls must serialise as [], never null")
	require.Len(t, got.Calls, 0)
}

// TestVoiceStatusReportsRealEncoderCapability checks the response describes this
// build rather than a hardcoded expectation. Encoder presence differs between a
// default build and a -tags libopus,libvpx build, and the panel has to be right in
// both.
//
// This wires a real voice service so the encoder branch is the one under test:
// with no service the reason is "not running", which is correct but says nothing
// about encoders.
func TestVoiceStatusReportsRealEncoderCapability(t *testing.T) {
	ctx := context.Background()
	xPub, xPriv, err := crypto.GenerateX25519KeyPair()
	require.NoError(t, err)
	_, edPriv, err := crypto.GenerateKeyPair()
	require.NoError(t, err)

	srv, err := transport.NewServer(ctx, "127.0.0.1:0", xPub, xPriv)
	require.NoError(t, err)
	defer srv.Stop()

	api := NewAPI([32]byte{1})
	api.SetVoiceService(voice.NewVoiceServer(srv, false, edPriv))

	got := api.VoiceStatus()
	require.True(t, got.ServiceLive)
	require.Empty(t, got.Calls, "a fresh service has no calls")
	require.NotNil(t, got.Calls)

	require.Equal(t, voice.OpusEncoderLinked(), got.OpusEncoder,
		"the reported encoder must match what this build actually links")
	require.Equal(t, voice.VPXEncoderLinked(), got.VPXEncoder)
	require.Equal(t, got.OpusEncoder, got.CanSendAudio)
	require.Equal(t, got.VPXEncoder, got.CanSendVideo)

	if got.OpusEncoder {
		require.Empty(t, got.Reason,
			"a build that can send audio should not claim it is blocked")
	} else {
		require.Contains(t, got.Reason, "receive",
			"the reason must say receiving still works, since the decoder is always linked")
		require.Contains(t, got.EncoderHint, "libopus")
	}
}

func TestVoiceStatusRejectsNonGET(t *testing.T) {
	h := NewHandler(NewAPI([32]byte{1}))
	rec := httptest.NewRecorder()
	h.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/voice/status", nil))
	require.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}
