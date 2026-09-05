package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/ram1234598766-dotcom/Local-WEB/pkg/crypto"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/discovery"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/gui"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/link"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/security"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/store"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/transport"
)

func main() {
	addr := flag.String("addr", "0.0.0.0:4443", "listen address")
	name := flag.String("name", "", "node name")
	storage := flag.String("storage", "", "path to BadgerDB storage directory")
	dataDir := flag.String("data-dir", "", "path to store node identity and keys")
	flag.Parse()

	if *name == "" {
		hostname, _ := os.Hostname()
		*name = hostname
	}

	if *dataDir == "" {
		homeDir, _ := os.UserHomeDir()
		*dataDir = filepath.Join(homeDir, ".localweb")
	}

	if *storage == "" {
		*storage = filepath.Join(*dataDir, "data")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Println("received shutdown signal, flushing and closing...")
		cancel()
	}()

	// Load or generate persistent identity — keys are NOT regenerated on every startup
	pub, priv, err := crypto.LoadOrGenerateIdentity(*dataDir)
	if err != nil {
		log.Fatalf("load identity: %v", err)
	}
	nodeID := crypto.NodeID(pub)
	log.Printf("node ID: %x", nodeID[:8])

	// Derive store encryption key from node identity
	encKey := crypto.DeriveStorageKey(priv)

	// Open encrypted store
	dbStore, err := store.Open(*storage, encKey)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}

	// Graceful shutdown: flush pending writes before close
	go func() {
		<-ctx.Done()
		if err := dbStore.Flush(); err != nil {
			log.Printf("flush error: %v", err)
		}
		if err := dbStore.Close(); err != nil {
			log.Printf("close error: %v", err)
		}
	}()

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

	// Start GUI API + SPA on :8080 (localhost-only read-only dashboard)
	api := gui.NewAPI(pub)
	if dbStore != nil {
		api.SetStore(dbStore)
		api.SetPeerStore(store.NewPeerStore(dbStore))
	}
	api.SetDiscovery(disc)

	auditLog := security.NewAuditLog()
	if auditLog != nil {
		api.SetAuditLog(auditLog)
	}

	guiHandler := gui.NewHandler(api)
	go func() {
		if err := guiHandler.ListenAndServe("localhost:8080"); err != nil {
			log.Printf("gui server: %v", err)
		}
	}()
	defer guiHandler.Shutdown(context.Background())

	<-ctx.Done()
	log.Println("shutting down")
}
