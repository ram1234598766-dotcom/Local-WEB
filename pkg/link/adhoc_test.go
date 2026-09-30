package link

import "testing"

func TestAdHocConstants(t *testing.T) {
	// The SSID and channel are the on-air contract for IBSS: two nodes only
	// meet if both agree, so a silent change breaks interop.
	if adhocSSID != "LocalWEB" {
		t.Errorf("adhocSSID = %q, want LocalWEB", adhocSSID)
	}
	if adhocChannel != 6 {
		t.Errorf("adhocChannel = %d, want 6", adhocChannel)
	}
	if adhocSecurity != "none" {
		t.Errorf("adhocSecurity = %q, want none", adhocSecurity)
	}
}

func TestAdHocWiFiSurfaceValues(t *testing.T) {
	a := &AdHocWiFi{}

	if got := a.Name(); got != "ad-hoc-wifi" {
		t.Errorf("Name() = %q, want ad-hoc-wifi", got)
	}
	if got := a.Mode(); got != ModeAdHocWiFi {
		t.Errorf("Mode() = %s, want ad-hoc-wifi", got)
	}
	if !a.RequiresWiFi() {
		t.Error("RequiresWiFi() = false, want true")
	}
	if a.RequiresRouter() {
		t.Error("RequiresRouter() = true, want false (IBSS needs no router)")
	}
	if got := a.Bandwidth(); got != 54 {
		t.Errorf("Bandwidth() = %d, want 54 (802.11g)", got)
	}
	if got := a.MaxPeers(); got != 20 {
		t.Errorf("MaxPeers() = %d, want 20", got)
	}
	if err := a.Advertise(PeerInfo{}); err != nil {
		t.Errorf("Advertise: %v", err)
	}
}

func TestAdHocWiFiLeaveNetworkIsANoOpWhenNotJoined(t *testing.T) {
	// leaveNetwork returns before touching any platform backend, so this is
	// safe to exercise on every OS.
	a := &AdHocWiFi{iface: "no-such-iface-xyz", ssid: adhocSSID, channel: adhocChannel}

	a.leaveNetwork()
	if a.joined {
		t.Error("leaveNetwork() set joined = true on a link that never joined")
	}

	if err := a.Stop(); err != nil {
		t.Errorf("Stop: %v", err)
	}
	if a.joined {
		t.Error("Stop() left joined = true on a link that never joined")
	}
}

func TestNewAdHocWiFi(t *testing.T) {
	a, err := NewAdHocWiFi()
	if err != nil {
		if a != nil {
			t.Error("NewAdHocWiFi returned a link alongside an error")
		}
		return
	}

	if a.iface == "" {
		t.Error("iface is empty on a successful constructor")
	}
	if a.ssid != adhocSSID {
		t.Errorf("ssid = %q, want %q", a.ssid, adhocSSID)
	}
	if a.channel != adhocChannel {
		t.Errorf("channel = %d, want %d", a.channel, adhocChannel)
	}
	if a.joined {
		t.Error("joined = true before Discover or joinNetwork was called")
	}
	if a.localIP != nil {
		t.Errorf("localIP = %v, want nil before joining", a.localIP)
	}
}

func TestContains(t *testing.T) {
	// contains gates the `iw phy` and `netsh wlan show drivers` output checks,
	// so a false positive would let IBSS capability detection pass on a driver
	// that does not support it.
	tests := []struct {
		name   string
		s      string
		substr string
		want   bool
	}{
		{"middle", "abcdef", "cde", true},
		{"prefix", "abcdef", "abc", true},
		{"suffix", "abcdef", "def", true},
		{"whole string", "abcdef", "abcdef", true},
		{"not present", "abcdef", "xyz", false},
		{"needle longer than haystack", "abcdef", "abcdefg", false},
		{"both empty", "", "", true},
		{"empty needle", "abc", "", true},
		{"empty haystack", "", "a", false},
		{"real IBSS marker", "Supported interface modes:\n * IBSS\n * managed\n", "* IBSS", true},
		{"absent IBSS marker", "Supported interface modes:\n * managed\n", "* IBSS", false},
		{"hostednetwork marker absent", "Host network supported : Yes", " hostednetwork supported", false},
		{"hostednetwork marker present", "driver supports\n hostednetwork supported : No\n", " hostednetwork supported", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := contains(tt.s, tt.substr); got != tt.want {
				t.Errorf("contains(%q, %q) = %v, want %v", tt.s, tt.substr, got, tt.want)
			}
		})
	}
}

func TestContainsSubstring(t *testing.T) {
	tests := []struct {
		name   string
		s      string
		substr string
		want   bool
	}{
		{"middle", "abcdef", "cde", true},
		{"prefix", "abcdef", "abc", true},
		{"suffix", "abcdef", "def", true},
		{"not present", "abcdef", "xyz", false},
		{"needle longer than haystack", "abc", "abcd", false},
		{"both empty", "", "", true},
		{"empty needle", "abc", "", true},
		{"empty haystack", "", "a", false},
		{"repeated", "aaaaa", "aa", true},
		{"overlapping", "abab", "bab", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := containsSubstring(tt.s, tt.substr); got != tt.want {
				t.Errorf("containsSubstring(%q, %q) = %v, want %v", tt.s, tt.substr, got, tt.want)
			}
		})
	}
}
