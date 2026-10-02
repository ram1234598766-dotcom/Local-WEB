//go:build !linux && !darwin && !windows

package vpn

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestStubTUNNeverSucceeds is the honest-failure test for a platform with no TUN
// implementation.
//
// The stub used to return a bare "not supported on this platform" from openTUN and
// a differently worded "no-op: ..." from every method. The daemon reported the
// missing device as "needs root or CAP_NET_ADMIN", which is wrong here: there is
// no privilege that would make a TUN appear on this platform, because the code to
// open one does not exist. That sent an operator looking at capabilities instead of
// at the actual blocker.
func TestStubTUNNeverSucceeds(t *testing.T) {
	iface, err := openTUN("tun0")
	require.Error(t, err, "a platform with no TUN must report that, not return a device")
	require.Nil(t, iface)
	require.ErrorIs(t, err, ErrNoTUNPlatform)
	require.Contains(t, err.Error(), "no TUN/TAP implementation",
		"the error must name the real blocker, not imply a permissions problem")

	tun := &stubTUN{name: "tun0"}
	require.Equal(t, "tun0", tun.Name())

	// Every operation that would move packets must fail. One that returned nil
	// would let a caller believe the tunnel was working.
	require.ErrorIs(t, tun.Up(), ErrNoTUNPlatform)
	require.ErrorIs(t, tun.Down(), ErrNoTUNPlatform)

	addrs, err := tun.Addrs()
	require.Error(t, err)
	require.Nil(t, addrs)

	require.ErrorIs(t, tun.AddRoute("10.0.0.0/8", "10.0.0.1"), ErrNoTUNPlatform)

	n, err := tun.Read(make([]byte, 1500))
	require.ErrorIs(t, err, ErrNoTUNPlatform)
	require.Zero(t, n)

	n, err = tun.Write(make([]byte, 1500))
	require.ErrorIs(t, err, ErrNoTUNPlatform)
	require.Zero(t, n)
}

// TestServerReportsWhyThereIsNoDevice checks the reason survives to the caller
// instead of being logged once inside NewServer and lost. Without it the daemon
// can only guess, which is how it came to say "needs root" on a platform where
// running as root changes nothing.
func TestServerReportsWhyThereIsNoDevice(t *testing.T) {
	srv := NewServer([32]byte{1})

	require.False(t, srv.HasDevice())
	require.ErrorIs(t, srv.DeviceError(), ErrNoTUNPlatform)
	require.Contains(t, srv.DeviceError().Error(), "vpn carrier works",
		"the carrier is real on this platform, so the message should say so")
	require.Empty(t, srv.DeviceName())
}

// TestServerWithFakeInterfaceHasNoDeviceError keeps DeviceError honest in the
// other direction: with a working device it must report no error rather than the
// stub's complaint.
func TestServerWithFakeInterfaceHasNoDeviceError(t *testing.T) {
	srv := NewServerWithInterface([32]byte{1}, &stubTUN{name: "tun0"})
	require.True(t, srv.HasDevice())
	require.NoError(t, srv.DeviceError())
}

// TestErrNoTUNPlatformIsNotAPermissionError pins the distinction the daemon log
// depends on. If someone rewrites this to mention root or capabilities, a user on
// this platform will chase a privilege that cannot help.
func TestErrNoTUNPlatformIsNotAPermissionError(t *testing.T) {
	msg := ErrNoTUNPlatform.Error()
	for _, misleading := range []string{"root", "CAP_NET_ADMIN", "administrator", "permission"} {
		require.NotContains(t, msg, misleading,
			"the message must not suggest %q, which cannot fix a missing implementation", misleading)
	}
	require.True(t, errors.Is(ErrNoTUNPlatform, ErrNoTUNPlatform))
}
