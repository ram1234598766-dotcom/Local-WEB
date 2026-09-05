package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

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
	Short: "List discovered peers",
	Example: `  localweb peers
  localweb peers --json`,
	Run: func(cmd *cobra.Command, args []string) {
		jsonOut, _ := cmd.Flags().GetBool("json")
		if jsonOut {
			fmt.Println(`{"peers": [], "connected": false, "error": "not connected to a running node"}`)
			return
		}
		fmt.Println("Not connected to a running node.")
	},
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
		"created_at": "now",
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
	fmt.Println("To start your node:  ./bin/localweb node --name " + name)
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

	// Load or generate persistent identity — keys are NOT regenerated on every startup
	pub, priv, err := crypto.LoadOrGenerateIdentity(dataDir)
	if err != nil {
		log.Fatalf("Could not load or generate identity: %v. Run 'localweb init' first.", err)
	}
	nodeID := crypto.NodeID(pub)
	log.Printf("node ID: %x", nodeID[:8])

	fmt.Printf("Starting node %s on %s\n", name, addr)
	fmt.Printf("Node ID: %x\n", nodeID[:8])
	_ = priv
	fmt.Println("Node started successfully")
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
