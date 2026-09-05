package qos

import (
	"context"
	"sync"
	"time"
)

// TokenBucket implements a token bucket rate limiter.
type TokenBucket struct {
	mu         sync.Mutex
	capacity   int64
	tokens     int64
	refillRate int64 // tokens per second
	lastRefill time.Time
}

// NewTokenBucket creates a new token bucket.
func NewTokenBucket(capacity int64, refillRate int64) *TokenBucket {
	return &TokenBucket{
		capacity:   capacity,
		tokens:     capacity,
		refillRate: refillRate,
		lastRefill: time.Now(),
	}
}

// Take attempts to take n tokens from the bucket.
func (tb *TokenBucket) Take(n int64) bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	tb.refill()
	if tb.tokens >= n {
		tb.tokens -= n
		return true
	}
	return false
}

// Wait blocks until n tokens are available.
func (tb *TokenBucket) Wait(ctx context.Context, n int64) error {
	for {
		if tb.Take(n) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Millisecond * 10):
		}
	}
}

// refill adds tokens based on elapsed time.
func (tb *TokenBucket) refill() {
	now := time.Now()
	elapsed := now.Sub(tb.lastRefill).Seconds()
	tb.tokens = min(tb.capacity, tb.tokens+int64(elapsed*float64(tb.refillRate)))
	tb.lastRefill = now
}

// Available returns the current number of available tokens.
func (tb *TokenBucket) Available() int64 {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	tb.refill()
	return tb.tokens
}

// QoSConfig holds configuration for a QoS class.
type QoSConfig struct {
	Name      string
	Priority  int
	Bandwidth int64 // bytes per second
	Burst     int64 // max burst bytes
	Latency   time.Duration
	Jitter    time.Duration
}

// QoSClass represents a traffic class with its own rate limiting.
type QoSClass struct {
	config QoSConfig
	bucket *TokenBucket
	mu     sync.Mutex
	stats  ClassStats
}

// ClassStats tracks statistics for a QoS class.
type ClassStats struct {
	BytesSent      int64
	BytesDropped   int64
	PacketsSent    int64
	PacketsDropped int64
	AvgLatency     time.Duration
	MaxLatency     time.Duration
}

// NewQoSClass creates a new QoS class.
func NewQoSClass(config QoSConfig) *QoSClass {
	return &QoSClass{
		config: config,
		bucket: NewTokenBucket(config.Burst, config.Bandwidth),
	}
}

// Send attempts to send data through this QoS class.
func (qc *QoSClass) Send(ctx context.Context, data []byte) error {
	if err := qc.bucket.Wait(ctx, int64(len(data))); err != nil {
		qc.stats.PacketsDropped++
		qc.stats.BytesDropped += int64(len(data))
		return err
	}
	qc.stats.PacketsSent++
	qc.stats.BytesSent += int64(len(data))
	return nil
}

// GetStats returns a copy of the class statistics.
func (qc *QoSClass) GetStats() ClassStats {
	qc.mu.Lock()
	defer qc.mu.Unlock()
	return qc.stats
}

// QoSManager manages multiple QoS classes and traffic shaping.
type QoSManager struct {
	mu           sync.RWMutex
	classes      map[string]*QoSClass
	defaultClass *QoSClass
	// Service-specific classes
	serviceClasses map[string]*QoSClass
	// Peer-specific classes
	peerClasses map[string]*QoSClass
	// Default policy
	policy Policy
}

// Policy defines the QoS policy.
type Policy int

const (
	PolicyFIFO     Policy = iota // First in, first out
	PolicyPriority               // Strict priority
	PolicyWFQ                    // Weighted Fair Queuing
	PolicyHTB                    // Hierarchical Token Bucket
)

// NewQoSManager creates a new QoS manager.
func NewQoSManager(policy Policy) *QoSManager {
	qm := &QoSManager{
		classes:        make(map[string]*QoSClass),
		serviceClasses: make(map[string]*QoSClass),
		peerClasses:    make(map[string]*QoSClass),
		policy:         policy,
	}

	// Create default class
	qm.defaultClass = NewQoSClass(QoSConfig{
		Name:      "default",
		Priority:  0,
		Bandwidth: 10 * 1024 * 1024, // 10 MB/s default
		Burst:     1024 * 1024,      // 1 MB burst
	})
	qm.classes["default"] = qm.defaultClass

	// Pre-create service classes
	services := []QoSConfig{
		{Name: "voice", Priority: 10, Bandwidth: 2 * 1024 * 1024, Burst: 256 * 1024, Latency: 20 * time.Millisecond},
		{Name: "vpn", Priority: 9, Bandwidth: 5 * 1024 * 1024, Burst: 512 * 1024, Latency: 50 * time.Millisecond},
		{Name: "files", Priority: 5, Bandwidth: 10 * 1024 * 1024, Burst: 2 * 1024 * 1024},
		{Name: "messaging", Priority: 7, Bandwidth: 1 * 1024 * 1024, Burst: 128 * 1024, Latency: 100 * time.Millisecond},
		{Name: "email", Priority: 3, Bandwidth: 512 * 1024, Burst: 64 * 1024},
		{Name: "dns", Priority: 8, Bandwidth: 128 * 1024, Burst: 32 * 1024, Latency: 10 * time.Millisecond},
		{Name: "http", Priority: 6, Bandwidth: 5 * 1024 * 1024, Burst: 1024 * 1024},
		{Name: "registry", Priority: 4, Bandwidth: 2 * 1024 * 1024, Burst: 512 * 1024},
		{Name: "docs", Priority: 5, Bandwidth: 2 * 1024 * 1024, Burst: 512 * 1024},
	}

	for _, cfg := range services {
		qm.serviceClasses[cfg.Name] = NewQoSClass(cfg)
	}

	return qm
}

// GetClass returns a QoS class by name.
func (qm *QoSManager) GetClass(name string) *QoSClass {
	qm.mu.RLock()
	defer qm.mu.RUnlock()

	if c, ok := qm.classes[name]; ok {
		return c
	}
	if c, ok := qm.serviceClasses[name]; ok {
		return c
	}
	if c, ok := qm.peerClasses[name]; ok {
		return c
	}
	return qm.defaultClass
}

// RegisterServiceClass registers a custom QoS class for a service.
func (qm *QoSManager) RegisterServiceClass(name string, config QoSConfig) {
	qm.mu.Lock()
	defer qm.mu.Unlock()
	qm.serviceClasses[name] = NewQoSClass(config)
	qm.classes[name] = qm.serviceClasses[name]
}

// RegisterPeerClass registers a custom QoS class for a peer.
func (qm *QoSManager) RegisterPeerClass(peerID string, config QoSConfig) {
	qm.mu.Lock()
	defer qm.mu.Unlock()
	qm.peerClasses[peerID] = NewQoSClass(config)
	qm.classes[peerID] = qm.peerClasses[peerID]
}

// Send sends data through the appropriate QoS class.
func (qm *QoSManager) Send(ctx context.Context, service string, peerID string, data []byte) error {
	// Priority: peer class > service class > default
	class := qm.defaultClass

	if peerID != "" {
		peerClass := qm.GetClass(peerID)
		if peerClass != qm.defaultClass {
			class = peerClass
		}
	}

	if class == qm.defaultClass && service != "" {
		serviceClass := qm.GetClass(service)
		if serviceClass != qm.defaultClass {
			class = serviceClass
		}
	}

	return class.Send(ctx, data)
}

// GetStats returns stats for all classes.
func (qm *QoSManager) GetStats() map[string]ClassStats {
	qm.mu.RLock()
	defer qm.mu.RUnlock()

	stats := make(map[string]ClassStats)
	for name, class := range qm.classes {
		stats[name] = class.GetStats()
	}
	for name, class := range qm.serviceClasses {
		stats[name] = class.GetStats()
	}
	for name, class := range qm.peerClasses {
		stats[name] = class.GetStats()
	}
	return stats
}

// SetPolicy changes the QoS policy.
func (qm *QoSManager) SetPolicy(policy Policy) {
	qm.mu.Lock()
	defer qm.mu.Unlock()
	qm.policy = policy
}

// GetPolicy returns the current policy.
func (qm *QoSManager) GetPolicy() Policy {
	qm.mu.RLock()
	defer qm.mu.RUnlock()
	return qm.policy
}

// SetBandwidth updates the bandwidth limit for a class.
func (qm *QoSManager) SetBandwidth(name string, bandwidth int64) error {
	class := qm.GetClass(name)
	if class == nil {
		return ErrClassNotFound
	}
	class.mu.Lock()
	defer class.mu.Unlock()
	class.config.Bandwidth = bandwidth
	class.bucket = NewTokenBucket(class.config.Burst, bandwidth)
	return nil
}

// SetPriority updates the priority for a class.
func (qm *QoSManager) SetPriority(name string, priority int) error {
	class := qm.GetClass(name)
	if class == nil {
		return ErrClassNotFound
	}
	class.mu.Lock()
	defer class.mu.Unlock()
	class.config.Priority = priority
	return nil
}

// ErrClassNotFound is returned when a QoS class is not found.
var ErrClassNotFound = &QoSError{"class not found"}

// QoSError represents a QoS-related error.
type QoSError struct {
	msg string
}

func (e *QoSError) Error() string {
	return "qos: " + e.msg
}

// DefaultQoSManager returns a pre-configured QoS manager.
func DefaultQoSManager() *QoSManager {
	return NewQoSManager(PolicyPriority)
}

// HTBHierarchy implements Hierarchical Token Bucket for advanced QoS.
type HTBHierarchy struct {
	mu     sync.RWMutex
	root   *HTBNode
	nodes  map[string]*HTBNode
	muNode sync.RWMutex
}

// HTBNode represents a node in the HTB hierarchy.
type HTBNode struct {
	name     string
	parent   *HTBNode
	children []*HTBNode
	rate     int64 // guaranteed rate
	ceil     int64 // maximum rate
	bucket   *TokenBucket
	level    int
}

// NewHTBHierarchy creates a new HTB hierarchy.
func NewHTBHierarchy() *HTBHierarchy {
	h := &HTBHierarchy{
		nodes: make(map[string]*HTBNode),
	}
	h.root = &HTBNode{
		name:   "root",
		rate:   100 * 1024 * 1024, // 100 MB/s
		ceil:   100 * 1024 * 1024,
		bucket: NewTokenBucket(100*1024*1024, 100*1024*1024),
		level:  0,
	}
	h.nodes["root"] = h.root
	return h
}

// AddClass adds a class to the HTB hierarchy.
func (h *HTBHierarchy) AddClass(name, parent string, rate, ceil int64) *HTBNode {
	h.muNode.Lock()
	defer h.muNode.Unlock()

	p, ok := h.nodes[parent]
	if !ok {
		p = h.root
	}

	node := &HTBNode{
		name:   name,
		parent: p,
		rate:   rate,
		ceil:   ceil,
		bucket: NewTokenBucket(ceil, rate),
		level:  p.level + 1,
	}

	p.children = append(p.children, node)
	h.nodes[name] = node
	return node
}

// GetNode returns a node by name.
func (h *HTBHierarchy) GetNode(name string) *HTBNode {
	h.muNode.RLock()
	defer h.muNode.RUnlock()
	return h.nodes[name]
}

// Borrow allows a child to borrow tokens from parent up to ceil.
func (n *HTBNode) Borrow(tokens int64) bool {
	return n.bucket.Take(tokens)
}

// min returns the minimum of two int64 values.
func min(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

// QoSContext carries QoS metadata through context.
type QoSContextKey string

const (
	QoSKeyService QoSContextKey = "qos_service"
	QoSKeyPeer    QoSContextKey = "qos_peer"
	QoSKeyClass   QoSContextKey = "qos_class"
)

// WithQoSService adds service name to context.
func WithQoSService(ctx context.Context, service string) context.Context {
	return context.WithValue(ctx, QoSKeyService, service)
}

// WithQoSPeer adds peer ID to context.
func WithQoSPeer(ctx context.Context, peerID string) context.Context {
	return context.WithValue(ctx, QoSKeyPeer, peerID)
}

// GetQoSService extracts service name from context.
func GetQoSService(ctx context.Context) (string, bool) {
	v := ctx.Value(QoSKeyService)
	if v == nil {
		return "", false
	}
	return v.(string), true
}

// GetQoSPeer extracts peer ID from context.
func GetQoSPeer(ctx context.Context) (string, bool) {
	v := ctx.Value(QoSKeyPeer)
	if v == nil {
		return "", false
	}
	return v.(string), true
}
