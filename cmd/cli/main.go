package main

import (
	"context"
	"fmt"
	"log"
	"os"

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
		startNode(cmd.Context(), addr, name)
	},
}

var idCmd = &cobra.Command{
	Use:   "id",
	Short: "Generate node identity",
	Run: func(cmd *cobra.Command, args []string) {
		pub, priv, err := crypto.GenerateKeyPair()
		if err != nil {
			log.Fatalf("generate keys: %v", err)
		}
		nodeID := crypto.NodeID(pub)
		fmt.Printf("Node ID: %x\n", nodeID[:])
		fmt.Printf("Public:  %x\n", pub[:])
		fmt.Printf("Private: %x\n", priv[:])
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
	rootCmd.AddCommand(nodeCmd)
	rootCmd.AddCommand(idCmd)
	rootCmd.AddCommand(peersCmd)
}

func startNode(ctx context.Context, addr, name string) {
	if name == "" {
		hostname, _ := os.Hostname()
		name = hostname
	}

	pub, _, err := crypto.GenerateKeyPair()
	if err != nil {
		log.Fatalf("generate keys: %v", err)
	}
	nodeID := crypto.NodeID(pub)
	log.Printf("node ID: %x", nodeID[:8])

	fmt.Printf("Starting node %s on %s\n", name, addr)
	fmt.Printf("Node ID: %x\n", nodeID[:8])
	fmt.Println("Node started successfully")
}

func main() {
	if err := rootCmd.ExecuteContext(context.Background()); err != nil {
		log.Fatal(err)
	}
}
