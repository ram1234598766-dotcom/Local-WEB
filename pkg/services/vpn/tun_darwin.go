//go:build darwin

package vpn

import (
	"fmt"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

type ifreq struct {
	Name  [unix.IFNAMSIZ]byte
	Flags uint16
	_     [22]byte
}

type sockaddrIn struct {
	Len    uint8
	Family uint8
	Port   uint16
	Addr   [4]byte
	Zero   [8]byte
}

type ifreqAddr struct {
	Name [unix.IFNAMSIZ]byte
	Addr sockaddrIn
	_    [8]byte
}

type darwinTUN struct {
	name string
	fd   int
}

func openTUN(name string) (Interface, error) {
	fd, err := unix.Open(fmt.Sprintf("/dev/%s", name), unix.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("open /dev/tun: %w", err)
	}

	return &darwinTUN{
		name: name,
		fd:   fd,
	}, nil
}

func (t *darwinTUN) Up() error {
	return t.setFlags(unix.IFF_UP | unix.IFF_RUNNING)
}

func (t *darwinTUN) Down() error {
	return t.setFlags(0)
}

func (t *darwinTUN) setFlags(flags uint16) error {
	sock, err := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM, 0)
	if err != nil {
		return fmt.Errorf("socket: %w", err)
	}
	defer unix.Close(sock)

	var req ifreq
	copy(req.Name[:], t.name)
	req.Flags = flags

	if _, _, errno := syscall.Syscall(
		syscall.SYS_IOCTL,
		uintptr(sock),
		uintptr(unix.SIOCSIFFLAGS),
		uintptr(unsafe.Pointer(&req)),
	); errno != 0 {
		return fmt.Errorf("SIOCSIFFLAGS: %v", errno)
	}
	return nil
}

func (t *darwinTUN) Addrs() ([]string, error) {
	sock, err := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM, 0)
	if err != nil {
		return nil, fmt.Errorf("socket: %w", err)
	}
	defer unix.Close(sock)

	var req ifreqAddr
	copy(req.Name[:], t.name)

	if _, _, errno := syscall.Syscall(
		syscall.SYS_IOCTL,
		uintptr(sock),
		uintptr(unix.SIOCGIFADDR),
		uintptr(unsafe.Pointer(&req)),
	); errno != 0 {
		return nil, fmt.Errorf("SIOCGIFADDR: %v", errno)
	}

	ip := fmt.Sprintf("%d.%d.%d.%d", req.Addr.Addr[0], req.Addr.Addr[1], req.Addr.Addr[2], req.Addr.Addr[3])
	return []string{ip}, nil
}

func (t *darwinTUN) AddRoute(dst string, gw string) error {
	return nil
}

func (t *darwinTUN) Name() string                  { return t.name }
func (t *darwinTUN) Read(buf []byte) (int, error)  { return unix.Read(t.fd, buf) }
func (t *darwinTUN) Write(buf []byte) (int, error) { return unix.Write(t.fd, buf) }
func (t *darwinTUN) Close() error                  { return unix.Close(t.fd) }

var _ Interface = (*darwinTUN)(nil)
