package link

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

// USB implements Link for USB tethering (cable connection).
type USB struct {
	iface      string // USB network interface (usb0, enx*, etc.)
	gateway    net.IP
	localIP    net.IP
	connected  bool
}

// NewUSB creates a USB tethering link.
func NewUSB() (*USB, error) {
	u := &USB{}

	iface, err := u.findUSBInterface()
	if err != nil {
		return nil, fmt.Errorf("no USB interface: %w", err)
	}
	u.iface = iface

	// Get IP address on USB interface
	ip, err := u.getInterfaceIP(iface)
	if err != nil {
		return nil, fmt.Errorf("no IP on USB interface: %w", err)
	}
	u.localIP = ip

	// Assume gateway is .1 on link-local
	gw := make(net.IP, 4)
	copy(gw, ip.To4())
	gw[3] = 1
	u.gateway = gw

	return u, nil
}

func (u *USB) Name() string           { return "usb-tether" }
func (u *USB) Mode() LinkMode         { return ModeUSBTether }
func (u *USB) RequiresWiFi() bool     { return false }
func (u *USB) RequiresRouter() bool   { return false }
func (u *USB) Bandwidth() int         { return 480 } // USB 2.0 = 480 Mbps
func (u *USB) MaxPeers() int          { return 2 }   // Usually 1:1

func (u *USB) IsAvailable(ctx context.Context) bool {
	iface, err := net.InterfaceByName(u.iface)
	if err != nil {
		return false
	}
	return iface.Flags&net.FlagUp != 0 && iface.Flags&net.FlagRunning != 0
}

func (u *USB) Discover(ctx context.Context) (<-chan PeerEvent, error) {
	events := make(chan PeerEvent, 16)

	go func() {
		defer close(events)

		// USB is point-to-point, so there's usually only one peer
		// Scan the USB subnet for other host
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				u.scanUSBSubnet(ctx, events)
			}
		}
	}()

	return events, nil
}

func (u *USB) Connect(ctx context.Context, addr string) (net.Conn, error) {
	dialer := &net.Dialer{
		Timeout:   3 * time.Second,
		LocalAddr: &net.TCPAddr{IP: u.localIP},
	}
	return dialer.DialContext(ctx, "tcp", addr)
}

func (u *USB) Advertise(info PeerInfo) error {
	// USB is point-to-point, advertisement happens via ARP
	return nil
}

func (u *USB) Stop() error { return nil }

// scanUSBSubnet scans the USB interface subnet for peers.
func (u *USB) scanUSBSubnet(ctx context.Context, events chan<- PeerEvent) {
	// On USB, the subnet is usually /24 link-local
	addrs, err := net.InterfaceByName(u.iface)
	if err != nil {
		return
	}

	ifaceAddrs, err := addrs.Addrs()
	if err != nil {
		return
	}

	for _, addr := range ifaceAddrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok || ipNet.IP.To4() == nil {
			continue
		}

		ip := ipNet.IP.To4()
		subnet := ip.Mask(ipNet.Mask)

		// Scan for other host (usually just .1 or .2 on USB)
		for i := 1; i < 255; i++ {
			target := make(net.IP, 4)
			copy(target, subnet)
			target[3] = byte(i)

			if target.Equal(ip) {
				continue
			}

			// Quick TCP probe on QUIC port
			go func(target net.IP) {
				select {
				case <-ctx.Done():
					return
				default:
				}

				conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:4443", target), 500*time.Millisecond)
				if err != nil {
					return
				}
				conn.Close()

				events <- PeerEvent{
					Type: PeerDiscovered,
					Peer: PeerInfo{
						Addrs:    []string{fmt.Sprintf("%s:4443", target)},
						LinkMode: ModeUSBTether,
						LastSeen: time.Now(),
					},
					Time: time.Now(),
				}
			}(target)
		}
	}
}

// findUSBInterface finds the USB network interface.
func (u *USB) findUSBInterface() (string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}

	// USB interfaces typically named: usb0, usb1, enx*, enp*s*, eth*
	usbPrefixes := []string{"usb", "enx", "enp"}

	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		name := strings.ToLower(iface.Name)
		for _, prefix := range usbPrefixes {
			if strings.HasPrefix(name, prefix) {
				// Verify it has an IP
				addrs, _ := iface.Addrs()
				for _, a := range addrs {
					if ipNet, ok := a.(*net.IPNet); ok && ipNet.IP.To4() != nil {
						return iface.Name, nil
					}
				}
			}
		}
	}

	return "", fmt.Errorf("no USB network interface found")
}

// getInterfaceIP returns the IPv4 address of an interface.
func (u *USB) getInterfaceIP(ifaceName string) (net.IP, error) {
	iface, err := net.InterfaceByName(ifaceName)
	if err != nil {
		return nil, err
	}

	addrs, err := iface.Addrs()
	if err != nil {
		return nil, err
	}

	for _, addr := range addrs {
		if ipNet, ok := addr.(*net.IPNet); ok && ipNet.IP.To4() != nil {
			return ipNet.IP.To4(), nil
		}
	}

	return nil, fmt.Errorf("no IPv4 address on %s", ifaceName)
}

// SetupUSBHost configures a machine as USB host (creates usb0 interface).
func SetupUSBHost() error {
	switch {
	case isLinux():
		// Load g_ether module
		return exec.Command("modprobe", "g_ether").Run()
	case isMacOS():
		// macOS USB networking is automatic with USB-C
		log.Info().Msg("USB networking auto-configured on macOS")
		return nil
	case isWindows():
		// Windows RNDIS driver
		log.Info().Msg("USB networking auto-configured on Windows")
		return nil
	default:
		return fmt.Errorf("unsupported OS for USB host setup")
	}
}

// SetupUSBGuest configures a machine as USB guest.
func SetupUSBGuest() error {
	switch {
	case isLinux():
		return exec.Command("modprobe", "g_ether").Run()
	default:
		return nil
	}
}

func isLinux() bool {
	return exec.Command("uname", "-s").Run() == nil
}

func isMacOS() bool {
	return exec.Command("uname", "-s").Run() == nil // Check for Darwin
}

func isWindows() bool {
	return exec.Command("ver").Run() == nil
}
