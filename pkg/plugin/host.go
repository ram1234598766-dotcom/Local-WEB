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

	"github.com/ram1234598766-dotcom/Local-WEB/pkg/crypto"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/discovery"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/security"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/store"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/transport"
)

// NodeHost implements the Host interface for the LocalWEB node.
type NodeHost struct {
	mu         sync.RWMutex
	nodeID     [32]byte
	pubKey     [32]byte
	privKey    [32]byte
	transport  *transport.HybridServer
	discovery  *discovery.Orchestrator
	dbStore    *store.Store
	auditLog   *security.AuditLog
	capManager *security.CapabilityManager
	router     *http.ServeMux
	services   map[string]Service
	plugins    map[string]Plugin
	config     map[string]json.RawMessage
	eventBus   chan PluginEvent
	logger     *zerolog.Logger
}

// NewNodeHost creates a new node host for plugins.
func NewNodeHost(
	nodeID [32]byte,
	pubKey [32]byte,
	privKey [32]byte,
	transport *transport.HybridServer,
	discovery *discovery.Orchestrator,
	dbStore *store.Store,
	auditLog *security.AuditLog,
	capManager *security.CapabilityManager,
) *NodeHost {
	return &NodeHost{
		nodeID:     nodeID,
		pubKey:     pubKey,
		privKey:    privKey,
		transport:  transport,
		discovery:  discovery,
		dbStore:    dbStore,
		auditLog:   auditLog,
		capManager: capManager,
		router:     http.NewServeMux(),
		services:   make(map[string]Service),
		plugins:    make(map[string]Plugin),
		config:     make(map[string]json.RawMessage),
		eventBus:   make(chan PluginEvent, 64),
		logger:     zerolog.DefaultContextLogger,
	}
}

// Host interface implementation

func (h *NodeHost) Identity() Identity {
	return &nodeIdentity{nodeID: h.nodeID, pubKey: h.pubKey, privKey: h.privKey}
}

func (h *NodeHost) Transport() PluginTransport {
	return &transportWrapper{server: h.transport}
}

func (h *NodeHost) Discovery() Discovery {
	return &discoveryWrapper{orchestrator: h.discovery}
}

func (h *NodeHost) Store() Store {
	return &storeWrapper{store: h.dbStore}
}

func (h *NodeHost) Security() Security {
	return &securityWrapper{capManager: h.capManager, auditLog: h.auditLog}
}

func (h *NodeHost) RegisterService(svc Service) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if _, exists := h.services[svc.Name()]; exists {
		return fmt.Errorf("service %s already registered", svc.Name())
	}
	h.services[svc.Name()] = svc
	return nil
}

func (h *NodeHost) UnregisterService(name string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if _, exists := h.services[name]; !exists {
		return fmt.Errorf("service %s not found", name)
	}
	delete(h.services, name)
	return nil
}

func (h *NodeHost) HTTPRouter() HTTPRouter {
	return &routerWrapper{mux: h.router}
}

func (h *NodeHost) Logger() Logger {
	return &loggerWrapper{logger: h.logger}
}

func (h *NodeHost) Config(name string) json.RawMessage {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.config[name]
}

func (h *NodeHost) EmitEvent(event string, data interface{}) error {
	evt := PluginEvent{
		Type:      event,
		Timestamp: time.Now(),
		Data:      data,
	}
	select {
	case h.eventBus <- evt:
	default:
	}
	return nil
}

// Wrapper types to implement Host interfaces

type nodeIdentity struct {
	nodeID  [32]byte
	pubKey  [32]byte
	privKey [32]byte
}

func (ni *nodeIdentity) PublicKey() [32]byte { return ni.pubKey }
func (ni *nodeIdentity) NodeID() [32]byte    { return ni.nodeID }
func (ni *nodeIdentity) Sign(msg []byte) ([]byte, error) {
	return crypto.Sign(ni.privKey, msg)
}
func (ni *nodeIdentity) Verify(pub [32]byte, msg, sig []byte) bool {
	return crypto.Verify(pub, msg, sig)
}

type transportWrapper struct {
	server *transport.HybridServer
}

func (tw *transportWrapper) Connect(ctx context.Context, peerID [32]byte) (PluginConn, error) {
	conn, err := tw.server.Connect(ctx, "", peerID)
	if err != nil {
		return nil, err
	}
	return &pluginConnWrapper{conn: conn}, nil
}

func (tw *transportWrapper) Listen(addr string) (net.Listener, error) {
	return nil, fmt.Errorf("not implemented")
}

func (tw *transportWrapper) RegisterHandler(svc string, handler func(PluginConn)) {
	// Not directly exposed via transport
}

// pluginConnWrapper wraps transport.Connection to implement PluginConn
type pluginConnWrapper struct {
	conn *transport.Connection
}

func (pcw *pluginConnWrapper) Read(p []byte) (n int, err error) {
	stream, err := pcw.conn.OpenStream(context.Background(), 0)
	if err != nil {
		return 0, err
	}
	return stream.Read(p)
}

func (pcw *pluginConnWrapper) Write(p []byte) (n int, err error) {
	stream, err := pcw.conn.OpenStream(context.Background(), 0)
	if err != nil {
		return 0, err
	}
	return stream.Write(p)
}

func (pcw *pluginConnWrapper) Close() error {
	return pcw.conn.Close()
}

func (pcw *pluginConnWrapper) LocalAddr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0}
}

func (pcw *pluginConnWrapper) RemoteAddr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0}
}

func (pcw *pluginConnWrapper) SetDeadline(t time.Time) error {
	return nil
}

func (pcw *pluginConnWrapper) SetReadDeadline(t time.Time) error {
	return nil
}

func (pcw *pluginConnWrapper) SetWriteDeadline(t time.Time) error {
	return nil
}

type discoveryWrapper struct {
	orchestrator *discovery.Orchestrator
}

func (dw *discoveryWrapper) Peers() []*discovery.PeerInfo {
	return dw.orchestrator.Peers()
}

func (dw *discoveryWrapper) BestPeers(n int) []*discovery.PeerInfo {
	return dw.orchestrator.BestPeers(n)
}

func (dw *discoveryWrapper) OnPeer(fn func(discovery.PeerEvent)) {
	dw.orchestrator.OnPeer(fn)
}

type storeWrapper struct {
	store *store.Store
}

func (sw *storeWrapper) Put(ctx context.Context, key string, value []byte) error {
	return sw.store.Put(ctx, []byte(key), value)
}

func (sw *storeWrapper) Get(ctx context.Context, key string) ([]byte, error) {
	return sw.store.Get(ctx, []byte(key))
}

func (sw *storeWrapper) Delete(ctx context.Context, key string) error {
	return sw.store.Delete(ctx, []byte(key))
}

func (sw *storeWrapper) Iterator(ctx context.Context, prefix string, fn func(key, value []byte) error) error {
	return sw.store.Iterator(ctx, []byte(prefix), func(k, v []byte) error {
		return fn(k, v)
	})
}

type securityWrapper struct {
	capManager *security.CapabilityManager
	auditLog   *security.AuditLog
}

func (sw *securityWrapper) CapabilityManager() *security.CapabilityManager {
	return sw.capManager
}

func (sw *securityWrapper) AuditLog() *security.AuditLog {
	return sw.auditLog
}

type routerWrapper struct {
	mux *http.ServeMux
}

func (rw *routerWrapper) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rw.mux.ServeHTTP(w, r)
}

func (rw *routerWrapper) Handle(path string, handler http.Handler) {
	rw.mux.Handle(path, handler)
}

func (rw *routerWrapper) HandleFunc(path string, handler func(http.ResponseWriter, *http.Request)) {
	rw.mux.HandleFunc(path, handler)
}

func (rw *routerWrapper) Group(prefix string) HTTPRouter {
	return &routerWrapper{mux: http.NewServeMux()}
}

type loggerWrapper struct {
	logger *zerolog.Logger
}

func (lw *loggerWrapper) Debug() *zerolog.Event {
	return lw.logger.Debug()
}

func (lw *loggerWrapper) Info() *zerolog.Event {
	return lw.logger.Info()
}

func (lw *loggerWrapper) Warn() *zerolog.Event {
	return lw.logger.Warn()
}

func (lw *loggerWrapper) Error() *zerolog.Event {
	return lw.logger.Error()
}
