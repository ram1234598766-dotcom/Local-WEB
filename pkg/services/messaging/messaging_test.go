package messaging

import (
	"testing"
)

func TestChannelIDFromString(t *testing.T) {
	// ChannelID is [32]byte, test construction
	ch := ChannelID{1: 0x42}
	if ch[1] != 0x42 {
		t.Fatalf("expected 0x42 at index 1")
	}
}

func TestMemoryStoreAppendHistory(t *testing.T) {
	store := newMemoryStore()
	ch := ChannelID{1: 1}

	msg1 := Message{ID: "m1", ChannelID: ch, Content: []byte("hello")}
	msg2 := Message{ID: "m2", ChannelID: ch, Content: []byte("world")}

	if err := store.Append(ch, msg1); err != nil {
		t.Fatalf("append msg1: %v", err)
	}
	if err := store.Append(ch, msg2); err != nil {
		t.Fatalf("append msg2: %v", err)
	}

	history, err := store.History(ch, "", 10)
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(history))
	}
	if string(history[0].Content) != "hello" {
		t.Fatalf("expected 'hello', got %q", history[0].Content)
	}
	if string(history[1].Content) != "world" {
		t.Fatalf("expected 'world', got %q", history[1].Content)
	}
}

func TestMemoryStoreHistoryAfter(t *testing.T) {
	store := newMemoryStore()
	ch := ChannelID{1: 1}

	for i := 0; i < 5; i++ {
		msg := Message{ID: string(rune('a' + i)), ChannelID: ch, Content: []byte{byte(i)}}
		store.Append(ch, msg)
	}

	// Get messages after 'c'
	history, err := store.History(ch, "c", 10)
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("expected 2 messages after 'c', got %d", len(history))
	}
}

func TestMemoryStoreHistoryEmpty(t *testing.T) {
	store := newMemoryStore()
	ch := ChannelID{1: 1}

	history, err := store.History(ch, "", 10)
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(history) != 0 {
		t.Fatalf("expected 0 messages, got %d", len(history))
	}
}
