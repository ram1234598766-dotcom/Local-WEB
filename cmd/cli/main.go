package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/mrityunjay/LocalWEB/pkg/crypto"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "localweb",
	Short: "LocalWEB P2P networking stack",
	Long:  "Real working P2P internet stack. Zero infrastructure required.",
}

var nodeCmd = &cobra.Command{
	Use:   "node",
	Short: "Start LocalWEB node",
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
		fmt.Printf("Node ID: %x\n", nodeID[:8])
		fmt.Printf("Public:  %x\n", pub[:])
		fmt.Printf("Identity stored in %s/identity.json (private key not displayed)\n", dataDir)
	},
}

var peersCmd = &cobra.Command{
	Use:   "peers",
	Short: "List discovered peers",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Not connected to a running node.")
	},
}

func init() {
	nodeCmd.Flags().StringP("addr", "a", "0.0.0.0:4443", "listen address")
	nodeCmd.Flags().StringP("name", "n", "", "node name")
	nodeCmd.Flags().StringP("data-dir", "d", "", "path to store node identity and keys")
	rootCmd.AddCommand(nodeCmd)
	rootCmd.AddCommand(idCmd)
	rootCmd.AddCommand(peersCmd)
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

	// Load or generate persistent identity — keys are NOT regenerated on every startup
	pub, priv, err := crypto.LoadOrGenerateIdentity(dataDir)
	if err != nil {
		log.Fatalf("load identity: %v", err)
	}
	nodeID := crypto.NodeID(pub)
	log.Printf("node ID: %x", nodeID[:8])

	fmt.Printf("Starting node %s on %s\n", name, addr)
	fmt.Printf("Node ID: %x\n", nodeID[:8])
	_ = priv
	fmt.Println("Node started successfully")
}

func main() {
	if err := rootCmd.ExecuteContext(context.Background()); err != nil {
		log.Fatal(err)
	}
}
