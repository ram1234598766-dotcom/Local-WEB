# LocalWEB Plugin API Reference

LocalWEB supports **Go plugins** (`.so` files) and **Built-in plugins** (compiled into binary).

**Author: Mrityunjay K**

---

## Plugin Interface

All plugins must implement the `Plugin` interface:

```go
type Plugin interface {
    // Metadata returns plugin metadata
    Metadata() Metadata

    // Init initializes the plugin with host capabilities
    Init(ctx context.Context, host Host) error

    // Start starts the plugin
    Start() error

    // Stop stops the plugin
    Stop() error

    // Services returns services provided by this plugin
    Services() []ServiceDescriptor
}
```

---

## Metadata

```go
type Metadata struct {
    Name        string   `json:"name"`         // Unique plugin name
    Version     string   `json:"version"`      // Semantic version
    Author      string   `json:"author"`       // Author name
    Description string   `json:"description"`  // What it does
    License     string   `json:"license"`      // SPDX license
    MinHostVer  string   `json:"min_host_version"`
    Dependencies []string `json:"dependencies,omitempty"`
}
```

---

## Host Interface

Plugins receive a `Host` with access to node capabilities:

```go
type Host interface {
    // Node identity
    Identity() Identity

    // Transport layer (P2P)
    Transport() PluginTransport

    // Discovery
    Discovery() Discovery

    // Encrypted store
    Store() Store

    // Security (capabilities, audit)
    Security() Security

    // HTTP routing
    HTTPRouter() HTTPRouter

    // Logging
    Logger() Logger

    // Config
    Config(name string) json.RawMessage

    // Event bus
    EmitEvent(event string, data interface{}) error
}
```

---

## Built-in Plugin Framework

Easiest way to create a plugin:

```go
import "github.com/ram1234598766-dotcom/Local-WEB/pkg/plugin"

func NewMyPlugin() *plugin.BuiltinPlugin {
    return plugin.NewBuiltinPlugin(plugin.Metadata{
        Name:        "my-plugin",
        Version:     "1.0.0",
        Author:      "Your Name",
        Description: "What it does",
        License:     "MIT",
        MinHostVer:  "1.0.0",
    },
        plugin.WithInit(func(ctx context.Context, host plugin.Host) error {
            // Initialize: register HTTP routes, etc.
            return nil
        }),
        plugin.WithStart(func() error {
            // Start background goroutines
            return nil
        }),
        plugin.WithStop(func() error {
            // Cleanup
            return nil
        }),
        plugin.WithServices([]plugin.ServiceDescriptor{
            {
                Name: "my-service",
                Type: plugin.ServiceTypeCustom,
                Port: 8082,
                Endpoints: []plugin.EndpointDesc{
                    {Path: "/api/hello", Method: "GET", Description: "Say hello", AuthRequired: false},
                },
            },
        }),
    )
}
```

---

## Example: Echo Plugin

```go
package myplugin

import (
    "context"
    "encoding/json"
    "net/http"

    "github.com/ram1234598766-dotcom/Local-WEB/pkg/plugin"
)

type EchoPlugin struct {
    *plugin.BuiltinPlugin
    host  plugin.Host
    server *http.Server
}

func NewEchoPlugin() *EchoPlugin {
    p := &EchoPlugin{}
    p.BuiltinPlugin = plugin.NewBuiltinPlugin(plugin.Metadata{
        Name:        "echo",
        Version:     "1.0.0",
        Author:      "LocalWEB Team",
        Description: "Echo service for testing",
        License:     "MIT",
        MinHostVer:  "1.0.0",
    },
        plugin.WithInit(p.init),
        plugin.WithStart(p.start),
        plugin.WithStop(p.stop),
        plugin.WithServices([]plugin.ServiceDescriptor{
            {
                Name: "echo",
                Type: plugin.ServiceTypeCustom,
                Port: 8081,
                Endpoints: []plugin.EndpointDesc{
                    {Path: "/echo", Method: "POST", Description: "Echo body", AuthRequired: false},
                    {Path: "/echo/{msg}", Method: "GET", Description: "Echo path", AuthRequired: false},
                },
            },
        })
    return p
}

func (p *EchoPlugin) init(ctx context.Context, host plugin.Host) error {
    p.host = host
    router := host.HTTPRouter()
    router.HandleFunc("/echo", p.handleEcho)
    router.HandleFunc("/echo/", p.handleEchoPath)
    host.Logger().Info().Msg("echo plugin initialized")
    return nil
}

func (p *EchoPlugin) start() error {
    router := p.host.HTTPRouter()
    p.server = &http.Server{
        Addr:    ":8081",
        Handler: router,
    }
    go func() {
        if err := p.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
            p.host.Logger().Error().Err(err).Msg("echo server error")
        }
    }()
    p.host.Logger().Info().Msg("echo plugin started on :8081")
    return nil
}

func (p *EchoPlugin) stop() error {
    if p.server != nil {
        ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
        defer cancel()
        return p.server.Shutdown(ctx)
    }
    return nil
}

func (p *EchoPlugin) handleEcho(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
        return
    }
    body, _ := io.ReadAll(r.Body)
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(map[string]interface{}{
        "echo":      string(body),
        "timestamp": time.Now().Format(time.RFC3339),
        "plugin":    "echo",
    })
}

func (p *EchoPlugin) handleEchoPath(w http.ResponseWriter, r *http.Request) {
    msg := r.URL.Path[len("/echo/"):]
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(map[string]interface{}{
        "echo":      msg,
        "timestamp": time.Now().Format(time.RFC3339),
        "plugin":    "echo",
    })
}
```

---

## Registering Plugins

### Built-in (at startup)

```go
// In cmd/node/main.go
pluginMgr := plugin.NewPluginManager(host)

echoPlugin := myplugin.NewEchoPlugin()
pluginMgr.RegisterPlugin(echoPlugin)
pluginMgr.StartPlugin("echo")
```

### Go Plugin (runtime)

```go
// Load .so file (not available on all platforms)
err := pluginMgr.LoadPlugin("./plugins/myplugin.so", configJSON)
```

---

## Plugin Manager API

```go
// Register built-in plugin
RegisterPlugin(p Plugin, config json.RawMessage) error

// Start/Stop
StartPlugin(name string) error
StopPlugin(name string) error

// Unregister
UnregisterPlugin(name string) error

// List
ListPlugins() []PluginInstance

// Get
GetPlugin(name string) (*PluginInstance, bool)

// Events
Events() <-chan PluginEvent

// Shutdown all
Shutdown()
```

---

## Plugin Event Types

| Event | Data |
|-------|------|
| `loaded` | Plugin loaded |
| `started` | Plugin started |
| `stopped` | Plugin stopped |
| `error` | Plugin error |

---

## Example: Metrics Plugin

```go
type MetricsPlugin struct {
    *plugin.BuiltinPlugin
    host plugin.Host
}

func NewMetricsPlugin() *MetricsPlugin {
    p := &MetricsPlugin{}
    p.BuiltinPlugin = plugin.NewBuiltinPlugin(plugin.Metadata{
        Name:        "metrics",
        Version:     "1.0.0",
        Author:      "LocalWEB Team",
        Description: "Prometheus metrics exporter",
        License:     "MIT",
        MinHostVer:  "1.0.0",
    },
        plugin.WithInit(p.init),
        plugin.WithServices([]plugin.ServiceDescriptor{
            {
                Name: "metrics",
                Type: plugin.ServiceTypeCustom,
                Endpoints: []plugin.EndpointDesc{
                    {Path: "/metrics", Method: "GET", Description: "Prometheus metrics", AuthRequired: false},
                    {Path: "/health", Method: "GET", Description: "Health check", AuthRequired: false},
                },
            },
        })
    return p
}

func (p *MetricsPlugin) init(ctx context.Context, host plugin.Host) error {
    p.host = host
    router := host.HTTPRouter()
    router.HandleFunc("/metrics", p.handleMetrics)
    router.HandleFunc("/health", p.handleHealth)
    return nil
}

func (p *MetricsPlugin) handleMetrics(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "text/plain; version=0.0.4")
    w.Write([]byte(`# HELP localweb_peers_total Total connected peers
# TYPE localweb_peers_total gauge
localweb_peers_total 0
`))
}

func (p *MetricsPlugin) handleHealth(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(map[string]string{
        "status": "healthy",
        "timestamp": time.Now().Format(time.RFC3339),
    })
}
```

---

## Capability Tokens

Plugins can request capabilities:

```go
// In plugin init
capMgr := host.Security().CapabilityManager()
token, err := capMgr.Grant(ctx, []string{"peers:read", "files:write"}, 24*time.Hour)
```

### Standard Capabilities

| Capability | Description |
|------------|-------------|
| `peers:read` | List peers |
| `peers:write` | Connect to peers |
| `files:read` | Read files |
| `files:write` | Send files |
| `messaging:read` | Read messages |
| `messaging:write` | Send messages |
| `vpn:connect` | Establish VPN |
| `admin:*` | Administrative |

---

## Security

| Aspect | Implementation |
|--------|----------------|
| Isolation | Separate Go plugin (separate memory) |
| Capabilities | Token-based, time-limited |
| Audit | All plugin actions logged to audit log |
| Sandbox | No direct syscalls (Go plugin limit) |

---

## Loading External Plugins

```go
// Not available on all platforms (Windows limited)
err := pluginMgr.LoadPlugin("./plugins/myplugin.so", configJSON)
```

**Requirements:**
- Built with `go build -buildmode=plugin`
- Same Go version as host
- Same dependency versions

---

## Best Practices

1. **Minimal permissions** — Request only needed capabilities
2. **Graceful shutdown** — Implement `Stop()` cleanly
3. **Error handling** — Return errors, don't panic
4. **Logging** — Use `host.Logger()` for structured logs
5. **Config** — Use `host.Config("plugin-name")` for settings

---

*LocalWEB Plugin API v1.0.0 | Module: `github.com/ram1234598766-dotcom/Local-WEB`*