package link

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// WiFiDirect implements Link for WiFi Direct (P2P, no router needed).
type WiFiDirect struct {
	mu          sync.Mutex
	iface       string             // Network interface name
	groupOwner  bool               // Are we the Group Owner?
	groupSSID   string             // Current group SSID
	groupPasswd string             // WPS password
	peers       map[string]*WDPeer // MAC → peer info
	wpaCtrlPath string             // Path to wpa_cli control socket
	ctx         context.Context
	cancel      context.CancelFunc
}

// WDPeer holds WiFi Direct peer information.
type WDPeer struct {
	MAC        net.HardwareAddr
	DeviceName string
	Interfaces []string // P2P interface addresses
	Connected  bool
	LastSeen   time.Time
}

// NewWiFiDirect creates a WiFi Direct link.
func NewWiFiDirect() (*WiFiDirect, error) {
	ctx, cancel := context.WithCancel(context.Background())

	iface, err := findWiFiInterface()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("no wifi interface for P2P: %w", err)
	}

	w := &WiFiDirect{
		iface:       iface.Name,
		peers:       make(map[string]*WDPeer),
		wpaCtrlPath: findWPACtrlPath(),
		ctx:         ctx,
		cancel:      cancel,
	}

	return w, nil
}

func (w *WiFiDirect) Name() string         { return "wifi-direct" }
func (w *WiFiDirect) Mode() LinkMode       { return ModeWiFiDirect }
func (w *WiFiDirect) RequiresWiFi() bool   { return true }
func (w *WiFiDirect) RequiresRouter() bool { return false }
func (w *WiFiDirect) Bandwidth() int       { return 250 } // Up to 250 Mbps
func (w *WiFiDirect) MaxPeers() int        { return 10 }

func (w *WiFiDirect) IsAvailable(ctx context.Context) bool {
	// Check if WiFi Direct is supported
	switch runtime.GOOS {
	case "linux":
		return w.checkLinuxP2P()
	case "darwin":
		return false // macOS has limited P2P support
	case "windows":
		return w.checkWindowsP2P()
	default:
		return false
	}
}

func (w *WiFiDirect) Discover(ctx context.Context) (<-chan PeerEvent, error) {
	events := make(chan PeerEvent, 16)

	go func() {
		defer close(events)

		// Start P2P discovery
		if err := w.startDiscovery(); err != nil {
			log.Error().Err(err).Msg("wifi direct discovery failed")
			return
		}

		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				w.stopDiscovery()
				return
			case <-ticker.C:
				peers := w.scanPeers()
				for _, peer := range peers {
					events <- PeerEvent{
						Type: PeerDiscovered,
						Peer: PeerInfo{
							Name:     peer.DeviceName,
							Addrs:    peer.Interfaces,
							LinkMode: ModeWiFiDirect,
							LastSeen: peer.LastSeen,
						},
						Time: time.Now(),
					}
				}
			}
		}
	}()

	return events, nil
}

func (w *WiFiDirect) Connect(ctx context.Context, addr string) (net.Conn, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	// If not group owner, need to connect to GO
	if !w.groupOwner {
		if err := w.connectToGroup(addr); err != nil {
			return nil, fmt.Errorf("connect to group: %w", err)
		}
	}

	// Wait for P2P interface to get IP
	time.Sleep(500 * time.Millisecond)

	// Find the P2P interface IP
	p2pIP, err := w.getP2PIP()
	if err != nil {
		return nil, fmt.Errorf("get P2P IP: %w", err)
	}

	// Connect via P2P interface
	dialer := &net.Dialer{
		Timeout:   5 * time.Second,
		LocalAddr: &net.TCPAddr{IP: p2pIP},
	}
	return dialer.DialContext(ctx, "tcp", addr)
}

func (w *WiFiDirect) Advertise(info PeerInfo) error {
	// WiFi Direct advertisement via P2P service discovery
	return w.advertiseService(info)
}

func (w *WiFiDirect) Stop() error {
	w.cancel()
	w.stopDiscovery()
	return nil
}

// startDiscovery initiates WiFi Direct P2P discovery.
func (w *WiFiDirect) startDiscovery() error {
	switch runtime.GOOS {
	case "linux":
		return w.startLinuxDiscovery()
	case "windows":
		return w.startWindowsDiscovery()
	default:
		return fmt.Errorf("unsupported OS for WiFi Direct: %s", runtime.GOOS)
	}
}

// stopDiscovery stops WiFi Direct P2P discovery.
func (w *WiFiDirect) stopDiscovery() {
	switch runtime.GOOS {
	case "linux":
		w.stopLinuxDiscovery()
	case "windows":
		w.stopWindowsDiscovery()
	}
}

// scanPeers returns currently visible WiFi Direct peers.
func (w *WiFiDirect) scanPeers() []*WDPeer {
	w.mu.Lock()
	defer w.mu.Unlock()

	var result []*WDPeer
	for _, p := range w.peers {
		if time.Since(p.LastSeen) < 30*time.Second {
			result = append(result, p)
		}
	}
	return result
}

// connectToGroup connects to a WiFi Direct Group Owner.
func (w *WiFiDirect) connectToGroup(goAddr string) error {
	switch runtime.GOOS {
	case "linux":
		return w.wpaCmd(fmt.Sprintf("P2P_CONNECT %s pbc", goAddr))
	case "windows":
		return fmt.Errorf("windows WiFi Direct connect not yet implemented")
	default:
		return fmt.Errorf("unsupported OS")
	}
}

// getP2PIP returns the IP address of the P2P interface.
func (w *WiFiDirect) getP2PIP() (net.IP, error) {
	// Look for p2p-wlan0-0 or similar interface
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}

	for _, iface := range ifaces {
		if strings.Contains(iface.Name, "p2p") || strings.Contains(iface.Name, "wp2p") {
			addrs, err := iface.Addrs()
			if err != nil {
				continue
			}
			for _, addr := range addrs {
				if ipNet, ok := addr.(*net.IPNet); ok && ipNet.IP.To4() != nil {
					return ipNet.IP.To4(), nil
				}
			}
		}
	}

	return nil, fmt.Errorf("no P2P interface found")
}

// advertiseService advertises via WiFi Direct service discovery.
func (w *WiFiDirect) advertiseService(info PeerInfo) error {
	// Use wpa_cli to set service discovery data
	svcData := fmt.Sprintf("localweb://%s:%d", info.Name, 4443)
	return w.wpaCmd(fmt.Sprintf("P2P_SERVICE_ADDBonjour _localweb._tcp %s", svcData))
}

// --- Linux Implementation ---

func (w *WiFiDirect) checkLinuxP2P() bool {
	// Check if wpa_supplicant supports P2P
	out, err := exec.Command("wpa_cli", "-i", w.iface, "capabilities").Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "p2p")
}

func (w *WiFiDirect) startLinuxDiscovery() error {
	// Enable P2P
	if err := w.wpaCmd("P2P_FIND"); err != nil {
		return fmt.Errorf("P2P_FIND: %w", err)
	}

	// Listen for P2P events
	go w.listenLinuxEvents()
	return nil
}

func (w *WiFiDirect) stopLinuxDiscovery() {
	w.wpaCmd("P2P_STOP_FIND")
}

func (w *WiFiDirect) listenLinuxEvents() {
	// Listen to wpa_supplicant control interface for P2P events
	// Events: P2P-DEVICE-FOUND, P2P-GO-NEG-SUCCESS, P2P-GROUP-STARTED
	for {
		select {
		case <-w.ctx.Done():
			return
		default:
		}

		// Read event from wpa_ctrl socket
		// Simplified: poll for now
		time.Sleep(1 * time.Second)

		// Parse events and update peer list
	}
}

func (w *WiFiDirect) wpaCmd(cmd string) error {
	if w.wpaCtrlPath == "" {
		return fmt.Errorf("wpa_ctrl not available")
	}
	out, err := exec.Command("wpa_cli", "-i", w.iface, cmd).CombinedOutput()
	if err != nil {
		return fmt.Errorf("wpa_cli %s: %w (%s)", cmd, err, string(out))
	}
	return nil
}

// --- Windows Implementation ---

func (w *WiFiDirect) checkWindowsP2P() bool {
	// Check Windows WiFi Direct capability
	out, err := exec.Command("netsh", "wlan", "show", "interfaces").Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "Wi-Fi Direct")
}

func (w *WiFiDirect) startWindowsDiscovery() error {
	// Windows WiFi Direct uses Windows.Devices.WiFiDirect APIs
	// For CLI: use netsh or custom PowerShell
	return fmt.Errorf("windows WiFi Direct discovery not yet implemented")
}

func (w *WiFiDirect) stopWindowsDiscovery() {
	// No-op for now
}

// findWPACtrlPath finds the wpa_supplicant control interface.
func findWPACtrlPath() string {
	// Common paths:
	// /var/run/wpa_supplicant/wlan0
	// /run/wpa_supplicant/wlan0
	paths := []string{
		"/var/run/wpa_supplicant",
		"/run/wpa_supplicant",
	}
	for _, p := range paths {
		if _, err := exec.Command("ls", p).Output(); err == nil {
			return p
		}
	}
	return ""
}
