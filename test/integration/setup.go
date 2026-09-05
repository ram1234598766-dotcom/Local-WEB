package integration

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/ram1234598766-dotcom/Local-WEB/pkg/crdt"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/crypto"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/dht"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/discovery"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/security"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/transport"
)

type testNode struct {
	nodeID  dht.NodeID
	pubKey  [32]byte
	privKey [32]byte
	peerDB  *discovery.PeerDatabase
	dht     *dht.DHT
	mt      *mockTransport
	ctx     context.Context
	cancel  context.CancelFunc
}

type mockTransport struct {
	mu       sync.Mutex
	conn     net.Conn
	listener net.Listener
}

func newMockTransport(t *testing.T) *mockTransport {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("mock listen: %v", err)
	}
	return &mockTransport{listener: ln}
}

func (m *mockTransport) Dial(ctx context.Context, addr string) (net.Conn, error) {
	return net.Dial("tcp", m.listener.Addr().String())
}

func (m *mockTransport) Listen(addr string) (net.Listener, error) {
	return m.listener, nil
}

func (m *mockTransport) Stop() {
	if m.listener != nil {
		m.listener.Close()
	}
}

func newTestNode(t *testing.T) *testNode {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())

	pub, priv, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate keys: %v", err)
	}

	nodeID := dht.NodeIDFromPub(pub)
	peerDB := discovery.NewPeerDatabase()
	mt := newMockTransport(t)
	dhtNode := dht.NewDHT(nodeID, pub, "testnode", mt)

	return &testNode{
		nodeID:  nodeID,
		pubKey:  pub,
		privKey: priv,
		peerDB:  peerDB,
		dht:     dhtNode,
		mt:      mt,
		ctx:     ctx,
		cancel:  cancel,
	}
}

func (n *testNode) Cleanup() {
	n.cancel()
	n.mt.Stop()
	n.dht.Stop()
}

func (n *testNode) AddPeer(name string, addrs []string) discovery.PeerInfo {
	pub, _, _ := crypto.GenerateKeyPair()
	peer := discovery.PeerInfo{
		ID:        dht.NodeIDFromPub(pub),
		PublicKey: pub,
		Name:      name,
		Addrs:     addrs,
		Score:     0.5,
		LastSeen:  time.Now(),
		FirstSeen: time.Now(),
		Version:   "1.0.0",
	}
	n.peerDB.Add(peer)
	return peer
}

func TestPeerDatabaseCRUD(t *testing.T) {
	db := discovery.NewPeerDatabase()

	p1 := discovery.PeerInfo{
		ID:       [32]byte{1},
		Name:     "alice",
		Addrs:    []string{"1.2.3.4:4443"},
		Score:    0.8,
		LastSeen: time.Now(),
	}
	db.Add(p1)

	if db.Count() != 1 {
		t.Fatalf("expected 1 peer, got %d", db.Count())
	}

	got, ok := db.Get([32]byte{1})
	if !ok {
		t.Fatal("peer not found")
	}
	if got.Name != "alice" {
		t.Fatalf("got %s", got.Name)
	}

	p2 := discovery.PeerInfo{
		ID:    [32]byte{1},
		Name:  "alice-updated",
		Score: 0.9,
	}
	db.Add(p2)
	got2, _ := db.Get([32]byte{1})
	if got2.Name != "alice-updated" {
		t.Fatalf("expected updated name, got %s", got2.Name)
	}

	best := db.BestPeers(5)
	if len(best) != 1 {
		t.Fatalf("expected 1 best peer, got %d", len(best))
	}

	db.Remove([32]byte{1})
	if db.Count() != 0 {
		t.Fatalf("expected 0 peers after remove, got %d", db.Count())
	}
}

func TestPeerDatabaseDedup(t *testing.T) {
	db := discovery.NewPeerDatabase()

	for i := 0; i < 10; i++ {
		p := discovery.PeerInfo{
			ID:    [32]byte{1},
			Name:  "dup",
			Score: float64(i) / 10.0,
		}
		db.Add(p)
	}

	if db.Count() != 1 {
		t.Fatalf("expected 1 peer after dedup, got %d", db.Count())
	}
}

func TestRoutingTable(t *testing.T) {
	localID := dht.NodeIDFromPub([32]byte{1})
	rt := dht.NewRoutingTable(localID)

	for i := 2; i < 22; i++ {
		pub := [32]byte{byte(i)}
		peerID := dht.NodeIDFromPub(pub)
		rt.Add(&dht.Peer{Info: dht.PeerInfo{
			ID:   peerID,
			Name: "peer",
		}})
	}

	all := rt.AllPeers()
	if len(all) != 20 {
		t.Fatalf("expected 20 peers in routing table, got %d", len(all))
	}

	target := dht.NodeIDFromPub([32]byte{5})
	closest := rt.FindClosest(target, dht.Alpha)
	if len(closest) != dht.Alpha {
		t.Fatalf("expected %d closest peers, got %d", dht.Alpha, len(closest))
	}
}

func TestTransportFrameEncoding(t *testing.T) {
	payload := []byte("hello world")

	frame := transport.EncodeFrameBare(transport.MsgPing, payload)
	decoded, err := transport.DecodeFrameBare(frame)
	if err != nil {
		t.Fatalf("decode frame: %v", err)
	}

	if decoded.Type != transport.MsgPing {
		t.Fatalf("expected MsgPing, got %v", decoded.Type)
	}
	if string(decoded.Payload) != string(payload) {
		t.Fatalf("payload mismatch: %s", decoded.Payload)
	}
}

func TestTransportFrameSizeLimit(t *testing.T) {
	payload := make([]byte, 2<<20)

	err := testFrameOversize(payload)
	if err == nil {
		t.Fatal("expected error for oversized frame")
	}
}

func testFrameOversize(payload []byte) error {
	frame := transport.EncodeFrameBare(transport.MsgStore, payload)
	_, err := transport.DecodeFrameBare(frame)
	return err
}

func TestNoiseHandshake(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	alicePub, alicePriv, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("alice keys: %v", err)
	}
	bobPub, bobPriv, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("bob keys: %v", err)
	}

	initiator, err := crypto.NewNoiseInitiator(alicePub, alicePriv)
	if err != nil {
		t.Fatalf("new initiator: %v", err)
	}
	responder, err := crypto.NewNoiseResponder(bobPub, bobPriv)
	if err != nil {
		t.Fatalf("new responder: %v", err)
	}

	msg1, _, done, err := initiator.WriteHandshake(nil)
	if err != nil {
		t.Fatalf("initiator msg1: %v", err)
	}
	_ = done

	msg2, _, done2, err := responder.WriteHandshake(msg1)
	if err != nil {
		t.Fatalf("responder msg2: %v", err)
	}
	_ = done2

	msg3, _, _, err := initiator.WriteHandshake(msg2)
	if err != nil {
		t.Fatalf("initiator msg3: %v", err)
	}

	_, _, _, err = responder.WriteHandshake(msg3)
	if err != nil {
		t.Fatalf("responder finalize: %v", err)
	}
}

func TestCRDTOperations(t *testing.T) {
	orSet := crdt.NewORSet()

	orSet.Add("item1")
	orSet.Add("item2")

	items := orSet.Items()
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}

	orSet.Remove("item1")
	orSet.Add("item3")

	mergeSet := crdt.NewORSet()
	mergeSet.Add("item4")

	orSet.Merge(mergeSet)
	items = orSet.Items()
	if len(items) != 2 {
		t.Fatalf("expected 2 items after merge, got %d: %v", len(items), items)
	}
}

func TestCRDTRGA(t *testing.T) {
	rga := crdt.NewRGA("client1")

	rga.Insert("head", "H")
	rga.Insert("head", "e")
	rga.Insert("head", "l")
	rga.Insert("head", "l")
	rga.Insert("head", "o")

	if rga.Length() != 5 {
		t.Fatalf("expected length 5, got %d", rga.Length())
	}

	v, _ := rga.Get(0)
	if v != "e" {
		t.Fatalf("expected second-inserted char at index 0, got %q", v)
	}
}

func TestCRDTLWWRegister(t *testing.T) {
	reg := crdt.NewLWWRegister("client1")
	reg.Set([]byte("v1"))

	val, _, _ := reg.Get()
	if string(val) != "v1" {
		t.Fatalf("expected v1, got %q", val)
	}
	pub, priv, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("keys: %v", err)
	}

	peerID := dht.NodeIDFromPub(pub)

	mgr := security.NewCapabilityManager(pub, priv)
	mgr.RegisterService("dns")

	token, err := mgr.GrantCapability(peerID, []string{"dns"}, time.Hour)
	if err != nil {
		t.Fatalf("grant capability: %v", err)
	}

	err = mgr.VerifyToken(token)
	if err != nil {
		t.Fatalf("verify token: %v", err)
	}

	if !mgr.CheckAccess(peerID, "dns") {
		t.Fatal("expected access to dns")
	}

	if mgr.CheckAccess(peerID, "http") {
		t.Fatal("expected no access to http")
	}
}

func TestPoW(t *testing.T) {
	data := []byte("test proof of work")
	difficulty := 8

	nonce, err := dht.SolvePoW(data, difficulty)
	if err != nil {
		t.Fatalf("solve PoW: %v", err)
	}

	if !dht.VerifyPoW(data, nonce, difficulty) {
		t.Fatal("PoW verification failed")
	}

	if dht.VerifyPoW(data, nonce, difficulty-1) {
		t.Fatal("unexpectedly valid PoW at lower difficulty")
	}
}

func TestTwoNodeDiscovery(t *testing.T) {
	node1 := newTestNode(t)
	defer node1.Cleanup()
	node1.AddPeer("node1-peer", []string{"127.0.0.1:4443"})

	node2 := newTestNode(t)
	defer node2.Cleanup()
	node2.AddPeer("node2-peer", []string{"127.0.0.1:4444"})

	if node1.nodeID == node2.nodeID {
		t.Fatal("nodes should have different IDs")
	}
	if node1.peerDB.Count() != 1 {
		t.Fatalf("expected 1 peer in node1's db, got %d", node1.peerDB.Count())
	}
}

func TestDHTRouting(t *testing.T) {
	node := newTestNode(t)
	defer node.Cleanup()

	rt := dht.NewRoutingTable(node.nodeID)
	for i := 0; i < 15; i++ {
		pub, _, _ := crypto.GenerateKeyPair()
		peerID := dht.NodeIDFromPub(pub)
		rt.Add(&dht.Peer{Info: dht.PeerInfo{
			ID:   peerID,
			Name: "peer",
		}})
	}

	peers := rt.AllPeers()
	if len(peers) != 15 {
		t.Fatalf("expected 15 peers in DHT, got %d", len(peers))
	}
}

func TestDNSRecordHandling(t *testing.T) {
	record := discovery.PeerInfo{
		ID:   [32]byte{0x42},
		Name: "test.localweb",
		Services: []discovery.ServiceInfo{
			{Name: "http", Port: 80},
			{Name: "dns", Port: 53},
		},
	}

	if record.Name != "test.localweb" {
		t.Fatalf("expected 'test.localweb', got %s", record.Name)
	}
	if len(record.Services) != 2 {
		t.Fatalf("expected 2 services, got %d", len(record.Services))
	}
}

func TestTransportStreamMultiplexing(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pub, priv, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("keys: %v", err)
	}

	server, err := transport.NewServer(ctx, "127.0.0.1:0", pub, priv)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	defer server.Stop()

	server.RegisterHandler(transport.ServiceControl, func(ctx context.Context, stream transport.Stream) {
		buf := make([]byte, 1024)
		n, _ := stream.Read(buf)
		response := "echo: " + string(buf[:n])
		stream.Write([]byte(response))
		stream.Close()
	})
}

func TestFullStack5Nodes(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	var nodes []*testNode
	for i := 0; i < 5; i++ {
		n := newTestNode(t)
		n.AddPeer("peer", []string{"127.0.0.1:4443"})
		nodes = append(nodes, n)
	}
	defer func() {
		for _, n := range nodes {
			n.Cleanup()
		}
	}()

	var wg sync.WaitGroup
	for _, n := range nodes {
		wg.Add(1)
		go func(node *testNode) {
			defer wg.Done()
			_ = node.peerDB.All()
			_ = node.peerDB.Count()
		}(n)
	}
	wg.Wait()
}

func TestFrameRoundTrip(t *testing.T) {
	tests := []struct {
		name    string
		msgType transport.MessageType
		payload []byte
	}{
		{"ping", transport.MsgPing, []byte("hello")},
		{"pong", transport.MsgPong, nil},
		{"store", transport.MsgStore, make([]byte, 256)},
		{"find_node", transport.MsgFindNode, make([]byte, 32)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			frame := transport.EncodeFrameBare(tc.msgType, tc.payload)
			decoded, err := transport.DecodeFrameBare(frame)
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if decoded.Type != tc.msgType {
				t.Fatalf("type: got %v want %v", decoded.Type, tc.msgType)
			}
			if string(decoded.Payload) != string(tc.payload) {
				t.Fatalf("payload mismatch for %s", tc.name)
			}
		})
	}
}
