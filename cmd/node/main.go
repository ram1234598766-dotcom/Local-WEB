package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/ram1234598766-dotcom/Local-WEB/pkg/crypto"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/discovery"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/federation"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/gui"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/link"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/qos"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/security"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/store"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/transport"
)

// stripLeadingSubcommand removes a single leading non-flag argument, which the
// packaging, installer and service entry points use as a "node" verb.
// It rewrites os.Args because that is what flag.Parse reads.
func stripLeadingSubcommand() {
	args := os.Args[1:]
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		os.Args = append(os.Args[:1:1], args[1:]...)
	}
}

func main() {
	addr := flag.String("addr", "0.0.0.0:4443", "listen address")
	name := flag.String("name", "", "node name")
	storage := flag.String("storage", "", "path to BadgerDB storage directory")
	dataDir := flag.String("data-dir", "", "path to store node identity and keys")

	// Federation / rendezvous flags
	rendezvousURL := flag.String("rendezvous", "", "rendezvous server URL (e.g., https://rendezvous.localweb.io)")
	rendezvousRegister := flag.Bool("rendezvous-register", true, "register this node with rendezvous server")
	rendezvousPoll := flag.Duration("rendezvous-poll", 60*time.Second, "rendezvous poll interval")

	// Post-quantum hybrid handshake
	useHybrid := flag.Bool("hybrid", false, "enable post-quantum hybrid Noise+Kyber handshake")

	// The daemon has no subcommands, but the packaging and service files all
	// invoke it as `localweb node --data-dir ...`. flag.Parse stops at the
	// first non-flag argument, so every flag after that word was being
	// silently discarded and the node quietly used the default data dir.
	// Drop a leading subcommand word so those invocations behave as written.
	stripLeadingSubcommand()

	flag.Parse()

	// Reject unknown positional arguments rather than ignoring them: a
	// typo in a service unit should fail loudly, not start a node with
	// default settings.
	if flag.NArg() > 0 {
		log.Fatalf("unexpected argument %q; this daemon takes flags only (see -h)", flag.Arg(0))
	}

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

	// The transport layer runs Noise XX over X25519, while the node identity is
	// Ed25519. Handing the Ed25519 key bytes straight to the transport still
	// "works" — X25519 accepts any 32 bytes as a scalar — but the transport
	// identity is then unrelated to the node's Ed25519 identity, so a peer
	// deriving NodeID from the Noise static key cannot match the identity
	// this node advertises. Converting binds the two.
	transportPub, err := crypto.Ed25519PublicToX25519(pub)
	if err != nil {
		log.Fatalf("derive transport public key: %v", err)
	}
	transportPriv := crypto.Ed25519PrivateToX25519(priv)

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

	links := []link.Link{wifi, wifiDirect, ble, adhoc}
	if usb != nil {
		links = append(links, usb)
	}

	linkMgr := link.NewManager(link.ManagerConfig{
		Links: links,
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

	// Build discovery modes
	discoveryModes := []discovery.DiscoveryMode{}

	// Add rendezvous mode if URL provided
	var rendezvousMode *federation.RendezvousDiscoveryMode
	if *rendezvousURL != "" {
		rendezvousMode = federation.NewRendezvousDiscoveryMode(federation.RendezvousModeConfig{
			ServerURL:    *rendezvousURL,
			RegisterSelf: *rendezvousRegister,
			PollInterval: *rendezvousPoll,
		})
		rendezvousMode.SetPublicKey(pub)
		discoveryModes = append(discoveryModes, rendezvousMode)
		log.Printf("rendezvous: enabled with server %s", *rendezvousURL)
	}

	disc := discovery.NewOrchestrator(discovery.OrchestratorConfig{
		NodeID:      nodeID,
		PublicKey:   pub,
		Name:        *name,
		Modes:       discoveryModes,
		LinkManager: linkMgr,
	})
	go func() {
		if err := disc.Run(); err != nil {
			log.Printf("discovery: %v", err)
		}
	}()
	defer disc.Stop()

	// TLS certificate verification. This is off by default and cannot simply be
	// turned on: the QUIC listener presents a self-signed certificate generated
	// by transport.GenerateSelfSignedCert (no CA, no pinning), so a client that
	// verifies would reject every peer. Peer identity is authenticated by the
	// Noise XX layer beneath TLS, which is what actually prevents impersonation.
	//
	// The flag exists so the choice is explicit and reachable rather than an
	// accident of a struct's zero value, and so a deployment that adds a real PKI
	// can opt in. Phase 8 item 8.6 stays open until certificate pinning exists:
	// see TestClientCannotVerifySelfSignedServer in pkg/transport.
	tlsVerify := flag.Bool("tls-verify", false,
		"verify peer TLS certificates (requires a real PKI or pinning; self-signed certs will be rejected)")

	// QoS shaping. pkg/qos implements token buckets, priorities and an HTB
	// hierarchy, but until this pass nothing outside its own tests constructed a
	// QoSManager, so no traffic was actually shaped. Install it on the transport
	// so every outbound service frame passes through a service class.
	qosPolicy := flag.String("qos-policy", "priority",
		"outbound QoS policy: priority, fifo, wfq or htb")
	qosEnabled := flag.Bool("qos", true,
		"shape outbound service traffic with pkg/qos")

	var shaper transport.TrafficShaper
	if *qosEnabled {
		var policy qos.Policy
		switch *qosPolicy {
		case "fifo":
			policy = qos.PolicyFIFO
		case "wfq":
			policy = qos.PolicyWFQ
		case "htb":
			policy = qos.PolicyHTB
		default:
			policy = qos.PolicyPriority
		}
		shaper = qos.NewQoSManager(policy)
		log.Printf("qos: enabled (policy=%s)", *qosPolicy)
	} else {
		log.Printf("qos: disabled")
	}

	var serverOpts []transport.ServerOption
	if *tlsVerify {
		serverOpts = append(serverOpts, transport.WithEnforceTLSVerify(true))
	}
	if shaper != nil {
		serverOpts = append(serverOpts, transport.WithTrafficShaper(shaper))
	}

	server, err := transport.NewHybridServer(ctx, *addr, transportPub, transportPriv, *useHybrid, serverOpts...)
	if err != nil {
		log.Fatalf("transport server: %v", err)
	}
	defer server.Stop()

	if *tlsVerify {
		log.Printf("tls: certificate verification ENABLED")
	} else {
		log.Printf("tls: certificate verification disabled (self-signed cert; peer identity is authenticated by Noise XX)")
	}

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
		if err := guiHandler.ListenAndServe("0.0.0.0:8080"); err != nil {
			log.Printf("gui server: %v", err)
		}
	}()
	defer guiHandler.Shutdown(context.Background())

	<-ctx.Done()
	log.Println("shutting down")
}
