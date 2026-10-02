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

// DefaultMTU is the largest IP packet the tunnel forwards. Anything larger is
// dropped rather than truncated, because a truncated packet is worse than a
// missing one: it reaches the far side looking valid.
const DefaultMTU = 1500

// Carrier moves tunnelled packets to and from a peer.
//
// This is the boundary that makes the forwarding loop testable. A real carrier
// writes to a transport stream; a test carrier is a channel, so the loop can be
// exercised end to end with no TUN device, no privileges and no network.
type Carrier interface {
	Send(ctx context.Context, packet []byte) error
	Recv(ctx context.Context) ([]byte, error)
	Close() error
}

type Server struct {
	mu      sync.Mutex
	tunnels map[TunnelID]*Tunnel
	routes  map[string]Route
	iface   Interface
	localID [32]byte

	// running maps a tunnel to the cancel func of its forwarding loop, so
	// closing a tunnel actually stops moving packets.
	running map[TunnelID]context.CancelFunc
	// forwarded and delivered count what actually crossed the tunnel. Before
	// this existed there was no way to tell a working tunnel from an idle one.
	forwarded uint64
	delivered uint64
}

func NewServer(localID [32]byte) *Server {
	iface, err := openTUN("tun0")
	if err != nil {
		log.Warn().Err(err).Msg("vpn: could not create TUN interface, running in userspace-only mode")
		iface = nil
	} else {
		if err := iface.Up(); err != nil {
			log.Warn().Err(err).Msg("vpn: failed to bring TUN interface up")
		}
	}
	return NewServerWithInterface(localID, iface)
}

// NewServerWithInterface builds a server around a specific device, or around no
// device at all for a userspace-only node. It performs no I/O, so a test can
// supply a fake device.
func NewServerWithInterface(localID [32]byte, iface Interface) *Server {
	return &Server{
		tunnels: make(map[TunnelID]*Tunnel),
		routes:  make(map[string]Route),
		iface:   iface,
		localID: localID,
		running: make(map[TunnelID]context.CancelFunc),
	}
}

// Attach binds a carrier to a tunnel and starts forwarding in both directions.
//
// Until this existed the server opened a TUN device, brought it up and then
// never read or wrote a packet: the device was created and ignored. Nothing
// crossed the tunnel because there was no loop to cross it.
func (s *Server) Attach(ctx context.Context, peer [32]byte, carrier Carrier) (TunnelID, error) {
	s.mu.Lock()
	iface := s.iface
	s.mu.Unlock()

	if iface == nil {
		return TunnelID{}, errors.New("no tunnel device: this node has no TUN interface")
	}
	if carrier == nil {
		return TunnelID{}, errors.New("no carrier: nothing to forward packets over")
	}

	id, err := s.CreateTunnel(ctx, peer, "")
	if err != nil {
		return TunnelID{}, err
	}

	loopCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	s.mu.Lock()
	s.running[id] = cancel
	s.mu.Unlock()

	go s.forwardOutbound(loopCtx, iface, carrier)
	go s.forwardInbound(loopCtx, iface, carrier)

	return id, nil
}

// forwardOutbound reads IP packets from the device and hands them to the peer.
func (s *Server) forwardOutbound(ctx context.Context, iface Interface, carrier Carrier) {
	buf := make([]byte, DefaultMTU)
	for {
		if ctx.Err() != nil {
			return
		}
		n, err := iface.Read(buf)
		if err != nil {
			// A closed device is the normal way this loop ends.
			return
		}
		if n == 0 {
			continue
		}
		if n > DefaultMTU {
			log.Warn().Int("bytes", n).Msg("vpn: packet larger than the mtu, dropped")
			continue
		}
		packet := make([]byte, n)
		copy(packet, buf[:n])
		if err := carrier.Send(ctx, packet); err != nil {
			if ctx.Err() == nil {
				log.Warn().Err(err).Msg("vpn: could not send a packet to the peer")
			}
			return
		}
		s.mu.Lock()
		s.forwarded++
		s.mu.Unlock()
	}
}

// forwardInbound takes packets from the peer and writes them to the device.
func (s *Server) forwardInbound(ctx context.Context, iface Interface, carrier Carrier) {
	for {
		if ctx.Err() != nil {
			return
		}
		packet, err := carrier.Recv(ctx)
		if err != nil {
			// Previously this returned silently, which made "the peer sent
			// nothing" and "the carrier failed immediately" indistinguishable:
			// a broken stream looked exactly like an idle tunnel.
			if ctx.Err() == nil {
				log.Warn().Err(err).Msg("vpn: inbound carrier stopped")
			}
			return
		}
		if len(packet) == 0 {
			continue
		}
		if _, err := iface.Write(packet); err != nil {
			if ctx.Err() == nil {
				log.Warn().Err(err).Msg("vpn: could not write a packet to the device")
			}
			return
		}
		s.mu.Lock()
		s.delivered++
		s.mu.Unlock()
	}
}

// Stats reports what has actually crossed the tunnels.
func (s *Server) Stats() (forwarded, delivered uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.forwarded, s.delivered
}

// HasDevice reports whether a real tunnel device was opened.
//
// A node without one can still hold tunnel state, but no packet can cross it,
// so this is what the daemon uses to decide whether to claim the service is up.
func (s *Server) HasDevice() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.iface != nil
}

// DeviceName returns the tunnel interface name, or "" when there is none.
func (s *Server) DeviceName() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.iface == nil {
		return ""
	}
	return s.iface.Name()
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

// CloseTunnel stops forwarding and drops the tunnel. The cancel matters: without
// it the forwarding goroutines keep running against a tunnel that no longer
// exists.
func (s *Server) CloseTunnel(id TunnelID) error {
	s.mu.Lock()
	cancel := s.running[id]
	delete(s.running, id)
	delete(s.tunnels, id)
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
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
