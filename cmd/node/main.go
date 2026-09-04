package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/mrityunjay/LocalWEB/pkg/crypto"
	"github.com/mrityunjay/LocalWEB/pkg/discovery"
	"github.com/mrityunjay/LocalWEB/pkg/link"
	"github.com/mrityunjay/LocalWEB/pkg/transport"
)

func main() {
	addr := flag.String("addr", "0.0.0.0:4443", "listen address")
	name := flag.String("name", "", "node name")
	flag.Parse()

	if *name == "" {
		hostname, _ := os.Hostname()
		*name = hostname
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	pub, priv, err := crypto.GenerateKeyPair()
	if err != nil {
		log.Fatalf("generate keys: %v", err)
	}
	nodeID := crypto.NodeID(pub)
	log.Printf("node ID: %x", nodeID[:8])

	wifi, _ := link.NewWiFiStation()
	wifiDirect, _ := link.NewWiFiDirect()
	ble, _ := link.NewBLE(nodeID, pub, *name, 0)
	usb, _ := link.NewUSB()
	adhoc, _ := link.NewAdHocWiFi()

	linkMgr := link.NewManager(link.ManagerConfig{
		Links: []link.Link{wifi, wifiDirect, ble, usb, adhoc},
		Preferences: []link.LinkMode{
			link.ModeWiFiDirect,
			link.ModeWiFiStation,
			link.ModeAdHocWiFi,
			link.ModeUSBTether,
			link.ModeBLE,
		},
	})
	go func() {
		if err := linkMgr.Run(); err != nil {
			log.Printf("link manager: %v", err)
		}
	}()
	defer linkMgr.Stop()

	disc := discovery.NewOrchestrator(discovery.OrchestratorConfig{
		NodeID:      nodeID,
		PublicKey:   pub,
		Name:        *name,
		LinkManager: linkMgr,
	})
	go func() {
		if err := disc.Run(); err != nil {
			log.Printf("discovery: %v", err)
		}
	}()
	defer disc.Stop()

	server, err := transport.NewServer(ctx, *addr, pub, priv)
	if err != nil {
		log.Fatalf("transport server: %v", err)
	}
	defer server.Stop()

	server.RegisterHandler(transport.ServiceControl, func(ctx context.Context, stream transport.Stream) {
		buf := make([]byte, 1024)
		n, _ := stream.Read(buf)
		log.Printf("control msg: %s", buf[:n])
		stream.Write([]byte("pong"))
	})

	log.Printf("node listening on %s", *addr)
	<-ctx.Done()
	log.Println("shutting down")
}
