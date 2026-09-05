//go:build linux

package vpn

import (
	"fmt"

	"golang.org/x/sys/unix"
)

type linuxTUN struct {
	name string
	fd   int
}

func openTUN(name string) (Interface, error) {
	fd, err := unix.Open("/dev/net/tun", unix.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("open /dev/net/tun: %w", err)
	}

	req, err := unix.NewIfreq(name)
	if err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("create ifreq: %w", err)
	}

	// IFF_TUN = layer 3 tunnel, IFF_NO_PI = no packet info header
	req.SetUint16(unix.IFF_TUN | unix.IFF_NO_PI)

	if err := unix.IoctlIfreq(fd, unix.TUNSETIFF, req); err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("TUNSETIFF ioctl: %w", err)
	}

	actualName := req.Name()
	if actualName == "" {
		actualName = name
	}

	return &linuxTUN{name: actualName, fd: fd}, nil
}

func (t *linuxTUN) Up() error {
	return t.setFlags(unix.IFF_UP | unix.IFF_RUNNING)
}

func (t *linuxTUN) Down() error {
	return t.setFlags(0)
}

func (t *linuxTUN) setFlags(flags uint16) error {
	sock, err := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM, 0)
	if err != nil {
		return fmt.Errorf("socket: %w", err)
	}
	defer unix.Close(sock)

	req, err := unix.NewIfreq(t.name)
	if err != nil {
		return fmt.Errorf("create ifreq: %w", err)
	}
	req.SetUint16(flags)

	if err := unix.IoctlIfreq(sock, unix.SIOCSIFFLAGS, req); err != nil {
		return fmt.Errorf("SIOCSIFFLAGS ioctl: %w", err)
	}
	return nil
}

func (t *linuxTUN) Addrs() ([]string, error) {
	sock, err := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM, 0)
	if err != nil {
		return nil, fmt.Errorf("socket: %w", err)
	}
	defer unix.Close(sock)

	req, err := unix.NewIfreq(t.name)
	if err != nil {
		return nil, fmt.Errorf("create ifreq: %w", err)
	}

	if err := unix.IoctlIfreq(sock, unix.SIOCGIFADDR, req); err != nil {
		return nil, fmt.Errorf("SIOCGIFADDR ioctl: %w", err)
	}

	addr, err := req.Inet4Addr()
	if err != nil {
		return nil, fmt.Errorf("get inet4 addr: %w", err)
	}

	if len(addr) == 4 {
		ip := fmt.Sprintf("%d.%d.%d.%d", addr[0], addr[1], addr[2], addr[3])
		return []string{ip}, nil
	}
	return nil, nil
}

func (t *linuxTUN) AddRoute(dst string, gw string) error {
	sock, err := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM, 0)
	if err != nil {
		return fmt.Errorf("socket: %w", err)
	}
	defer unix.Close(sock)

	parsed := parseIPv4(gw)
	if parsed == nil {
		return fmt.Errorf("invalid gateway address: %s", gw)
	}

	req, err := unix.NewIfreq(t.name)
	if err != nil {
		return fmt.Errorf("create ifreq: %w", err)
	}
	if err := req.SetInet4Addr(parsed[:]); err != nil {
		return fmt.Errorf("set inet4 addr: %w", err)
	}

	if err := unix.IoctlIfreq(sock, unix.SIOCSIFDSTADDR, req); err != nil {
		return fmt.Errorf("SIOCSIFDSTADDR ioctl: %w", err)
	}
	return nil
}

func (t *linuxTUN) Name() string {
	return t.name
}

func (t *linuxTUN) Read(buf []byte) (int, error) {
	return unix.Read(t.fd, buf)
}

func (t *linuxTUN) Write(buf []byte) (int, error) {
	return unix.Write(t.fd, buf)
}

func (t *linuxTUN) Close() error {
	return unix.Close(t.fd)
}

func parseIPv4(s string) *[4]byte {
	parts := splitIP(s)
	if len(parts) != 4 {
		return nil
	}
	var ip [4]byte
	for i, p := range parts {
		v := 0
		for _, c := range p {
			if c < '0' || c > '9' {
				return nil
			}
			v = v*10 + int(c-'0')
		}
		if v > 255 {
			return nil
		}
		ip[i] = byte(v)
	}
	return &ip
}

func splitIP(s string) []string {
	var parts []string
	start := 0
	for i, c := range s {
		if c == '.' {
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	parts = append(parts, s[start:])
	return parts
}

func trimCStr(b []byte) string {
	for i, c := range b {
		if c == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}

var _ Interface = (*linuxTUN)(nil)
