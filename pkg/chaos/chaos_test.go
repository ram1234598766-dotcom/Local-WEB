package chaos

import (
	"net"
	"testing"
	"time"
)

func TestFaultyConnPacketLoss(t *testing.T) {
	c := NewFaultyConn(100, 0.5)

	received := 0
	buf := make([]byte, 100)
	for i := 0; i < 100; i++ {
		n, err := c.Read(buf)
		if err == nil && n > 0 {
			received++
		}
	}

	if received == 0 {
		t.Fatal("expected at least some packets to get through with 50% loss")
	}
	if received > 80 {
		t.Errorf("expected packet loss to reduce received count, got %d/100", received)
	}
}

func TestFaultyConnNoLoss(t *testing.T) {
	c := NewFaultyConn(100, 0.0)

	total := 0
	buf := make([]byte, 100)
	for i := 0; i < 10; i++ {
		n, err := c.Read(buf)
		if err == nil && n > 0 {
			total++
		}
	}

	if total != 10 {
		t.Errorf("expected 10/10 packets with 0%% loss, got %d", total)
	}
}

func TestFaultyConnFullLoss(t *testing.T) {
	c := NewFaultyConn(100, 1.0)

	buf := make([]byte, 100)
	count, _ := c.Read(buf)
	if count != 0 {
		t.Errorf("expected 0 bytes with 100%% loss, got %d", count)
	}
}

func TestFaultyConnLatency(t *testing.T) {
	c := NewFaultyConn(10, 0.0)
	WithLatency(c, 50*time.Millisecond)

	start := time.Now()
	c.Read(make([]byte, 10))
	elapsed := time.Since(start)

	if elapsed < 40*time.Millisecond {
		t.Errorf("expected latency of >=40ms, got %v", elapsed)
	}
}

func TestFaultyConnWrite(t *testing.T) {
	c := NewFaultyConn(100, 0.0)

	written, err := c.Write([]byte("hello world"))
	if err != nil {
		t.Fatalf("write failed: %v", err)
	}
	if written != 11 {
		t.Errorf("expected 11 bytes written, got %d", written)
	}
}

func TestFaultyConnBuffer(t *testing.T) {
	c := NewFaultyConn(100, 0.0)
	c.SetBuffer([]byte("test"))

	buf := make([]byte, 4)
	n, err := c.Read(buf)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if n != 4 || string(buf) != "test" {
		t.Errorf("expected 'test', got %q (%d bytes)", buf[:n], n)
	}
}

func TestFaultyConnDuplicate(t *testing.T) {
	c := NewFaultyConn(50, 0.0)
	WithDuplicate(c, 1)

	buf := make([]byte, 50)
	n1, _ := c.Read(buf)
	if n1 != 50 {
		t.Errorf("expected 50 bytes on first read, got %d", n1)
	}

	// With duplicate=1, the next read should return the same data again
	n2, _ := c.Read(buf)
	if n2 != 50 {
		t.Errorf("expected 50 bytes on duplicate read, got %d", n2)
	}
}

func TestFaultyConnClose(t *testing.T) {
	c := NewFaultyConn(100, 0.0)
	if err := c.Close(); err != nil {
		t.Fatalf("close failed: %v", err)
	}
}

func TestFaultyConnImplementsNetConn(t *testing.T) {
	var _ net.Conn = NewFaultyConn(100, 0.0)
}

func TestFaultyConnPartition(t *testing.T) {
	c := NewFaultyConn(50, 0.0)
	WithPartition(c, true)

	buf := make([]byte, 50)
	n, _ := c.Read(buf)
	if n != 0 {
		t.Errorf("expected 0 bytes during partition, got %d", n)
	}

	WithPartition(c, false)
	c.SetBuffer([]byte("recovered"))
	n2, _ := c.Read(buf)
	if n2 != 9 {
		t.Errorf("expected 11 bytes after partition ends, got %d", n2)
	}
}

func TestNewPipe(t *testing.T) {
	a, b := NewPipe(100, 0.0)

	go func() {
		time.Sleep(10 * time.Millisecond)
		a.Write([]byte("hello"))
	}()

	buf := make([]byte, 5)
	var n int
	for i := 0; i < 100; i++ {
		n, _ = b.Read(buf)
		if n > 0 {
			break
		}
		time.Sleep(1 * time.Millisecond)
	}

	if n != 5 || string(buf[:n]) != "hello" {
		t.Errorf("got %q, want 'hello'", buf[:n])
	}
}

func TestPipePacketLoss(t *testing.T) {
	a, b := NewPipe(50, 0.8)

	sent := 0
	received := 0
	for i := 0; i < 50; i++ {
		a.Write([]byte{byte(i)})
		sent++
		// Use a timeout since with 80% loss, some reads will hit EOF
		done := make(chan struct{})
		var n int
		go func() {
			buf := make([]byte, 1)
			n, _ = b.Read(buf)
			close(done)
		}()
		select {
		case <-done:
			if n > 0 {
				received++
			}
		case <-time.After(100 * time.Millisecond):
		}
	}

	if received == 0 {
		t.Error("expected at least some packets through with low loss")
	}
}
