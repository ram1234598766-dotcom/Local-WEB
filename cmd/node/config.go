package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

// nodeConfig is the on-disk configuration the daemon accepts.
//
// It exists because the installers write a config file and the daemon ignored it.
// A file that looks like configuration but changes nothing is worse than no file:
// an operator sets `dht.bootstrap`, sees it echoed back by the installer, and
// concludes the node joined a network when it did not.
//
// Only settings the daemon can actually honour are modelled. The installer also
// writes security, plugin and link-tuning blocks; those are reported as inert by
// unmappedKeys rather than parsed and ignored, so an operator learns which of their
// settings do nothing.
type nodeConfig struct {
	// Node holds the settings that map onto the daemon's own flags.
	Node struct {
		Name    string `json:"name"`
		Listen  string `json:"listen"`
		DataDir string `json:"data_dir"`
		Storage string `json:"storage"`
	} `json:"node"`

	Discovery struct {
		// Accepted and inert: no flag exposes these.
		MDNS       *bool `json:"mdns"`
		BLE        *bool `json:"ble"`
		Rendezvous struct {
			URL          string `json:"url"`
			Register     *bool  `json:"register"`
			PollInterval string `json:"poll_interval"`
		} `json:"rendezvous"`
	} `json:"discovery"`

	DHT struct {
		Bootstrap []string `json:"bootstrap"`
		// ReplicationFactor is accepted so the file parses, but no flag exposes it.
		ReplicationFactor int `json:"replication_factor"`
	} `json:"dht"`

	GUI struct {
		// Accepted and inert: the dashboard reads its own theme.
		Theme string `json:"theme"`
		// Enabled has no flag behind it: the dashboard always starts.
		Enabled *bool  `json:"enabled"`
		Listen  string `json:"listen"`
	} `json:"gui"`

	QoS struct {
		// Accepted and inert: only the on/off switch maps to a flag.
		DefaultClass string `json:"default_class"`
		Classes      int    `json:"classes"`
		Enabled      *bool  `json:"enabled"`
	} `json:"qos"`

	// The blocks below are accepted and ignored.
	//
	// They are modelled as raw JSON rather than left out, because DisallowUnknownFields
	// is what makes a file written for another version fail loudly instead of being
	// half-read. Dropping them would make the daemon reject the very file its own
	// installer writes.
	Transport json.RawMessage `json:"transport"`
	Links     json.RawMessage `json:"links"`
	Security  json.RawMessage `json:"security"`
	Plugins   json.RawMessage `json:"plugins"`
}

// nodeConfigPaths lists where a config file is looked for, in order.
//
// The data directory comes first because that is where `cli init` writes one and
// where a developer expects it. /etc/localweb is where the Linux installers write
// theirs, so a packaged node still picks up the file its installer produced.
func nodeConfigPaths(dataDir string) []string {
	var paths []string
	if dataDir != "" {
		paths = append(paths, dataDir+string(os.PathSeparator)+"config.json")
	}
	if dir := os.Getenv("LOCALWEB_ETC"); dir != "" {
		paths = append(paths, dir+string(os.PathSeparator)+"config.json")
	}
	return append(paths, "/etc/localweb/config.json")
}

// loadNodeConfig reads the first config file that exists.
//
// A missing file is not an error: a default node has none, and requiring one would
// break first-run. A file that exists but is malformed IS an error, because silently
// ignoring it would reproduce the exact failure this fixes.
func loadNodeConfig(dataDir string) (*nodeConfig, string, error) {
	for _, p := range nodeConfigPaths(dataDir) {
		raw, err := os.ReadFile(p)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, p, fmt.Errorf("read %s: %w", p, err)
		}
		cfg := &nodeConfig{}
		dec := json.NewDecoder(strings.NewReader(string(raw)))
		dec.DisallowUnknownFields()
		if err := dec.Decode(cfg); err != nil {
			// An unknown field means the file was written for a different version.
			// That is worth saying, because ignoring it is how a setting silently
			// stops working.
			return nil, p, fmt.Errorf("parse %s: %w", p, err)
		}
		return cfg, p, nil
	}
	return nil, "", nil
}

// applyNodeConfig turns a config file into flag values, for settings the caller did
// not pass explicitly.
//
// explicit holds the flag names that appeared on the command line. A flag always
// beats the file, which is the precedence an operator expects: the file is the
// machine's defaults, the command line is this run's intent.
func applyNodeConfig(cfg *nodeConfig, explicit map[string]bool) error {
	if cfg == nil {
		return nil
	}

	set := func(flagName, value string) error {
		if value == "" || explicit[flagName] {
			return nil
		}
		if err := setFlag(flagName, value); err != nil {
			return err
		}
		applied = append(applied, flagName+"="+value)
		return nil
	}

	if err := set("addr", cfg.Node.Listen); err != nil {
		return err
	}
	if err := set("gui-addr", normaliseListen(cfg.GUI.Listen)); err != nil {
		return err
	}
	if err := set("name", cfg.Node.Name); err != nil {
		return err
	}
	if err := set("rendezvous", cfg.Discovery.Rendezvous.URL); err != nil {
		return err
	}
	if cfg.Discovery.Rendezvous.Register != nil {
		if err := set("rendezvous-register", boolString(*cfg.Discovery.Rendezvous.Register)); err != nil {
			return err
		}
	}
	if d := strings.TrimSpace(cfg.Discovery.Rendezvous.PollInterval); d != "" {
		if err := set("rendezvous-poll", d); err != nil {
			return err
		}
	}
	if len(cfg.DHT.Bootstrap) > 0 {
		if err := set("dht-bootstrap", strings.Join(cfg.DHT.Bootstrap, ",")); err != nil {
			return err
		}
	}
	if cfg.QoS.Enabled != nil {
		if err := set("qos", boolString(*cfg.QoS.Enabled)); err != nil {
			return err
		}
	}
	return nil
}

// setFlag assigns a flag value after parsing, which is how a config file becomes
// the default for a flag that was not given explicitly.
var setFlag = func(name, value string) error {
	if err := flag.Set(name, value); err != nil {
		return fmt.Errorf("config: setting -%s=%q: %w", name, value, err)
	}
	return nil
}

// applied records what the config file contributed, for the startup log.
var applied []string

// normaliseListen turns "localhost:8080" into "127.0.0.1:8080".
//
// The installers write localhost, and a loopback bind is the default anyway, so
// accepting both spellings keeps the file's meaning the same whether it names
// localhost or the address it resolves to.
func normaliseListen(listen string) string {
	listen = strings.TrimSpace(listen)
	if listen == "" {
		return ""
	}
	if listen == "localhost:8080" {
		return "127.0.0.1:8080"
	}
	return listen
}

func boolString(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}

// unmappedKeys lists the settings the installer writes that this daemon does not
// honour.
//
// Reporting them is the point. The installer writes security, plugin, link and
// QoS-tuning blocks; none of them reach any flag. An operator who reads the file and
// assumes it works is worse off than one who is told.
func unmappedKeys(raw []byte) []string {
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil
	}
	honoured := map[string]bool{
		"node":      true,
		"discovery": true,
		"dht":       true,
		"gui":       true,
		"qos":       true,
	}
	var out []string
	for k := range doc {
		if !honoured[k] {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// durationOr validates a duration string from a config file.
func durationOr(s string, fallback time.Duration) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil {
		return fallback
	}
	return d
}
