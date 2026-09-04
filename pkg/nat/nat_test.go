package nat

import (
	"context"
	"testing"
	"time"
)

func TestHolePunchClientStart(t *testing.T) {
	client := NewHolePunchClient()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := client.Start(ctx, 0)
	if err != nil {
		t.Fatalf("start failed: %v", err)
	}
}

func TestUPnPClientDiscover(t *testing.T) {
	client := NewUPnPClient()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	err := client.Discover(ctx)
	if err == nil && !client.IsAvailable() {
		t.Fatal("expected discovery failure in test env")
	}
}

func TestUPnPAddPortMappingWithoutDiscovery(t *testing.T) {
	client := NewUPnPClient()
	err := client.AddPortMapping(4443, 4443, "UDP", 3600)
	if err == nil {
		t.Fatal("expected error when UPnP not discovered")
	}
}

func TestCircuitRelaySelectAndEstablish(t *testing.T) {
	relay := NewCircuitRelay()
	relay.RegisterNode(&RelayNode{ID: [32]byte{1: 1}, Score: 0.9, Capacity: 10, Load: 1})
	relay.RegisterNode(&RelayNode{ID: [32]byte{2: 1}, Score: 0.8, Capacity: 10, Load: 2})
	relay.RegisterNode(&RelayNode{ID: [32]byte{3: 1}, Score: 0.7, Capacity: 10, Load: 3})

	circuits, err := relay.SelectRelay(3)
	if err != nil {
		t.Fatalf("select relay: %v", err)
	}
	if len(circuits) != 3 {
		t.Fatalf("expected 3 relays, got %d", len(circuits))
	}
	if circuits[0].Score != 0.9 {
		t.Fatalf("expected highest score first, got %f", circuits[0].Score)
	}

	circuit, err := relay.EstablishCircuit([32]byte{1: 1}, [32]byte{2: 2}, 3)
	if err != nil {
		t.Fatalf("establish circuit: %v", err)
	}
	if len(circuit.Hops) != 3 {
		t.Fatalf("expected 3 hops, got %d", len(circuit.Hops))
	}

	out, err := relay.RelayPacket(circuit.ID, []byte("test"), 1)
	if err != nil {
		t.Fatalf("relay packet: %v", err)
	}
	if string(out) != "relayed via hop 1" {
		t.Fatalf("unexpected relay output: %s", out)
	}
}

func TestExtractHeader(t *testing.T) {
	resp := "HTTP/1.1 200 OK\r\nLocation: http://192.168.1.1:8080/desc.xml\r\n\r\n"
	loc := extractHeader(resp, "Location")
	if loc != "http://192.168.1.1:8080/desc.xml" {
		t.Fatalf("unexpected location: %q", loc)
	}
}
