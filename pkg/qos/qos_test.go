package qos

import (
	"context"
	"testing"
	"time"
)

func TestTokenBucket(t *testing.T) {
	tb := NewTokenBucket(100, 10) // 100 capacity, 10 tokens/sec

	// Should be able to take up to capacity
	if !tb.Take(100) {
		t.Fatal("expected to take 100 tokens")
	}

	// Should not be able to take more
	if tb.Take(1) {
		t.Fatal("expected to fail taking 1 token")
	}

	// Wait for refill
	time.Sleep(110 * time.Millisecond)
	if !tb.Take(1) {
		t.Fatal("expected to take 1 token after refill")
	}
}

func TestTokenBucketWait(t *testing.T) {
	tb := NewTokenBucket(10, 100) // 10 capacity, 100 tokens/sec

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	// Should be able to wait for tokens
	if err := tb.Wait(ctx, 5); err != nil {
		t.Fatalf("Wait failed: %v", err)
	}
}

func TestQoSClass(t *testing.T) {
	qc := NewQoSClass(QoSConfig{
		Name:      "test",
		Bandwidth: 1000,
		Burst:     1000,
	})

	ctx := context.Background()

	// Should be able to send within burst
	if err := qc.Send(ctx, make([]byte, 500)); err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	stats := qc.GetStats()
	if stats.BytesSent != 500 {
		t.Errorf("expected 500 bytes sent, got %d", stats.BytesSent)
	}
	if stats.PacketsSent != 1 {
		t.Errorf("expected 1 packet sent, got %d", stats.PacketsSent)
	}
}

func TestQoSClassDropped(t *testing.T) {
	qc := NewQoSClass(QoSConfig{
		Name:      "test",
		Bandwidth: 10000, // 10 KB/s for faster test
		Burst:     100,
	})

	ctx := context.Background()

	// Send more than burst - with Wait, it should delay not drop
	// First packet succeeds immediately
	if err := qc.Send(ctx, make([]byte, 100)); err != nil {
		t.Fatalf("first send failed: %v", err)
	}

	// Second packet should wait for refill (10000 bytes/sec = 10 bytes/ms)
	// 100 bytes need 100 tokens, refill rate is 10000 tokens/sec
	// Wait up to 20ms for second packet
	ctx2, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := qc.Send(ctx2, make([]byte, 100)); err != nil {
		// Expected timeout since 10ms refill for 100 bytes
	}

	stats := qc.GetStats()
	if stats.BytesSent != 200 {
		t.Errorf("expected 200 bytes sent, got %d", stats.BytesSent)
	}
	if stats.PacketsSent != 2 {
		t.Errorf("expected 2 packets sent, got %d", stats.PacketsSent)
	}
}

func TestQoSManager(t *testing.T) {
	qm := NewQoSManager(PolicyPriority)

	// Default class should exist
	defaultClass := qm.GetClass("default")
	if defaultClass == nil {
		t.Fatal("default class not found")
	}

	// Service classes should exist
	voiceClass := qm.GetClass("voice")
	if voiceClass == nil {
		t.Fatal("voice class not found")
	}

	// Send through service
	ctx := context.Background()
	if err := qm.Send(ctx, "voice", "", make([]byte, 100)); err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	stats := qm.GetStats()
	if stats["voice"].BytesSent != 100 {
		t.Errorf("expected 100 bytes sent for voice, got %d", stats["voice"].BytesSent)
	}
}

func TestQoSManagerCustomClass(t *testing.T) {
	qm := NewQoSManager(PolicyPriority)

	// Register custom class
	qm.RegisterServiceClass("custom", QoSConfig{
		Name:      "custom",
		Priority:  100,
		Bandwidth: 5000,
		Burst:     2000,
	})

	class := qm.GetClass("custom")
	if class == nil {
		t.Fatal("custom class not found")
	}
	if class.config.Bandwidth != 5000 {
		t.Errorf("expected bandwidth 5000, got %d", class.config.Bandwidth)
	}
}

func TestQoSManagerPeerClass(t *testing.T) {
	qm := NewQoSManager(PolicyPriority)

	qm.RegisterPeerClass("peer1", QoSConfig{
		Name:      "peer1",
		Priority:  50,
		Bandwidth: 2000,
		Burst:     1000,
	})

	class := qm.GetClass("peer1")
	if class == nil {
		t.Fatal("peer class not found")
	}
}

func TestQoSManagerPriority(t *testing.T) {
	qm := NewQoSManager(PolicyPriority)

	// Voice should have higher priority than files
	voice := qm.GetClass("voice")
	files := qm.GetClass("files")

	if voice.config.Priority <= files.config.Priority {
		t.Errorf("voice priority (%d) should be higher than files (%d)", voice.config.Priority, files.config.Priority)
	}
}

func TestQoSManagerSetBandwidth(t *testing.T) {
	qm := NewQoSManager(PolicyPriority)

	err := qm.SetBandwidth("voice", 5000)
	if err != nil {
		t.Fatalf("SetBandwidth failed: %v", err)
	}

	class := qm.GetClass("voice")
	if class.config.Bandwidth != 5000 {
		t.Errorf("expected bandwidth 5000, got %d", class.config.Bandwidth)
	}
}

func TestQoSManagerSetPriority(t *testing.T) {
	qm := NewQoSManager(PolicyPriority)

	err := qm.SetPriority("files", 20)
	if err != nil {
		t.Fatalf("SetPriority failed: %v", err)
	}

	class := qm.GetClass("files")
	if class.config.Priority != 20 {
		t.Errorf("expected priority 20, got %d", class.config.Priority)
	}
}

func TestQoSManagerPolicy(t *testing.T) {
	qm := NewQoSManager(PolicyPriority)
	if qm.GetPolicy() != PolicyPriority {
		t.Errorf("expected PolicyPriority, got %d", qm.GetPolicy())
	}

	qm.SetPolicy(PolicyWFQ)
	if qm.GetPolicy() != PolicyWFQ {
		t.Errorf("expected PolicyWFQ, got %d", qm.GetPolicy())
	}
}

func TestQoSContextKeys(t *testing.T) {
	ctx := context.Background()
	ctx = WithQoSService(ctx, "voice")
	ctx = WithQoSPeer(ctx, "peer1")

	service, ok := GetQoSService(ctx)
	if !ok || service != "voice" {
		t.Errorf("expected service 'voice', got %q", service)
	}

	peer, ok := GetQoSPeer(ctx)
	if !ok || peer != "peer1" {
		t.Errorf("expected peer 'peer1', got %q", peer)
	}
}

func TestHTBHierarchy(t *testing.T) {
	h := NewHTBHierarchy()

	root := h.GetNode("root")
	if root == nil {
		t.Fatal("root not found")
	}

	voice := h.AddClass("voice", "root", 2*1024*1024, 4*1024*1024)
	if voice == nil {
		t.Fatal("voice node not created")
	}
	if voice.parent != root {
		t.Error("voice parent should be root")
	}

	files := h.AddClass("files", "root", 5*1024*1024, 10*1024*1024)
	if files == nil {
		t.Fatal("files node not created")
	}

	// Test borrow
	if !voice.Borrow(1024) {
		t.Error("expected borrow to succeed")
	}
}

func TestHTBBorrow(t *testing.T) {
	h := NewHTBHierarchy()
	voice := h.AddClass("voice", "root", 1000, 2000)

	// Should be able to borrow up to rate
	if !voice.Borrow(1000) {
		t.Error("expected to borrow 1000")
	}

	// Should not be able to borrow more than ceil
	if voice.Borrow(2000) {
		t.Error("expected to fail borrowing 2000")
	}
}

func TestTokenBucketRefill(t *testing.T) {
	tb := NewTokenBucket(100, 1000) // 1000 tokens/sec

	// Use all tokens
	for i := 0; i < 100; i++ {
		if !tb.Take(1) {
			t.Fatalf("failed to take token %d", i)
		}
	}

	// Wait for refill
	time.Sleep(110 * time.Millisecond)

	// Should have ~110 tokens
	if tb.Available() < 100 {
		t.Errorf("expected ~110 tokens after refill, got %d", tb.Available())
	}
}

func TestQoSManagerStats(t *testing.T) {
	qm := NewQoSManager(PolicyPriority)

	ctx := context.Background()
	qm.Send(ctx, "voice", "", make([]byte, 100))
	qm.Send(ctx, "voice", "", make([]byte, 200))
	qm.Send(ctx, "files", "", make([]byte, 500))

	stats := qm.GetStats()
	if stats["voice"].BytesSent != 300 {
		t.Errorf("expected voice 300 bytes, got %d", stats["voice"].BytesSent)
	}
	if stats["files"].BytesSent != 500 {
		t.Errorf("expected files 500 bytes, got %d", stats["files"].BytesSent)
	}
}
