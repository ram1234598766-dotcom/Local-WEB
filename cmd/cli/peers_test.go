package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// peers used to print "Not connected to a running node." unconditionally, so it
// reported that even when a node was up and serving peers. It now queries the
// running node's API and reports what it actually finds.

func TestFetchPeersReadsRealEndpoint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/peers" {
			t.Errorf("unexpected path %q", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"id": "abc123", "name": "laptop", "score": 0.75, "source": "mdns"},
			{"id": "def456", "name": "", "score": 0.25, "source": "ble"},
		})
	}))
	defer srv.Close()

	peers, err := fetchPeers(strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatalf("fetchPeers: %v", err)
	}
	if len(peers) != 2 {
		t.Fatalf("expected 2 peers, got %d", len(peers))
	}
	if peers[0].ID != "abc123" || peers[0].Name != "laptop" || peers[0].Source != "mdns" {
		t.Errorf("unexpected first peer: %+v", peers[0])
	}
}

func TestFetchPeersEmptyList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{})
	}))
	defer srv.Close()

	peers, err := fetchPeers(strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatalf("fetchPeers: %v", err)
	}
	if len(peers) != 0 {
		t.Errorf("expected no peers, got %d", len(peers))
	}
}

func TestFetchPeersReportsServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "peer store not initialized", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	if _, err := fetchPeers(strings.TrimPrefix(srv.URL, "http://")); err == nil {
		t.Error("expected an error when the node API fails")
	}
}

func TestFetchPeersRejectsBadAddress(t *testing.T) {
	for _, addr := range []string{"not-a-host-port", "127.0.0.1", ":", "host:"} {
		if _, err := fetchPeers(addr); err == nil {
			t.Errorf("expected %q to be rejected", addr)
		}
	}
}

func TestFetchPeersConnectionRefusedIsAnError(t *testing.T) {
	// Port 1 on loopback is not listening, so this must be an error rather than
	// a silent empty list.
	if _, err := fetchPeers("127.0.0.1:1"); err == nil {
		t.Error("expected a connection error when no node is running")
	}
}

// idCmd read "data-dir" in its Run body and documented it in its Example, but
// the flag was never registered, so `localweb id --data-dir ~/.localweb` - the
// documented example - failed with "unknown flag".
func TestIDCommandRegistersDataDirFlag(t *testing.T) {
	if idCmd.Flags().Lookup("data-dir") == nil {
		t.Error("id command does not register --data-dir, so its own documented example fails")
	}
	if !strings.Contains(idCmd.Example, "--data-dir") {
		t.Error("id command no longer documents --data-dir; keep the example and the flag in step")
	}
}

func TestPeersCommandRegistersAddrFlag(t *testing.T) {
	if peersCmd.Flags().Lookup("addr") == nil {
		t.Error("peers command does not register --addr")
	}
}
