package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ram1234598766-dotcom/Local-WEB/pkg/crypto"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "localweb",
	Short: "LocalWEB P2P networking stack",
	Long:  "Real working P2P internet stack. Zero infrastructure required.",
}

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize Local-WEB node (generate identity & config)",
	Long:  initHelpText(),
	Run: func(cmd *cobra.Command, args []string) {
		dataDir, _ := cmd.Flags().GetString("data-dir")
		if err := runInit(bufio.NewReader(os.Stdin), dataDir); err != nil {
			log.Fatalf("init: %v", err)
		}
	},
}

var nodeCmd = &cobra.Command{
	Use:   "node",
	Short: "Start LocalWEB node",
	Example: `  localweb node
  localweb node --name my-laptop --addr 0.0.0.0:4444
  localweb node --data-dir ~/.localweb`,
	Run: func(cmd *cobra.Command, args []string) {
		addr, _ := cmd.Flags().GetString("addr")
		name, _ := cmd.Flags().GetString("name")
		dataDir, _ := cmd.Flags().GetString("data-dir")
		startNode(cmd.Context(), addr, name, dataDir)
	},
}

var idCmd = &cobra.Command{
	Use:   "id",
	Short: "Generate or display node identity",
	Example: `  localweb id
  localweb id --data-dir ~/.localweb
  localweb id --json`,
	Run: func(cmd *cobra.Command, args []string) {
		dataDir, _ := cmd.Flags().GetString("data-dir")
		if dataDir == "" {
			homeDir, _ := os.UserHomeDir()
			dataDir = filepath.Join(homeDir, ".localweb")
		}

		pub, _, err := crypto.LoadOrGenerateIdentity(dataDir)
		if err != nil {
			log.Fatalf("identity: %v", err)
		}
		nodeID := crypto.NodeID(pub)
		if jsonOut, _ := cmd.Flags().GetBool("json"); jsonOut {
			fmt.Printf(`{"node_id": "%x", "public_key": "%x"}`+"\n", nodeID[:8], pub[:])
			return
		}
		fmt.Printf("Node ID: %x\n", nodeID[:8])
		fmt.Printf("Public:  %x\n", pub[:])
		fmt.Printf("Identity stored in %s/identity.json (private key not displayed)\n", dataDir)
	},
}

var peersCmd = &cobra.Command{
	Use:   "peers",
	Short: "List peers discovered by the running node",
	Example: `  localweb peers
  localweb peers --json
  localweb peers --addr 127.0.0.1:8080`,
	Run: func(cmd *cobra.Command, args []string) {
		addr, _ := cmd.Flags().GetString("addr")
		peers, err := fetchPeers(addr)
		jsonOut, _ := cmd.Flags().GetBool("json")

		if jsonOut {
			if err != nil {
				fmt.Printf(`{"peers": [], "connected": false, "error": %q}`+"\n", err.Error())
				return
			}
			out, merr := json.Marshal(peers)
			if merr != nil {
				fmt.Printf(`{"peers": [], "connected": true, "error": %q}`+"\n", merr.Error())
				return
			}
			fmt.Printf(`{"peers": %s, "connected": true}`+"\n", out)
			return
		}

		if err != nil {
			// Previously this printed "Not connected to a running node."
			// unconditionally, which was false whenever a node was up.
			fmt.Printf("Could not reach the node's API at %s: %v\n", addr, err)
			fmt.Println("Is the node running?  Start it with:  localweb node")
			return
		}

		if len(peers) == 0 {
			fmt.Println("No peers yet.")
			fmt.Println("Peers appear automatically on the same network. If you expected one:")
			fmt.Println("  - check both machines are on the same subnet")
			fmt.Println("  - allow the node through the firewall")
			return
		}

		fmt.Printf("%d peer(s):\n", len(peers))
		for _, p := range peers {
			name := p.Name
			if name == "" {
				name = "(unnamed)"
			}
			fmt.Printf("  %s  %s  score=%.2f  %s\n", p.ID, name, p.Score, p.Source)
		}
	},
}

// peerInfo mirrors the fields the GUI's /api/peers endpoint returns that the CLI
// displays.
type peerInfo struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Addrs   []string `json:"addrs"`
	Score   float64  `json:"score"`
	Latency string   `json:"latency"`
	Source  string   `json:"source"`
}

// fetchPeers asks a running node for its peer list over the local GUI API.
//
// The address is an operator-supplied input, so it is parsed and validated
// rather than pasted into a URL.
func fetchPeers(addr string) ([]peerInfo, error) {
	if addr == "" {
		addr = "127.0.0.1:8080"
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("invalid address %q: expected host:port", addr)
	}
	if host == "" {
		host = "127.0.0.1"
	}
	if port == "" {
		return nil, fmt.Errorf("invalid address %q: missing port", addr)
	}

	client := &http.Client{Timeout: 3 * time.Second}
	url := "http://" + net.JoinHostPort(host, port) + "/api/peers"
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("node API returned %s", resp.Status)
	}

	var peers []peerInfo
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&peers); err != nil {
		return nil, fmt.Errorf("could not read peer list: %w", err)
	}
	return peers, nil
}

var useJSON bool

func init() {
	rootCmd.PersistentFlags().BoolVar(&useJSON, "json", false, "output in JSON format")
	nodeCmd.Flags().StringP("addr", "a", "0.0.0.0:4443", "listen address")
	nodeCmd.Flags().StringP("name", "n", "", "node name")
	nodeCmd.Flags().StringP("data-dir", "d", "", "path to store node identity and keys")
	rootCmd.AddCommand(nodeCmd)
	rootCmd.AddCommand(idCmd)
	rootCmd.AddCommand(peersCmd)
	rootCmd.AddCommand(initCmd)
	initCmd.Flags().StringP("data-dir", "d", "", "path to store node identity and config")
	// idCmd documented "--data-dir" in its own Example and read the flag in its
	// Run body, but never registered it, so the documented invocation failed with
	// "unknown flag: --data-dir".
	idCmd.Flags().StringP("data-dir", "d", "", "path to store node identity and keys")
	peersCmd.Flags().StringP("addr", "a", "127.0.0.1:8080", "address of the running node's GUI API")
}

func initHelpText() string {
	return `Guided setup for Local-WEB.

This wizard creates:
  - identity.json  — your Ed25519 keypair for node identity and signing
  - config.json    — node name, data directory, and service ports

You will be asked:
  1. Data directory — where node data is stored (defaults to ~/.localweb)
  2. Node name    — just a label for this machine (e.g. "laptop" or "phone")

Your private key never leaves this machine.`
}

func runInit(scanner *bufio.Reader, dataDir string) error {
	fmt.Println("Welcome to Local-WEB setup!")
	fmt.Println("This will create your node identity and config file.")
	fmt.Println("")

	if dataDir == "" {
		homeDir, _ := os.UserHomeDir()
		dataDir = filepath.Join(homeDir, ".localweb")
	}

	fmt.Printf("Data directory [%s]: ", dataDir)
	input, _ := scanner.ReadString('\n')
	input = strings.TrimSpace(strings.TrimRight(input, "\r\n"))
	if input != "" {
		dataDir = input
	}
	fmt.Printf("A node identity (Ed25519 keypair) will be stored in %s/identity.json.\n", dataDir)
	fmt.Println("This key is used for authentication and encryption. It never leaves this machine.")

	if _, err := os.Stat(filepath.Join(dataDir, "identity.json")); err == nil {
		fmt.Println("")
		fmt.Printf("An identity already exists in %s/identity.json.\n", dataDir)
		fmt.Print("Overwrite it? [y/N]: ")
		confirm, _ := scanner.ReadString('\n')
		confirm = strings.ToLower(strings.TrimSpace(confirm))
		if confirm != "y" && confirm != "yes" {
			fmt.Println("Keeping existing identity.")
			return nil
		}
	}

	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return fmt.Errorf("Could not create data directory %s: %w", dataDir, err)
	}

	fmt.Print("Node name [laptop]: ")
	name, _ := scanner.ReadString('\n')
	name = strings.TrimSpace(name)
	if name == "" {
		name = "laptop"
	}

	pub, _, err := crypto.LoadOrGenerateIdentity(dataDir)
	if err != nil {
		return fmt.Errorf("Could not generate identity: %w", err)
	}
	nodeID := crypto.NodeID(pub)

	config := map[string]interface{}{
		"name":       name,
		"data_dir":   dataDir,
		"node_id":    fmt.Sprintf("%x", nodeID[:8]),
		"listen":     "0.0.0.0:4443",
		"created_at": time.Now().UTC().Format(time.RFC3339),
	}

	configPath := filepath.Join(dataDir, "config.json")
	data, _ := json.MarshalIndent(config, "", "  ")
	if err := os.WriteFile(configPath, data, 0600); err != nil {
		return fmt.Errorf("Could not write config: %w", err)
	}

	fmt.Println("")
	fmt.Println("Setup complete!")
	fmt.Printf("  Node ID: %x\n", nodeID[:8])
	fmt.Printf("  Name:    %s\n", name)
	fmt.Printf("  Data:    %s/\n", dataDir)
	fmt.Println("")
	// The printed hint has to work from the directory the user is standing in,
	// and --data-dir is the flag that actually selects the identity `init` just
	// created. The previous "./bin/localweb" path exists only in a built repo.
	fmt.Printf("Start your node with:\n  localweb node --name %s --data-dir %q\n", name, dataDir)
	fmt.Println("Then, on another machine on the same network:  localweb peers")
	return nil
}

func startNode(ctx context.Context, addr, name, dataDir string) {
	if name == "" {
		hostname, _ := os.Hostname()
		name = hostname
	}

	if dataDir == "" {
		homeDir, _ := os.UserHomeDir()
		dataDir = filepath.Join(homeDir, ".localweb")
	}

	// Check if the port is already in use before starting
	host, port, _ := net.SplitHostPort(addr)
	if port == "" {
		port = "4443"
	}
	if ln, err := net.Listen("tcp", host+":"+port); err == nil {
		ln.Close()
	} else {
		fmt.Printf("Could not bind to %s — another process is using this port. Try --addr 0.0.0.0:%s or a different port.\n", addr, nextFreePort(port))
		log.Fatalf("port conflict: %v", err)
	}

	// Load or generate the persistent identity so the node ID can be shown. This
	// is the same identity the daemon would use; keys are never regenerated.
	pub, _, err := crypto.LoadOrGenerateIdentity(dataDir)
	if err != nil {
		log.Fatalf("Could not load or generate identity: %v. Run 'localweb init' first.", err)
	}
	nodeID := crypto.NodeID(pub)
	log.Printf("node ID: %x", nodeID[:8])

	fmt.Printf("Node identity: %x\n", nodeID[:8])
	fmt.Printf("Data dir     : %s\n", dataDir)
	fmt.Printf("Listen addr  : %s\n\n", addr)

	// The daemon is a separate program. This subcommand used to print
	// "Node started successfully" and return without starting anything, which is
	// the failure Phase 8 item 8.14 refers to. Rather than duplicating the
	// daemon's startup sequence here, exec the real binary if it sits next to
	// this one, and otherwise tell the user exactly what to run.
	daemon := findDaemonBinary()
	if daemon == "" {
		fmt.Println("The node daemon ships as a separate binary, not as a 'localweb node' mode.")
		fmt.Println("This command does NOT start a daemon.")
		fmt.Println()
		fmt.Println("Run one of:")
		fmt.Printf("  localweb-node -addr %s -data-dir %s\n", addr, dataDir)
		fmt.Println("or build it from source:")
		fmt.Println("  make build-node")
		return
	}

	fmt.Printf("Starting the daemon: %s\n\n", daemon)
	args := []string{"-addr", addr, "-data-dir", dataDir}
	if name != "" {
		args = append(args, "-name", name)
	}

	cmd := exec.Command(daemon, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		log.Fatalf("daemon exited: %v", err)
	}
}

// findDaemonBinary looks for the node binary alongside the running executable,
// trying the platform's own name first and then the .exe suffix Windows needs.
func findDaemonBinary() string {
	self, err := os.Executable()
	if err != nil {
		return ""
	}
	dir := filepath.Dir(self)

	candidates := []string{
		filepath.Join(dir, "localweb-node"),
		filepath.Join(dir, "localweb-node.exe"),
		filepath.Join(dir, "localweb-windows-amd64.exe"),
	}
	for _, c := range candidates {
		if info, err := os.Stat(c); err == nil && !info.IsDir() {
			return c
		}
	}
	return ""
}

func nextFreePort(current string) string {
	port, _ := strconv.Atoi(current)
	for p := port + 1; p < port+100; p++ {
		if ln, err := net.Listen("tcp", "0.0.0.0:"+strconv.Itoa(p)); err == nil {
			ln.Close()
			return strconv.Itoa(p)
		}
	}
	return strconv.Itoa(port + 1)
}

func main() {
	if err := rootCmd.ExecuteContext(context.Background()); err != nil {
		log.Fatal(err)
	}
}
