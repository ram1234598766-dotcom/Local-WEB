package link

import (
	"testing"
	"time"
)

// LinkMode is an iota enum, so a reordered or inserted constant silently
// changes every persisted mode value. Pin the numbers and the strings.
func TestLinkModeConstantValues(t *testing.T) {
	want := map[LinkMode]int{
		ModeWiFiStation: 0,
		ModeWiFiDirect:  1,
		ModeAdHocWiFi:   2,
		ModeUSBTether:   3,
		ModeBLE:         4,
		ModeAcoustic:    5,
		ModeNone:        6,
	}
	for mode, n := range want {
		if int(mode) != n {
			t.Errorf("%s = %d, want %d", mode, int(mode), n)
		}
	}
}

func TestLinkModeString(t *testing.T) {
	tests := []struct {
		name string
		mode LinkMode
		want string
	}{
		{"ModeWiFiStation", ModeWiFiStation, "wifi-station"},
		{"ModeWiFiDirect", ModeWiFiDirect, "wifi-direct"},
		{"ModeAdHocWiFi", ModeAdHocWiFi, "ad-hoc-wifi"},
		{"ModeUSBTether", ModeUSBTether, "usb-tether"},
		{"ModeBLE", ModeBLE, "ble"},
		{"ModeAcoustic", ModeAcoustic, "acoustic"},
		{"ModeNone", ModeNone, "none"},
		// Anything outside the enum must degrade to "none" rather than
		// returning an empty string that would silently break log scraping.
		{"positive out of range", LinkMode(99), "none"},
		{"negative out of range", LinkMode(-1), "none"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.mode.String(); got != tt.want {
				t.Errorf("LinkMode(%d).String() = %q, want %q", int(tt.mode), got, tt.want)
			}
		})
	}
}

func TestEventTypeConstantValues(t *testing.T) {
	if PeerDiscovered != 0 {
		t.Errorf("PeerDiscovered = %d, want 0", PeerDiscovered)
	}
	if PeerLost != 1 {
		t.Errorf("PeerLost = %d, want 1", PeerLost)
	}
	if PeerUpdated != 2 {
		t.Errorf("PeerUpdated = %d, want 2", PeerUpdated)
	}
}

func TestDefaultLinkConfigs(t *testing.T) {
	cfgs := DefaultLinkConfigs()

	// Only the five modes that have an actual transport are configured.
	// ModeAcoustic and ModeNone deliberately have no implementation, so they
	// are absent: a Link reporting one of those modes gets a zero-value
	// LinkConfig (Enabled == false) and is skipped by Manager.Run.
	if len(cfgs) != 5 {
		t.Fatalf("DefaultLinkConfigs() has %d entries, want 5", len(cfgs))
	}
	for _, mode := range []LinkMode{ModeAcoustic, ModeNone} {
		if _, ok := cfgs[mode]; ok {
			t.Errorf("DefaultLinkConfigs() unexpectedly contains %s", mode)
		}
	}

	for mode, cfg := range cfgs {
		if cfg.Mode != mode {
			t.Errorf("config key %s holds Mode %s", mode, cfg.Mode)
		}
		if !cfg.Enabled {
			t.Errorf("%s default config is disabled", mode)
		}
		if cfg.AdvertiseInterval <= 0 {
			t.Errorf("%s AdvertiseInterval = %v, want > 0", mode, cfg.AdvertiseInterval)
		}
		if cfg.ScanInterval <= 0 {
			t.Errorf("%s ScanInterval = %v, want > 0", mode, cfg.ScanInterval)
		}
		if cfg.ScanInterval > cfg.AdvertiseInterval {
			t.Errorf("%s ScanInterval %v exceeds AdvertiseInterval %v", mode, cfg.ScanInterval, cfg.AdvertiseInterval)
		}
		if cfg.MaxRetries <= 0 {
			t.Errorf("%s MaxRetries = %d, want > 0", mode, cfg.MaxRetries)
		}
		if cfg.Timeout <= 0 {
			t.Errorf("%s Timeout = %v, want > 0", mode, cfg.Timeout)
		}
	}

	// The defaults encode real policy: station mode is slow to churn, BLE is
	// fast but retry-hard, USB gives up after a single attempt.
	assertConfig := func(mode LinkMode, adv, scan time.Duration, retries int, timeout time.Duration) {
		t.Helper()
		cfg, ok := cfgs[mode]
		if !ok {
			t.Fatalf("no config for %s", mode)
		}
		if cfg.AdvertiseInterval != adv {
			t.Errorf("%s AdvertiseInterval = %v, want %v", mode, cfg.AdvertiseInterval, adv)
		}
		if cfg.ScanInterval != scan {
			t.Errorf("%s ScanInterval = %v, want %v", mode, cfg.ScanInterval, scan)
		}
		if cfg.MaxRetries != retries {
			t.Errorf("%s MaxRetries = %d, want %d", mode, cfg.MaxRetries, retries)
		}
		if cfg.Timeout != timeout {
			t.Errorf("%s Timeout = %v, want %v", mode, cfg.Timeout, timeout)
		}
	}
	assertConfig(ModeWiFiStation, 30*time.Second, 10*time.Second, 3, 5*time.Second)
	assertConfig(ModeWiFiDirect, 5*time.Second, 2*time.Second, 3, 10*time.Second)
	assertConfig(ModeAdHocWiFi, 10*time.Second, 5*time.Second, 3, 5*time.Second)
	assertConfig(ModeUSBTether, 5*time.Second, 2*time.Second, 1, 3*time.Second)
	assertConfig(ModeBLE, 1*time.Second, 1*time.Second, 5, 5*time.Second)
}

func TestDefaultLinkConfigsReturnsFreshMap(t *testing.T) {
	first := DefaultLinkConfigs()
	cfg := first[ModeBLE]
	cfg.Enabled = false
	first[ModeBLE] = cfg

	second := DefaultLinkConfigs()
	if !second[ModeBLE].Enabled {
		t.Error("mutating a returned config map leaked into the next call")
	}
}

// Every concrete transport must keep satisfying the Link interface; this is a
// compile-time guard against a signature change in types.go.
func TestLinkImplementations(t *testing.T) {
	var _ Link = (*BLE)(nil)
	var _ Link = (*USB)(nil)
	var _ Link = (*WiFiStation)(nil)
	var _ Link = (*AdHocWiFi)(nil)
	var _ Link = (*WiFiDirect)(nil)
	var _ Link = (*fakeLink)(nil)
}
