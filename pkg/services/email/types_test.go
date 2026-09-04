package email

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateID(t *testing.T) {
	id1 := GenerateID()
	id2 := GenerateID()
	assert.NotEmpty(t, id1)
	assert.NotEmpty(t, id2)
	assert.NotEqual(t, id1, id2)
	assert.Len(t, id1, 24)
}

func TestGenerateQueueID(t *testing.T) {
	id1 := GenerateQueueID()
	id2 := GenerateQueueID()
	assert.NotEmpty(t, id1)
	assert.NotEmpty(t, id2)
	assert.NotEqual(t, id1, id2)
}

func TestMaildirConstants(t *testing.T) {
	assert.Equal(t, "new", maildirNew)
	assert.Equal(t, "cur", maildirCur)
	assert.Equal(t, "tmp", maildirTmp)
}

func TestMailFlags(t *testing.T) {
	assert.Equal(t, MailFlags(1), FlagSeen)
	assert.Equal(t, MailFlags(2), FlagAnswered)
	assert.Equal(t, MailFlags(4), FlagFlagged)
	assert.Equal(t, MailFlags(8), FlagDeleted)
	assert.Equal(t, MailFlags(16), FlagDraft)
	assert.Equal(t, MailFlags(32), FlagRecent)
}

func TestSMTPStateConstants(t *testing.T) {
	assert.Equal(t, SMTPState(0), SMTPStateGreeting)
	assert.Equal(t, SMTPState(1), SMTPStateEHLO)
	assert.Equal(t, SMTPState(2), SMTPStateMAIL)
	assert.Equal(t, SMTPState(3), SMTPStateRCPT)
	assert.Equal(t, SMTPState(4), SMTPStateDATA)
	assert.Equal(t, SMTPState(5), SMTPStateAUTH)
	assert.Equal(t, SMTPState(6), SMTPStateQuit)
}

func TestIMAPStateConstants(t *testing.T) {
	assert.Equal(t, IMAPState(0), IMAPStateNotAuth)
	assert.Equal(t, IMAPState(1), IMAPStateAuth)
	assert.Equal(t, IMAPState(2), IMAPStateSelected)
}

func TestMessage_String(t *testing.T) {
	msg := &Message{
		From:    "sender@example.com",
		To:      []string{"recipient@example.com"},
		Subject: "Test",
		Body:    "Hello",
		Headers: map[string]string{"X-Custom": "value"},
	}
	s := msg.String()
	assert.Contains(t, s, "From: sender@example.com")
	assert.Contains(t, s, "To: recipient@example.com")
	assert.Contains(t, s, "Subject: Test")
	assert.Contains(t, s, "X-Custom: value")
	assert.Contains(t, s, "Hello")
}

func TestMailboxStore_GetOrCreateMailbox(t *testing.T) {
	dir := t.TempDir()
	store := NewMailboxStore(dir)

	mb, err := store.GetOrCreateMailbox("alice")
	require.NoError(t, err)
	assert.Equal(t, "alice", mb.Name)
	assert.NotEmpty(t, mb.Path)

	mb2, err := store.GetOrCreateMailbox("alice")
	require.NoError(t, err)
	assert.Equal(t, mb, mb2)
}

func TestQueue(t *testing.T) {
	q := NewQueue()
	assert.Equal(t, 0, q.Count())

	entry := &QueueEntry{
		ID:          "1",
		MaxAttempts: 3,
		NextRetry:   time.Now(),
	}
	q.Add(entry)
	assert.Equal(t, 1, q.Count())

	next := q.Next()
	assert.NotNil(t, next)
	assert.Equal(t, "1", next.ID)

	q.Remove("1")
	assert.Equal(t, 0, q.Count())
}
