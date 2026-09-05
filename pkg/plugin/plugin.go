package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/ram1234598766-dotcom/Local-WEB/pkg/discovery"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/security"
)

// Plugin interface that all plugins must implement.
type Plugin interface {
	// Metadata returns plugin metadata.
	Metadata() Metadata

	// Init initializes the plugin with the host context.
	Init(ctx context.Context, host Host) error

	// Start starts the plugin.
	Start() error

	// Stop stops the plugin.
	Stop() error

	// Services returns the services provided by this plugin.
	Services() []ServiceDescriptor
}

// Metadata contains plugin identification and version info.
type Metadata struct {
	Name         string   `json:"name"`
	Version      string   `json:"version"`
	Author       string   `json:"author"`
	Description  string   `json:"description"`
	License      string   `json:"license"`
	MinHostVer   string   `json:"min_host_version"`
	Dependencies []string `json:"dependencies,omitempty"`
}

// ServiceDescriptor describes a service provided by a plugin.
type ServiceDescriptor struct {
	Name      string          `json:"name"`
	Type      ServiceType     `json:"type"`
	Port      int             `json:"port,omitempty"`
	Config    json.RawMessage `json:"config,omitempty"`
	Endpoints []EndpointDesc  `json:"endpoints,omitempty"`
}

// ServiceType categorizes the service.
type ServiceType string

const (
	ServiceTypeMessaging ServiceType = "messaging"
	ServiceTypeFiles     ServiceType = "files"
	ServiceTypeDocs      ServiceType = "docs"
	ServiceTypeVoice     ServiceType = "voice"
	ServiceTypeVPN       ServiceType = "vpn"
	ServiceTypeRegistry  ServiceType = "registry"
	ServiceTypeCustom    ServiceType = "custom"
)

// EndpointDesc describes an HTTP/gRPC endpoint.
type EndpointDesc struct {
	Path         string `json:"path"`
	Method       string `json:"method"`
	Description  string `json:"description"`
	AuthRequired bool   `json:"auth_required"`
}

// Host provides host capabilities to plugins.
type Host interface {
	// Identity returns the node's identity.
	Identity() Identity

	// Transport returns the transport layer for P2P communication.
	Transport() PluginTransport

	// Discovery returns the discovery orchestrator.
	Discovery() Discovery

	// Store returns the encrypted store.
	Store() Store

	// Security returns security capabilities.
	Security() Security

	// RegisterService registers a plugin service.
	RegisterService(svc Service) error

	// UnregisterService unregisters a plugin service.
	UnregisterService(name string) error

	// HTTPRouter returns the HTTP router for plugin endpoints.
	HTTPRouter() HTTPRouter

	// Logger returns a logger for the plugin.
	Logger() Logger

	// Config returns plugin configuration.
	Config(name string) json.RawMessage

	// EmitEvent emits an event to the host event bus.
	EmitEvent(event string, data interface{}) error
}

// Identity represents the node's cryptographic identity.
type Identity interface {
	PublicKey() [32]byte
	NodeID() [32]byte
	Sign(msg []byte) ([]byte, error)
	Verify(pub [32]byte, msg, sig []byte) bool
}

// PluginTransport abstracts the P2P transport layer for plugins.
type PluginTransport interface {
	Connect(ctx context.Context, peerID [32]byte) (PluginConn, error)
	Listen(addr string) (net.Listener, error)
	RegisterHandler(svc string, handler func(PluginConn))
}

// PluginConn is a connection interface for plugins (simpler than net.Conn).
type PluginConn interface {
	Read(p []byte) (n int, err error)
	Write(p []byte) (n int, err error)
	Close() error
	LocalAddr() net.Addr
	RemoteAddr() net.Addr
	SetDeadline(t time.Time) error
	SetReadDeadline(t time.Time) error
	SetWriteDeadline(t time.Time) error
}

// Discovery abstracts the discovery layer.
type Discovery interface {
	Peers() []*discovery.PeerInfo
	BestPeers(n int) []*discovery.PeerInfo
	OnPeer(func(discovery.PeerEvent))
}

// Store abstracts the encrypted store.
type Store interface {
	Put(ctx context.Context, key string, value []byte) error
	Get(ctx context.Context, key string) ([]byte, error)
	Delete(ctx context.Context, key string) error
	Iterator(ctx context.Context, prefix string, fn func(key, value []byte) error) error
}

// Security provides security capabilities.
type Security interface {
	CapabilityManager() *security.CapabilityManager
	AuditLog() *security.AuditLog
}

// HTTPRouter provides HTTP routing for plugins.
type HTTPRouter interface {
	http.Handler
	Handle(path string, handler http.Handler)
	HandleFunc(path string, handler func(http.ResponseWriter, *http.Request))
	Group(prefix string) HTTPRouter
}

// Logger provides structured logging.
type Logger interface {
	Debug() *zerolog.Event
	Info() *zerolog.Event
	Warn() *zerolog.Event
	Error() *zerolog.Event
}

// Service represents a running service instance.
type Service interface {
	Name() string
	Type() ServiceType
	Start() error
	Stop() error
	Status() ServiceStatus
}

// ServiceStatus represents the runtime status of a service.
type ServiceStatus struct {
	Running   bool
	Healthy   bool
	Peers     int
	Uptime    time.Duration
	Stats     json.RawMessage
	LastError string
}

// PluginManager manages plugin lifecycle.
type PluginManager struct {
	mu         sync.RWMutex
	plugins    map[string]*PluginInstance
	host       Host
	httpServer *http.Server
	events     chan PluginEvent
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
}

// PluginInstance wraps a plugin with its runtime state.
type PluginInstance struct {
	Plugin   Plugin
	Metadata Metadata
	Status   PluginStatus
	Config   json.RawMessage
}

// PluginStatus represents the runtime status of a plugin.
type PluginStatus string

const (
	PluginStatusLoaded  PluginStatus = "loaded"
	PluginStatusRunning PluginStatus = "running"
	PluginStatusStopped PluginStatus = "stopped"
	PluginStatusError   PluginStatus = "error"
)

// PluginEvent represents a plugin lifecycle event.
type PluginEvent struct {
	Type      string
	Plugin    string
	Timestamp time.Time
	Data      interface{}
}

// NewPluginManager creates a new plugin manager.
func NewPluginManager(host Host) *PluginManager {
	ctx, cancel := context.WithCancel(context.Background())
	return &PluginManager{
		plugins: make(map[string]*PluginInstance),
		host:    host,
		events:  make(chan PluginEvent, 32),
		ctx:     ctx,
		cancel:  cancel,
	}
}

// LoadPlugin loads a plugin from a .so file (Go plugin).
func (pm *PluginManager) LoadPlugin(path string, config json.RawMessage) error {
	// In Go, plugins are loaded via plugin.Open (not available on all platforms)
	// This is a stub for the interface - actual implementation would use
	// Go's plugin package or a custom loader
	return fmt.Errorf("Go plugin loading not implemented; use built-in plugins")
}

// RegisterPlugin registers a built-in plugin.
func (pm *PluginManager) RegisterPlugin(p Plugin, config json.RawMessage) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	meta := p.Metadata()
	if _, exists := pm.plugins[meta.Name]; exists {
		return fmt.Errorf("plugin %s already registered", meta.Name)
	}

	instance := &PluginInstance{
		Plugin:   p,
		Metadata: meta,
		Status:   PluginStatusLoaded,
		Config:   config,
	}

	if err := p.Init(pm.ctx, pm.host); err != nil {
		instance.Status = PluginStatusError
		pm.plugins[meta.Name] = instance
		return fmt.Errorf("init plugin %s: %w", meta.Name, err)
	}

	pm.plugins[meta.Name] = instance
	pm.emitEvent(PluginEvent{
		Type:      "loaded",
		Plugin:    meta.Name,
		Timestamp: time.Now(),
	})

	log.Info().Str("plugin", meta.Name).Str("version", meta.Version).Msg("plugin registered")
	return nil
}

// StartPlugin starts a registered plugin.
func (pm *PluginManager) StartPlugin(name string) error {
	pm.mu.Lock()
	instance, exists := pm.plugins[name]
	pm.mu.Unlock()

	if !exists {
		return fmt.Errorf("plugin %s not found", name)
	}

	if instance.Status == PluginStatusRunning {
		return nil
	}

	if err := instance.Plugin.Start(); err != nil {
		instance.Status = PluginStatusError
		return fmt.Errorf("start plugin %s: %w", name, err)
	}

	instance.Status = PluginStatusRunning
	pm.emitEvent(PluginEvent{
		Type:      "started",
		Plugin:    name,
		Timestamp: time.Now(),
	})

	// Register plugin services
	for _, svcDesc := range instance.Plugin.Services() {
		// Services would be registered with host
		log.Info().Str("plugin", name).Str("service", svcDesc.Name).Msg("service registered")
	}

	return nil
}

// StopPlugin stops a running plugin.
func (pm *PluginManager) StopPlugin(name string) error {
	pm.mu.Lock()
	instance, exists := pm.plugins[name]
	pm.mu.Unlock()

	if !exists {
		return fmt.Errorf("plugin %s not found", name)
	}

	if err := instance.Plugin.Stop(); err != nil {
		return fmt.Errorf("stop plugin %s: %w", name, err)
	}

	instance.Status = PluginStatusStopped
	pm.emitEvent(PluginEvent{
		Type:      "stopped",
		Plugin:    name,
		Timestamp: time.Now(),
	})

	return nil
}

// UnregisterPlugin unregisters a plugin.
func (pm *PluginManager) UnregisterPlugin(name string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	instance, exists := pm.plugins[name]
	if !exists {
		return fmt.Errorf("plugin %s not found", name)
	}

	if instance.Status == PluginStatusRunning {
		if err := instance.Plugin.Stop(); err != nil {
			log.Error().Err(err).Str("plugin", name).Msg("error stopping plugin")
		}
	}

	delete(pm.plugins, name)
	pm.emitEvent(PluginEvent{
		Type:      "unregistered",
		Plugin:    name,
		Timestamp: time.Now(),
	})

	return nil
}

// ListPlugins returns all registered plugins.
func (pm *PluginManager) ListPlugins() []PluginInstance {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	result := make([]PluginInstance, 0, len(pm.plugins))
	for _, inst := range pm.plugins {
		result = append(result, *inst)
	}
	return result
}

// GetPlugin returns a plugin by name.
func (pm *PluginManager) GetPlugin(name string) (*PluginInstance, bool) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	inst, ok := pm.plugins[name]
	return inst, ok
}

// Shutdown stops all plugins and the manager.
func (pm *PluginManager) Shutdown() {
	pm.cancel()
	pm.wg.Wait()

	pm.mu.Lock()
	defer pm.mu.Unlock()

	for _, inst := range pm.plugins {
		if inst.Status == PluginStatusRunning {
			inst.Plugin.Stop()
		}
	}
}

func (pm *PluginManager) emitEvent(evt PluginEvent) {
	select {
	case pm.events <- evt:
	default:
	}
}

// Events returns the plugin event channel.
func (pm *PluginManager) Events() <-chan PluginEvent {
	return pm.events
}

// BuiltinPlugin provides a simple way to create plugins from Go code.
type BuiltinPlugin struct {
	meta      Metadata
	initFunc  func(ctx context.Context, host Host) error
	startFunc func() error
	stopFunc  func() error
	services  []ServiceDescriptor
}

func NewBuiltinPlugin(meta Metadata, opts ...BuiltinOption) *BuiltinPlugin {
	p := &BuiltinPlugin{meta: meta}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

type BuiltinOption func(*BuiltinPlugin)

func WithInit(f func(ctx context.Context, host Host) error) BuiltinOption {
	return func(p *BuiltinPlugin) { p.initFunc = f }
}

func WithStart(f func() error) BuiltinOption {
	return func(p *BuiltinPlugin) { p.startFunc = f }
}

func WithStop(f func() error) BuiltinOption {
	return func(p *BuiltinPlugin) { p.stopFunc = f }
}

func WithServices(services []ServiceDescriptor) BuiltinOption {
	return func(p *BuiltinPlugin) { p.services = services }
}

func (p *BuiltinPlugin) Metadata() Metadata { return p.meta }
func (p *BuiltinPlugin) Init(ctx context.Context, host Host) error {
	if p.initFunc != nil {
		return p.initFunc(ctx, host)
	}
	return nil
}
func (p *BuiltinPlugin) Start() error {
	if p.startFunc != nil {
		return p.startFunc()
	}
	return nil
}
func (p *BuiltinPlugin) Stop() error {
	if p.stopFunc != nil {
		return p.stopFunc()
	}
	return nil
}
func (p *BuiltinPlugin) Services() []ServiceDescriptor { return p.services }
