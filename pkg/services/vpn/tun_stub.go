//go:build !linux && !darwin

package vpn

import (
	"errors"
	"fmt"
)

// ErrNoTUNPlatform reports that this build has no TUN implementation for the
// host platform.
//
// It is a distinct sentinel rather than a bare error string because the failure
// modes are not interchangeable and reporting the wrong one sends an operator
// down the wrong path. On Linux and macOS an open failure usually means missing
// CAP_NET_ADMIN, which the operator fixes by granting a capability or running
// as root. On Windows the blocker is different in kind: there is no TUN
// implementation at all, because Wintun ships as a driver that has to be
// installed and bound to with cgo. Reinstalling as administrator, or granting a
// capability, does not help there.
var ErrNoTUNPlatform = errors.New(
	"this build has no TUN/TAP implementation for " + tunPlatformName +
		"; the vpn carrier works but cannot open a device")

// tunPlatformName is filled in by each platform's stub. It exists so the error
// names the platform instead of saying "this platform".
const tunPlatformName = "windows and other non-linux/darwin hosts"

// stubTUN is a no-op TUN implementation for platforms that lack native TUN.
//
// Every method other than Name fails. Nothing here reports success, so a caller
// cannot mistake this for a working device: vpn.Service only claims the tunnel is
// live when openTUN returned a real interface.
type stubTUN struct {
	name string
}

func openTUN(name string) (Interface, error) {
	return nil, ErrNoTUNPlatform
}

func (t *stubTUN) Name() string {
	return t.name
}

func (t *stubTUN) Up() error {
	return ErrNoTUNPlatform
}

func (t *stubTUN) Down() error {
	return ErrNoTUNPlatform
}

func (t *stubTUN) Addrs() ([]string, error) {
	return nil, ErrNoTUNPlatform
}

func (t *stubTUN) AddRoute(dst string, gw string) error {
	return fmt.Errorf("%w: cannot add a route", ErrNoTUNPlatform)
}

func (t *stubTUN) Close() error {
	return nil
}

func (t *stubTUN) Read(buf []byte) (int, error) {
	return 0, ErrNoTUNPlatform
}

func (t *stubTUN) Write(buf []byte) (int, error) {
	return 0, ErrNoTUNPlatform
}

var _ Interface = (*stubTUN)(nil)
