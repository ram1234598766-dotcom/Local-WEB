package link

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"
)

// Phase 6.3: multipath was writing every payload to every active link for all
// modes except failover, so the receiver got the stream once per link. These
// tests pin the corrected contract: one write, exactly one link, and a per-mode
// rule for which link that is.

// countingConn records writes and can be told to fail, so a test can observe
// which link carried a payload and whether it was retried elsewhere.
type countingConn struct {
	mu      sync.Mutex
	written [][]byte
	fail    error
}

func (c *countingConn) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.fail != nil {
		return 0, c.fail
	}
	buf := make([]byte, len(p))
	copy(buf, p)
	c.written = append(c.written, buf)
	return len(p), nil
}

func (c *countingConn) Read([]byte) (int, error) { return 0, nil }

func (c *countingConn) Close() error                     { return nil }
func (c *countingConn) LocalAddr() net.Addr              { return nil }
func (c *countingConn) RemoteAddr() net.Addr             { return nil }
func (c *countingConn) SetDeadline(time.Time) error      { return nil }
func (c *countingConn) SetReadDeadline(time.Time) error  { return nil }
func (c *countingConn) SetWriteDeadline(time.Time) error { return nil }

func (c *countingConn) writes() [][]byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([][]byte, len(c.written))
	copy(out, c.written)
	return out
}

func (c *countingConn) totalBytes() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	total := 0
	for _, w := range c.written {
		total += len(w)
	}
	return total
}

func (c *countingConn) setFail(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.fail = err
}

// threeLinkPeer builds a manager with one peer reachable over three modes, each
// backed by its own countingConn, and returns those conns keyed by mode.
func threeLinkPeer(t *testing.T, agg AggregationMode, latency map[LinkMode]time.Duration) (*MultiPathManager, [32]byte, map[LinkMode]*countingConn) {
	t.Helper()

	modes := []LinkMode{ModeBLE, ModeWiFiDirect, ModeWiFiStation}
	conns := make(map[LinkMode]*countingConn, len(modes))
	links := make([]Link, 0, len(modes))
	for _, mode := range modes {
		cc := &countingConn{}
		conns[mode] = cc
		fl := newFakeLink("link-"+mode.String(), mode)
		fl.setConnect(func(string) (net.Conn, error) { return cc, nil })
		links = append(links, fl)
	}

	m := NewMultiPathManager(MultiPathConfig{
		Links:       links,
		MaxLinks:    3,
		Aggregation: &agg,
	})
	t.Cleanup(func() { m.Stop() })

	id := testPeerID(99)
	for _, mode := range modes {
		lat := 10 * time.Millisecond
		if l, ok := latency[mode]; ok {
			lat = l
		}
		m.handleEvent(PeerEvent{
			Type: PeerDiscovered,
			Peer: PeerInfo{
				ID: id, Addrs: []string{"10.0.0.1:4443"},
				LinkMode: mode, Latency: lat,
			},
		})
	}
	if _, err := m.ConnectToPeer(context.Background(), &PeerInfo{ID: id}); err != nil {
		t.Fatalf("ConnectToPeer: %v", err)
	}
	return m, id, conns
}

func sumWrites(conns map[LinkMode]*countingConn) int {
	total := 0
	for _, c := range conns {
		total += c.totalBytes()
	}
	return total
}

func TestSendToPeerWritesExactlyOnceAcrossLinks(t *testing.T) {
	payload := []byte("one write, one link")

	for _, agg := range []AggregationMode{
		AggregationFailover, AggregationRoundRobin,
		AggregationBandwidth, AggregationLatency,
	} {
		t.Run(agg.String(), func(t *testing.T) {
			m, id, conns := threeLinkPeer(t, agg, nil)

			n, err := m.SendToPeer(id, payload)
			if err != nil {
				t.Fatalf("SendToPeer: %v", err)
			}
			if n != len(payload) {
				t.Errorf("n = %d, want %d", n, len(payload))
			}

			// The decisive assertion: the payload appears exactly once in total
			// across all three links, never once per link.
			if got := sumWrites(conns); got != len(payload) {
				t.Errorf("total bytes across all links = %d, want %d; the payload was duplicated", got, len(payload))
			}

			carriers := 0
			for mode, c := range conns {
				if c.totalBytes() > 0 {
					carriers++
					if got := c.writes(); len(got) != 1 || string(got[0]) != string(payload) {
						t.Errorf("%s received %d writes, want exactly the payload", mode, len(got))
					}
				}
			}
			if carriers != 1 {
				t.Errorf("%d links carried the payload, want exactly 1", carriers)
			}
		})
	}
}

func TestSendToPeerRoundRobinSpreadsAcrossLinks(t *testing.T) {
	m, id, conns := threeLinkPeer(t, AggregationRoundRobin, nil)

	const sends = 6
	for i := 0; i < sends; i++ {
		if _, err := m.SendToPeer(id, []byte("chunk")); err != nil {
			t.Fatalf("send #%d: %v", i, err)
		}
	}

	total := 0
	for mode, c := range conns {
		got := c.totalBytes()
		total += got
		if got == 0 {
			t.Errorf("%s received nothing; round-robin is not distributing", mode)
		}
	}
	if total != sends*len("chunk") {
		t.Errorf("total = %d, want %d", total, sends*len("chunk"))
	}

	// Round-robin must actually alternate rather than always picking the
	// primary, so no single link may carry every write.
	for mode, c := range conns {
		if c.totalBytes() == sends*len("chunk") {
			t.Errorf("%s carried every write; the cursor never advanced", mode)
		}
	}
}

func TestSendToPeerFailoverRetriesOnTheNextLink(t *testing.T) {
	m, id, conns := threeLinkPeer(t, AggregationFailover, nil)

	links, _ := m.GetPeerLinks(id)
	primaryMode := links != nil
	_ = primaryMode

	// Determine the current primary, then make that link fail.
	var primary LinkMode
	{
		m.mu.RLock()
		pls := m.peerLinks[id]
		m.mu.RUnlock()
		pls.mu.RLock()
		primary = pls.primary
		pls.mu.RUnlock()
	}

	conns[primary].setFail(errors.New("link down"))

	payload := []byte("resend me")
	if _, err := m.SendToPeer(id, payload); err != nil {
		t.Fatalf("failover should have retried another link, got %v", err)
	}

	if got := sumWrites(conns); got != len(payload) {
		t.Errorf("total bytes = %d, want %d; the payload was lost or duplicated", got, len(payload))
	}
	// Exactly one non-failing link must have taken it.
	carriers := 0
	for mode, c := range conns {
		if c.totalBytes() > 0 {
			carriers++
			if mode == primary {
				t.Error("the failing link reported a successful write")
			}
		}
	}
	if carriers != 1 {
		t.Errorf("%d links carried the retry, want 1", carriers)
	}
}

func TestSendToPeerLatencyModePrefersTheFastestLink(t *testing.T) {
	latency := map[LinkMode]time.Duration{
		ModeBLE:         80 * time.Millisecond,
		ModeWiFiDirect:  5 * time.Millisecond,
		ModeWiFiStation: 40 * time.Millisecond,
	}
	m, id, conns := threeLinkPeer(t, AggregationLatency, latency)

	if _, err := m.SendToPeer(id, []byte("fast path")); err != nil {
		t.Fatalf("SendToPeer: %v", err)
	}

	if got := conns[ModeWiFiDirect].totalBytes(); got != len("fast path") {
		t.Errorf("the 5ms link received %d bytes, want %d; the lowest-latency link should win",
			got, len("fast path"))
	}
	if got := sumWrites(conns); got != len("fast path") {
		t.Errorf("total = %d, want %d", got, len("fast path"))
	}
}

func TestSendToPeerBandwidthModeFollowsMeasuredThroughput(t *testing.T) {
	m, id, conns := threeLinkPeer(t, AggregationBandwidth, nil)

	var primary LinkMode
	{
		m.mu.RLock()
		pls := m.peerLinks[id]
		m.mu.RUnlock()
		pls.mu.RLock()
		primary = pls.primary
		pls.mu.RUnlock()
	}

	// The link that is not the primary has demonstrably moved more bytes, so a
	// bandwidth-weighted pick must prefer it.
	other := ModeWiFiDirect
	if primary == ModeWiFiDirect {
		other = ModeBLE
	}
	m.mu.RLock()
	pls := m.peerLinks[id]
	m.mu.RUnlock()
	pls.mu.Lock()
	pls.connections[other].BytesSent = 1 << 20
	pls.mu.Unlock()

	if _, err := m.SendToPeer(id, []byte("weighted")); err != nil {
		t.Fatalf("SendToPeer: %v", err)
	}

	if got := conns[other].totalBytes(); got != len("weighted") {
		t.Errorf("the link with more measured throughput received %d bytes, want %d", got, len("weighted"))
	}
	if got := sumWrites(conns); got != len("weighted") {
		t.Errorf("total = %d, want %d", got, len("weighted"))
	}
}

func TestSendToPeerErrorsWhenEveryLinkFails(t *testing.T) {
	m, id, conns := threeLinkPeer(t, AggregationFailover, nil)
	for _, c := range conns {
		c.setFail(errors.New("all down"))
	}

	n, err := m.SendToPeer(id, []byte("doomed"))
	if err == nil {
		t.Fatal("SendToPeer succeeded with every link failing, want an error")
	}
	if n != 0 {
		t.Errorf("n = %d, want 0", n)
	}
	if got := sumWrites(conns); got != 0 {
		t.Errorf("total bytes = %d, want 0", got)
	}
}
