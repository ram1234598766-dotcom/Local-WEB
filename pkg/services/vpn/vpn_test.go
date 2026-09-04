package vpn

import (
	"context"
	"testing"
)

func TestServerCreateAndCloseTunnel(t *testing.T) {
	s := NewServer([32]byte{1: 1})
	id, err := s.CreateTunnel(context.Background(), [32]byte{2: 2}, "addr")
	if err != nil {
		t.Fatalf("create tunnel: %v", err)
	}
	if len(s.Tunnels()) != 1 {
		t.Fatalf("expected 1 tunnel, got %d", len(s.Tunnels()))
	}
	if err := s.CloseTunnel(id); err != nil {
		t.Fatalf("close tunnel: %v", err)
	}
	if len(s.Tunnels()) != 0 {
		t.Fatalf("expected 0 tunnels, got %d", len(s.Tunnels()))
	}
}

func TestServerAddRoute(t *testing.T) {
	s := NewServer([32]byte{1: 1})
	s.AddRoute([32]byte{3: 3}, [32]byte{4: 4}, 10)
	routes := s.Routes()
	if len(routes) != 1 {
		t.Fatalf("expected 1 route, got %d", len(routes))
	}
	if routes[0].Metric != 10 {
		t.Fatalf("expected metric 10, got %d", routes[0].Metric)
	}
}

func TestMarshalUnmarshalTunnel(t *testing.T) {
	tun := Tunnel{Src: [32]byte{1: 1}, Dst: [32]byte{2: 2}, Created: 12345}
	data := MarshalTunnel(tun)
	out, err := UnmarshalTunnel(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Src != tun.Src || out.Dst != tun.Dst || out.Created != tun.Created {
		t.Fatal("marshal round-trip mismatch")
	}
}
