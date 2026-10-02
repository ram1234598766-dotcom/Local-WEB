package main

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// installerConfig is the shape the Linux installers write. It is copied from
// installers/linux/postinstall.sh rather than invented, so this test fails if the
// installer's file and the daemon's parser drift apart.
const installerConfig = `{
  "node": {
    "name": "packaged-node",
    "listen": "0.0.0.0:4443",
    "data_dir": "/var/lib/localweb",
    "storage": "/var/lib/localweb/data"
  },
  "transport": { "quic": { "max_idle_timeout": "30s", "keep_alive": "10s" }, "hybrid_pq": false, "zero_rtt": true, "datagrams": true },
  "links": { "enabled": ["wifi", "ble"], "multipath": { "policy": "weighted_bw", "max_paths": 3 } },
  "discovery": {
    "mdns": true,
    "rendezvous": { "url": "https://rendezvous.example.net", "register": true, "poll_interval": "90s" }
  },
  "dht": { "bootstrap": ["10.0.0.5:9094"], "replication_factor": 10 },
  "security": { "audit_log_max_size": "100MB", "pow_difficulty_target": "1s", "capability_ttl": "24h" },
  "qos": { "enabled": true, "default_class": "best_effort", "classes": 9 },
  "gui": { "enabled": false, "listen": "localhost:8080", "theme": "system" },
  "plugins": { "enabled": true, "directory": "/var/lib/localweb/plugins", "allow_unsafe": false }
}`

// The installer's real file, copied from installers/linux/postinstall.sh, so this
// fails if the installer and the daemon's parser drift apart.

// registerConfigFlags declares the flags applyNodeConfig writes to, on a private
// FlagSet so the test does not disturb the daemon's own flags.
func registerConfigFlags(t *testing.T) *flag.FlagSet {
	t.Helper()
	fs := flag.NewFlagSet("cfg", flag.ContinueOnError)
	fs.String("name", "", "")
	fs.String("addr", "0.0.0.0:4443", "")
	fs.String("http-addr", "0.0.0.0:8082", "")
	fs.String("smtp-addr", "0.0.0.0:587", "")
	fs.String("imap-addr", "0.0.0.0:993", "")
	fs.String("registry-addr", "0.0.0.0:9092", "")
	fs.String("dns-port", "5353", "")
	fs.String("gui-addr", "127.0.0.1:8080", "")
	fs.String("rendezvous", "", "")
	fs.Bool("rendezvous-register", true, "")
	fs.Duration("rendezvous-poll", 60*time.Second, "")
	fs.String("dht-bootstrap", "", "")
	fs.Bool("qos", true, "")
	return fs
}

// TestInstallerConfigIsHonoured is the test for the bug: the installers write a
// config file and the daemon ignored it, so a setting like dht.bootstrap appeared
// to be honoured because the installer echoed it back.
func TestInstallerConfigIsHonoured(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	require.NoError(t, os.WriteFile(path, []byte(installerConfig), 0o600))

	cfg, found, err := loadNodeConfig(dir)
	require.NoError(t, err)
	require.NotNil(t, cfg, "the installer's config file must be found")
	require.Equal(t, path, found)

	require.Equal(t, []string{"10.0.0.5:9094"}, cfg.DHT.Bootstrap)
	require.Equal(t, "https://rendezvous.example.net", cfg.Discovery.Rendezvous.URL)
	require.Equal(t, "0.0.0.0:4443", cfg.Node.Listen, "listen lives under node, not transport")

	// localhost is the spelling the installer writes; it has to mean the same thing
	// as the address it resolves to.
	require.Equal(t, "127.0.0.1:8080", normaliseListen(cfg.GUI.Listen))
}

// TestConfigDoesNotOverrideExplicitFlags is the precedence rule: the file is the
// machine's defaults, the command line is this run's intent.
func TestConfigDoesNotOverrideExplicitFlags(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.json"),
		[]byte(installerConfig), 0o600))

	cfg, _, err := loadNodeConfig(dir)
	require.NoError(t, err)

	fs := registerConfigFlags(t)
	// The operator asked for a different port on the command line.
	require.NoError(t, fs.Parse([]string{"-http-addr", "0.0.0.0:9999", "-dht-bootstrap", "seed.example:9094"}))

	explicit := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { explicit[f.Name] = true })
	require.True(t, explicit["http-addr"])
	require.True(t, explicit["dht-bootstrap"])

	setFlag = func(name, value string) error {
		return fs.Set(name, value)
	}
	applied = nil
	require.NoError(t, applyNodeConfig(cfg, explicit))

	require.Equal(t, "0.0.0.0:9999", fs.Lookup("http-addr").Value.String(),
		"an explicit flag must beat the config file")
	require.Equal(t, "seed.example:9094", fs.Lookup("dht-bootstrap").Value.String(),
		"an explicit flag must beat the config file")

	// A flag that was not given still comes from the file.
	require.Equal(t, "https://rendezvous.example.net", fs.Lookup("rendezvous").Value.String())
	require.Equal(t, 90*time.Second, fs.Lookup("rendezvous-poll").Value.(flag.Getter).Get(),
		"rendezvous-poll should come from the config file's 90s")
}

// TestMissingConfigIsNotAnError checks a default node, which has no config file,
// still starts.
func TestMissingConfigIsNotAnError(t *testing.T) {
	cfg, found, err := loadNodeConfig(t.TempDir())
	require.NoError(t, err)
	require.Nil(t, cfg)
	require.Empty(t, found)

	// And applying nothing is a no-op rather than a panic.
	applied = nil
	require.NoError(t, applyNodeConfig(nil, nil))
	require.Empty(t, applied)
}

// TestMalformedConfigFailsLoudly is the important half of the behaviour: a file
// that exists but cannot be understood must not be silently skipped, because running
// on defaults the operator did not ask for is exactly what this change exists to
// prevent.
func TestMalformedConfigFailsLoudly(t *testing.T) {
	cases := []struct{ name, body string }{
		{"not json", `not json at all`},
		{"truncated", `{"transport": {"listen": `},
		{"wrong type for a port", `{"services": {"dns_port": "not a number"}}`},
		{"wrong type for bootstrap", `{"dht": {"bootstrap": "one-string-not-a-list"}}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "config.json"),
				[]byte(tc.body), 0o600))
			_, _, err := loadNodeConfig(dir)
			require.Error(t, err, "a config file that cannot be parsed must be an error")
			require.Contains(t, err.Error(), "config.json")
		})
	}
}

// TestUnknownConfigKeysAreReported checks a file written for a different version is
// called out rather than half-read.
func TestUnknownConfigKeysAreReported(t *testing.T) {
	dir := t.TempDir()
	body := `{"transport": {"listen": "0.0.0.0:4443"}, "future_feature": {"x": 1}}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.json"), []byte(body), 0o600))

	_, _, err := loadNodeConfig(dir)
	require.Error(t, err, "an unrecognised key means the file is for another version")
	require.Contains(t, err.Error(), "future_feature")
}

// TestUnmappedKeysAreListed names what the installer writes that does nothing, so
// an operator is not left believing a setting took effect.
func TestUnmappedKeysAreListed(t *testing.T) {
	keys := unmappedKeys([]byte(installerConfig))
	require.Contains(t, keys, "security",
		"the installer writes a security block that reaches no flag")
	require.Contains(t, keys, "plugins")
	require.NotContains(t, keys, "dht", "dht is honoured, so it must not be listed as inert")
	require.NotContains(t, keys, "node")
	require.NotContains(t, keys, "gui")

	// Sorted, so the startup log is stable between runs.
	for i := 1; i < len(keys); i++ {
		require.Less(t, keys[i-1], keys[i], "inert keys must be sorted")
	}
}

// TestNormaliseListen checks the two spellings of loopback mean the same thing.
func TestNormaliseListen(t *testing.T) {
	require.Equal(t, "127.0.0.1:8080", normaliseListen("localhost:8080"))
	require.Equal(t, "127.0.0.1:8080", normaliseListen("  127.0.0.1:8080 "))
	require.Equal(t, "0.0.0.0:9092", normaliseListen("0.0.0.0:9092"))
	require.Empty(t, normaliseListen(""))
	require.Empty(t, normaliseListen("   "))
}

// TestConfigJSONShapeMatchesTheInstaller is a guard against the two drifting: if the
// installer changes what it writes, this fails rather than the daemon quietly
// reading less.
func TestConfigJSONShapeMatchesTheInstaller(t *testing.T) {
	installer, err := filepath.Abs(filepath.Join("..", "..", "installers", "linux", "postinstall.sh"))
	require.NoError(t, err)
	raw, err := os.ReadFile(installer)
	require.NoError(t, err)

	// Pull the heredoc out of the script. Locating it by brace counting would pick
	// up shell braces as well as JSON.
	text := string(raw)
	marker := "config.json << 'EOF'"
	idx := strings.Index(text, marker)
	require.Positive(t, idx, "expected a heredoc in the installer")
	rest := text[idx:]
	bodyStart := strings.Index(rest, "\n")
	require.Positive(t, bodyStart)
	bodyEnd := strings.Index(rest[bodyStart:], "\nEOF")
	require.Positive(t, bodyEnd, "expected the heredoc to be closed")

	// Trimmed because a stray carriage return before the opening brace would make it
	// invalid JSON.
	block := strings.TrimSpace(rest[bodyStart+1 : bodyStart+bodyEnd])

	var doc map[string]any
	require.NoError(t, json.Unmarshal([]byte(block), &doc),
		"the installer's config block must be valid JSON")

	for _, key := range []string{"node", "transport", "links", "discovery", "dht", "security", "qos", "gui", "plugins"} {
		require.Containsf(t, doc, key, "the installer writes a %q block", key)
	}

	// Every block the installer writes is either honoured or reported inert. A block
	// that is neither would be a setting that silently does nothing.
	honoured := map[string]bool{
		"node": true, "discovery": true, "dht": true, "gui": true, "qos": true,
	}
	inert := unmappedKeys([]byte(block))
	for _, k := range inert {
		require.Falsef(t, honoured[k], "%q is reported inert but is actually honoured", k)
	}
}
