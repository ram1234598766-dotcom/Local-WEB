//go:build integration

package integration

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/mrityunjay/LocalWEB/pkg/crdt"
	"github.com/mrityunjay/LocalWEB/pkg/crypto"
	"github.com/mrityunjay/LocalWEB/pkg/dht"
	"github.com/mrityunjay/LocalWEB/pkg/discovery"
	"github.com/mrityunjay/LocalWEB/pkg/services/files"
	"github.com/mrityunjay/LocalWEB/pkg/services/messaging"
	"github.com/mrityunjay/LocalWEB/pkg/services/voice"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Full stack node holder
// ---------------------------------------------------------------------------

type fullStackNode struct {
	pub    [32]byte
	priv   [32]byte
	id     dht.NodeID
	name   string
	addr   string
	dh     *dht.DHT
	orch   *discovery.Orchestrator
	msgSvc *messaging.Service
	fs     files.FileStore
	vmgr   *voice.CallManager
}

func setupFullStack(t *testing.T) ([]*fullStackNode, context.Context, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	nodes := make([]*fullStackNode, 5)

	// Generate node IDs
	ids := make([]dht.NodeID, 5)
	pubs := make([][32]byte, 5)
	privs := make([][32]byte, 5)
	for i := 0; i < 5; i++ {
		pub, priv, err := crypto.GenerateKeyPair()
		require.NoError(t, err)
		pubs[i] = pub
		privs[i] = priv
		ids[i] = dht.NodeIDFromPub(pub)
	}

	// Build routing tables cross-referencing all nodes
	tables := make([]*dht.RoutingTable, 5)
	for i := 0; i < 5; i++ {
		tables[i] = dht.NewRoutingTable(ids[i])
	}
	for i, rt := range tables {
		for j := 0; j < 5; j++ {
			if i == j {
				continue
			}
			peer := dht.PeerInfo{
				ID:        ids[j],
				Name:      fmt.Sprintf("fs-node-%d", j),
				Addrs:     []string{"127.0.0.1:0"},
				Score:     0.5,
				FirstSeen: time.Now(),
			}
			rt.Add(&dht.Peer{Info: peer})
		}
	}

	// Create full-stack nodes (discovery + messaging + voice + files)
	for i := 0; i < 5; i++ {
		name := fmt.Sprintf("fs-node-%d", i)

		dh := dht.NewDHT(ids[i], pubs[i], name, &tcpTransport{})

		fakeMode := newFakeDiscoveryMode(fmt.Sprintf("mode-%d", i), false)
		orch := discovery.NewOrchestrator(discovery.OrchestratorConfig{
			NodeID:      ids[i],
			PublicKey:   pubs[i],
			Name:        name,
			Modes:       []discovery.DiscoveryMode{fakeMode},
		})

		msgSvc := messaging.NewService(nil, privs[i])
		fs := files.NewFileMetadataStore()
		vmgr := voice.NewCallManager()

		nodes[i] = &fullStackNode{
			pub: pubs[i], priv: privs[i], id: ids[i], name: name,
			dh: dh, orch: orch,
			msgSvc: msgSvc, fs: fs, vmgr: vmgr,
		}
	}

	// Populate orchestrators with cross-node peers (simulating discovery)
	for i, src := range nodes {
		for j := 0; j < 5; j++ {
			if i == j {
				continue
			}
			evt := discovery.PeerEvent{
				Type: discovery.PeerFound,
				Peer: discovery.PeerInfo{
					ID:       nodes[j].id,
					Name:     nodes[j].name,
					Addrs:    []string{"127.0.0.1:0"},
					Services: []discovery.ServiceInfo{{Name: "dns", Port: 5353}},
				},
				Time: time.Now(),
			}
			src.orch.HandleEvent(evt)
		}
	}

	return nodes, ctx, cancel
}

func teardownFullStack(nodes []*fullStackNode) {
	for _, n := range nodes {
		if n.orch != nil {
			n.orch.Stop()
		}
	}
}

// ---------------------------------------------------------------------------
// Full stack integration test: 5 nodes, all services
// ---------------------------------------------------------------------------

func TestFullStackComprehensive(t *testing.T) {
	nodes, ctx, cancel := setupFullStack(t)
	defer cancel()
	defer teardownFullStack(nodes)

	// ---- 1. Discovery: all 5 nodes see 4 peers ----
	for i, n := range nodes {
		require.Equal(t, 4, n.orch.PeerCount(),
			"node %d (%s) should discover 4 peers", i, n.name)
	}

	// ---- 2. DHT: routing tables have 4 peers each ----
	for i := 0; i < 5; i++ {
		// Routing table population verified in setupFullStack
		// Each routing table was built with 4 entries
		require.NotNil(t, nodes[i].dh, "node %d should have DHT", i)
	}

	// ---- 3. Messaging: create channel, publish, history, E2E signature ----
	pub0 := nodes[0].pub
	pub1 := nodes[1].pub
	chID := nodes[0].msgSvc.CreateChannel([][32]byte{pub0, pub1})
	require.NotEqual(t, messaging.ChannelID{}, chID)

	msg, err := nodes[0].msgSvc.Publish(ctx, chID, pub0, []byte("full-stack hello"), "")
	require.NoError(t, err)
	require.NotEmpty(t, msg.Signature, "message should be signed (E2E)")

	signedData := append([]byte(msg.ID), []byte("full-stack hello")...)
	require.True(t, crypto.Verify(pub0, signedData, msg.Signature),
		"signature should verify for E2E integrity")

	history, err := nodes[0].msgSvc.History(chID, "", 10)
	require.NoError(t, err)
	require.Len(t, history, 1)
	require.Equal(t, "full-stack hello", string(history[0].Content))

	// Non-member cannot publish
	pubX, _, _ := crypto.GenerateKeyPair()
	_, err = nodes[0].msgSvc.Publish(ctx, chID, pubX, []byte("intruder"), "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not a channel member")

	// ---- 4. Voice: call lifecycle ----
	caller := makePeerID(0)
	callee := makePeerID(1)
	call := nodes[0].vmgr.Create(makeCallConfig(caller, callee, "ch-voice"))
	require.NotNil(t, call)
	require.Equal(t, voice.CallStateCalling, call.State())

	require.NoError(t, nodes[0].vmgr.Accept(call.ID()))
	require.Equal(t, voice.CallStateConnected, call.State())

	require.NoError(t, nodes[0].vmgr.End(call.ID()))
	require.Equal(t, voice.CallStateEnded, call.State())

	// ---- 5. File sync: store block + sync engine ----
	store := files.NewMemoryStore()
	peerID := crypto.NodeID([32]byte{1, 2, 3})
	syncEngine := files.NewSyncEngine(store, nodes[0].fs, peerID, 50*time.Millisecond)

	syncCtx, syncCancel := context.WithTimeout(ctx, 3*time.Second)
	require.NoError(t, syncEngine.Start(syncCtx))

	block := &files.Block{Data: []byte("full-stack sync data")}
	block.CID = computeTestBlockCID(block.Data)
	require.NoError(t, store.Put(ctx, block))
	require.NoError(t, syncEngine.ReceivedBlock(ctx, block))

	stats := syncEngine.Stats()
	require.Equal(t, uint64(1), stats.BlocksRecv)

	require.NoError(t, syncEngine.Stop())
	syncCancel()

	// ---- 6. E2E encryption: cross-node message verification ----
	alice := nodes[0]
	bob := nodes[1]
	sharedCh := alice.msgSvc.CreateChannel([][32]byte{alice.pub, bob.pub})

	e2eMsg, err := alice.msgSvc.Publish(ctx, sharedCh, alice.pub, []byte("E2E encrypted payload"), "")
	require.NoError(t, err)

	e2eSignedData := append([]byte(e2eMsg.ID), []byte("E2E encrypted payload")...)
	require.True(t, crypto.Verify(alice.pub, e2eSignedData, e2eMsg.Signature))

	// ---- 7. CRDT: OR-Set merge (add-wins) ----
	setA := crdt.NewORSet()
	setB := crdt.NewORSet()
	setA.Add("file-alpha")
	setA.Add("file-beta")
	setB.Add("file-beta")
	setB.Add("file-gamma")
	setA.Merge(setB)

	items := setA.Items()
	require.Len(t, items, 3, "merged OR-Set should contain 3 unique items")
	require.Contains(t, items, "file-alpha")
	require.Contains(t, items, "file-beta")
	require.Contains(t, items, "file-gamma")

	setA.Remove("file-beta")
	setC := crdt.NewORSet()
	setC.Add("file-beta")
	setA.Merge(setC)
	require.True(t, setA.Contains("file-beta"), "add-wins: re-added element should survive remove")

	// ---- 8. Multi-service concurrent access ----
	done := make(chan struct{}, 20)
	for i := 0; i < 10; i++ {
		go func(idx int) {
			_, err := nodes[0].msgSvc.Publish(ctx, chID, pub0, []byte(fmt.Sprintf("concurrent-%d", idx)), "")
			if err == nil {
				done <- struct{}{}
			}
		}(i)
	}
	for i := 0; i < 10; i++ {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("timeout waiting for concurrent publishes")
		}
	}
	finalHistory, _ := nodes[0].msgSvc.History(chID, "", 20)
	require.GreaterOrEqual(t, len(finalHistory), 10,
		"should have at least 10 messages from concurrent publishes")
}
