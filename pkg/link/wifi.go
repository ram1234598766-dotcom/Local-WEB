package link

import (
	"context"
	"fmt"
	"net"
	"time"
)

// WiFiStation implements Link for standard WiFi (connected to router).
type WiFiStation struct {
	iface    *net.Interface
	gateway  net.IP
	ssid     string
	channel  int
}

// NewWiFiStation creates a WiFi station link from an active WiFi interface.
func NewWiFiStation() (*WiFiStation, error) {
	iface, err := findWiFiInterface()
	if err != nil {
		return nil, fmt.Errorf("no wifi interface: %w", err)
	}

	gw, err := detectGateway(iface)
	if err != nil {
		return nil, fmt.Errorf("no gateway: %w", err)
	}

	return &WiFiStation{
		iface:   iface,
		gateway: gw,
	}, nil
}

func (w *WiFiStation) Name() string           { return "wifi-station" }
func (w *WiFiStation) Mode() LinkMode         { return ModeWiFiStation }
func (w *WiFiStation) RequiresWiFi() bool     { return true }
func (w *WiFiStation) RequiresRouter() bool   { return true }
func (w *WiFiStation) Bandwidth() int         { return 1000 } // Up to 1 Gbps
func (w *WiFiStation) MaxPeers() int          { return 1000 }

func (w *WiFiStation) IsAvailable(ctx context.Context) bool {
	if w.iface == nil {
		return false
	}
	// Check interface is still up
	iface, err := net.InterfaceByName(w.iface.Name)
	if err != nil {
		return false
	}
	return iface.Flags&net.FlagUp != 0 && iface.Flags&net.FlagRunning != 0
}

func (w *WiFiStation) Discover(ctx context.Context) (<-chan PeerEvent, error) {
	events := make(chan PeerEvent, 16)

	go func() {
		defer close(events)

		// WiFi station discovery uses mDNS + ARP
		// mDNS is handled by the discovery layer
		// Here we just watch for ARP table changes

		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		// Initial ARP scan of local subnet
		scanSubnet(ctx, w.iface, events)

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				scanSubnet(ctx, w.iface, events)
			}
		}
	}()

	return events, nil
}

func (w *WiFiStation) Connect(ctx context.Context, addr string) (net.Conn, error) {
	// Direct TCP connection (QUIC will handle encryption)
	dialer := &net.Dialer{
		Timeout: 5 * time.Second,
	}
	return dialer.DialContext(ctx, "tcp", addr)
}

func (w *WiFiStation) Advertise(info PeerInfo) error {
	// mDNS advertisement is handled by the discovery layer
	return nil
}

func (w *WiFiStation) Stop() error { return nil }

// scanSubnet sends ARP requests to all IPs in the subnet.
func scanSubnet(ctx context.Context, iface *net.Interface, events chan<- PeerEvent) {
	addrs, err := iface.Addrs()
	if err != nil {
		return
	}

	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok || ipNet.IP.To4() == nil {
			continue
		}

		// Calculate subnet range
		ip := ipNet.IP.To4()
		mask := ipNet.Mask
		subnet := ip.Mask(mask)

		// Scan all hosts in /24 (or smaller)
		for i := 1; i < 255; i++ {
			target := make(net.IP, 4)
			copy(target, subnet)
			target[3] = byte(i)

			if target.Equal(ip) {
				continue // Skip self
			}

			// Try QUIC on port 4443
			go func(target net.IP) {
				select {
				case <-ctx.Done():
					return
				default:
				}

				conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:4443", target), 200*time.Millisecond)
				if err != nil {
					return
				}
				conn.Close()

				events <- PeerEvent{
					Type: PeerDiscovered,
					Peer: PeerInfo{
						Addrs:    []string{fmt.Sprintf("%s:4443", target)},
						LinkMode: ModeWiFiStation,
						LastSeen: time.Now(),
					},
					Time: time.Now(),
				}
			}(target)
		}
	}
}

// findWiFiInterface finds the active WiFi network interface.
func findWiFiInterface() (*net.Interface, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}

	for _, iface := range ifaces {
		// Skip down interfaces
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		// Skip loopback
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		// Skip point-to-point (VPN, etc.)
		if iface.Flags&net.FlagPointToPoint != 0 {
			continue
		}
		// Must have multicast for mDNS
		if iface.Flags&net.FlagMulticast == 0 {
			continue
		}
		// Check for IPv4 address
		addrs, _ := iface.Addrs()
		for _, a := range addrs {
			if ipNet, ok := a.(*net.IPNet); ok && ipNet.IP.To4() != nil && !ipNet.IP.IsLoopback() {
				return &iface, nil
			}
		}
	}

	return nil, fmt.Errorf("no suitable WiFi interface found")
}

// detectGateway finds the default gateway for an interface.
func detectGateway(iface *net.Interface) (net.IP, error) {
	// Parse routing table to find default gateway
	// On Linux: parse /proc/net/route
	// On macOS: parse netstat -rn
	// On Windows: parse route print
	// Simplified: return first non-loopback gateway
	addrs, err := iface.Addrs()
	if err != nil {
		return nil, err
	}

	for _, addr := range addrs {
		if ipNet, ok := addr.(*net.IPNet); ok && ipNet.IP.To4() != nil {
			// Assume gateway is .1 in the subnet
			ip := ipNet.IP.To4()
			gw := make(net.IP, 4)
			copy(gw, ip)
			gw[3] = 1
			return gw, nil
		}
	}

	return nil, fmt.Errorf("no gateway found")
}
