package gui

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ram1234598766-dotcom/Local-WEB/pkg/crypto"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/services/messaging"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/services/voice"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/transport"
)

func newVoiceAPI(t *testing.T) (*NodeAPI, *Handler) {
	t.Helper()
	ctx := context.Background()
	xPub, xPriv, err := crypto.GenerateX25519KeyPair()
	require.NoError(t, err)
	_, edPriv, err := crypto.GenerateKeyPair()
	require.NoError(t, err)

	srv, err := transport.NewServer(ctx, "127.0.0.1:0", xPub, xPriv)
	require.NoError(t, err)
	t.Cleanup(srv.Stop)

	api := NewAPI([32]byte{1})
	vs := voice.NewVoiceServer(srv, false, edPriv)
	// The signalling transport is what the daemon supplies. Without it a call
	// cannot be placed, which is the state this endpoint has to report honestly.
	vs.SetSignalingChannel(voice.NewStoreChannel(messaging.NewMemoryStore()))
	api.SetVoiceService(vs)

	return api, NewHandler(api)
}

// TestPlaceCallPublishesASignedOffer covers the endpoint that makes a call
// placeable. Before this, the voice service had a complete signalling
// implementation that no real caller could reach.
func TestPlaceCallPublishesASignedOffer(t *testing.T) {
	_, h := newVoiceAPI(t)

	peer := strings.Repeat("ab", 32)
	body := strings.NewReader(`{"peer_id":"` + peer + `","enable_video":false}`)
	req := httptest.NewRequest(http.MethodPost, "/api/voice/call", body)
	rec := httptest.NewRecorder()
	h.mux.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var got PlaceCallResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.NotEmpty(t, got.CallID)
	require.Equal(t, "call:"+peer, got.Channel)

	// "offer_sent" is the whole truth: nothing exchanges SDP yet.
	require.Equal(t, "offer_sent", got.State)
	require.Len(t, got.Tracks, 1)
	require.Equal(t, voice.CodecOpus, got.Tracks[0].Codec)
}

// TestPlaceCallAddsAVideoTrackWhenAsked checks the video track follows what the
// build can actually encode, rather than advertising a codec that is not there.
func TestPlaceCallAddsAVideoTrackWhenAsked(t *testing.T) {
	_, h := newVoiceAPI(t)

	peer := strings.Repeat("cd", 32)
	body := strings.NewReader(`{"peer_id":"` + peer + `","enable_video":true}`)
	rec := httptest.NewRecorder()
	h.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/voice/call", body))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var got PlaceCallResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Len(t, got.Tracks, 2)
	require.Equal(t, voice.TrackKindVideo, got.Tracks[1].Kind)

	// The advertised codec must be one this build can encode, or the peer would
	// negotiate a track that can never carry a frame.
	if voice.VPXEncoderLinked() {
		require.Contains(t, []voice.CodecID{voice.CodecVP8, voice.CodecVP9}, got.Tracks[1].Codec)
	} else {
		require.Equal(t, voice.CodecVP8, got.Tracks[1].Codec,
			"a build with no VPX encoder must still advertise VP8, which is what libvpx would provide")
	}
}

func TestPlaceCallRejectsBadInput(t *testing.T) {
	_, h := newVoiceAPI(t)

	cases := []struct{ name, body, query string }{
		{"not json", `{`, ""},
		{"short peer id", `{"peer_id":"abcd"}`, ""},
		{"non-hex peer id", `{"peer_id":"` + strings.Repeat("zz", 32) + `"}`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/voice/call", strings.NewReader(tc.body)))
			require.Equal(t, http.StatusBadRequest, rec.Code)
		})
	}

	t.Run("wrong method", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/voice/call", nil))
		require.Equal(t, http.StatusMethodNotAllowed, rec.Code)
	})
}

// TestAwaitSignalFindsAnAnswer is the two-sided handshake over HTTP: one node
// places a call, and a peer reads a signal off the same channel.
func TestAwaitSignalFindsAnAnswer(t *testing.T) {
	api, h := newVoiceAPI(t)

	peer := strings.Repeat("ef", 32)
	channel := "call:" + peer

	// The peer answers on the same signalling channel, standing in for the node
	// on the other end of the call.
	go func() {
		time.Sleep(150 * time.Millisecond)
		publishAnswer(t, api, channel, peer)
	}()

	url := "/api/voice/signal?peer_id=" + peer + "&type=answer&wait_ms=3000"
	rec := httptest.NewRecorder()
	h.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var got AwaitSignalResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.True(t, got.Found, "the answer should have been found: %s", rec.Body.String())
	require.Equal(t, "answer", got.Type)
	require.Len(t, got.CallID, 32, "call_id is 16 bytes hex-encoded")
}

// publishAnswer puts a signed answer on the API's signalling channel, standing in
// for the remote node.
//
// peerHex must be the identity the receiver is waiting for: a signal from anyone
// else is filtered out by sender, which is the point of the check.
func publishAnswer(t *testing.T, api *NodeAPI, channel, peerHex string) {
	t.Helper()
	ch := api.voiceSrv.SignalingChannel()
	require.NotNil(t, ch)

	raw, err := hex.DecodeString(peerHex)
	require.NoError(t, err)
	var peer [32]byte
	copy(peer[:], raw)

	msg := voice.SignalAnswer(voice.CallID{1, 2, 3}, voice.PeerID(peer), nil)
	payload, err := json.Marshal(msg)
	require.NoError(t, err)
	require.NoError(t, ch.Publish(context.Background(), channel, peer, voice.SignaledContentType, payload))
}

// TestAwaitSignalReportsNothingRatherThanFailing checks an unanswered call is a
// normal outcome, not an error.
func TestAwaitSignalReportsNothingRatherThanFailing(t *testing.T) {
	_, h := newVoiceAPI(t)

	peer := strings.Repeat("11", 32)
	url := "/api/voice/signal?peer_id=" + peer + "&type=offer&wait_ms=150"
	rec := httptest.NewRecorder()
	h.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
	require.Equal(t, http.StatusOK, rec.Code)

	var got AwaitSignalResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.False(t, got.Found)
	require.Empty(t, got.CallID)
}

func TestAwaitSignalRejectsBadInput(t *testing.T) {
	_, h := newVoiceAPI(t)

	peer := strings.Repeat("22", 32)
	cases := []struct {
		name, url string
	}{
		{"missing type", "/api/voice/signal?peer_id=" + peer},
		{"missing peer", "/api/voice/signal?type=offer"},
		{"short peer", "/api/voice/signal?peer_id=abcd&type=offer"},
		{"unknown type", "/api/voice/signal?peer_id=" + peer + "&type=nonsense"},
		{"absurd wait", "/api/voice/signal?peer_id=" + peer + "&type=offer&wait_ms=999999"},
		{"negative wait", "/api/voice/signal?peer_id=" + peer + "&type=offer&wait_ms=-5"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.url, nil))
			require.Equal(t, http.StatusBadRequest, rec.Code)
		})
	}
}

// TestVoiceStatusReportsSignalling checks the status endpoint distinguishes a
// service that can place a call from one that cannot.
func TestVoiceStatusReportsSignalling(t *testing.T) {
	api, _ := newVoiceAPI(t)
	require.True(t, api.VoiceStatus().Signaling)

	// Without a channel, the service is live but not placeable, and the status has
	// to say so rather than implying a call could be placed.
	ctx := context.Background()
	xPub, xPriv, err := crypto.GenerateX25519KeyPair()
	require.NoError(t, err)
	srv, err := transport.NewServer(ctx, "127.0.0.1:0", xPub, xPriv)
	require.NoError(t, err)
	defer srv.Stop()

	bare := NewAPI([32]byte{1})
	bare.SetVoiceService(voice.NewVoiceServer(srv, false, [32]byte{2}))
	status := bare.VoiceStatus()
	require.True(t, status.ServiceLive)
	require.False(t, status.Signaling)

	_, err = bare.PlaceCall(ctx, strings.Repeat("33", 32), false)
	require.ErrorIs(t, err, voice.ErrNoSignalingChannel)
}
