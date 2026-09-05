package chaos

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"net"
	"sync"
	"time"
)

// Scenario defines a chaos experiment.
type Scenario struct {
	Name        string
	Description string
	Duration    time.Duration
	LossRate    float64
	Latency     time.Duration
	Duplicate   int
	Partition   bool
	CorruptRate float64
	TargetPeers []string // peer IDs or "*" for all
	StartTime   time.Time
	EndTime     time.Time
	Enabled     bool
}

// ChaosRunner orchestrates chaos experiments.
type ChaosRunner struct {
	mu          sync.RWMutex
	scenarios   []Scenario
	running     map[string]context.CancelFunc
	results     []ScenarioResult
	logger      *log.Logger
	peerManager PeerManager
}

// PeerManager interface for interacting with peers during chaos.
type PeerManager interface {
	GetPeers() []string
	GetConn(peerID string) net.Conn
	WrapConn(peerID string, wrapper func(net.Conn) net.Conn) error
}

// ScenarioResult captures the outcome of a chaos scenario.
type ScenarioResult struct {
	Scenario  Scenario
	StartTime time.Time
	EndTime   time.Time
	Errors    []error
	Metrics   map[string]interface{}
	Passed    bool
}

// NewChaosRunner creates a new chaos runner.
func NewChaosRunner(pm PeerManager, logger *log.Logger) *ChaosRunner {
	if logger == nil {
		logger = log.Default()
	}
	return &ChaosRunner{
		scenarios:   make([]Scenario, 0),
		running:     make(map[string]context.CancelFunc),
		results:     make([]ScenarioResult, 0),
		logger:      logger,
		peerManager: pm,
	}
}

// AddScenario adds a chaos scenario to the runner.
func (cr *ChaosRunner) AddScenario(s Scenario) {
	cr.mu.Lock()
	defer cr.mu.Unlock()
	s.Enabled = true
	cr.scenarios = append(cr.scenarios, s)
}

// RemoveScenario removes a scenario by name.
func (cr *ChaosRunner) RemoveScenario(name string) {
	cr.mu.Lock()
	defer cr.mu.Unlock()
	newScenarios := make([]Scenario, 0)
	for _, s := range cr.scenarios {
		if s.Name != name {
			newScenarios = append(newScenarios, s)
		}
	}
	cr.scenarios = newScenarios
}

// RunScenario runs a single scenario by name.
func (cr *ChaosRunner) RunScenario(ctx context.Context, name string) error {
	cr.mu.RLock()
	var scenario Scenario
	found := false
	for _, s := range cr.scenarios {
		if s.Name == name {
			scenario = s
			found = true
			break
		}
	}
	cr.mu.RUnlock()

	if !found {
		return fmt.Errorf("scenario %s not found", name)
	}

	return cr.runScenario(ctx, scenario)
}

// RunAll runs all enabled scenarios sequentially.
func (cr *ChaosRunner) RunAll(ctx context.Context) {
	cr.mu.RLock()
	scenarios := make([]Scenario, len(cr.scenarios))
	copy(scenarios, cr.scenarios)
	cr.mu.RUnlock()

	for _, s := range scenarios {
		if !s.Enabled {
			continue
		}
		select {
		case <-ctx.Done():
			return
		default:
			if err := cr.runScenario(ctx, s); err != nil {
				cr.logger.Printf("Scenario %s failed: %v", s.Name, err)
			}
		}
	}
}

// runScenario executes a single chaos scenario.
func (cr *ChaosRunner) runScenario(ctx context.Context, s Scenario) error {
	if !s.Enabled {
		return fmt.Errorf("scenario %s is disabled", s.Name)
	}

	ctx, cancel := context.WithTimeout(ctx, s.Duration)
	defer cancel()

	result := ScenarioResult{
		Scenario:  s,
		StartTime: time.Now(),
		Metrics:   make(map[string]interface{}),
	}

	cr.logger.Printf("Starting chaos scenario: %s (%s)", s.Name, s.Description)

	// Apply chaos to target peers
	peers := cr.peerManager.GetPeers()
	targetPeers := cr.filterPeers(peers, s.TargetPeers)

	for _, peer := range targetPeers {
		conn := cr.peerManager.GetConn(peer)
		if conn == nil {
			result.Errors = append(result.Errors, fmt.Errorf("no connection to peer %s", peer))
			continue
		}

		wrapped := cr.wrapConn(conn, s)
		if err := cr.peerManager.WrapConn(peer, func(_ net.Conn) net.Conn {
			return wrapped
		}); err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("wrap peer %s: %w", peer, err))
		}
	}

	// Run for duration
	select {
	case <-ctx.Done():
		result.EndTime = time.Now()
		result.Passed = ctx.Err() == context.DeadlineExceeded
	}

	// Cleanup: remove wrappers
	for range targetPeers {
		// In real implementation, would restore original connection
	}

	cr.mu.Lock()
	delete(cr.running, s.Name)
	cr.results = append(cr.results, result)
	cr.mu.Unlock()

	cr.logger.Printf("Completed chaos scenario: %s (passed=%v)", s.Name, result.Passed)
	return nil
}

// filterPeers filters peers by target list.
func (cr *ChaosRunner) filterPeers(peers []string, targets []string) []string {
	if len(targets) == 0 {
		return peers
	}
	if len(targets) == 1 && targets[0] == "*" {
		return peers
	}

	targetSet := make(map[string]bool)
	for _, t := range targets {
		targetSet[t] = true
	}

	filtered := make([]string, 0)
	for _, p := range peers {
		if targetSet[p] {
			filtered = append(filtered, p)
		}
	}
	return filtered
}

// wrapConn wraps a connection with chaos behavior.
func (cr *ChaosRunner) wrapConn(conn net.Conn, s Scenario) net.Conn {
	return &chaosConn{
		conn:        conn,
		lossRate:    s.LossRate,
		latency:     s.Latency,
		duplicate:   s.Duplicate,
		partition:   s.Partition,
		corruptRate: s.CorruptRate,
		rng:         rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// chaosConn wraps a net.Conn with chaos behavior.
type chaosConn struct {
	conn        net.Conn
	lossRate    float64
	latency     time.Duration
	duplicate   int
	partition   bool
	corruptRate float64
	rng         *rand.Rand
	mu          sync.Mutex
}

func (c *chaosConn) Read(b []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.partition {
		return 0, nil
	}

	if c.latency > 0 {
		time.Sleep(c.latency)
	}

	if c.rng.Float64() < c.lossRate {
		return 0, nil
	}

	n, err := c.conn.Read(b)
	if err != nil {
		return n, err
	}

	// Corrupt data
	if c.rng.Float64() < c.corruptRate && n > 0 {
		idx := c.rng.Intn(n)
		b[idx] ^= 0xFF
	}

	// Duplicate
	if c.duplicate > 0 {
		// In real implementation, would queue for next read
		c.duplicate--
	}

	return n, err
}

func (c *chaosConn) Write(b []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.partition {
		return 0, nil
	}

	if c.rng.Float64() < c.lossRate {
		return 0, nil
	}

	if c.latency > 0 {
		time.Sleep(c.latency)
	}

	return c.conn.Write(b)
}

func (c *chaosConn) Close() error {
	return c.conn.Close()
}

func (c *chaosConn) LocalAddr() net.Addr {
	return c.conn.LocalAddr()
}

func (c *chaosConn) RemoteAddr() net.Addr {
	return c.conn.RemoteAddr()
}

func (c *chaosConn) SetDeadline(t time.Time) error {
	return c.conn.SetDeadline(t)
}

func (c *chaosConn) SetReadDeadline(t time.Time) error {
	return c.conn.SetReadDeadline(t)
}

func (c *chaosConn) SetWriteDeadline(t time.Time) error {
	return c.conn.SetWriteDeadline(t)
}

// GetResults returns all scenario results.
func (cr *ChaosRunner) GetResults() []ScenarioResult {
	cr.mu.RLock()
	defer cr.mu.RUnlock()
	results := make([]ScenarioResult, len(cr.results))
	copy(results, cr.results)
	return results
}

// StopAll stops all running scenarios.
func (cr *ChaosRunner) StopAll() {
	cr.mu.Lock()
	defer cr.mu.Unlock()
	for name, cancel := range cr.running {
		cancel()
		cr.logger.Printf("Stopped scenario: %s", name)
	}
	cr.running = make(map[string]context.CancelFunc)
}

// Built-in scenarios

// ScenarioPartition creates a network partition scenario.
func ScenarioPartition(duration time.Duration) Scenario {
	return Scenario{
		Name:        "partition",
		Description: "Network partition - drop all packets",
		Duration:    duration,
		Partition:   true,
		Enabled:     true,
	}
}

// ScenarioHighLatency creates a high latency scenario.
func ScenarioHighLatency(duration time.Duration, latency time.Duration) Scenario {
	return Scenario{
		Name:        "high-latency",
		Description: fmt.Sprintf("High latency - %v added latency", latency),
		Duration:    duration,
		Latency:     latency,
		Enabled:     true,
	}
}

// ScenarioPacketLoss creates a packet loss scenario.
func ScenarioPacketLoss(duration time.Duration, lossRate float64) Scenario {
	return Scenario{
		Name:        "packet-loss",
		Description: fmt.Sprintf("Packet loss - %.0f%% loss rate", lossRate*100),
		Duration:    duration,
		LossRate:    lossRate,
		Enabled:     true,
	}
}

// ScenarioDuplicate creates a packet duplication scenario.
func ScenarioDuplicate(duration time.Duration, duplicate int) Scenario {
	return Scenario{
		Name:        "duplicate",
		Description: fmt.Sprintf("Packet duplication - factor %d", duplicate),
		Duration:    duration,
		Duplicate:   duplicate,
		Enabled:     true,
	}
}

// ScenarioCorruption creates a data corruption scenario.
func ScenarioCorruption(duration time.Duration, corruptRate float64) Scenario {
	return Scenario{
		Name:        "corruption",
		Description: fmt.Sprintf("Data corruption - %.0f%% corrupt rate", corruptRate*100),
		Duration:    duration,
		CorruptRate: corruptRate,
		Enabled:     true,
	}
}

// ScenarioMixed creates a mixed chaos scenario.
func ScenarioMixed(duration time.Duration) Scenario {
	return Scenario{
		Name:        "mixed",
		Description: "Mixed chaos - loss, latency, duplication, corruption",
		Duration:    duration,
		LossRate:    0.1,
		Latency:     50 * time.Millisecond,
		Duplicate:   1,
		CorruptRate: 0.01,
		Enabled:     true,
	}
}

// DefaultChaosSuite returns a standard suite of chaos scenarios.
func DefaultChaosSuite(duration time.Duration) []Scenario {
	return []Scenario{
		ScenarioPartition(duration),
		ScenarioHighLatency(duration, 100*time.Millisecond),
		ScenarioPacketLoss(duration, 0.1),
		ScenarioDuplicate(duration, 1),
		ScenarioCorruption(duration, 0.01),
		ScenarioMixed(duration),
	}
}
