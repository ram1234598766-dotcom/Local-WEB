package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ram1234598766-dotcom/Local-WEB/pkg/crypto"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/discovery"
	"github.com/rs/zerolog"
)

// testHost is a NodeHost whose plugin log output is discarded. NodeHost's own
// logger is usable, so this only keeps the lifecycle tests quiet.
type testHost struct {
	*NodeHost
}

func (h *testHost) Logger() Logger {
	l := zerolog.New(io.Discard)
	return &loggerWrapper{logger: &l}
}

func newTestHost() *testHost {
	return &testHost{NodeHost: NewNodeHost([32]byte{}, [32]byte{}, [32]byte{}, nil, nil, nil, nil, nil)}
}

func getStatus(t *testing.T, url string) int {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}

func doRequest(t *testing.T, method, url, body string) (int, string) {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = bytes.NewBufferString(body)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp.StatusCode, string(raw)
}

func TestNewNodeHost(t *testing.T) {
	var nodeID, pub [32]byte
	nodeID[0] = 7
	pub[0] = 9

	h := NewNodeHost(nodeID, pub, [32]byte{}, nil, nil, nil, nil, nil)

	if h == nil {
		t.Fatal("expected a non-nil host")
	}
	if h.nodeID != nodeID {
		t.Errorf("expected the node ID to be stored, got %x", h.nodeID[:2])
	}
	if h.router == nil {
		t.Error("expected an HTTP router to be created")
	}
	if h.services == nil || h.plugins == nil || h.config == nil {
		t.Error("expected the service, plugin and config maps to be created")
	}
	if h.eventBus == nil {
		t.Error("expected the event bus to be created")
	}
	if h.Discovery() == nil {
		t.Error("expected a discovery wrapper")
	}
	if h.Store() == nil {
		t.Error("expected a store wrapper")
	}
	if h.Transport() == nil {
		t.Error("expected a transport wrapper")
	}
}

func TestNodeHostIdentity(t *testing.T) {
	pub, priv, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	var nodeID [32]byte
	nodeID[0] = 42

	h := NewNodeHost(nodeID, pub, priv, nil, nil, nil, nil, nil)
	id := h.Identity()

	if got := id.NodeID(); got != nodeID {
		t.Errorf("expected the node ID to round trip, got %x", got[:2])
	}
	if got := id.PublicKey(); got != pub {
		t.Errorf("expected the public key to round trip, got %x", got[:2])
	}

	msg := []byte("plugin handshake")
	sig, err := id.Sign(msg)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if len(sig) != 64 {
		t.Errorf("expected a 64 byte signature, got %d", len(sig))
	}
	if !id.Verify(pub, msg, sig) {
		t.Error("expected the signature to verify")
	}
	if id.Verify(pub, []byte("tampered"), sig) {
		t.Error("expected verification of a different message to fail")
	}
	if id.Verify([32]byte{1}, msg, sig) {
		t.Error("expected verification against a different public key to fail")
	}
}

func TestNodeHostIdentityRejectsGarbage(t *testing.T) {
	pub, priv, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	h := NewNodeHost([32]byte{}, pub, priv, nil, nil, nil, nil, nil)

	if h.Identity().Verify(pub, []byte("m"), []byte("too short")) {
		t.Error("expected a short signature to be rejected")
	}
	if h.Identity().Verify(pub, []byte("m"), nil) {
		t.Error("expected an empty signature to be rejected")
	}
}

func TestNodeHostRegisterService(t *testing.T) {
	h := newTestHost()

	if err := h.RegisterService(&fakeService{name: "dns", typ: ServiceTypeCustom}); err != nil {
		t.Fatalf("RegisterService: %v", err)
	}
	if err := h.RegisterService(&fakeService{name: "http", typ: ServiceTypeCustom}); err != nil {
		t.Fatalf("RegisterService: %v", err)
	}
	if got := len(h.services); got != 2 {
		t.Fatalf("expected 2 services, got %d", got)
	}

	if err := h.UnregisterService("dns"); err != nil {
		t.Fatalf("UnregisterService: %v", err)
	}
	if got := len(h.services); got != 1 {
		t.Errorf("expected 1 remaining service, got %d", got)
	}

	// The freed name must be reusable.
	if err := h.RegisterService(&fakeService{name: "dns", typ: ServiceTypeCustom}); err != nil {
		t.Errorf("expected the freed name to be reusable, got %v", err)
	}
}

func TestNodeHostServiceValidation(t *testing.T) {
	// Each case gets a fresh host so the cases stay independent.

	tests := []struct {
		name string
		call func(Host) error
		want string
	}{
		{
			name: "duplicate name is rejected",
			call: func(h Host) error {
				if err := h.RegisterService(&fakeService{name: "dns", typ: ServiceTypeCustom}); err != nil {
					return err
				}
				return h.RegisterService(&fakeService{name: "dns", typ: ServiceTypeCustom})
			},
			want: "already registered",
		},
		{
			name: "unregistering an unknown name is rejected",
			call: func(h Host) error { return h.UnregisterService("nope") },
			want: "not found",
		},
		{
			name: "unregistering twice is rejected",
			call: func(h Host) error {
				if err := h.RegisterService(&fakeService{name: "twice", typ: ServiceTypeCustom}); err != nil {
					return err
				}
				if err := h.UnregisterService("twice"); err != nil {
					return err
				}
				return h.UnregisterService("twice")
			},
			want: "not found",
		},
		{
			name: "register then unregister is accepted",
			call: func(h Host) error {
				if err := h.RegisterService(&fakeService{name: "ok", typ: ServiceTypeCustom}); err != nil {
					return err
				}
				return h.UnregisterService("ok")
			},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.call(newTestHost())
			if tt.want == "" {
				if err != nil {
					t.Fatalf("expected the call to succeed, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected the call to be rejected")
			}
			if !contains(err.Error(), tt.want) {
				t.Errorf("expected an error containing %q, got %v", tt.want, err)
			}
		})
	}
}

func TestNodeHostHTTPRouter(t *testing.T) {
	h := newTestHost()
	router := h.HTTPRouter()

	router.HandleFunc("/hello", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("hi"))
	})
	router.Handle("/also", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))

	// httptest binds 127.0.0.1 on an ephemeral port.
	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)

	if code, body := doRequest(t, http.MethodGet, srv.URL+"/hello", ""); code != http.StatusOK || body != "hi" {
		t.Errorf("expected 200 %q from /hello, got %d %q", "hi", code, body)
	}
	if code := getStatus(t, srv.URL+"/also"); code != http.StatusTeapot {
		t.Errorf("expected 418 from /also, got %d", code)
	}
	if code := getStatus(t, srv.URL+"/missing"); code != http.StatusNotFound {
		t.Errorf("expected 404 for an unregistered path, got %d", code)
	}
}

func TestNodeHostRouterGroupServesItsOwnRoutes(t *testing.T) {
	h := newTestHost()
	group := h.HTTPRouter().Group("/sub")

	group.HandleFunc("/x", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("gx")) })

	srv := httptest.NewServer(group)
	t.Cleanup(srv.Close)

	if code, body := doRequest(t, http.MethodGet, srv.URL+"/x", ""); code != http.StatusOK || body != "gx" {
		t.Errorf("expected 200 %q from the group route, got %d %q", "gx", code, body)
	}
}

// A group is only useful if it is reachable on the parent under its prefix: the
// prefix used to be ignored and a detached mux returned, so every route it
// registered 404'd.
func TestNodeHostRouterGroupMountsUnderItsPrefix(t *testing.T) {
	h := newTestHost()
	group := h.HTTPRouter().Group("/api")
	group.HandleFunc("/x", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("gx")) })
	nested := group.Group("/v1")
	nested.HandleFunc("/y", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("gy")) })

	srv := httptest.NewServer(h.HTTPRouter())
	t.Cleanup(srv.Close)

	if code, body := doRequest(t, http.MethodGet, srv.URL+"/api/x", ""); code != http.StatusOK || body != "gx" {
		t.Errorf("expected 200 %q from /api/x, got %d %q", "gx", code, body)
	}
	if code, body := doRequest(t, http.MethodGet, srv.URL+"/api/v1/y", ""); code != http.StatusOK || body != "gy" {
		t.Errorf("expected 200 %q from the nested /api/v1/y, got %d %q", "gy", code, body)
	}
	// Group paths are relative to the prefix, so the bare path must not also
	// answer at the root of the host router.
	if code := getStatus(t, srv.URL+"/x"); code != http.StatusNotFound {
		t.Errorf("expected 404 for the unprefixed /x, got %d", code)
	}
}

// Registering a built-in runs its Init, which logs through the host; a host
// holding a nil logger panicked there and took the caller down with it.
func TestRegisterBuiltinPluginOnStockHostDoesNotPanic(t *testing.T) {
	// A stock host, with no test-only Logger override.
	pm := NewPluginManager(NewNodeHost([32]byte{}, [32]byte{}, [32]byte{}, nil, nil, nil, nil, nil))
	t.Cleanup(pm.Shutdown)

	for _, p := range []Plugin{NewExampleEchoPlugin(), NewExampleMetricsPlugin()} {
		name := p.Metadata().Name
		if err := pm.RegisterPlugin(p, nil); err != nil {
			t.Fatalf("RegisterPlugin(%s): %v", name, err)
		}
		inst, ok := pm.GetPlugin(name)
		if !ok {
			t.Fatalf("plugin %s is not registered", name)
		}
		if inst.Status != PluginStatusLoaded {
			t.Errorf("%s status = %q, want %q", name, inst.Status, PluginStatusLoaded)
		}
	}
}

func TestNodeHostConfig(t *testing.T) {
	h := newTestHost()

	if got := h.Config("absent"); got != nil {
		t.Errorf("expected a nil config for an unknown key, got %s", got)
	}
	if got := h.Config(""); got != nil {
		t.Errorf("expected a nil config for an empty key, got %s", got)
	}
}

func TestNodeHostEmitEventIsNonBlocking(t *testing.T) {
	h := newTestHost()

	// The bus holds 64 events; far more must not block the caller.
	for i := 0; i < 500; i++ {
		if err := h.EmitEvent("tick", i); err != nil {
			t.Fatalf("EmitEvent %d: %v", i, err)
		}
	}
	if got := len(h.eventBus); got != 64 {
		t.Errorf("expected the bus to fill to its 64 event capacity, got %d", got)
	}

	evt := <-h.eventBus
	if evt.Type != "tick" {
		t.Errorf("expected a %q event, got %q", "tick", evt.Type)
	}
	if evt.Timestamp.IsZero() {
		t.Error("expected the event to be timestamped")
	}
	if evt.Data != 0 {
		t.Errorf("expected the payload to be preserved, got %v", evt.Data)
	}
}

func TestNodeHostSecurityAccessors(t *testing.T) {
	h := newTestHost()
	sec := h.Security()

	if sec == nil {
		t.Fatal("expected a security wrapper")
	}
	if sec.CapabilityManager() != nil {
		t.Error("expected the nil capability manager to be returned unchanged")
	}
	if sec.AuditLog() != nil {
		t.Error("expected the nil audit log to be returned unchanged")
	}
}

func TestNodeHostDiscoveryWrapper(t *testing.T) {
	orch := discovery.NewOrchestrator(discovery.OrchestratorConfig{
		NodeID: [32]byte{255},
		Name:   "self",
	})
	// Drive the orchestrator through its exported entry point; Run is not needed
	// because the wrappers only read the database and register handlers.
	var peerID [32]byte
	peerID[0] = 1
	orch.HandleEvent(discovery.PeerEvent{
		Type: discovery.PeerFound,
		Peer: discovery.PeerInfo{ID: peerID, Name: "alpha", Addrs: []string{"10.0.0.1:4443"}},
		Time: time.Now(),
	})

	h := NewNodeHost([32]byte{}, [32]byte{}, [32]byte{}, nil, orch, nil, nil, nil)
	disc := h.Discovery()

	peers := disc.Peers()
	if len(peers) != 1 || peers[0].Name != "alpha" {
		t.Fatalf("expected the discovered peer, got %+v", peers)
	}
	if got := disc.BestPeers(1); len(got) != 1 {
		t.Errorf("expected BestPeers to return 1 peer, got %d", len(got))
	}

	notified := make(chan discovery.PeerEvent, 1)
	disc.OnPeer(func(e discovery.PeerEvent) { notified <- e })

	var second [32]byte
	second[0] = 2
	orch.HandleEvent(discovery.PeerEvent{Type: discovery.PeerFound, Peer: discovery.PeerInfo{ID: second, Name: "beta"}})

	select {
	case evt := <-notified:
		if evt.Peer.Name != "beta" {
			t.Errorf("expected the handler to see beta, got %q", evt.Peer.Name)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the OnPeer handler registered through the host was never invoked")
	}
}

func TestNodeHostTransportWrapperIsInert(t *testing.T) {
	h := newTestHost()
	tr := h.Transport()

	if tr == nil {
		t.Fatal("expected a transport wrapper")
	}
	// RegisterHandler is a documented no-op in the wrapper; calling it must be
	// safe even with no transport behind it.
	tr.RegisterHandler("svc", func(PluginConn) {})
}

func TestPluginManagerAcceptsHostLifecycle(t *testing.T) {
	// The manager must hand its own host to the plugin so the plugin can
	// register services and routes.
	pm, host := newTestPluginManager()
	t.Cleanup(pm.Shutdown)

	seen := make(chan Host, 1)
	p := NewBuiltinPlugin(
		Metadata{Name: "hostile"},
		WithInit(func(ctx context.Context, h Host) error {
			seen <- h
			return host.RegisterService(&fakeService{name: "from-plugin", typ: ServiceTypeCustom})
		}),
	)

	if err := pm.RegisterPlugin(p, nil); err != nil {
		t.Fatalf("RegisterPlugin: %v", err)
	}
	if got := <-seen; got != Host(host) {
		t.Error("expected the manager's host to be handed to the plugin")
	}
	if _, ok := host.services["from-plugin"]; !ok {
		t.Error("expected the plugin to have registered its service with the host")
	}

	if err := host.UnregisterService("from-plugin"); err != nil {
		t.Errorf("expected the plugin's service to be removable, got %v", err)
	}
}

func TestExampleEchoPluginMetadataAndServices(t *testing.T) {
	p := NewExampleEchoPlugin()

	meta := p.Metadata()
	if meta.Name != "echo" {
		t.Errorf("expected the plugin name %q, got %q", "echo", meta.Name)
	}
	if meta.Version == "" || meta.License == "" || meta.MinHostVer == "" {
		t.Errorf("expected the metadata to be fully populated, got %+v", meta)
	}

	svcs := p.Services()
	if len(svcs) != 1 {
		t.Fatalf("expected 1 service, got %d", len(svcs))
	}
	if svcs[0].Name != "echo" || svcs[0].Type != ServiceTypeCustom || svcs[0].Port != 8081 {
		t.Errorf("unexpected service descriptor: %+v", svcs[0])
	}
	if len(svcs[0].Endpoints) != 2 {
		t.Fatalf("expected 2 endpoints, got %d", len(svcs[0].Endpoints))
	}
	for _, ep := range svcs[0].Endpoints {
		if ep.Path == "" || ep.Method == "" {
			t.Errorf("expected every endpoint to declare a path and method, got %+v", ep)
		}
		if ep.AuthRequired {
			t.Errorf("expected %s to be unauthenticated", ep.Path)
		}
	}
}

func TestExampleEchoPluginInitRegistersRoutes(t *testing.T) {
	host := newTestHost()
	p := NewExampleEchoPlugin()

	if err := p.Init(context.Background(), host); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if p.host != Host(host) {
		t.Error("expected the plugin to retain the host")
	}

	srv := httptest.NewServer(host.HTTPRouter())
	t.Cleanup(srv.Close)

	code, body := doRequest(t, http.MethodPost, srv.URL+"/echo", "ping")
	if code != http.StatusOK {
		t.Fatalf("expected 200 from POST /echo, got %d", code)
	}
	var echoed struct {
		Echo      string `json:"echo"`
		Plugin    string `json:"plugin"`
		Timestamp string `json:"timestamp"`
	}
	if err := json.Unmarshal([]byte(body), &echoed); err != nil {
		t.Fatalf("decode response %q: %v", body, err)
	}
	if echoed.Echo != "ping" {
		t.Errorf("expected the body to be echoed as %q, got %q", "ping", echoed.Echo)
	}
	if echoed.Plugin != "echo" {
		t.Errorf("expected plugin %q, got %q", "echo", echoed.Plugin)
	}
	if _, err := time.Parse(time.RFC3339, echoed.Timestamp); err != nil {
		t.Errorf("expected an RFC3339 timestamp, got %q (%v)", echoed.Timestamp, err)
	}
}

func TestExampleEchoPluginRouteTable(t *testing.T) {
	host := newTestHost()
	p := NewExampleEchoPlugin()
	if err := p.Init(context.Background(), host); err != nil {
		t.Fatalf("Init: %v", err)
	}

	srv := httptest.NewServer(host.HTTPRouter())
	t.Cleanup(srv.Close)

	tests := []struct {
		name     string
		method   string
		path     string
		wantCode int
		wantEcho string
	}{
		{"post echoes the body", http.MethodPost, "/echo", http.StatusOK, "hello"},
		{"get on the exact path is rejected", http.MethodGet, "/echo", http.StatusMethodNotAllowed, ""},
		{"get echoes the path segment", http.MethodGet, "/echo/world", http.StatusOK, "world"},
		{"bare subtree path echoes nothing", http.MethodGet, "/echo/", http.StatusOK, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, body := doRequest(t, tt.method, srv.URL+tt.path, "hello")
			if code != tt.wantCode {
				t.Fatalf("expected status %d, got %d (%s)", tt.wantCode, code, body)
			}
			if tt.wantCode != http.StatusOK {
				return
			}
			var echoed struct {
				Echo string `json:"echo"`
			}
			if err := json.Unmarshal([]byte(body), &echoed); err != nil {
				t.Fatalf("decode response %q: %v", body, err)
			}
			if echoed.Echo != tt.wantEcho {
				t.Errorf("expected echo %q, got %q", tt.wantEcho, echoed.Echo)
			}
		})
	}
}

func TestExampleEchoPluginStopWithoutServer(t *testing.T) {
	p := NewExampleEchoPlugin()
	if err := p.Init(context.Background(), newTestHost()); err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Stop must be safe before start has built the HTTP server.
	if err := p.Stop(); err != nil {
		t.Errorf("expected Stop to succeed with no server, got %v", err)
	}
	if err := p.Stop(); err != nil {
		t.Errorf("expected Stop to be repeatable with no server, got %v", err)
	}
}

// The bind used to happen inside the serving goroutine, so a busy port was logged
// and swallowed while start reported success, and the manager recorded the plugin
// as running with nothing listening.
func TestExampleEchoPluginStartReportsBindFailure(t *testing.T) {
	ctx := context.Background()

	first := NewExampleEchoPlugin()
	if err := first.Init(ctx, newTestHost()); err != nil {
		t.Fatalf("first Init: %v", err)
	}
	if err := first.Start(); err != nil {
		t.Fatalf("first Start could not bind %s: %v", echoAddr, err)
	}
	t.Cleanup(func() { first.Stop() })

	second := NewExampleEchoPlugin()
	if err := second.Init(ctx, newTestHost()); err != nil {
		t.Fatalf("second Init: %v", err)
	}

	err := second.Start()
	if err == nil {
		t.Fatal("the second echo instance reported success although the port was taken")
	}
	if !strings.Contains(err.Error(), echoAddr) {
		t.Errorf("expected the error to name %s, got %v", echoAddr, err)
	}
	if second.server != nil {
		t.Error("a failed start left an HTTP server behind")
	}
}

func TestExampleEchoPluginThroughManager(t *testing.T) {
	pm, host := newTestPluginManager()
	t.Cleanup(pm.Shutdown)

	p := NewExampleEchoPlugin()
	if err := pm.RegisterPlugin(p, nil); err != nil {
		t.Fatalf("RegisterPlugin: %v", err)
	}

	inst, _ := pm.GetPlugin("echo")
	if inst.Status != PluginStatusLoaded {
		t.Errorf("expected status %q, got %q", PluginStatusLoaded, inst.Status)
	}

	srv := httptest.NewServer(host.HTTPRouter())
	t.Cleanup(srv.Close)

	if code := getStatus(t, srv.URL+"/echo/routed"); code != http.StatusOK {
		t.Errorf("expected the registered route to answer, got %d", code)
	}
}

func TestExampleMetricsPluginMetadataAndServices(t *testing.T) {
	p := NewExampleMetricsPlugin()

	meta := p.Metadata()
	if meta.Name != "metrics" {
		t.Errorf("expected the plugin name %q, got %q", "metrics", meta.Name)
	}
	svcs := p.Services()
	if len(svcs) != 1 {
		t.Fatalf("expected 1 service, got %d", len(svcs))
	}
	if svcs[0].Name != "metrics" || svcs[0].Type != ServiceTypeCustom {
		t.Errorf("unexpected service descriptor: %+v", svcs[0])
	}
	if svcs[0].Port != 0 {
		t.Errorf("expected no declared port, got %d", svcs[0].Port)
	}
	paths := make([]string, 0, len(svcs[0].Endpoints))
	for _, ep := range svcs[0].Endpoints {
		paths = append(paths, ep.Path)
	}
	if len(paths) != 2 || paths[0] != "/metrics" || paths[1] != "/health" {
		t.Errorf("expected the /metrics and /health endpoints, got %v", paths)
	}
}

func TestExampleMetricsPluginInitRegistersRoutes(t *testing.T) {
	host := newTestHost()
	p := NewExampleMetricsPlugin()
	if err := p.Init(context.Background(), host); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if p.host != Host(host) {
		t.Error("expected the plugin to retain the host")
	}

	srv := httptest.NewServer(host.HTTPRouter())
	t.Cleanup(srv.Close)

	code, body := doRequest(t, http.MethodGet, srv.URL+"/metrics", "")
	if code != http.StatusOK {
		t.Fatalf("expected 200 from /metrics, got %d", code)
	}
	for _, want := range []string{
		"# HELP localweb_peers_total",
		"# TYPE localweb_peers_total gauge",
		"localweb_uptime_seconds",
		"localweb_plugin_count",
	} {
		if !contains(body, want) {
			t.Errorf("expected %q in the metrics payload", want)
		}
	}

	code, body = doRequest(t, http.MethodGet, srv.URL+"/health", "")
	if code != http.StatusOK {
		t.Fatalf("expected 200 from /health, got %d", code)
	}
	var health struct {
		Status    string `json:"status"`
		Timestamp string `json:"timestamp"`
	}
	if err := json.Unmarshal([]byte(body), &health); err != nil {
		t.Fatalf("decode health %q: %v", body, err)
	}
	if health.Status != "healthy" {
		t.Errorf("expected status %q, got %q", "healthy", health.Status)
	}
	if _, err := time.Parse(time.RFC3339, health.Timestamp); err != nil {
		t.Errorf("expected an RFC3339 timestamp, got %q (%v)", health.Timestamp, err)
	}
}

func TestExampleMetricsPluginContentType(t *testing.T) {
	host := newTestHost()
	p := NewExampleMetricsPlugin()
	if err := p.Init(context.Background(), host); err != nil {
		t.Fatalf("Init: %v", err)
	}

	srv := httptest.NewServer(host.HTTPRouter())
	t.Cleanup(srv.Close)

	for path, want := range map[string]string{
		"/metrics": "text/plain; version=0.0.4",
		"/health":  "application/json",
	} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if got := resp.Header.Get("Content-Type"); got != want {
			t.Errorf("%s: expected content type %q, got %q", path, want, got)
		}
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return len(needle) == 0
}
