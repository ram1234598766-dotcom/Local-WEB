//go:build !linux && !darwin

package vpn

import (
	"errors"
	"fmt"
)

// stubTUN is a no-op TUN implementation for platforms that lack native TUN.
type stubTUN struct {
	name string
}

func openTUN(name string) (Interface, error) {
	return nil, errors.New("TUN/TAP interface not supported on this platform")
}

func (t *stubTUN) Name() string {
	return t.name
}

func (t *stubTUN) Up() error {
	return fmt.Errorf("no-op: %s not implemented on this platform", t.name)
}

func (t *stubTUN) Down() error {
	return fmt.Errorf("no-op: %s not implemented on this platform", t.name)
}

func (t *stubTUN) Addrs() ([]string, error) {
	return nil, fmt.Errorf("no-op: %s not implemented on this platform", t.name)
}

func (t *stubTUN) AddRoute(dst string, gw string) error {
	return fmt.Errorf("no-op: add route on this platform")
}

func (t *stubTUN) Close() error {
	return fmt.Errorf("no-op: close on this platform")
}

func (t *stubTUN) Read(buf []byte) (int, error) {
	return 0, fmt.Errorf("no-op: read not supported on this platform")
}

func (t *stubTUN) Write(buf []byte) (int, error) {
	return 0, fmt.Errorf("no-op: write not supported on this platform")
}

var _ Interface = (*stubTUN)(nil)
