package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog/log"
)

func init() {
	log.Logger = log.Output(io.Discard)
}

// fakePlugin is a scriptable Plugin used to drive the PluginManager.
type fakePlugin struct {
	meta     Metadata
	initErr  error
	startErr error
	stopErr  error

	mu       sync.Mutex
	initN    int
	startN   int
	stopN    int
	services []ServiceDescriptor
	host     Host
	initCtx  context.Context
}

func newFakePlugin(name string) *fakePlugin {
	return &fakePlugin{
		meta: Metadata{Name: name, Version: "1.0.0", Author: "test", MinHostVer: "1.0.0"},
		services: []ServiceDescriptor{
			{Name: name + "-svc", Type: ServiceTypeCustom, Port: 9000},
		},
	}
}

func (f *fakePlugin) Metadata() Metadata { return f.meta }

func (f *fakePlugin) Init(ctx context.Context, host Host) error {
	f.mu.Lock()
	f.initN++
	f.initCtx = ctx
	f.host = host
	f.mu.Unlock()
	return f.initErr
}

func (f *fakePlugin) Start() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.startN++
	return f.startErr
}

func (f *fakePlugin) Stop() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopN++
	return f.stopErr
}

func (f *fakePlugin) Services() []ServiceDescriptor { return f.services }

func (f *fakePlugin) counts() (init, start, stop int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.initN, f.startN, f.stopN
}

// fakeService is a minimal Service implementation.
type fakeService struct {
	name string
	typ  ServiceType
}

func (f *fakeService) Name() string          { return f.name }
func (f *fakeService) Type() ServiceType     { return f.typ }
func (f *fakeService) Start() error          { return nil }
func (f *fakeService) Stop() error           { return nil }
func (f *fakeService) Status() ServiceStatus { return ServiceStatus{Running: true, Healthy: true} }

func newTestPluginManager() (*PluginManager, *testHost) {
	host := newTestHost()
	return NewPluginManager(host), host
}

func TestNewPluginManager(t *testing.T) {
	pm, host := newTestPluginManager()
	t.Cleanup(pm.Shutdown)

	if pm == nil {
		t.Fatal("expected a non-nil manager")
	}
	if pm.host != Host(host) {
		t.Error("expected the manager to hold the supplied host")
	}
	if pm.plugins == nil || len(pm.plugins) != 0 {
		t.Errorf("expected an empty plugin map, got %v", pm.plugins)
	}
	if pm.events == nil {
		t.Error("expected the event channel to be constructed")
	}
	if pm.ctx == nil || pm.ctx.Err() != nil {
		t.Error("expected a live context")
	}
	if got := pm.ListPlugins(); len(got) != 0 {
		t.Errorf("expected no plugins, got %d", len(got))
	}
}

func TestPluginManagerRegisterPlugin(t *testing.T) {
	pm, _ := newTestPluginManager()
	t.Cleanup(pm.Shutdown)

	p := newFakePlugin("alpha")
	cfg := json.RawMessage(`{"level":1}`)
	if err := pm.RegisterPlugin(p, cfg); err != nil {
		t.Fatalf("RegisterPlugin: %v", err)
	}

	inst, ok := pm.GetPlugin("alpha")
	if !ok {
		t.Fatal("expected the plugin to be registered")
	}
	if inst.Status != PluginStatusLoaded {
		t.Errorf("expected status %q, got %q", PluginStatusLoaded, inst.Status)
	}
	if inst.Metadata.Version != "1.0.0" {
		t.Errorf("expected the metadata to be snapshotted, got %+v", inst.Metadata)
	}
	if string(inst.Config) != string(cfg) {
		t.Errorf("expected the config to be stored, got %s", inst.Config)
	}
	if inst.Plugin != Plugin(p) {
		t.Error("expected the instance to hold the plugin it was given")
	}

	init, _, _ := p.counts()
	if init != 1 {
		t.Errorf("expected Init to be called once, got %d", init)
	}

	evt := wantEvent(t, pm, "loaded")
	if evt.Type != "loaded" {
		t.Errorf("expected a %q event, got %q", "loaded", evt.Type)
	}
	if evt.Plugin != "alpha" {
		t.Errorf("expected the event to name the plugin, got %q", evt.Plugin)
	}
	if evt.Timestamp.IsZero() {
		t.Error("expected the event to be timestamped")
	}
}

func TestPluginManagerRegisterDuplicate(t *testing.T) {
	pm, _ := newTestPluginManager()
	t.Cleanup(pm.Shutdown)

	if err := pm.RegisterPlugin(newFakePlugin("alpha"), nil); err != nil {
		t.Fatalf("first RegisterPlugin: %v", err)
	}

	err := pm.RegisterPlugin(newFakePlugin("alpha"), nil)
	if err == nil {
		t.Fatal("expected a duplicate registration to be rejected")
	}
	if !strings.Contains(err.Error(), "already registered") {
		t.Errorf("expected an 'already registered' error, got %v", err)
	}
	if got := len(pm.ListPlugins()); got != 1 {
		t.Errorf("expected the duplicate to be dropped, got %d plugins", got)
	}
}

func TestPluginManagerRegisterInitError(t *testing.T) {
	pm, _ := newTestPluginManager()
	t.Cleanup(pm.Shutdown)

	p := newFakePlugin("broken")
	p.initErr = errors.New("bad config")

	err := pm.RegisterPlugin(p, nil)
	if err == nil {
		t.Fatal("expected an Init failure to surface")
	}
	if !strings.Contains(err.Error(), "init plugin broken") {
		t.Errorf("expected the error to name the plugin, got %v", err)
	}

	// The instance is kept so the failure can be inspected.
	inst, ok := pm.GetPlugin("broken")
	if !ok {
		t.Fatal("expected the errored instance to be retained")
	}
	if inst.Status != PluginStatusError {
		t.Errorf("expected status %q, got %q", PluginStatusError, inst.Status)
	}

	// A failed plugin occupies its name, so re-registration is still refused.
	if err := pm.RegisterPlugin(newFakePlugin("broken"), nil); err == nil {
		t.Error("expected the name to remain reserved after an Init failure")
	}
}

func TestPluginManagerRegisterNilConfig(t *testing.T) {
	pm, _ := newTestPluginManager()
	t.Cleanup(pm.Shutdown)

	if err := pm.RegisterPlugin(newFakePlugin("alpha"), nil); err != nil {
		t.Fatalf("RegisterPlugin: %v", err)
	}
	inst, _ := pm.GetPlugin("alpha")
	if inst.Config != nil {
		t.Errorf("expected a nil config to be stored as nil, got %s", inst.Config)
	}
}

func TestPluginManagerStartPlugin(t *testing.T) {
	pm, _ := newTestPluginManager()
	t.Cleanup(pm.Shutdown)

	p := newFakePlugin("alpha")
	if err := pm.RegisterPlugin(p, nil); err != nil {
		t.Fatalf("RegisterPlugin: %v", err)
	}
	if err := pm.StartPlugin("alpha"); err != nil {
		t.Fatalf("StartPlugin: %v", err)
	}

	inst, _ := pm.GetPlugin("alpha")
	if inst.Status != PluginStatusRunning {
		t.Errorf("expected status %q, got %q", PluginStatusRunning, inst.Status)
	}
	if _, start, _ := p.counts(); start != 1 {
		t.Errorf("expected Start to be called once, got %d", start)
	}

	evt := wantEvent(t, pm, "started")
	if evt.Plugin != "alpha" {
		t.Errorf("expected the event to name the plugin, got %q", evt.Plugin)
	}
	if evt.Timestamp.IsZero() {
		t.Error("expected the event to be timestamped")
	}
}

func TestPluginManagerStartIsIdempotent(t *testing.T) {
	pm, _ := newTestPluginManager()
	t.Cleanup(pm.Shutdown)

	p := newFakePlugin("alpha")
	if err := pm.RegisterPlugin(p, nil); err != nil {
		t.Fatalf("RegisterPlugin: %v", err)
	}
	for i := 0; i < 3; i++ {
		if err := pm.StartPlugin("alpha"); err != nil {
			t.Fatalf("StartPlugin #%d: %v", i+1, err)
		}
	}

	if _, start, _ := p.counts(); start != 1 {
		t.Errorf("expected Start to be called exactly once, got %d", start)
	}
}

// StartPlugin used to read the status under the lock and then write it back
// outside it, so concurrent callers all saw "not running" and all called
// Plugin.Start. The start hook blocks here so the losers would be caught in the
// act rather than after the fact.
func TestPluginManagerConcurrentStartRunsStartOnce(t *testing.T) {
	pm, _ := newTestPluginManager()
	t.Cleanup(pm.Shutdown)

	const callers = 8
	var starts int32
	release := make(chan struct{})
	p := NewBuiltinPlugin(
		Metadata{Name: "racy"},
		WithStart(func() error {
			atomic.AddInt32(&starts, 1)
			<-release
			return nil
		}),
	)
	if err := pm.RegisterPlugin(p, nil); err != nil {
		t.Fatalf("RegisterPlugin: %v", err)
	}

	begin := make(chan struct{})
	var ready, done sync.WaitGroup
	ready.Add(callers)
	done.Add(callers)
	for i := 0; i < callers; i++ {
		go func() {
			defer done.Done()
			ready.Done()
			<-begin
			if err := pm.StartPlugin("racy"); err != nil {
				t.Errorf("StartPlugin: %v", err)
			}
		}()
	}
	ready.Wait()
	close(begin)

	// With the status check guarded, every other caller is still blocked on the
	// manager lock, so only one start can ever be in flight.
	deadline := time.Now().Add(250 * time.Millisecond)
	for time.Now().Before(deadline) && atomic.LoadInt32(&starts) < callers {
		time.Sleep(2 * time.Millisecond)
	}
	close(release)
	done.Wait()

	if got := atomic.LoadInt32(&starts); got != 1 {
		t.Errorf("Plugin.Start ran %d times, want exactly 1", got)
	}
	inst, _ := pm.GetPlugin("racy")
	if inst.Status != PluginStatusRunning {
		t.Errorf("status = %q, want %q", inst.Status, PluginStatusRunning)
	}
}

func TestPluginManagerStartAfterStop(t *testing.T) {
	pm, _ := newTestPluginManager()
	t.Cleanup(pm.Shutdown)

	p := newFakePlugin("alpha")
	if err := pm.RegisterPlugin(p, nil); err != nil {
		t.Fatalf("RegisterPlugin: %v", err)
	}
	if err := pm.StartPlugin("alpha"); err != nil {
		t.Fatalf("StartPlugin: %v", err)
	}
	if err := pm.StopPlugin("alpha"); err != nil {
		t.Fatalf("StopPlugin: %v", err)
	}
	if err := pm.StartPlugin("alpha"); err != nil {
		t.Fatalf("restart: %v", err)
	}

	inst, _ := pm.GetPlugin("alpha")
	if inst.Status != PluginStatusRunning {
		t.Errorf("expected status %q after a restart, got %q", PluginStatusRunning, inst.Status)
	}
	if _, start, _ := p.counts(); start != 2 {
		t.Errorf("expected Start to be called twice, got %d", start)
	}
}

func TestPluginManagerStartError(t *testing.T) {
	pm, _ := newTestPluginManager()
	t.Cleanup(pm.Shutdown)

	p := newFakePlugin("alpha")
	p.startErr = errors.New("port in use")
	if err := pm.RegisterPlugin(p, nil); err != nil {
		t.Fatalf("RegisterPlugin: %v", err)
	}

	err := pm.StartPlugin("alpha")
	if err == nil {
		t.Fatal("expected a Start failure to surface")
	}
	if !strings.Contains(err.Error(), "start plugin alpha") {
		t.Errorf("expected the error to name the plugin, got %v", err)
	}
	inst, _ := pm.GetPlugin("alpha")
	if inst.Status != PluginStatusError {
		t.Errorf("expected status %q, got %q", PluginStatusError, inst.Status)
	}
}

func TestPluginManagerStopPlugin(t *testing.T) {
	pm, _ := newTestPluginManager()
	t.Cleanup(pm.Shutdown)

	p := newFakePlugin("alpha")
	if err := pm.RegisterPlugin(p, nil); err != nil {
		t.Fatalf("RegisterPlugin: %v", err)
	}
	if err := pm.StartPlugin("alpha"); err != nil {
		t.Fatalf("StartPlugin: %v", err)
	}
	if err := pm.StopPlugin("alpha"); err != nil {
		t.Fatalf("StopPlugin: %v", err)
	}

	inst, _ := pm.GetPlugin("alpha")
	if inst.Status != PluginStatusStopped {
		t.Errorf("expected status %q, got %q", PluginStatusStopped, inst.Status)
	}
	if _, _, stop := p.counts(); stop != 1 {
		t.Errorf("expected Stop to be called once, got %d", stop)
	}

	evt := wantEvent(t, pm, "stopped")
	if evt.Plugin != "alpha" {
		t.Errorf("expected the event to name the plugin, got %q", evt.Plugin)
	}
}

func TestPluginManagerStopError(t *testing.T) {
	pm, _ := newTestPluginManager()
	t.Cleanup(pm.Shutdown)

	p := newFakePlugin("alpha")
	p.stopErr = errors.New("still busy")
	if err := pm.RegisterPlugin(p, nil); err != nil {
		t.Fatalf("RegisterPlugin: %v", err)
	}
	if err := pm.StartPlugin("alpha"); err != nil {
		t.Fatalf("StartPlugin: %v", err)
	}

	err := pm.StopPlugin("alpha")
	if err == nil {
		t.Fatal("expected a Stop failure to surface")
	}
	if !strings.Contains(err.Error(), "stop plugin alpha") {
		t.Errorf("expected the error to name the plugin, got %v", err)
	}
	inst, _ := pm.GetPlugin("alpha")
	if inst.Status != PluginStatusRunning {
		t.Errorf("expected the status to stay %q after a failed stop, got %q", PluginStatusRunning, inst.Status)
	}
}

func TestPluginManagerUnregisterPlugin(t *testing.T) {
	pm, _ := newTestPluginManager()
	t.Cleanup(pm.Shutdown)

	p := newFakePlugin("alpha")
	if err := pm.RegisterPlugin(p, nil); err != nil {
		t.Fatalf("RegisterPlugin: %v", err)
	}
	if err := pm.StartPlugin("alpha"); err != nil {
		t.Fatalf("StartPlugin: %v", err)
	}
	if err := pm.UnregisterPlugin("alpha"); err != nil {
		t.Fatalf("UnregisterPlugin: %v", err)
	}

	if _, ok := pm.GetPlugin("alpha"); ok {
		t.Error("expected the plugin to be gone")
	}
	if _, _, stop := p.counts(); stop != 1 {
		t.Errorf("expected the running plugin to be stopped, got %d stops", stop)
	}

	evt := wantEvent(t, pm, "unregistered")
	if evt.Plugin != "alpha" {
		t.Errorf("expected the event to name the plugin, got %q", evt.Plugin)
	}
}

func TestPluginManagerUnregisterDoesNotStopIdlePlugin(t *testing.T) {
	pm, _ := newTestPluginManager()
	t.Cleanup(pm.Shutdown)

	p := newFakePlugin("alpha")
	if err := pm.RegisterPlugin(p, nil); err != nil {
		t.Fatalf("RegisterPlugin: %v", err)
	}
	if err := pm.UnregisterPlugin("alpha"); err != nil {
		t.Fatalf("UnregisterPlugin: %v", err)
	}

	if _, _, stop := p.counts(); stop != 0 {
		t.Errorf("expected an unloaded plugin not to be stopped, got %d stops", stop)
	}
}

func TestPluginManagerUnregisterAfterStopError(t *testing.T) {
	pm, _ := newTestPluginManager()
	t.Cleanup(pm.Shutdown)

	p := newFakePlugin("alpha")
	p.stopErr = errors.New("wedged")
	if err := pm.RegisterPlugin(p, nil); err != nil {
		t.Fatalf("RegisterPlugin: %v", err)
	}
	if err := pm.StartPlugin("alpha"); err != nil {
		t.Fatalf("StartPlugin: %v", err)
	}

	// A wedged plugin is still removed; the error is logged, not returned.
	if err := pm.UnregisterPlugin("alpha"); err != nil {
		t.Fatalf("UnregisterPlugin: %v", err)
	}
	if _, ok := pm.GetPlugin("alpha"); ok {
		t.Error("expected the wedged plugin to be removed anyway")
	}
}

func TestPluginManagerUnknownPlugin(t *testing.T) {
	pm, _ := newTestPluginManager()
	t.Cleanup(pm.Shutdown)

	if err := pm.RegisterPlugin(newFakePlugin("alpha"), nil); err != nil {
		t.Fatalf("RegisterPlugin: %v", err)
	}

	tests := []struct {
		name string
		call func(string) error
	}{
		{"start", pm.StartPlugin},
		{"stop", pm.StopPlugin},
		{"unregister", pm.UnregisterPlugin},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.call("missing")
			if err == nil {
				t.Fatal("expected an error for an unknown plugin")
			}
			if !strings.Contains(err.Error(), "not found") {
				t.Errorf("expected a 'not found' error, got %v", err)
			}
		})
	}
}

func TestPluginManagerListPlugins(t *testing.T) {
	pm, _ := newTestPluginManager()
	t.Cleanup(pm.Shutdown)

	names := []string{"alpha", "beta", "gamma"}
	for _, n := range names {
		if err := pm.RegisterPlugin(newFakePlugin(n), nil); err != nil {
			t.Fatalf("RegisterPlugin %s: %v", n, err)
		}
	}

	got := pm.ListPlugins()
	if len(got) != len(names) {
		t.Fatalf("expected %d plugins, got %d", len(names), len(got))
	}
	seen := make(map[string]bool, len(got))
	for _, inst := range got {
		seen[inst.Metadata.Name] = true
	}
	for _, n := range names {
		if !seen[n] {
			t.Errorf("expected %q in the listing", n)
		}
	}

	// The listing is a copy; mutating it must not corrupt the registry.
	got[0].Status = PluginStatusError
	first, ok := pm.GetPlugin(got[0].Metadata.Name)
	if !ok {
		t.Fatal("expected the listed plugin to still be registered")
	}
	if first.Status == PluginStatusError {
		t.Error("expected ListPlugins to return copies")
	}
}

func TestPluginManagerGetPluginMissing(t *testing.T) {
	pm, _ := newTestPluginManager()
	t.Cleanup(pm.Shutdown)

	inst, ok := pm.GetPlugin("nope")
	if ok {
		t.Error("expected the lookup to miss")
	}
	if inst != nil {
		t.Errorf("expected a nil instance, got %+v", inst)
	}
}

func TestPluginManagerShutdown(t *testing.T) {
	pm, _ := newTestPluginManager()

	running := newFakePlugin("running")
	idle := newFakePlugin("idle")
	for _, p := range []*fakePlugin{running, idle} {
		if err := pm.RegisterPlugin(p, nil); err != nil {
			t.Fatalf("RegisterPlugin %s: %v", p.meta.Name, err)
		}
	}
	if err := pm.StartPlugin("running"); err != nil {
		t.Fatalf("StartPlugin: %v", err)
	}

	pm.Shutdown()

	if _, _, stop := running.counts(); stop != 1 {
		t.Errorf("expected the running plugin to be stopped, got %d", stop)
	}
	if _, _, stop := idle.counts(); stop != 0 {
		t.Errorf("expected the idle plugin to be left alone, got %d stops", stop)
	}
	if pm.ctx.Err() == nil {
		t.Error("expected the manager context to be cancelled by Shutdown")
	}
}

func TestPluginManagerShutdownToleratesStopError(t *testing.T) {
	pm, _ := newTestPluginManager()

	p := newFakePlugin("wedged")
	p.stopErr = errors.New("nope")
	if err := pm.RegisterPlugin(p, nil); err != nil {
		t.Fatalf("RegisterPlugin: %v", err)
	}
	if err := pm.StartPlugin("wedged"); err != nil {
		t.Fatalf("StartPlugin: %v", err)
	}

	pm.Shutdown()
	if _, _, stop := p.counts(); stop != 1 {
		t.Errorf("expected Stop to be attempted once, got %d", stop)
	}
}

func TestPluginManagerLoadPlugin(t *testing.T) {
	pm, _ := newTestPluginManager()
	t.Cleanup(pm.Shutdown)

	err := pm.LoadPlugin("C:/plugins/demo.so", nil)
	if err == nil {
		t.Fatal("expected .so loading to be refused")
	}
	if !strings.Contains(err.Error(), "not implemented") {
		t.Errorf("expected a 'not implemented' error, got %v", err)
	}
	if got := len(pm.ListPlugins()); got != 0 {
		t.Errorf("expected a failed load to register nothing, got %d", got)
	}
}

func TestPluginManagerEventsDropWhenNobodyReads(t *testing.T) {
	pm, _ := newTestPluginManager()
	t.Cleanup(pm.Shutdown)

	p := newFakePlugin("chatty")
	if err := pm.RegisterPlugin(p, nil); err != nil {
		t.Fatalf("RegisterPlugin: %v", err)
	}
	// The event channel holds 32 entries; far more than that must not block.
	for i := 0; i < 200; i++ {
		if err := pm.StartPlugin("chatty"); err != nil {
			t.Fatalf("StartPlugin: %v", err)
		}
		if err := pm.StopPlugin("chatty"); err != nil {
			t.Fatalf("StopPlugin: %v", err)
		}
	}

	received := 0
drain:
	for {
		select {
		case <-pm.Events():
			received++
		default:
			break drain
		}
	}
	if received == 0 {
		t.Error("expected the buffered events to still be readable")
	}
	if received > 32 {
		t.Errorf("expected the channel to cap at 32 events, drained %d", received)
	}
}

func TestPluginManagerConcurrentUse(t *testing.T) {
	pm, _ := newTestPluginManager()
	t.Cleanup(pm.Shutdown)

	const n = 16
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(3)
		name := fmt.Sprintf("plugin-%02d", i)
		p := newFakePlugin(name)
		go func() {
			defer wg.Done()
			if err := pm.RegisterPlugin(p, nil); err != nil {
				t.Errorf("RegisterPlugin %s: %v", name, err)
			}
		}()
		go func() {
			defer wg.Done()
			pm.ListPlugins()
		}()
		go func() {
			defer wg.Done()
			pm.GetPlugin(name)
		}()
	}
	wg.Wait()

	if got := len(pm.ListPlugins()); got != n {
		t.Errorf("expected %d plugins, got %d", n, got)
	}
}

func TestNewBuiltinPluginDefaults(t *testing.T) {
	meta := Metadata{Name: "bare", Version: "0.1.0", Author: "test"}
	p := NewBuiltinPlugin(meta)

	if p == nil {
		t.Fatal("expected a non-nil plugin")
	}
	got := p.Metadata()
	if got.Name != meta.Name || got.Version != meta.Version || got.Author != meta.Author {
		t.Errorf("expected the metadata to be returned verbatim, got %+v", got)
	}
	// With no hooks wired, every lifecycle call is a successful no-op.
	if err := p.Init(context.Background(), nil); err != nil {
		t.Errorf("expected Init to be a no-op, got %v", err)
	}
	if err := p.Start(); err != nil {
		t.Errorf("expected Start to be a no-op, got %v", err)
	}
	if err := p.Stop(); err != nil {
		t.Errorf("expected Stop to be a no-op, got %v", err)
	}
	if got := p.Services(); got != nil {
		t.Errorf("expected no services, got %+v", got)
	}
}

func TestBuiltinPluginOptionsAreWired(t *testing.T) {
	var gotHost Host
	calls := map[string]int{}
	p := NewBuiltinPlugin(
		Metadata{Name: "wired"},
		WithInit(func(ctx context.Context, host Host) error {
			calls["init"]++
			if ctx == nil {
				t.Error("expected a non-nil context")
			}
			gotHost = host
			return nil
		}),
		WithStart(func() error { calls["start"]++; return nil }),
		WithStop(func() error { calls["stop"]++; return nil }),
		WithServices([]ServiceDescriptor{{Name: "svc", Type: ServiceTypeMessaging, Port: 1234}}),
	)

	host := newTestHost()
	if err := p.Init(context.Background(), host); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := p.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := p.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	for _, name := range []string{"init", "start", "stop"} {
		if calls[name] != 1 {
			t.Errorf("expected %s to be called once, got %d", name, calls[name])
		}
	}
	if gotHost != Host(host) {
		t.Error("expected the host to be handed to the init hook")
	}
	if got := p.Services(); len(got) != 1 || got[0].Name != "svc" {
		t.Errorf("expected the wired services, got %+v", got)
	}
}

func TestBuiltinPluginPropagatesHookErrors(t *testing.T) {
	boom := errors.New("boom")

	tests := []struct {
		name string
		opt  BuiltinOption
		call func(*BuiltinPlugin) error
	}{
		{
			name: "init",
			opt:  WithInit(func(context.Context, Host) error { return boom }),
			call: func(p *BuiltinPlugin) error { return p.Init(context.Background(), nil) },
		},
		{
			name: "start",
			opt:  WithStart(func() error { return boom }),
			call: func(p *BuiltinPlugin) error { return p.Start() },
		},
		{
			name: "stop",
			opt:  WithStop(func() error { return boom }),
			call: func(p *BuiltinPlugin) error { return p.Stop() },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewBuiltinPlugin(Metadata{Name: "boom"}, tt.opt)
			if err := tt.call(p); !errors.Is(err, boom) {
				t.Errorf("expected the hook error to propagate, got %v", err)
			}
		})
	}
}

func TestBuiltinPluginOptionOrder(t *testing.T) {
	// Later options must win, which is what lets a caller override a default.
	p := NewBuiltinPlugin(Metadata{Name: "x"},
		WithServices([]ServiceDescriptor{{Name: "first"}}),
		WithServices([]ServiceDescriptor{{Name: "second"}}),
	)
	if got := p.Services(); len(got) != 1 || got[0].Name != "second" {
		t.Errorf("expected the last option to win, got %+v", got)
	}
}

// A full register/start/stop cycle through the manager: the endpoint the plugin
// registers must answer while it is running and be torn down when it stops.
func TestBuiltinPluginLifecycleThroughManager(t *testing.T) {
	pm, host := newTestPluginManager()
	t.Cleanup(pm.Shutdown)

	var running atomic.Bool
	router := host.HTTPRouter()
	router.HandleFunc("/svc/ping", func(w http.ResponseWriter, r *http.Request) {
		if !running.Load() {
			http.Error(w, "plugin stopped", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	p := NewBuiltinPlugin(
		Metadata{Name: "lifecycle", Version: "2.0.0"},
		WithStart(func() error {
			running.Store(true)
			return nil
		}),
		WithStop(func() error {
			running.Store(false)
			return nil
		}),
		WithServices([]ServiceDescriptor{{
			Name: "ping",
			Type: ServiceTypeCustom,
			Endpoints: []EndpointDesc{{
				Path:         "/svc/ping",
				Method:       "GET",
				Description:  "liveness probe",
				AuthRequired: false,
			}},
		}}),
	)

	// httptest binds 127.0.0.1 on an ephemeral port.
	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)

	if err := pm.RegisterPlugin(p, nil); err != nil {
		t.Fatalf("RegisterPlugin: %v", err)
	}
	if running.Load() {
		t.Fatal("expected the plugin to be stopped after registration")
	}
	if code := getStatus(t, srv.URL+"/svc/ping"); code != http.StatusServiceUnavailable {
		t.Errorf("expected the endpoint to be unavailable before start, got %d", code)
	}

	if err := pm.StartPlugin("lifecycle"); err != nil {
		t.Fatalf("StartPlugin: %v", err)
	}
	if !running.Load() {
		t.Fatal("expected the start hook to be observable")
	}
	inst, _ := pm.GetPlugin("lifecycle")
	if inst.Status != PluginStatusRunning {
		t.Errorf("expected status %q, got %q", PluginStatusRunning, inst.Status)
	}
	if code := getStatus(t, srv.URL+"/svc/ping"); code != http.StatusOK {
		t.Errorf("expected the endpoint to answer while running, got %d", code)
	}

	if err := pm.StopPlugin("lifecycle"); err != nil {
		t.Fatalf("StopPlugin: %v", err)
	}
	if running.Load() {
		t.Fatal("expected the stop hook to tear the plugin down")
	}
	inst, _ = pm.GetPlugin("lifecycle")
	if inst.Status != PluginStatusStopped {
		t.Errorf("expected status %q, got %q", PluginStatusStopped, inst.Status)
	}
	if code := getStatus(t, srv.URL+"/svc/ping"); code != http.StatusServiceUnavailable {
		t.Errorf("expected the endpoint to be torn down after stop, got %d", code)
	}
}

func TestBuiltinPluginInitReceivesManagerContext(t *testing.T) {
	pm, host := newTestPluginManager()

	var sawCancelled atomic.Bool
	p := NewBuiltinPlugin(
		Metadata{Name: "ctx"},
		WithInit(func(ctx context.Context, h Host) error {
			if h != Host(host) {
				t.Error("expected the manager's host")
			}
			go func() {
				<-ctx.Done()
				sawCancelled.Store(true)
			}()
			return nil
		}),
	)
	if err := pm.RegisterPlugin(p, nil); err != nil {
		t.Fatalf("RegisterPlugin: %v", err)
	}

	pm.Shutdown()

	select {
	case <-pm.ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("Shutdown did not cancel the manager context")
	}
	deadline := time.Now().Add(time.Second)
	for !sawCancelled.Load() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !sawCancelled.Load() {
		t.Error("expected the plugin's context to be cancelled by Shutdown")
	}
}

func TestMetadataJSONFieldNames(t *testing.T) {
	raw, err := json.Marshal(Metadata{
		Name: "demo", Version: "1.2.3", Author: "me", Description: "d",
		License: "MIT", MinHostVer: "1.0.0", Dependencies: []string{"other"},
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	want := map[string]string{
		"name":             "demo",
		"version":          "1.2.3",
		"author":           "me",
		"description":      "d",
		"license":          "MIT",
		"min_host_version": "1.0.0",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("field %q: expected %q, got %v", k, v, got[k])
		}
	}
	if _, ok := got["dependencies"]; !ok {
		t.Error("expected the dependencies key to be present")
	}

	// Dependencies carries omitempty, so an empty list must not be emitted.
	raw, err = json.Marshal(Metadata{Name: "bare"})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if strings.Contains(string(raw), "dependencies") {
		t.Errorf("expected dependencies to be omitted, got %s", raw)
	}
}

func TestServiceDescriptorJSONRoundTrip(t *testing.T) {
	want := ServiceDescriptor{
		Name:   "messaging",
		Type:   ServiceTypeMessaging,
		Port:   9090,
		Config: json.RawMessage(`{"queue":"inbox"}`),
		Endpoints: []EndpointDesc{
			{Path: "/send", Method: "POST", Description: "send", AuthRequired: true},
		},
	}

	raw, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got ServiceDescriptor
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if got.Name != want.Name || got.Type != want.Type || got.Port != want.Port {
		t.Errorf("scalar fields did not round trip: %+v", got)
	}
	if string(got.Config) != string(want.Config) {
		t.Errorf("config did not round trip: %s", got.Config)
	}
	if len(got.Endpoints) != 1 {
		t.Fatalf("expected 1 endpoint, got %d", len(got.Endpoints))
	}
	if got.Endpoints[0] != want.Endpoints[0] {
		t.Errorf("endpoint did not round trip: %+v", got.Endpoints[0])
	}
}

func TestServiceTypesAreDistinct(t *testing.T) {
	all := []ServiceType{
		ServiceTypeMessaging, ServiceTypeFiles, ServiceTypeDocs,
		ServiceTypeVoice, ServiceTypeVPN, ServiceTypeRegistry, ServiceTypeCustom,
	}
	seen := make(map[ServiceType]bool, len(all))
	for _, st := range all {
		if seen[st] {
			t.Errorf("duplicate service type %q", st)
		}
		seen[st] = true
	}
	if len(seen) != 7 {
		t.Errorf("expected 7 distinct service types, got %d", len(seen))
	}
}

func TestPluginStatusConstants(t *testing.T) {
	statuses := []PluginStatus{
		PluginStatusLoaded, PluginStatusRunning, PluginStatusStopped, PluginStatusError,
	}
	want := []string{"loaded", "running", "stopped", "error"}
	for i, st := range statuses {
		if string(st) != want[i] {
			t.Errorf("expected %q, got %q", want[i], st)
		}
	}
}

func TestBuiltinPluginImplementsPlugin(t *testing.T) {
	var p Plugin = NewBuiltinPlugin(Metadata{Name: "iface"})
	if p.Metadata().Name != "iface" {
		t.Errorf("expected the metadata to be reachable through the interface, got %q", p.Metadata().Name)
	}
	if err := p.Start(); err != nil {
		t.Errorf("Start: %v", err)
	}
	if err := p.Stop(); err != nil {
		t.Errorf("Stop: %v", err)
	}
	if p.Services() != nil {
		t.Error("expected no services")
	}
}

// wantEvent drains the buffered events until one of the wanted type shows up.
func wantEvent(t *testing.T, pm *PluginManager, want string) PluginEvent {
	t.Helper()
	deadline := time.After(2 * time.Second)
	var seen []string
	for {
		select {
		case evt := <-pm.Events():
			seen = append(seen, evt.Type)
			if evt.Type == want {
				return evt
			}
		case <-deadline:
			t.Fatalf("timed out waiting for a %q event, saw %v", want, seen)
			return PluginEvent{}
		}
	}
}

func drainEvent(t *testing.T, pm *PluginManager) PluginEvent {
	t.Helper()
	select {
	case evt := <-pm.Events():
		return evt
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for a plugin event")
		return PluginEvent{}
	}
}
