package files

import (
	"context"
	"testing"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/crypto"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/transport"
)

// exchangePair is two real Local-WEB nodes, each with its own transport server
// and block store, connected over loopback QUIC.
type exchangePair struct {
	serverA, serverB *transport.Server
	exchangeA        ExchangeProtocol
	exchangeB        ExchangeProtocol
	storeA, storeB   BlockStore
	idA, idB         [32]byte
}

func newExchangePair(t *testing.T) *exchangePair {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	pubA, privA, err := crypto.GenerateX25519KeyPair()
	if err != nil {
		t.Fatalf("keypair A: %v", err)
	}
	pubB, privB, err := crypto.GenerateX25519KeyPair()
	if err != nil {
		t.Fatalf("keypair B: %v", err)
	}
	idA := crypto.NodeID(pubA)
	idB := crypto.NodeID(pubB)

	srvA, err := transport.NewServer(ctx, "127.0.0.1:0", pubA, privA)
	if err != nil {
		t.Fatalf("server A: %v", err)
	}
	t.Cleanup(srvA.Stop)
	srvB, err := transport.NewServer(ctx, "127.0.0.1:0", pubB, privB)
	if err != nil {
		t.Fatalf("server B: %v", err)
	}
	t.Cleanup(srvB.Stop)

	storeA := NewMemoryStore()
	storeB := NewMemoryStore()
	t.Cleanup(func() { _ = storeA.Close(); _ = storeB.Close() })

	pair := &exchangePair{
		serverA:   srvA,
		serverB:   srvB,
		exchangeA: NewExchangeProtocol(srvA, storeA, nil, idA),
		exchangeB: NewExchangeProtocol(srvB, storeB, nil, idB),
		storeA:    storeA,
		storeB:    storeB,
		idA:       idA,
		idB:       idB,
	}

	if _, err := srvA.Connect(ctx, srvB.Addr(), idB); err != nil {
		t.Fatalf("A connect B: %v", err)
	}
	// The reply direction needs the connection visible from B's side too.
	waitForPeer(t, srvB, idA)
	return pair
}

func waitForPeer(t *testing.T, srv *transport.Server, want [32]byte) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		for _, p := range srv.Peers() {
			if p.ID == want {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("peer %x never appeared", want[:8])
}

// collectBlocks records every block the node receives through the exchange.
func collectBlocks(t *testing.T, ep ExchangeProtocol) <-chan *Block {
	t.Helper()
	out := make(chan *Block, 64)
	ep.SetHandler(func(ctx context.Context, peer [32]byte, msg *ExchangeMessage) error {
		if msg.Type != MsgBlock {
			return nil
		}
		cp := make([]byte, len(msg.Data))
		copy(cp, msg.Data)
		out <- &Block{CID: msg.CID, Data: cp}
		return nil
	})
	return out
}

// TestExchangeWantRoundTripOverTransport is the test the Files service never
// had: a node asks a real peer over a real QUIC stream for a block the peer
// holds, and the block comes back with its content intact.
//
// Before this, NewExchangeProtocol had no production caller at all, so the
// entire want/have/block protocol was dead code, and its CID encoding decoded
// to the single-byte CID "b" for every entry.
func TestExchangeWantRoundTripOverTransport(t *testing.T) {
	pair := newExchangePair(t)

	payload := []byte("the block the requester does not have")
	block := &Block{Data: payload, CID: computeBlockCID(payload)}
	if err := pair.storeB.Put(context.Background(), block); err != nil {
		t.Fatalf("seed B: %v", err)
	}
	if pair.storeA.Has(context.Background(), block.CID) {
		t.Fatal("A should not already have the block")
	}

	received := collectBlocks(t, pair.exchangeA)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := pair.exchangeA.SendWant(ctx, pair.idB, []WantEntry{
		{CID: block.CID, Type: WantWant, Priority: 1},
	}); err != nil {
		t.Fatalf("SendWant: %v", err)
	}

	select {
	case got := <-received:
		if got.CID != block.CID {
			t.Fatalf("received CID %s, want %s", got.CID, block.CID)
		}
		if string(got.Data) != string(payload) {
			t.Fatalf("received %q, want %q", got.Data, payload)
		}
	case <-time.After(30 * time.Second):
		// Diagnose where the chain stopped rather than just timing out.
		epB, ok := pair.exchangeB.(*exchangeProtocol)
		if !ok {
			t.Fatalf("unexpected exchange type %T", pair.exchangeB)
		}
		epB.mu.RLock()
		peer := epB.peers[pair.idA]
		var wantCount, served, received int
		var replyErr error
		if peer != nil {
			wantCount = len(peer.want)
			served = peer.blocksServed
			received = peer.blocksReceived
			replyErr = peer.lastErr
		}
		epB.mu.RUnlock()
		stored, getErr := pair.storeB.Get(context.Background(), block.CID)
		t.Fatalf("the block never came back: B recorded %d wants, served %d, received %d, replyErr=%v, B has the block=%v (err %v)",
			wantCount, served, received, replyErr, stored != nil, getErr)
	}
}

// TestExchangeIgnoresWantsItCannotServe proves a request for a block nobody has
// is answered with silence rather than an error or a wrong block.
func TestExchangeIgnoresWantsItCannotServe(t *testing.T) {
	pair := newExchangePair(t)

	received := collectBlocks(t, pair.exchangeA)
	missing := &Block{Data: []byte("nobody has this"), CID: computeBlockCID([]byte("nobody has this"))}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := pair.exchangeA.SendWant(ctx, pair.idB, []WantEntry{
		{CID: missing.CID, Type: WantWant, Priority: 1},
	}); err != nil {
		t.Fatalf("SendWant: %v", err)
	}

	select {
	case got := <-received:
		t.Fatalf("B sent a block for a CID it does not have: %s", got.CID)
	case <-time.After(1500 * time.Millisecond):
	}
}

// TestExchangeRejectsOversizedPayload feeds a peer a length prefix claiming far
// more than the message can carry, which used to become a direct allocation of
// whatever the peer asked for.
func TestExchangeRejectsOversizedPayload(t *testing.T) {
	pair := newExchangePair(t)

	received := collectBlocks(t, pair.exchangeA)

	// MsgBlock with a payload length of 0xFFFFFFF0.
	hdr := []byte{byte(MsgBlock), 0xFF, 0xFF, 0xFF, 0xF0}
	stream, err := pair.exchangeA.OpenStream(context.Background(), pair.idB)
	if err != nil {
		t.Fatalf("OpenStream: %v", err)
	}
	if _, err := stream.Write(hdr); err != nil {
		t.Fatalf("write header: %v", err)
	}

	select {
	case got := <-received:
		t.Fatalf("an oversized frame produced a block: %s", got.CID)
	case <-time.After(1500 * time.Millisecond):
	}
}

// TestSyncEngineAcquiresWantedBlock drives the whole loop the daemon will use:
// B advertises what it has, A diffs its own store against that, asks for the
// difference, and the block lands in A's store.
func TestSyncEngineAcquiresWantedBlock(t *testing.T) {
	pair := newExchangePair(t)

	payload := []byte("content A must fetch from B")
	block := &Block{Data: payload, CID: computeBlockCID(payload)}
	if err := pair.storeB.Put(context.Background(), block); err != nil {
		t.Fatalf("seed B: %v", err)
	}

	engine := NewSyncEngine(pair.storeA, nil, pair.idA, time.Minute)
	sync, ok := engine.(*syncEngine)
	if !ok {
		t.Fatalf("unexpected engine type %T", engine)
	}
	sync.exchange = pair.exchangeA
	// Close the loop: a block that arrives has to reach the engine, which is
	// exactly what the daemon's wiring does.
	pair.exchangeA.SetHandler(sync.HandleBlock)

	// What B holds, in the shape the engine records.
	sync.recordPeerHave(pair.idB, []cid.Cid{block.CID})

	if err := engine.Sync(context.Background(), pair.idB); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if pair.storeA.Has(context.Background(), block.CID) {
			got, err := pair.storeA.Get(context.Background(), block.CID)
			if err != nil {
				t.Fatalf("A get: %v", err)
			}
			if string(got.Data) != string(payload) {
				t.Fatalf("A stored %q, want %q", got.Data, payload)
			}
			stats := engine.Stats()
			if stats.BlocksRecv == 0 {
				t.Error("SyncStats.BlocksRecv did not move")
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("A never acquired the block it asked for")
}
