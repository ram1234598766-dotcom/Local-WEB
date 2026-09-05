//go:build linux

package vpn

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/unix"
)

// linuxTUN implements Interface using /dev/net/tun.
type linuxTUN struct {
	name string
	fd   int
}

// openTUN creates a TUN device on Linux via /dev/net/tun.
func openTUN(name string) (Interface, error) {
	fd, err := unix.Open("/dev/net/tun", unix.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("open /dev/net/tun: %w", err)
	}

	var req unix.Ifreq
	copy(req.Ifr_name[:], name)

	// IFF_TUN = layer 3 tunnel, IFF_NO_PI = no packet info header
	*(*uint16)(unsafe.Pointer(&req.Ifr_flags)) = unix.IFF_TUN | unix.IFF_NO_PI

	if _, _, errno := unix.Syscall(
		unix.SYS_IOCTL,
		uintptr(fd),
		uintptr(unix.TUNSETIFF),
		uintptr(unsafe.Pointer(&req)),
	); errno != 0 {
		unix.Close(fd)
		return nil, fmt.Errorf("TUNSETIFF ioctl: %w", errno)
	}

	actualName := trimCStr(req.Ifr_name[:])
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

	var req unix.Ifreq
	copy(req.Ifr_name[:], t.name)

	*(*uint16)(unsafe.Pointer(&req.Ifr_flags)) = flags

	if _, _, errno := unix.Syscall(
		unix.SYS_IOCTL,
		uintptr(sock),
		uintptr(unix.SIOCSIFFLAGS),
		uintptr(unsafe.Pointer(&req)),
	); errno != 0 {
		return fmt.Errorf("SIOCSIFFLAGS ioctl: %w", errno)
	}
	return nil
}

func (t *linuxTUN) Addrs() ([]string, error) {
	sock, err := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM, 0)
	if err != nil {
		return nil, fmt.Errorf("socket: %w", err)
	}
	defer unix.Close(sock)

	var req unix.Ifreq
	copy(req.Ifr_name[:], t.name)

	if _, _, errno := unix.Syscall(
		unix.SYS_IOCTL,
		uintptr(sock),
		uintptr(unix.SIOCGIFADDR),
		uintptr(unsafe.Pointer(&req)),
	); errno != 0 {
		return nil, fmt.Errorf("SIOCGIFADDR ioctl: %w", errno)
	}

	addr := (*unix.RawSockaddrInet4)(unsafe.Pointer(&req.Ifr_addr))
	ip := fmt.Sprintf("%d.%d.%d.%d", addr.Addr[0], addr.Addr[1], addr.Addr[2], addr.Addr[3])
	return []string{ip}, nil
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

	var req unix.Ifreq
	copy(req.Ifr_name[:], t.name)

	addr := (*unix.RawSockaddrInet4)(unsafe.Pointer(&req.Ifr_addr))
	addr.Family = unix.AF_INET
	addr.Addr = *parsed

	if _, _, errno := unix.Syscall(
		unix.SYS_IOCTL,
		uintptr(sock),
		uintptr(unix.SIOCSIFDSTADDR),
		uintptr(unsafe.Pointer(&req)),
	); errno != 0 {
		return fmt.Errorf("SIOCSIFDSTADDR ioctl: %w", errno)
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
