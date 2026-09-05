package link

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"runtime"
	"time"

	"github.com/rs/zerolog/log"
)

const (
	adhocSSID     = "LocalWEB"
	adhocChannel  = 6 // 2.437 GHz
	adhocSecurity = "none"
)

// AdHocWiFi implements Link for 802.11 ad-hoc (IBSS) mode.
type AdHocWiFi struct {
	iface   string
	localIP net.IP
	ssid    string
	channel int
	joined  bool
}

// NewAdHocWiFi creates an ad-hoc WiFi link.
func NewAdHocWiFi() (*AdHocWiFi, error) {
	iface, err := findWiFiInterface()
	if err != nil {
		return nil, fmt.Errorf("no wifi interface for ad-hoc: %w", err)
	}

	return &AdHocWiFi{
		iface:   iface.Name,
		ssid:    adhocSSID,
		channel: adhocChannel,
	}, nil
}

func (a *AdHocWiFi) Name() string         { return "ad-hoc-wifi" }
func (a *AdHocWiFi) Mode() LinkMode       { return ModeAdHocWiFi }
func (a *AdHocWiFi) RequiresWiFi() bool   { return true }
func (a *AdHocWiFi) RequiresRouter() bool { return false }
func (a *AdHocWiFi) Bandwidth() int       { return 54 } // 802.11g rates
func (a *AdHocWiFi) MaxPeers() int        { return 20 }

func (a *AdHocWiFi) IsAvailable(ctx context.Context) bool {
	// Check if IBSS mode is supported
	switch runtime.GOOS {
	case "linux":
		return a.checkLinuxIBSS()
	case "windows":
		return a.checkWindowsIBSS()
	default:
		return false // macOS deprecated IBSS
	}
}

func (a *AdHocWiFi) Discover(ctx context.Context) (<-chan PeerEvent, error) {
	// Join the ad-hoc network
	if err := a.joinNetwork(); err != nil {
		return nil, fmt.Errorf("join ad-hoc: %w", err)
	}

	events := make(chan PeerEvent, 16)

	go func() {
		defer close(events)

		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				a.leaveNetwork()
				return
			case <-ticker.C:
				a.scanAdHocPeers(ctx, events)
			}
		}
	}()

	return events, nil
}

func (a *AdHocWiFi) Connect(ctx context.Context, addr string) (net.Conn, error) {
	dialer := &net.Dialer{
		Timeout:   5 * time.Second,
		LocalAddr: &net.TCPAddr{IP: a.localIP},
	}
	return dialer.DialContext(ctx, "tcp", addr)
}

func (a *AdHocWiFi) Advertise(info PeerInfo) error {
	// Ad-hoc advertisement happens via ARP
	return nil
}

func (a *AdHocWiFi) Stop() error {
	a.leaveNetwork()
	return nil
}

// joinNetwork joins the LocalWEB ad-hoc network.
func (a *AdHocWiFi) joinNetwork() error {
	if a.joined {
		return nil
	}

	switch runtime.GOOS {
	case "linux":
		return a.joinLinux()
	case "windows":
		return a.joinWindows()
	default:
		return fmt.Errorf("ad-hoc not supported on %s", runtime.GOOS)
	}
}

// leaveNetwork leaves the ad-hoc network.
func (a *AdHocWiFi) leaveNetwork() {
	if !a.joined {
		return
	}

	switch runtime.GOOS {
	case "linux":
		a.leaveLinux()
	case "windows":
		a.leaveWindows()
	}

	a.joined = false
}

// scanAdHocPeers scans for peers on the ad-hoc network.
func (a *AdHocWiFi) scanAdHocPeers(ctx context.Context, events chan<- PeerEvent) {
	// ARP scan of ad-hoc subnet
	// Ad-hoc networks typically use 10.0.0.0/24 or link-local
	addrs, err := net.InterfaceByName(a.iface)
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

		for i := 1; i < 255; i++ {
			target := make(net.IP, 4)
			copy(target, subnet)
			target[3] = byte(i)

			if target.Equal(ip) {
				continue
			}

			go func(target net.IP) {
				select {
				case <-ctx.Done():
					return
				default:
				}

				conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:4443", target), 1*time.Second)
				if err != nil {
					return
				}
				conn.Close()

				events <- PeerEvent{
					Type: PeerDiscovered,
					Peer: PeerInfo{
						Addrs:    []string{fmt.Sprintf("%s:4443", target)},
						LinkMode: ModeAdHocWiFi,
						LastSeen: time.Now(),
					},
					Time: time.Now(),
				}
			}(target)
		}
	}
}

// --- Linux IBSS ---

func (a *AdHocWiFi) checkLinuxIBSS() bool {
	// Check if IBSS mode is supported
	out, err := exec.Command("iw", "phy", "phy0", "info").Output()
	if err != nil {
		return false
	}
	return contains(string(out), "* IBSS")
}

func (a *AdHocWiFi) joinLinux() error {
	// Set interface to managed mode first
	if err := exec.Command("iw", "dev", a.iface, "set", "type", "managed").Run(); err != nil {
		// Might already be in managed mode
		log.Warn().Err(err).Msg("set managed mode")
	}

	// Join IBSS network
	cmd := exec.Command("iw", "dev", a.iface, "ibss", "join", a.ssid, fmt.Sprintf("%d", a.channel))
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("iw ibss join: %w (%s)", err, string(out))
	}

	a.joined = true

	// Assign IP address
	// Use 10.0.0.X where X is random 1-254
	ip := net.IP{10, 0, 0, byte(time.Now().UnixNano()%253 + 1)}
	a.localIP = ip

	// Configure IP on interface
	exec.Command("ip", "addr", "add", fmt.Sprintf("%s/24", ip.String()), "dev", a.iface).Run()
	exec.Command("ip", "link", "set", a.iface, "up").Run()

	log.Info().Str("iface", a.iface).Str("ip", ip.String()).Msg("joined ad-hoc network")
	return nil
}

func (a *AdHocWiFi) leaveLinux() {
	exec.Command("iw", "dev", a.iface, "ibss", "leave").Run()
	exec.Command("ip", "addr", "flush", "dev", a.iface).Run()
	log.Info().Str("iface", a.iface).Msg("left ad-hoc network")
}

// --- Windows IBSS ---

func (a *AdHocWiFi) checkWindowsIBSS() bool {
	// Check Windows ad-hoc support
	out, err := exec.Command("netsh", "wlan", "show", "drivers").Output()
	if err != nil {
		return false
	}
	return contains(string(out), " hostednetwork supported")
}

func (a *AdHocWiFi) joinWindows() error {
	// Create hosted network
	cmd := exec.Command("netsh", "wlan", "set", "hostednetwork",
		"mode=allow",
		fmt.Sprintf("ssid=%s", a.ssid),
		"key=None",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("set hosted network: %w (%s)", err, string(out))
	}

	// Start hosted network
	if out, err := exec.Command("netsh", "wlan", "start", "hostednetwork").CombinedOutput(); err != nil {
		return fmt.Errorf("start hosted network: %w (%s)", err, string(out))
	}

	a.joined = true

	// Get IP from hosted network interface
	time.Sleep(1 * time.Second)
	a.localIP = net.IP{10, 0, 0, 1}

	log.Info().Str("ssid", a.ssid).Msg("started ad-hoc network (Windows)")
	return nil
}

func (a *AdHocWiFi) leaveWindows() {
	exec.Command("netsh", "wlan", "stop", "hostednetwork").Run()
	log.Info().Msg("stopped ad-hoc network (Windows)")
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
