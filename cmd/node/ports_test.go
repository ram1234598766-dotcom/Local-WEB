package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The ports a node listens on have been wrong in three places at once: the
// daemon's flag defaults, the Linux installers' firewall rules, and the README
// service table. At one point the installers opened 8080, which the dashboard
// binds to loopback, while leaving every real service port closed, and the README
// claimed nine distinct ports for services that multiplex onto one QUIC socket.
//
// These tests read the files rather than asserting against a Go constant: the
// point is to catch an edit to any one of them that did not reach the others.

var (
	listenFlagRe = regexp.MustCompile(`flag\.String\("([a-z0-9-]+)",\s*(?:defaultGUIAddr|"([^"]*)")`)
	guiAddrRe    = regexp.MustCompile(`const defaultGUIAddr = "([^"]+)"`)
	udpPortsRe   = regexp.MustCompile(`LOCALWEB_UDP_PORTS="([^"]*)"`)
	tcpPortsRe   = regexp.MustCompile(`LOCALWEB_TCP_PORTS="([^"]*)"`)
)

func readMain(t *testing.T) string {
	t.Helper()
	src, err := os.ReadFile("main.go")
	require.NoError(t, err)
	return string(src)
}

// daemonListenPorts returns each flag whose default is a host:port the daemon
// binds, keyed by flag name. dns-port is an int flag and is not in this set.
func daemonListenPorts(t *testing.T) map[string]string {
	t.Helper()
	src := readMain(t)

	guiDefault := guiAddrRe.FindStringSubmatch(src)
	require.NotNil(t, guiDefault,
		"defaultGUIAddr not found; the dashboard default is no longer pinned by a test")

	out := map[string]string{}
	for _, m := range listenFlagRe.FindAllStringSubmatch(src, -1) {
		addr := m[2]
		if m[1] == "gui-addr" {
			addr = guiDefault[1]
		}
		if !strings.Contains(addr, ":") {
			continue
		}
		out[m[1]] = addr
	}
	return out
}

func hostOf(addr string) string {
	host, _, _ := strings.Cut(addr, ":")
	return host
}

func portPart(addr string) string {
	_, port, _ := strings.Cut(addr, ":")
	return port
}

func isLoopback(addr string) bool {
	return strings.HasPrefix(hostOf(addr), "127.")
}

// listenProto maps a flag to the protocol its default binds. Guessing tcp for all
// of them was wrong: the main QUIC transport and mDNS are UDP, and a test that
// asserts the wrong protocol is worse than no test.
var listenProto = map[string]string{
	"addr":          "udp", // QUIC transport
	"dns-port":      "udp", // mDNS
	"http-addr":     "tcp",
	"smtp-addr":     "tcp",
	"imap-addr":     "tcp",
	"registry-addr": "tcp",
	"dht-addr":      "tcp",
	"gui-addr":      "tcp",
}

// TestDaemonListenPortsMatchDocs checks every routable listener appears in the
// README as the right protocol, so the table cannot drift from the flags.
func TestDaemonListenPortsMatchDocs(t *testing.T) {
	readme, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	require.NoError(t, err)
	text := string(readme)

	listens := daemonListenPorts(t)
	require.NotEmpty(t, listens, "no host:port listen defaults found; the regex needs updating")

	for flag, addr := range listens {
		if isLoopback(addr) {
			continue
		}
		proto, known := listenProto[flag]
		require.Truef(t, known,
			"%s binds %s but the test does not know its protocol; add it to listenProto", flag, addr)
		require.Containsf(t, text, "`"+portPart(addr)+"/"+proto+"`",
			"%s binds %s but the README does not document %s/%s", flag, addr, portPart(addr), proto)
	}

	// The dashboard is loopback, so the README must not imply it is reachable.
	require.True(t, isLoopback(listens["gui-addr"]),
		"the dashboard must bind loopback by default, got %s", listens["gui-addr"])
}

// TestInstallersFirewallPortsMatchDaemon keeps the Linux installers opening what
// the node actually listens on.
//
// The installers used to spell the list out separately in each of the three
// firewall backends, which is how 8080 came to be opened while 8082, 587, 993,
// 9092 and 9094 were all missed. They now declare it once per file.
func TestInstallersFirewallPortsMatchDaemon(t *testing.T) {
	scripts, err := filepath.Glob(filepath.Join("..", "..", "installers", "linux", "*.sh"))
	require.NoError(t, err)
	require.NotEmpty(t, scripts)

	listens := daemonListenPorts(t)

	// Every routable listener the daemon opens, which the installers must open.
	want := map[string]bool{"5353": true} // dns-port is an int flag
	for _, addr := range listens {
		if !isLoopback(addr) {
			want[portPart(addr)] = true
		}
	}
	require.NotEmpty(t, want)

	var checked int
	for _, s := range scripts {
		src, err := os.ReadFile(s)
		require.NoError(t, err)
		text := string(src)

		udp := udpPortsRe.FindStringSubmatch(text)
		tcp := tcpPortsRe.FindStringSubmatch(text)
		if udp == nil && tcp == nil {
			continue // this script does not manage the firewall
		}
		checked++

		opened := map[string]bool{}
		for _, group := range []*regexp.Regexp{udpPortsRe, tcpPortsRe} {
			m := group.FindStringSubmatch(text)
			if m == nil {
				continue
			}
			for _, p := range strings.Fields(m[1]) {
				opened[p] = true
			}
		}

		name := filepath.Base(s)
		for port := range want {
			require.Truef(t, opened[port],
				"%s does not open %s, which the daemon listens on by default", name, port)
		}
		// The dashboard must not be opened: it is unauthenticated and can write
		// files and restore backups, so it binds loopback by default.
		require.Falsef(t, opened["8080"],
			"%s opens 8080/tcp, but the dashboard binds loopback and is unauthenticated", name)
	}
	require.NotZero(t, checked, "no installer script declared a firewall port list")
}
