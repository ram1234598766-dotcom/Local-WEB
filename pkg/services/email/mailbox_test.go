package email

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMailbox_StoreAndLoad(t *testing.T) {
	dir := t.TempDir()
	store := NewMailboxStore(dir)

	mb, err := store.GetOrCreateMailbox("alice")
	require.NoError(t, err)

	msg := &Message{
		ID:     "test-id",
		From:   "bob@example.com",
		To:     []string{"alice@localweb"},
		Subject: "Hello",
		Body:    "World",
		Headers: map[string]string{"X-Test": "true"},
	}

	err = mb.StoreMessage(msg)
	require.NoError(t, err)
	assert.Equal(t, "test-id", msg.ID)
	assert.NotEmpty(t, msg.Filename)
	assert.NotEmpty(t, msg.Maildir)
	assert.Equal(t, uint32(1), msg.UID)
}

func TestMailbox_MoveToCur(t *testing.T) {
	dir := t.TempDir()
	store := NewMailboxStore(dir)

	mb, err := store.GetOrCreateMailbox("alice")
	require.NoError(t, err)

	msg := &Message{
		From:     "bob@example.com",
		To:       []string{"alice@localweb"},
		Subject:  "Hello",
		Body:     "World",
		Filename: "test.mail",
		Maildir:  mb.Path + "/new/test.mail",
	}

	// Create the file first
	require.NoError(t, os.WriteFile(msg.Maildir, []byte("test"), 0644))

	err = mb.MoveToCur(msg, FlagSeen)
	require.NoError(t, err)
	assert.Equal(t, "test.mail", msg.Filename)
	assert.Equal(t, FlagSeen, msg.Flags)
}

func TestMailbox_DeleteMessage(t *testing.T) {
	dir := t.TempDir()
	store := NewMailboxStore(dir)

	mb, err := store.GetOrCreateMailbox("alice")
	require.NoError(t, err)

	msg := &Message{
		From:     "bob@example.com",
		To:       []string{"alice@localweb"},
		Subject:  "Hello",
		Body:     "World",
		Filename: "test.mail",
		Maildir:  mb.Path + "/new/test.mail",
	}

	require.NoError(t, os.WriteFile(msg.Maildir, []byte("test"), 0644))
	mb.Messages = append(mb.Messages, msg)

	err = mb.DeleteMessage(msg)
	require.NoError(t, err)
	assert.Empty(t, msg.Maildir)
	assert.Equal(t, 0, len(mb.Messages))
}

func TestMailbox_GetByUID(t *testing.T) {
	dir := t.TempDir()
	store := NewMailboxStore(dir)

	mb, err := store.GetOrCreateMailbox("alice")
	require.NoError(t, err)

	msg := &Message{
		From:    "bob@example.com",
		To:      []string{"alice@localweb"},
		Subject: "Hello",
		Body:    "World",
		UID:     42,
	}
	mb.Messages = append(mb.Messages, msg)

	found := mb.GetByUID(42)
	assert.NotNil(t, found)
	assert.Equal(t, msg, found)

	notFound := mb.GetByUID(99)
	assert.Nil(t, notFound)
}

func TestMailbox_Counts(t *testing.T) {
	dir := t.TempDir()
	store := NewMailboxStore(dir)

	mb, err := store.GetOrCreateMailbox("alice")
	require.NoError(t, err)

	mb.Messages = []*Message{
		{Flags: FlagSeen},
		{Flags: 0},
		{Flags: FlagRecent},
	}

	assert.Equal(t, 3, mb.MessageCount())
	assert.Equal(t, 2, mb.UnseenCount())
	assert.Equal(t, 1, mb.RecentCount())
}

func TestMailbox_Expunge(t *testing.T) {
	dir := t.TempDir()
	store := NewMailboxStore(dir)

	mb, err := store.GetOrCreateMailbox("alice")
	require.NoError(t, err)

	msg1 := &Message{Subject: "Keep", Maildir: dir + "/keep"}
	msg2 := &Message{Subject: "Delete", Maildir: dir + "/delete", Flags: FlagDeleted}
	mb.Messages = []*Message{msg1, msg2}

	deleted := mb.Expunge()
	assert.Len(t, deleted, 1)
	assert.Equal(t, "Delete", deleted[0].Subject)
	assert.Len(t, mb.Messages, 1)
}

func TestMailbox_Append(t *testing.T) {
	dir := t.TempDir()
	store := NewMailboxStore(dir)

	mb, err := store.GetOrCreateMailbox("alice")
	require.NoError(t, err)

	raw := []byte("From: bob@example.com\r\nTo: alice@localweb\r\nSubject: Test\r\n\r\nBody")
	msg, err := mb.Append(raw)
	require.NoError(t, err)
	assert.Equal(t, "bob@example.com", msg.From)
	assert.Equal(t, "Test", msg.Subject)
	assert.Equal(t, "Body\n", msg.Body)
}

func TestParseMessage(t *testing.T) {
	raw := []byte("From: sender@example.com\r\nTo: recipient@example.com\r\nSubject: Test\r\nX-Custom: value\r\n\r\nHello World")
	msg, err := ParseMessage(raw)
	require.NoError(t, err)

	assert.Equal(t, "sender@example.com", msg.From)
	assert.Equal(t, []string{"recipient@example.com"}, msg.To)
	assert.Equal(t, "Test", msg.Subject)
	assert.Equal(t, "value", msg.Headers["X-Custom"])
	assert.Equal(t, "Hello World\n", msg.Body)
}

func TestFormatMaildirName(t *testing.T) {
	name := FormatMaildirName("abc123", "host.local")
	assert.Contains(t, name, ".")
	assert.Contains(t, name, "host.local")
}

func TestParseMaildirName(t *testing.T) {
	name := FormatMaildirName("abc123", "host.local")
	tm, err := ParseMaildirName(name)
	require.NoError(t, err)
	assert.False(t, tm.IsZero())
	assert.WithinDuration(t, time.Now(), tm, 2*time.Second)
}

func TestMailbox_CopyTo(t *testing.T) {
	dir := t.TempDir()
	store := NewMailboxStore(dir)

	src, err := store.GetOrCreateMailbox("alice")
	require.NoError(t, err)
	dst, err := store.GetOrCreateMailbox("bob")
	require.NoError(t, err)

	msg := &Message{
		From:    "alice@localweb",
		To:      []string{"bob@localweb"},
		Subject: "Copy Test",
		Body:    "Body",
		UID:     1,
	}
	require.NoError(t, src.StoreMessage(msg))

	require.NoError(t, dst.CopyTo(msg, dst, FlagSeen))
	assert.Equal(t, 1, dst.MessageCount())
	copied := dst.Messages[0]
	assert.Equal(t, FlagSeen, copied.Flags)
	assert.Equal(t, "Copy Test", copied.Subject)
}

func TestParseMessage_EmptyBody(t *testing.T) {
	raw := []byte("From: test@example.com\r\nTo: dest@example.com\r\nSubject: Hi\r\n\r\n")
	msg, err := ParseMessage(raw)
	require.NoError(t, err)
	assert.Equal(t, "test@example.com", msg.From)
	assert.Equal(t, "", msg.Body)
}

func TestMailbox_LoadMessages(t *testing.T) {
	dir := t.TempDir()
	store := NewMailboxStore(dir)

	mb, err := store.GetOrCreateMailbox("alice")
	require.NoError(t, err)

	msg := &Message{
		From:    "bob@example.com",
		To:      []string{"alice@localweb"},
		Subject: "Persist",
		Body:    "Data",
	}
	require.NoError(t, mb.StoreMessage(msg))

	mb2, _ := store.GetOrCreateMailbox("alice")
	require.NoError(t, mb2.LoadMessages())
	assert.Equal(t, 1, mb2.MessageCount())
	assert.Equal(t, "Persist", mb2.Messages[0].Subject)
}
