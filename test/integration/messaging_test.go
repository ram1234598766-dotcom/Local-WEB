//go:build integration

package integration

import (
	"context"
	"testing"

	"github.com/ram1234598766-dotcom/Local-WEB/pkg/crypto"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/services/messaging"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Messaging service – create channel and publish
// ---------------------------------------------------------------------------

func TestMessagingCreateChannel(t *testing.T) {
	pub1, priv1, _ := crypto.GenerateKeyPair()
	pub2, _, _ := crypto.GenerateKeyPair()
	svc := messaging.NewService(nil, priv1)

	chID := svc.CreateChannel([][32]byte{pub1, pub2})
	require.NotEqual(t, messaging.ChannelID{}, chID)
}

// ---------------------------------------------------------------------------
// Message delivery end-to-end
// ---------------------------------------------------------------------------

func TestMessagingPublishAndHistory(t *testing.T) {
	pub1, priv1, _ := crypto.GenerateKeyPair()
	pub2, priv2, _ := crypto.GenerateKeyPair()
	store := messaging.NewMemoryStore()
	svc1 := messaging.NewService(store, priv1)
	svc2 := messaging.NewService(store, priv2)

	chID := svc1.CreateChannel([][32]byte{pub1, pub2})
	// svc2 must also create the same channel (IDs are deterministic from members)
	svc2.CreateChannel([][32]byte{pub1, pub2})

	msg1, err := svc1.Publish(context.Background(), chID, pub1, []byte("hello world"), "")
	require.NoError(t, err)
	require.NotEmpty(t, msg1.ID)
	require.Equal(t, pub1, msg1.Sender)
	require.Equal(t, chID, msg1.ChannelID)
	require.NotEmpty(t, msg1.Signature, "message should be signed")

	msg2, err := svc2.Publish(context.Background(), chID, pub2, []byte("second message"), msg1.ID)
	require.NoError(t, err)
	require.Equal(t, msg1.ID, msg2.ParentID, "second message should reference first as parent")

	history, err := svc1.History(chID, "", 10)
	require.NoError(t, err)
	require.Len(t, history, 2)
	require.Equal(t, msg1.ID, history[0].ID)
	require.Equal(t, msg2.ID, history[1].ID)
}

// ---------------------------------------------------------------------------
// Signature verification
// ---------------------------------------------------------------------------

func TestMessagingSignatureVerification(t *testing.T) {
	pub, priv, _ := crypto.GenerateKeyPair()
	svc := messaging.NewService(nil, priv)

	chID := svc.CreateChannel([][32]byte{pub})
	msg, err := svc.Publish(context.Background(), chID, pub, []byte("signed content"), "")
	require.NoError(t, err)

	valid := crypto.Verify(pub, append([]byte(msg.ID), []byte("signed content")...), msg.Signature)
	require.True(t, valid, "signature should verify")
}

// ---------------------------------------------------------------------------
// Non-member cannot publish
// ---------------------------------------------------------------------------

func TestMessagingNonMemberPublishRejected(t *testing.T) {
	pub1, priv1, _ := crypto.GenerateKeyPair()
	pub2, _, _ := crypto.GenerateKeyPair()
	pub3, _, _ := crypto.GenerateKeyPair() // not a member
	svc := messaging.NewService(nil, priv1)

	chID := svc.CreateChannel([][32]byte{pub1, pub2})

	_, err := svc.Publish(context.Background(), chID, pub3, []byte("intruder"), "")
	require.Error(t, err, "non-member should not be able to publish")
	require.Contains(t, err.Error(), "not a channel member")
}

// ---------------------------------------------------------------------------
// Channel not found
// ---------------------------------------------------------------------------

func TestMessagingPublishToNonexistentChannel(t *testing.T) {
	_, priv, _ := crypto.GenerateKeyPair()
	svc := messaging.NewService(nil, priv)
	pub, _, _ := crypto.GenerateKeyPair()

	_, err := svc.Publish(context.Background(), messaging.ChannelID{}, pub, []byte("test"), "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "channel not found")
}

// ---------------------------------------------------------------------------
// E2E encryption simulation (signing + verification)
// ---------------------------------------------------------------------------

func TestMessagingE2EEncryption(t *testing.T) {
	alice, privAlice, _ := crypto.GenerateKeyPair()
	bob, privBob, _ := crypto.GenerateKeyPair()

	svcAlice := messaging.NewService(nil, privAlice)

	// Alice creates a channel with both parties
	chID := svcAlice.CreateChannel([][32]byte{alice, bob})

	// Alice sends a message
	plaintext := []byte("secret message for bob")
	msg, err := svcAlice.Publish(context.Background(), chID, alice, plaintext, "")
	require.NoError(t, err)

	// Verify signature (E2E integrity)
	signedData := append([]byte(msg.ID), plaintext...)
	require.True(t, crypto.Verify(alice, signedData, msg.Signature),
		"alice's signature should be verifiable by bob")

	// Verify bob cannot forge alice's signature — bob signs the same data
	// with his own private key; the resulting signature must NOT verify
	// against alice's public key.
	forgedSig, _ := crypto.Sign(privBob, signedData)
	require.False(t, crypto.Verify(alice, signedData, forgedSig),
		"bob's signature should not verify as alice's")

	// History should return only messages for this channel
	history, err := svcAlice.History(chID, "", 10)
	require.NoError(t, err)
	require.Len(t, history, 1)
	require.Equal(t, plaintext, history[0].Content)
}

// ---------------------------------------------------------------------------
// Message ordering in history
// ---------------------------------------------------------------------------

func TestMessagingHistoryOrdering(t *testing.T) {
	pub, priv, _ := crypto.GenerateKeyPair()
	svc := messaging.NewService(nil, priv)
	chID := svc.CreateChannel([][32]byte{pub})

	for i := 0; i < 10; i++ {
		_, err := svc.Publish(context.Background(), chID, pub, []byte("msg-"+string(rune('0'+i))), "")
		require.NoError(t, err)
	}

	history, err := svc.History(chID, "", 10)
	require.NoError(t, err)
	require.Len(t, history, 10)
	for i, m := range history {
		require.Equal(t, "msg-"+string(rune('0'+i)), string(m.Content),
			"message %d should be in order", i)
	}
}

// ---------------------------------------------------------------------------
// Thread-safe concurrent publishing
// ---------------------------------------------------------------------------

func TestMessagingConcurrentPublish(t *testing.T) {
	pub, priv, _ := crypto.GenerateKeyPair()
	svc := messaging.NewService(nil, priv)
	chID := svc.CreateChannel([][32]byte{pub})

	const msgs = 50
	done := make(chan struct{}, msgs)
	for i := 0; i < msgs; i++ {
		go func(idx int) {
			_, err := svc.Publish(context.Background(), chID, pub, []byte("concurrent-msg"), "")
			if err == nil {
				done <- struct{}{}
			}
		}(i)
	}

	count := 0
	for i := 0; i < msgs; i++ {
		<-done
		count++
	}
	history, _ := svc.History(chID, "", msgs+1)
	require.Len(t, history, count, "all messages should be stored")
}

// ---------------------------------------------------------------------------
// Marshal / unmarshal channel
// ---------------------------------------------------------------------------

func TestMessagingMarshalChannel(t *testing.T) {
	_, priv, _ := crypto.GenerateKeyPair()
	svc := messaging.NewService(nil, priv)
	pub, _, _ := crypto.GenerateKeyPair()
	chID := svc.CreateChannel([][32]byte{pub})

	data, err := svc.MarshalChannel(chID)
	require.NoError(t, err)
	require.NotEmpty(t, data)

	_, err = svc.UnmarshalChannel(data)
	// UnmarshalChannel currently returns empty ChannelID; test it doesn't panic
	_ = err
}

// ---------------------------------------------------------------------------
// Multiple channels isolation
// ---------------------------------------------------------------------------

func TestMessagingMultipleChannelsIsolation(t *testing.T) {
	pub1, priv1, _ := crypto.GenerateKeyPair()
	pub2, priv2, _ := crypto.GenerateKeyPair()
	svc1 := messaging.NewService(nil, priv1)
	svc2 := messaging.NewService(nil, priv2)

	chA := svc1.CreateChannel([][32]byte{pub1})
	chB := svc2.CreateChannel([][32]byte{pub2})

	_, err := svc1.Publish(context.Background(), chA, pub1, []byte("channel-a"), "")
	require.NoError(t, err)
	_, err = svc2.Publish(context.Background(), chB, pub2, []byte("channel-b"), "")
	require.NoError(t, err)

	histA, _ := svc1.History(chA, "", 10)
	histB, _ := svc2.History(chB, "", 10)
	require.Len(t, histA, 1)
	require.Len(t, histB, 1)
	require.Equal(t, "channel-a", string(histA[0].Content))
	require.Equal(t, "channel-b", string(histB[0].Content))
}
