package plugin

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/ram1234598766-dotcom/Local-WEB/pkg/transport"
)

// ExampleEchoPlugin is a simple example plugin that provides an echo service.
type ExampleEchoPlugin struct {
	*BuiltinPlugin
	host   Host
	server *http.Server
}

func NewExampleEchoPlugin() *ExampleEchoPlugin {
	p := &ExampleEchoPlugin{}
	opts := []BuiltinOption{
		WithInit(p.init),
		WithStart(p.start),
		WithStop(p.stop),
		WithServices([]ServiceDescriptor{
			{
				Name: "echo",
				Type: ServiceTypeCustom,
				Port: 8081,
				Endpoints: []EndpointDesc{
					{Path: "/echo", Method: "POST", Description: "Echo back request body", AuthRequired: false},
					{Path: "/echo/{msg}", Method: "GET", Description: "Echo path parameter", AuthRequired: false},
				},
			},
		}),
	}
	p.BuiltinPlugin = NewBuiltinPlugin(Metadata{
		Name:        "echo",
		Version:     "1.0.0",
		Author:      "LocalWEB Team",
		Description: "Simple echo service for testing plugin system",
		License:     "MIT",
		MinHostVer:  "1.0.0",
	}, opts...)
	return p
}

func (p *ExampleEchoPlugin) init(ctx context.Context, host Host) error {
	p.host = host
	router := host.HTTPRouter()
	router.HandleFunc("/echo", p.handleEcho)
	router.HandleFunc("/echo/", p.handleEchoPath)
	host.Logger().Info().Msg("echo plugin initialized")

	// Use transport for type completeness
	_ = transport.DefaultHybridConfig

	return nil
}

func (p *ExampleEchoPlugin) start() error {
	host := p.host
	router := host.HTTPRouter()
	p.server = &http.Server{
		Addr:    ":8081",
		Handler: router,
	}
	go func() {
		if err := p.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			host.Logger().Error().Err(err).Msg("echo server error")
		}
	}()
	host.Logger().Info().Msg("echo plugin started on :8081")
	return nil
}

func (p *ExampleEchoPlugin) stop() error {
	if p.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return p.server.Shutdown(ctx)
	}
	return nil
}

func (p *ExampleEchoPlugin) handleEcho(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"echo":      string(body),
		"timestamp": time.Now().Format(time.RFC3339),
		"plugin":    "echo",
	})
}

func (p *ExampleEchoPlugin) handleEchoPath(w http.ResponseWriter, r *http.Request) {
	msg := r.URL.Path[len("/echo/"):]
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"echo":      msg,
		"timestamp": time.Now().Format(time.RFC3339),
		"plugin":    "echo",
	})
}

// ExampleMetricsPlugin provides Prometheus metrics endpoint.
type ExampleMetricsPlugin struct {
	*BuiltinPlugin
	host Host
}

func NewExampleMetricsPlugin() *ExampleMetricsPlugin {
	p := &ExampleMetricsPlugin{}
	opts := []BuiltinOption{
		WithInit(p.init),
		WithServices([]ServiceDescriptor{
			{
				Name: "metrics",
				Type: ServiceTypeCustom,
				Endpoints: []EndpointDesc{
					{Path: "/metrics", Method: "GET", Description: "Prometheus metrics", AuthRequired: false},
					{Path: "/health", Method: "GET", Description: "Health check", AuthRequired: false},
				},
			},
		}),
	}
	p.BuiltinPlugin = NewBuiltinPlugin(Metadata{
		Name:        "metrics",
		Version:     "1.0.0",
		Author:      "LocalWEB Team",
		Description: "Prometheus metrics exporter",
		License:     "MIT",
		MinHostVer:  "1.0.0",
	}, opts...)
	return p
}

func (p *ExampleMetricsPlugin) init(ctx context.Context, host Host) error {
	p.host = host
	router := host.HTTPRouter()
	router.HandleFunc("/metrics", p.handleMetrics)
	router.HandleFunc("/health", p.handleHealth)
	host.Logger().Info().Msg("metrics plugin initialized")
	return nil
}

func (p *ExampleMetricsPlugin) handleMetrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	// In real implementation, collect actual metrics
	w.Write([]byte(`# HELP localweb_peers_total Total connected peers
# TYPE localweb_peers_total gauge
localweb_peers_total 0

# HELP localweb_uptime_seconds Node uptime
# TYPE localweb_uptime_seconds counter
localweb_uptime_seconds 0

# HELP localweb_plugin_count Total loaded plugins
# TYPE localweb_plugin_count gauge
localweb_plugin_count 2
`))
}

func (p *ExampleMetricsPlugin) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":    "healthy",
		"timestamp": time.Now().Format(time.RFC3339),
	})
}
