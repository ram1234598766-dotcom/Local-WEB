//go:build windows

package vpn

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// vendoredWintunPath finds the DLL committed to the repository, if present.
func vendoredWintunPath(t *testing.T) string {
	t.Helper()
	rel := filepath.Join("..", "..", "..", "installers", "windows", "wintun", "wintun.dll")
	if p, err := filepath.Abs(rel); err == nil {
		if _, statErr := os.Stat(p); statErr == nil {
			return p
		}
	}
	return ""
}

// TestWintunVendoredDLLLoadsAndExports is the test that matters for the binding.
//
// The DLL is vendored in this repository and the Windows installers install it, so
// the question worth asking is not "is Wintun a thing" but "is this DLL really
// Wintun, and are the names this file binds actually in it". A renamed or missing
// export would otherwise surface as a call that silently returns zero, which looks
// like a device failure rather than a binding error.
func TestWintunVendoredDLLLoadsAndExports(t *testing.T) {
	dll := vendoredWintunPath(t)
	if dll == "" {
		t.Skip("no vendored wintun.dll in this checkout")
	}
	t.Setenv("LOCALWEB_WINTUN_DLL", dll)
	resetWintunForTest()

	lib, err := loadWintun()
	require.NoError(t, err, "the vendored wintun.dll should load")
	require.NotNil(t, lib)

	for name, proc := range map[string]interface{ Find() error }{
		"WintunCreateAdapter":           lib.create,
		"WintunOpenAdapter":             lib.open,
		"WintunCloseAdapter":            lib.close,
		"WintunStartSession":            lib.startSess,
		"WintunEndSession":              lib.endSess,
		"WintunReceivePacket":           lib.receive,
		"WintunReleaseReceivePacket":    lib.releaseRx,
		"WintunAllocateSendPacket":      lib.allocSend,
		"WintunSendPacket":              lib.send,
		"WintunGetRunningDriverVersion": lib.getVersion,
	} {
		require.NoErrorf(t, proc.Find(), "the vendored DLL is missing %s", name)
	}
}

// TestWintunLUIDIsWellFormed checks the LUID shape Wintun requires.
//
// A LUID is "{" plus 32 hex digits plus "}". A malformed one is rejected by the
// driver with an opaque error, so the shape is asserted where it is generated.
func TestWintunLUIDIsWellFormed(t *testing.T) {
	luid := wintunLUID("tun0")
	require.True(t, wintunLUIDIsWellFormed(luid), "generated LUID %q is malformed", luid)
	require.Len(t, luid, 34)
	require.Equal(t, byte('{'), luid[0])
	require.Equal(t, byte('}'), luid[33])

	// Stable, or a restart would leave a new adapter behind.
	require.Equal(t, luid, wintunLUID("tun0"))
	// Distinct, or two interfaces would share one device.
	require.NotEqual(t, luid, wintunLUID("tun1"))

	require.False(t, wintunLUIDIsWellFormed(""))
	require.False(t, wintunLUIDIsWellFormed("{short}"))
	require.False(t, wintunLUIDIsWellFormed(strings.Repeat("z", 34)))
}

// TestWintunMissingDLLIsReportedHonestly covers a machine with no driver.
//
// The message has to name the fix. "no TUN implementation" sends someone looking
// for a privilege they do not need; naming the installer and the override variable
// is actionable.
func TestWintunMissingDLLIsReportedHonestly(t *testing.T) {
	t.Setenv("LOCALWEB_WINTUN_DLL", filepath.Join(t.TempDir(), "absent.dll"))
	resetWintunForTest()

	_, err := wintunDriverVersion()
	require.Error(t, err)
	require.ErrorIs(t, err, ErrNoTUNPlatform)

	msg := err.Error()
	require.Contains(t, msg, "LOCALWEB_WINTUN_DLL",
		"the message must say how to point at the DLL, since that is the escape hatch")
	require.Contains(t, strings.ToLower(msg), "administrator",
		"the message must name the actual remedy: install the driver as administrator")

	iface, err := openTUN("tun0")
	require.Error(t, err)
	require.Nil(t, iface, "a failed open must not return a usable device")
}

// TestWintunSearchPathCoversKnownInstallLocations checks the loader looks where
// the installers actually put the DLL.
func TestWintunSearchPathCoversKnownInstallLocations(t *testing.T) {
	joined := strings.Join(wintunSearchPaths(), "|")
	require.Contains(t, joined, filepath.Join("System32", "drivers", "wintun.dll"),
		"the path the installer uses must be searched")
	require.Contains(t, joined, filepath.Join("installers", "windows", "wintun", "wintun.dll"),
		"the vendored copy must be searched so a developer can try it")

	t.Setenv("LOCALWEB_WINTUN_DLL", `C:\explicit\wintun.dll`)
	require.Equal(t, `C:\explicit\wintun.dll`, wintunSearchPaths()[0],
		"the explicit override must win")
}

// TestOpenTUNNamesTheActualBlocker is the honesty test for this platform.
//
// Windows does have a TUN implementation and a vendored driver, so "unsupported on
// this platform" is no longer true and would be a lie. What is true is that each
// step has a specific blocker, and the error has to name the one that applies.
func TestOpenTUNNamesTheActualBlocker(t *testing.T) {
	dll := vendoredWintunPath(t)
	if dll == "" {
		t.Skip("no vendored wintun.dll in this checkout")
	}
	t.Setenv("LOCALWEB_WINTUN_DLL", dll)
	resetWintunForTest()

	lib, err := loadWintun()
	require.NoError(t, err)
	require.NotNil(t, lib)

	// This call must return rather than crash. WintunCreateAdapter faults inside
	// the driver loader when it cannot load the driver, which took the node down
	// with 0xC0000005; the elevation check exists to prevent exactly that.
	iface, err := openTUN("tun0-localweb-test")
	require.Error(t, err)
	require.Nil(t, iface, "no device may be returned while the send path is gated off")
	require.ErrorIs(t, err, ErrNoTUNPlatform)

	msg := err.Error()
	require.NotContains(t, msg, "no TUN/TAP implementation for windows",
		"Windows does have a TUN implementation, so that claim would be false")

	// Whichever blocker applies, it must be one of the two specific ones rather
	// than a generic refusal, and neither may mention a TUN implementation that
	// does not exist.
	if !isElevated() {
		require.ErrorIs(t, err, errNotElevated,
			"an unelevated process must be told elevation is the blocker")
	} else {
		require.ErrorIs(t, err, errVetUnsafeBuffer,
			"an elevated process must be told the gate is the blocker, not its setup")
		require.Contains(t, msg, "go vet",
			"the message must name what is blocking it so it can be decided rather than guessed at")
	}
}

// TestParseCIDRv4 checks the parsing that any future route or address call depends
// on, so it is correct before it is used rather than after.
func TestParseCIDRv4(t *testing.T) {
	cases := []struct{ cidr, ip, mask string }{
		{"10.6.0.1/24", "10.6.0.1", "255.255.255.0"},
		{"192.168.1.1/16", "192.168.1.1", "255.255.0.0"},
		{"0.0.0.0/0", "0.0.0.0", "0.0.0.0"},
		{"10.0.0.1/32", "10.0.0.1", "255.255.255.255"},
		{"10.0.0.1/8", "10.0.0.1", "255.0.0.0"},
	}
	for _, tc := range cases {
		ip, mask, err := parseCIDRv4(tc.cidr)
		require.NoErrorf(t, err, "%s", tc.cidr)
		require.Equal(t, tc.ip, ip, tc.cidr)
		require.Equal(t, tc.mask, mask, tc.cidr)
	}

	for _, bad := range []string{
		"10.6.0.1",       // no prefix
		"10.6.0.1/33",    // prefix out of range
		"10.6.0/24",      // three octets
		"10.6.0.1.1/24",  // five octets
		"10.6.0.x/24",    // not numeric
		"10.6.0.1/abc",   // non-numeric prefix
		"10.6.0.1000/24", // octet too long
	} {
		_, _, err := parseCIDRv4(bad)
		require.Errorf(t, err, "%q should be rejected", bad)
	}
}
