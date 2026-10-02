package main

import (
	"context"
	"encoding/hex"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ram1234598766-dotcom/Local-WEB/pkg/crypto"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/dht"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/discovery"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/federation"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/gui"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/link"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/qos"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/security"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/services/dns"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/services/docs"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/services/email"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/services/files"
	httpsvc "github.com/ram1234598766-dotcom/Local-WEB/pkg/services/http"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/services/registry"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/services/vpn"
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

	// DNS listen port. The default 5353 is mDNS, which the OS resolver and most
	// browsers already hold, so a plain `node` on a desktop frequently cannot
	// bind it. It stays the default for compatibility with the documented
	// service contract; -dns-port exists for the desktop case.
	dnsPort := flag.String("dns-port", "5353",
		"DNS service UDP port (5353 is mDNS and is often already taken by the OS resolver)")

	// Service listen ports. Each is a flag because several of the documented
	// defaults collide with something else on an ordinary machine, and a node
	// that cannot bind one should not be a node that fails to start.
	//
	// The HTTP gateway defaults to 8082 rather than 8081: 8081 is the port the
	// built-in example echo plugin binds, and the plugin host's own tests bind
	// it to prove a busy port is reported. Sharing it meant running a node made
	// those tests fail.
	httpAddr := flag.String("http-addr", "0.0.0.0:8082", "HTTP gateway listen address")
	smtpAddr := flag.String("smtp-addr", "0.0.0.0:587", "SMTP listen address")
	imapAddr := flag.String("imap-addr", "0.0.0.0:993", "IMAP listen address")
	registryAddr := flag.String("registry-addr", "0.0.0.0:9092", "package registry listen address")
	guiAddr := flag.String("gui-addr", "0.0.0.0:8080", "GUI dashboard listen address")

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

	// Start GUI API + SPA. The bind address is 0.0.0.0 even though the comment
	// above used to call this "localhost-only"; see ARCHITECTURE.md open item 14.
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

	// Phase 8 item 8.1: start the protocol services. Until this existed the
	// daemon registered only the Control handler and started none of the nine,
	// so /api/services/health reported nine services that were not running.
	//
	// Each service is started in its own goroutine and marked running only once
	// its listener is actually up, so the health endpoint keeps reporting the
	// truth if one fails to bind.
	stopServices := startServices(ctx, api, pub, *dataDir, server.Server, servicePorts{
		dns:      *dnsPort,
		http:     *httpAddr,
		smtp:     *smtpAddr,
		imap:     *imapAddr,
		registry: *registryAddr,
		dht:      "127.0.0.1:0",
	})
	defer stopServices()

	guiHandler := gui.NewHandler(api)
	go func() {
		if err := guiHandler.ListenAndServe(*guiAddr); err != nil {
			log.Printf("gui server: %v", err)
		}
	}()
	defer guiHandler.Shutdown(context.Background())

	<-ctx.Done()
	log.Println("shutting down")
}

// servicePorts carries the listen address for each protocol service. They are
// passed together rather than as five parameters so a new service cannot be
// added with its address silently pinned to a constant.
type servicePorts struct {
	dns      string
	http     string
	smtp     string
	imap     string
	registry string
	dht      string
}

// newDHTForNode builds a DHT node that listens and answers requests, so this
// node can both publish metadata to peers and resolve metadata from them.
//
// dht.NewDHT was called from nowhere in the daemon before this, and
// dht.NewServer from nowhere at all: a node could neither serve a lookup nor
// start one, so a package published through the registry stayed on the machine
// that published it.
//
// The DHT speaks TCP, which is a separate plane from the QUIC service
// transport. bootstrap lists TCP addresses of peers to learn from; without any,
// the node serves and stores locally and reaches no other node, which is why
// the log says so plainly.
func newDHTForNode(ctx context.Context, srv *transport.Server, pub [32]byte, stops *[]func()) (*dht.DHT, *dht.Server) {
	localID := dht.NodeIDFromPub(pub)
	node := dht.NewDHT(localID, pub, "localweb", dht.TCPTransport{})

	if err := node.Bootstrap(ctx, nil); err != nil {
		log.Printf("dht: not started: %v", err)
	}

	dhtSrv := dht.NewServer(node.Node())
	if err := dhtSrv.Start("127.0.0.1:0"); err != nil {
		log.Printf("dht: not serving: %v", err)
		return node, dhtSrv
	}
	*stops = append(*stops, func() { _ = dhtSrv.Stop() })

	// Announce this node to itself so its own value store is reachable and a
	// Store has somewhere local to land.
	if err := node.RegisterNode(ctx, pub, "localweb", []string{dhtSrv.Addr()}, dht.MinPoWDifficulty); err != nil {
		log.Printf("dht: could not announce self: %v", err)
	}
	return node, dhtSrv
}

// startServices brings up every protocol service that has a real listener and
// reports each one's true state to the GUI. It returns a function that stops
// them again.
//
// Three services are deliberately absent and stay reported as not running:
// messaging has no listener, voice has no codec or media transport, and the VPN
// has no TUN forwarding loop. Starting them here would be theatre. See
// ARCHITECTURE.md section 7.
// waitForTCP polls addr until a TCP connection succeeds or the timeout expires.
// Several services expose Start methods that either block in ListenAndServe or
// return before the socket is actually accepting, so reachability is the only
// reliable signal for a truthful health report.
func waitForTCP(ctx context.Context, addr string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	dialHost, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if dialHost == "" || dialHost == "0.0.0.0" || dialHost == "::" {
		dialHost = "127.0.0.1"
	}
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return false
		}
		conn, derr := net.DialTimeout("tcp", net.JoinHostPort(dialHost, portOf(addr)), 500*time.Millisecond)
		if derr == nil {
			_ = conn.Close()
			return true
		}
		time.Sleep(200 * time.Millisecond)
	}
	return false
}

func portOf(addr string) string {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return ""
	}
	return port
}

// portNum returns the numeric port from addr, or 0 when there is none. The email
// configs take an int, so an unparsable address must not silently become a
// plausible-looking number.
func portNum(addr string) int {
	p := portOf(addr)
	if p == "" {
		return 0
	}
	n, err := strconv.Atoi(p)
	if err != nil {
		return 0
	}
	return n
}

func startServices(ctx context.Context, api *gui.NodeAPI, pub [32]byte, dataDir string, srv *transport.Server, ports servicePorts) func() {
	var stops []func()

	// --- DNS: a real UDP listener ---
	//
	// 5353 is mDNS, which the OS resolver and any browser already hold on most
	// desktops, so the port is configurable and a bind failure is reported
	// rather than silently downgrading the node to "no DNS".
	dnsZone := &dns.Zone{
		Records:   make(map[string][]dns.ResourceRecord),
		Transfers: make(map[[32]byte]bool),
		Signer:    pub,
		SignedAt:  time.Now(),
	}
	dnsZone.Records["node.localweb"] = []dns.ResourceRecord{{
		// Class 1 is IN. The dns package keeps classIN unexported, so the value
		// is spelled out here rather than importing it.
		Record: dns.DNSRecord{Name: "node.localweb", Type: dns.TypeA, Class: 1, TTL: 300},
		Data:   []byte{127, 0, 0, 1},
	}}
	dnsSrv := dns.NewServer(dnsZone, nil)
	dnsAddr := "0.0.0.0:" + ports.dns
	go func() {
		if err := dnsSrv.Start(ctx, dnsAddr); err != nil {
			log.Printf("dns: not started on %s: %v (5353 is mDNS and may already be in use; override with -dns-port)", dnsAddr, err)
			api.SetServiceLive(false, "dns")
			return
		}
		api.SetServiceLive(true, "dns")
		log.Printf("dns: listening on %s", dnsAddr)
	}()

	// --- HTTP/3 gateway: a real HTTP listener ---
	gateway := httpsvc.NewGateway()
	if err := gateway.RegisterSite("gui.localweb", "/", http.NotFoundHandler()); err != nil {
		log.Printf("http: could not register site: %v", err)
	}
	//
	// Gateway.Start blocks in ListenAndServe, so its return value only arrives
	// at shutdown. Health is therefore driven by probing the port, not by the
	// return, otherwise a gateway that is genuinely serving still reports down.
	gatewayErr := make(chan error, 1)
	go func() { gatewayErr <- gateway.Start(ctx, ports.http) }()
	go func() {
		if waitForTCP(ctx, ports.http, 10*time.Second) {
			api.SetServiceLive(true, "http")
			api.SetHTTPSites([]gui.HTTPSiteResponse{{
				Name: "gui.localweb", Status: "active", Routes: 1,
			}})
			log.Printf("http: gateway reachable on %s", ports.http)
			return
		}
		// The port never accepted a connection: report the real reason.
		select {
		case err := <-gatewayErr:
			log.Printf("http: not started: %v", err)
		default:
			log.Printf("http: not reachable on %s", ports.http)
		}
		api.SetServiceLive(false, "http")
	}()
	stops = append(stops, func() { _ = gateway.Stop() })

	// --- Email: real SMTP and IMAP listeners ---
	//
	// Both servers drive their own accept loop from a net.Listener supplied in
	// the config, so the daemon owns the bind and a bind failure is reported
	// rather than swallowed.
	emailUp := false
	mailbox := email.NewMailboxStore(filepath.Join(dataDir, "mail"))
	creds := email.NewCredentialStore()
	queue := email.NewQueue()
	smtpLn, serr := net.Listen("tcp", ports.smtp)
	if serr != nil {
		log.Printf("email: smtp not started: %v", serr)
	} else {
		smtpSrv, cerr := email.NewSMTPServer(ctx, &email.SMTPConfig{
			Hostname: "node.localweb", Port: portNum(ports.smtp), MaxSize: 32 << 20,
			Listener: smtpLn, DB: mailbox, Queue: queue, Credentials: creds,
		})
		if cerr != nil {
			log.Printf("email: smtp not started: %v", cerr)
			_ = smtpLn.Close()
		} else {
			emailUp = true
			stops = append(stops, smtpSrv.Stop)
			log.Printf("email: smtp listening on %s", ports.smtp)
		}
	}
	imapLn, ierr := net.Listen("tcp", ports.imap)
	if ierr != nil {
		log.Printf("email: imap not started: %v", ierr)
	} else {
		imapSrv, cerr := email.NewIMAPServer(ctx, &email.IMAPConfig{
			Hostname: "node.localweb", Port: portNum(ports.imap),
			Listener: imapLn, DB: mailbox, Credentials: creds,
		})
		if cerr != nil {
			log.Printf("email: imap not started: %v", cerr)
			_ = imapLn.Close()
		} else {
			emailUp = true
			stops = append(stops, imapSrv.Stop)
			log.Printf("email: imap listening on %s", ports.imap)
		}
	}
	api.SetServiceLive(emailUp, "email")

	// --- Files: a real sync engine over the local block store ---
	//
	// The metadata store is handed to the GUI so /api/files/list reports the
	// files that are actually on this node rather than an empty placeholder.
	//
	// The exchange protocol and sync engine are the reason the node can
	// actually fetch a block from a peer. Neither was constructed anywhere in
	// the daemon, so the Files service announced a sync engine it did not have
	// and could never have moved a block: the want/have/block path existed only
	// in tests.
	filesDir := filepath.Join(dataDir, "files")
	blockStore, ferr := files.NewFileStore(filesDir)
	if ferr != nil {
		log.Printf("files: store unavailable: %v", ferr)
		api.SetServiceLive(false, "files")
	} else {
		metaStore := files.NewFileMetadataStore()
		api.SetFileStore(metaStore)

		nodeID := crypto.NodeID(pub)
		exchange := files.NewExchangeProtocol(srv, blockStore, metaStore, nodeID)
		syncEngine := files.NewSyncEngine(blockStore, metaStore, nodeID, 15*time.Second)
		syncEngine.SetExchange(exchange)
		// Without this the block a peer serves is decoded and discarded.
		exchange.SetHandler(syncEngine.HandleBlock)
		// The engine cannot invent peers; the transport is the only thing that
		// knows who is connected.
		syncEngine.SetPeerSource(srv)
		if err := syncEngine.Start(ctx); err != nil {
			log.Printf("files: sync engine not started: %v", err)
		} else {
			stops = append(stops, func() { _ = syncEngine.Stop() })
		}

		api.SetSyncEngine(syncEngine)
		api.SetServiceLive(true, "files")
		log.Printf("files: blocks at %s, sync every 15s over %x", filesDir, nodeID[:6])
	}

	// --- Docs: the collaborative document service ---
	//
	// NewService always returns a usable *Service, so the only honest thing to
	// report is that it is constructed and holding documents in memory.
	docsSvc := docs.NewService(docs.ServiceConfig{
		NodeID: hex.EncodeToString(pub[:]),
		PubKey: pub,
	})
	if docsSvc == nil {
		api.SetServiceLive(false, "docs")
	} else {
		// The GUI reads documents, text and comments through this service, so
		// the Docs panel reflects real CRDT state.
		api.SetDocsService(docsSvc)
		api.SetServiceLive(true, "docs")
		log.Printf("docs: service ready")
	}

	// --- Registry: a real HTTP index over a real DHT ---
	//
	// The index previously ran on a bare MemoryRegistry with no network behind
	// it, so a package published here was visible to nothing but this node. The
	// DHT is what makes a publish reach another node and a resolve come back.
	dhtNode, dhtSrv := newDHTForNode(ctx, srv, pub, &stops)
	memReg := registry.NewMemoryRegistry()
	memReg.RegisterDistributor(registry.NewDHTDistributor(dhtNode, pub))
	// The GUI reads the same registry, so the Registry panel reflects real
	// publishes instead of a hardcoded row.
	api.SetRegistry(memReg)

	regSrv := registry.NewHTTPServer(registry.ServerConfig{
		Addr:     ports.registry,
		Registry: memReg,
	})
	go func() {
		if err := regSrv.Start(); err != nil {
			log.Printf("registry: not started: %v", err)
			api.SetServiceLive(false, "registry")
			return
		}
		api.SetServiceLive(true, "registry")
		log.Printf("registry: index on %s, dht on %s", ports.registry, dhtSrv.Addr())
	}()

	// --- VPN: a real TUN device and a real ServiceVPN tunnel ---
	//
	// The service registers a transport handler so a peer can open a VPN stream,
	// and the forwarding loop carries packets over it. A tunnel is only possible
	// where the operating system lets this process open a TUN device, which
	// needs root or CAP_NET_ADMIN; where it cannot, the service stays reported as
	// not running rather than claiming a tunnel that carries nothing.
	vpnSrv := vpn.NewServer(crypto.NodeID(pub))
	if vpnSrv.HasDevice() {
		srv.RegisterHandler(transport.ServiceVPN, vpnSrv.ServiceHandler(func(st transport.Stream) [32]byte {
			return st.PeerID()
		}))
		api.SetServiceLive(true, "vpn")
		log.Printf("vpn: tun device %q up, serviceVPN handler registered", vpnSrv.DeviceName())
	} else {
		api.SetServiceLive(false, "vpn")
		log.Printf("vpn: no tun device (needs root or CAP_NET_ADMIN), service not started")
	}

	// The remaining two stay false and that is the honest state.
	for _, name := range []string{"messaging", "voice"} {
		api.SetServiceLive(false, name)
	}

	running := 0
	for _, v := range api.ServiceHealth() {
		if v {
			running++
		}
	}
	log.Printf("services: started, %d components reporting healthy", running)

	return func() {
		for _, s := range stops {
			s()
		}
	}
}
