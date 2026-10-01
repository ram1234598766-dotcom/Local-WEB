package gui

import (
	"testing"
)

// ServiceHealth used to hardcode all nine protocol services to false, so the
// dashboard reported a uniform "everything down" no matter what the daemon did.
// These tests pin the real contract: unset services are false, started ones are
// true, and every known service name is always present.

func TestServiceHealthAllServicesFalseByDefault(t *testing.T) {
	api := NewAPI([32]byte{1})

	health := api.ServiceHealth()

	if len(health) != len(serviceNames)+5 {
		t.Fatalf("health should have %d keys, got %d", len(serviceNames)+5, len(health))
	}
	for _, name := range serviceNames {
		val, ok := health[name]
		if !ok {
			t.Errorf("service %q missing from health map", name)
			continue
		}
		if val {
			t.Errorf("service %q reported running before it was started", name)
		}
	}
	if !health["gui"] {
		t.Error("gui should always report healthy")
	}
}

func TestSetServiceRunningReflectedInHealth(t *testing.T) {
	api := NewAPI([32]byte{1})

	api.SetServiceRunning("dns", true)
	api.SetServiceRunning("files", true)

	health := api.ServiceHealth()
	if !health["dns"] {
		t.Error("dns should report running after SetServiceRunning")
	}
	if !health["files"] {
		t.Error("files should report running after SetServiceRunning")
	}
	if health["registry"] {
		t.Error("registry should stay false when only dns and files were started")
	}
}

func TestSetServiceRunningCanReportStop(t *testing.T) {
	api := NewAPI([32]byte{1})

	api.SetServiceRunning("vpn", true)
	if !api.ServiceHealth()["vpn"] {
		t.Fatal("vpn should report running")
	}

	api.SetServiceRunning("vpn", false)
	if api.ServiceHealth()["vpn"] {
		t.Error("vpn should report stopped after SetServiceRunning(false)")
	}
}

func TestSetServiceLiveRejectsUnknownName(t *testing.T) {
	api := NewAPI([32]byte{1})

	// A typo would otherwise create a health key the GUI never reads, silently
	// reporting a service that does not exist.
	defer func() {
		if recover() == nil {
			t.Error("expected SetServiceLive to panic on an unknown service name")
		}
	}()
	api.SetServiceLive(true, "dnsx")
}

func TestSetServiceLiveAcceptsKnownNames(t *testing.T) {
	api := NewAPI([32]byte{1})

	for _, name := range serviceNames {
		api.SetServiceLive(true, name)
	}

	health := api.ServiceHealth()
	for _, name := range serviceNames {
		if !health[name] {
			t.Errorf("service %q should report running", name)
		}
	}
}

// HTTPSites returned two invented rows even with no gateway running, which is
// how the dashboard ended up listing sites that did not exist.
func TestHTTPSitesEmptyWithoutGateway(t *testing.T) {
	api := NewAPI([32]byte{1})

	sites, err := api.HTTPSites()
	if err != nil {
		t.Fatalf("HTTPSites: %v", err)
	}
	if len(sites) != 0 {
		t.Errorf("expected no sites before the gateway is registered, got %d", len(sites))
	}
}

func TestSetHTTPSitesReported(t *testing.T) {
	api := NewAPI([32]byte{1})

	want := []HTTPSiteResponse{{Name: "gui.localweb", Status: "active", Routes: 3}}
	api.SetHTTPSites(want)

	sites, err := api.HTTPSites()
	if err != nil {
		t.Fatalf("HTTPSites: %v", err)
	}
	if len(sites) != 1 {
		t.Fatalf("expected 1 site, got %d", len(sites))
	}
	if sites[0].Name != "gui.localweb" || sites[0].Routes != 3 {
		t.Errorf("unexpected site reported: %+v", sites[0])
	}
}
