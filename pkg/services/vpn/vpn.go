package vpn

import (
	"context"
	"crypto/sha3"
	"encoding/binary"
	"errors"
	"sync"

	"github.com/rs/zerolog/log"
)

type TunnelID [32]byte

type Tunnel struct {
	ID      TunnelID
	Src     [32]byte
	Dst     [32]byte
	Addr    string
	State   string
	Created int64
}

type Route struct {
	Dest   [32]byte
	Via    [32]byte
	Metric int
}

type Interface interface {
	Name() string
	Up() error
	Down() error
	Addrs() ([]string, error)
	AddRoute(dst string, gw string) error
	Read(buf []byte) (int, error)
	Write(buf []byte) (int, error)
	Close() error
}

type Server struct {
	mu      sync.Mutex
	tunnels map[TunnelID]*Tunnel
	routes  map[string]Route
	iface   Interface
	localID [32]byte
}

func NewServer(localID [32]byte) *Server {
	iface, err := openTUN("tun0")
	if err != nil {
		log.Warn().Err(err).Msg("vpn: could not create TUN interface, running in userspace-only mode")
		iface = nil
	} else {
		go func() {
			if err := iface.Up(); err != nil {
				log.Warn().Err(err).Msg("vpn: failed to bring TUN interface up")
			}
		}()
	}
	return &Server{
		tunnels: make(map[TunnelID]*Tunnel),
		routes:  make(map[string]Route),
		iface:   iface,
		localID: localID,
	}
}

func (s *Server) CreateTunnel(ctx context.Context, peerID [32]byte, addr string) (TunnelID, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := TunnelID(sha3.Sum256(append(s.localID[:], peerID[:]...)))
	s.tunnels[id] = &Tunnel{
		ID:      id,
		Src:     s.localID,
		Dst:     peerID,
		Addr:    addr,
		State:   "open",
		Created: ctxDeadline(ctx),
	}
	return id, nil
}

func (s *Server) CloseTunnel(id TunnelID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.tunnels, id)
	return nil
}

func (s *Server) AddRoute(dest [32]byte, via [32]byte, metric int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := string(dest[:])
	s.routes[key] = Route{Dest: dest, Via: via, Metric: metric}
}

func (s *Server) Routes() []Route {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Route, 0, len(s.routes))
	for _, r := range s.routes {
		out = append(out, r)
	}
	return out
}

func (s *Server) Tunnels() []Tunnel {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Tunnel, 0, len(s.tunnels))
	for _, t := range s.tunnels {
		out = append(out, *t)
	}
	return out
}

func ctxDeadline(ctx context.Context) int64 {
	if dl, ok := ctx.Deadline(); ok {
		return dl.UnixNano()
	}
	return 0
}

func MarshalTunnel(t Tunnel) []byte {
	buf := make([]byte, 32+32+32+8+8)
	copy(buf[:32], t.Src[:])
	copy(buf[32:64], t.Dst[:])
	copy(buf[64:96], t.ID[:])
	binary.BigEndian.PutUint64(buf[96:104], uint64(t.Created))
	return buf
}

func UnmarshalTunnel(data []byte) (Tunnel, error) {
	if len(data) < 104 {
		return Tunnel{}, errors.New("data too short")
	}
	var t Tunnel
	copy(t.Src[:], data[:32])
	copy(t.Dst[:], data[32:64])
	copy(t.ID[:], data[64:96])
	t.Created = int64(binary.BigEndian.Uint64(data[96:104]))
	return t, nil
}
