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

func TestChaosConnPartition(t *testing.T) {
	conn1, _ := net.Pipe()
	c := &chaosConn{
		conn:      conn1,
		partition: true,
		rng:       nil,
	}

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

	c := &chaosConn{
		conn:      conn1,
		latency:   10 * time.Millisecond,
		partition: false,
		lossRate:  0,
		rng:       rand.New(rand.NewSource(time.Now().UnixNano())),
	}

	start := time.Now()
	_, _ = c.Read(make([]byte, 10))
	elapsed := time.Since(start)
	if elapsed < 5*time.Millisecond {
		t.Errorf("expected latency, got %v", elapsed)
	}
}

func TestChaosConnLoss(t *testing.T) {
	conn1, _ := net.Pipe()
	c := &chaosConn{
		conn:      conn1,
		lossRate:  1.0, // 100% loss
		partition: false,
		rng:       rand.New(rand.NewSource(time.Now().UnixNano())),
	}

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
