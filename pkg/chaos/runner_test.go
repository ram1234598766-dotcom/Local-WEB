package chaos

import (
	"context"
	"math/rand"
	"net"
	"sync"
	"testing"
	"time"
)

// mockPeerManager implements PeerManager for testing
type mockPeerManager struct {
	peers map[string]net.Conn
	mu    sync.Mutex
}

func newMockPeerManager() *mockPeerManager {
	return &mockPeerManager{
		peers: make(map[string]net.Conn),
	}
}

func (m *mockPeerManager) GetPeers() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	peers := make([]string, 0, len(m.peers))
	for p := range m.peers {
		peers = append(peers, p)
	}
	return peers
}

func (m *mockPeerManager) GetConn(peerID string) net.Conn {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.peers[peerID]
}

func (m *mockPeerManager) WrapConn(peerID string, wrapper func(net.Conn) net.Conn) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if conn, ok := m.peers[peerID]; ok {
		m.peers[peerID] = wrapper(conn)
		return nil
	}
	return nil
}

func TestChaosRunnerPartition(t *testing.T) {
	pm := newMockPeerManager()
	runner := NewChaosRunner(pm, nil)

	// Add a mock peer
	conn1, _ := net.Pipe()
	pm.peers["peer1"] = conn1

	scenario := ScenarioPartition(100 * time.Millisecond)
	runner.AddScenario(scenario)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := runner.RunScenario(ctx, "partition")
	if err != nil {
		t.Fatalf("RunScenario failed: %v", err)
	}

	results := runner.GetResults()
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if !results[0].Passed {
		t.Errorf("expected scenario to pass (timeout)")
	}
}

func TestChaosRunnerPacketLoss(t *testing.T) {
	pm := newMockPeerManager()
	runner := NewChaosRunner(pm, nil)

	conn1, _ := net.Pipe()
	pm.peers["peer1"] = conn1

	scenario := ScenarioPacketLoss(100*time.Millisecond, 0.5)
	runner.AddScenario(scenario)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := runner.RunScenario(ctx, "packet-loss")
	if err != nil {
		t.Fatalf("RunScenario failed: %v", err)
	}

	results := runner.GetResults()
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if !results[0].Passed {
		t.Errorf("expected scenario to pass (timeout)")
	}
}

func TestChaosRunnerHighLatency(t *testing.T) {
	pm := newMockPeerManager()
	runner := NewChaosRunner(pm, nil)

	conn1, _ := net.Pipe()
	pm.peers["peer1"] = conn1

	scenario := ScenarioHighLatency(100*time.Millisecond, 50*time.Millisecond)
	runner.AddScenario(scenario)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := runner.RunScenario(ctx, "high-latency")
	if err != nil {
		t.Fatalf("RunScenario failed: %v", err)
	}

	results := runner.GetResults()
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if !results[0].Passed {
		t.Errorf("expected scenario to pass (timeout)")
	}
}

func TestChaosRunnerMultipleScenarios(t *testing.T) {
	pm := newMockPeerManager()
	runner := NewChaosRunner(pm, nil)

	conn1, _ := net.Pipe()
	pm.peers["peer1"] = conn1

	scenarios := []Scenario{
		ScenarioPartition(50 * time.Millisecond),
		ScenarioPacketLoss(50*time.Millisecond, 0.3),
		ScenarioHighLatency(50*time.Millisecond, 20*time.Millisecond),
	}
	for _, s := range scenarios {
		runner.AddScenario(s)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Run all sequentially
	runner.RunAll(ctx)

	results := runner.GetResults()
	if len(results) != 3 {
		t.Errorf("expected 3 results, got %d", len(results))
	}
	for _, r := range results {
		if !r.Passed {
			t.Errorf("scenario %s did not pass", r.Scenario.Name)
		}
	}
}

func TestChaosRunnerTargetPeers(t *testing.T) {
	pm := newMockPeerManager()
	runner := NewChaosRunner(pm, nil)

	conn1, _ := net.Pipe()
	pm.peers["peer1"] = conn1
	conn3, _ := net.Pipe()
	pm.peers["peer2"] = conn3

	scenario := Scenario{
		Name:        "targeted-partition",
		Description: "Partition only peer1",
		Duration:    100 * time.Millisecond,
		Partition:   true,
		TargetPeers: []string{"peer1"},
		Enabled:     true,
	}
	runner.AddScenario(scenario)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := runner.RunScenario(ctx, "targeted-partition")
	if err != nil {
		t.Fatalf("RunScenario failed: %v", err)
	}

	results := runner.GetResults()
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
}

func TestDefaultChaosSuite(t *testing.T) {
	suite := DefaultChaosSuite(100 * time.Millisecond)
	if len(suite) == 0 {
		t.Fatal("empty suite")
	}

	expectedNames := []string{"partition", "high-latency", "packet-loss", "duplicate", "corruption", "mixed"}
	for i, s := range suite {
		if s.Name != expectedNames[i] {
			t.Errorf("suite[%d]: expected %s, got %s", i, expectedNames[i], s.Name)
		}
		if !s.Enabled {
			t.Errorf("scenario %s not enabled", s.Name)
		}
	}
}

// newTestChaosConn builds a chaosConn with the given faults. The fault state
// lives behind a shared pointer so a test (or the runner) can clear it.
func newTestChaosConn(conn net.Conn, s Scenario) *chaosConn {
	return &chaosConn{
		conn: conn,
		f: &chaosFaults{
			lossRate:    s.LossRate,
			latency:     s.Latency,
			duplicate:   s.Duplicate,
			partition:   s.Partition,
			corruptRate: s.CorruptRate,
		},
		rng: rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

func TestChaosConnPartition(t *testing.T) {
	conn1, _ := net.Pipe()
	c := newTestChaosConn(conn1, Scenario{Partition: true})

	_, err := c.Read(make([]byte, 10))
	if err != nil {
		t.Errorf("expected no error on partition read, got %v", err)
	}
}

func TestChaosConnLatency(t *testing.T) {
	conn1, conn2 := net.Pipe()
	// Write some data so Read doesn't block
	go func() {
		time.Sleep(1 * time.Millisecond)
		conn2.Write([]byte("test data"))
		conn2.Close()
	}()

	c := newTestChaosConn(conn1, Scenario{Latency: 10 * time.Millisecond})

	start := time.Now()
	_, _ = c.Read(make([]byte, 10))
	elapsed := time.Since(start)
	if elapsed < 5*time.Millisecond {
		t.Errorf("expected latency, got %v", elapsed)
	}
}

func TestChaosConnLoss(t *testing.T) {
	conn1, _ := net.Pipe()
	c := newTestChaosConn(conn1, Scenario{LossRate: 1.0}) // 100% loss

	// With 100% loss and no rng, the behavior depends on implementation
	// This test just ensures no panic
	_, _ = c.Read(make([]byte, 10))
}

func TestChaosRunnerStopAll(t *testing.T) {
	pm := newMockPeerManager()
	runner := NewChaosRunner(pm, nil)

	conn1, _ := net.Pipe()
	pm.peers["peer1"] = conn1

	scenario := ScenarioPartition(10 * time.Second)
	runner.AddScenario(scenario)

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	err := runner.RunScenario(ctx, "partition")
	// Context canceled, so error is expected
	if err == nil {
		t.Log("RunScenario returned nil (context canceled)")
	}

	runner.StopAll()
}

// The cleanup loop used to be empty, so a faulted connection stayed faulted
// for the rest of the process. Faults must be removable.
func TestChaosFaultsAreReversible(t *testing.T) {
	conn1, conn2 := net.Pipe()
	go func() {
		_, _ = conn2.Write([]byte("payload"))
	}()

	c := newTestChaosConn(conn1, Scenario{Partition: true})

	// While partitioned, reads return nothing.
	buf := make([]byte, 16)
	if n, _ := c.Read(buf); n != 0 {
		t.Fatalf("expected a partitioned read to yield nothing, got %d bytes", n)
	}

	c.f.clear()

	// The wrapper stays installed, but with faults cleared it forwards again.
	go func() {
		_, _ = conn2.Write([]byte("payload"))
	}()
	c.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := c.Read(buf)
	if err != nil {
		t.Fatalf("read after clearing faults: %v", err)
	}
	if n == 0 {
		t.Fatal("expected data to flow after faults were cleared")
	}
}

func TestChaosFaultsClearResetsEveryField(t *testing.T) {
	f := &chaosFaults{lossRate: 1, latency: time.Second, duplicate: 3, partition: true, corruptRate: 0.5}
	f.clear()
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.lossRate != 0 || f.latency != 0 || f.duplicate != 0 || f.partition || f.corruptRate != 0 {
		t.Fatalf("clear left fault state behind: loss=%v latency=%v dup=%d partition=%v corrupt=%v",
			f.lossRate, f.latency, f.duplicate, f.partition, f.corruptRate)
	}
}

// The duplication scenario previously decremented a counter and queued
// nothing, so it injected no duplication at all.
func TestChaosConnDuplicatesRead(t *testing.T) {
	conn1, conn2 := net.Pipe()
	c := newTestChaosConn(conn1, Scenario{Duplicate: 1})

	go func() {
		_, _ = conn2.Write([]byte("abc"))
	}()

	buf := make([]byte, 16)
	c.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := c.Read(buf)
	if err != nil {
		t.Fatalf("first read: %v", err)
	}
	if n == 0 {
		t.Fatal("first read yielded nothing")
	}

	// The queued duplicate is served without the peer sending anything more.
	c.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err = c.Read(buf)
	if err != nil {
		t.Fatalf("replay read: %v", err)
	}
	if n == 0 {
		t.Fatal("expected the read to be replayed as a duplicate")
	}
}

// StopAll could never work: the running map was only ever deleted from, so
// there was never a cancel func to call.
func TestStopAllCancelsRunningScenario(t *testing.T) {
	pm := newMockPeerManager()
	runner := NewChaosRunner(pm, nil)
	pm.peers["peer1"], _ = net.Pipe()

	runner.AddScenario(ScenarioPartition(30 * time.Second))

	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = runner.RunScenario(context.Background(), "partition")
	}()

	// Wait for the scenario to register itself as running.
	deadline := time.Now().Add(3 * time.Second)
	registered := false
	for time.Now().Before(deadline) {
		runner.mu.RLock()
		n := len(runner.running)
		runner.mu.RUnlock()
		if n > 0 {
			registered = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !registered {
		t.Fatal("scenario never registered as running, so StopAll could never stop it")
	}

	runner.StopAll()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("StopAll did not stop the running scenario")
	}
}

// A scenario stopped early did not run for its duration, so it must not be
// reported as passed.
func TestStoppedScenarioIsNotPassed(t *testing.T) {
	pm := newMockPeerManager()
	runner := NewChaosRunner(pm, nil)
	pm.peers["peer1"], _ = net.Pipe()

	runner.AddScenario(ScenarioPartition(30 * time.Second))

	go func() {
		time.Sleep(50 * time.Millisecond)
		runner.StopAll()
	}()
	_ = runner.RunScenario(context.Background(), "partition")

	results := runner.GetResults()
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Passed {
		t.Fatal("a scenario stopped before its duration must not report Passed")
	}
}
