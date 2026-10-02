//go:build !linux && !darwin && !windows

package vpn

import (
	"errors"
	"fmt"
)

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
