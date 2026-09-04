package files

import (
	"context"
	"testing"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/mrityunjay/LocalWEB/pkg/crypto"
)

func TestSyncEngineStartStop(t *testing.T) {
	store := NewMemoryStore()
	fs := NewFileMetadataStore()
	peerID := crypto.NodeID([32]byte{1, 2, 3})

	engine := NewSyncEngine(store, fs, peerID, 100*time.Millisecond)
	ctx := context.Background()

	if err := engine.Start(ctx); err != nil {
		t.Fatalf("start failed: %v", err)
	}
	if err := engine.Stop(); err != nil {
		t.Fatalf("stop failed: %v", err)
	}
}

func TestSyncEngineWantList(t *testing.T) {
	store := NewMemoryStore()
	fs := NewFileMetadataStore()
	peerID := crypto.NodeID([32]byte{1, 2, 3})

	engine := NewSyncEngine(store, fs, peerID, 100*time.Millisecond)
	ctx := context.Background()

	block := &Block{Data: []byte("want list test")}
	block.CID = computeBlockCID(block.Data)
	store.Put(ctx, block)

	wantList, err := engine.WantList(ctx, peerID)
	if err != nil {
		t.Fatalf("want list failed: %v", err)
	}
	// WantList returns blocks we want from a peer; initially empty
	// because no peer state has been established.
	if len(wantList) != 0 {
		t.Fatalf("expected 0 want entries initially, got %d", len(wantList))
	}
}

func TestSyncEngineReceivedBlock(t *testing.T) {
	store := NewMemoryStore()
	fs := NewFileMetadataStore()
	peerID := crypto.NodeID([32]byte{1, 2, 3})

	engine := NewSyncEngine(store, fs, peerID, 100*time.Millisecond)
	ctx := context.Background()

	block := &Block{Data: []byte("received block")}
	block.CID = computeBlockCID(block.Data)

	if err := engine.ReceivedBlock(ctx, block); err != nil {
		t.Fatalf("received block failed: %v", err)
	}
	if !store.Has(ctx, block.CID) {
		t.Fatal("expected block to be in store")
	}
}

func TestSyncEnginePeers(t *testing.T) {
	store := NewMemoryStore()
	fs := NewFileMetadataStore()
	peerID := crypto.NodeID([32]byte{1, 2, 3})

	engine := NewSyncEngine(store, fs, peerID, 100*time.Millisecond)
	ctx := context.Background()

	if err := engine.Start(ctx); err != nil {
		t.Fatalf("start failed: %v", err)
	}
	defer engine.Stop()

	peers := engine.Peers()
	if len(peers) != 0 {
		t.Fatalf("expected 0 peers initially, got %d", len(peers))
	}
}

func TestSyncEngineStats(t *testing.T) {
	store := NewMemoryStore()
	fs := NewFileMetadataStore()
	peerID := crypto.NodeID([32]byte{1, 2, 3})

	engine := NewSyncEngine(store, fs, peerID, 100*time.Millisecond)
	ctx := context.Background()

	stats := engine.Stats()
	if stats.TotalSyncs != 0 {
		t.Fatalf("expected 0 total syncs, got %d", stats.TotalSyncs)
	}

	block := &Block{Data: []byte("stats test")}
	block.CID = computeBlockCID(block.Data)
	engine.ReceivedBlock(ctx, block)

	stats = engine.Stats()
	if stats.BlocksRecv != 1 {
		t.Fatalf("expected 1 block received, got %d", stats.BlocksRecv)
	}
}

func TestComputeFileCID(t *testing.T) {
	data := []byte("file content")
	c := computeFileCID(data)
	if c == (cid.Cid{}) {
		t.Fatal("expected non-empty CID")
	}
}

func TestComputeMerkleRoot(t *testing.T) {
	c1 := computeFileCID([]byte("file1"))
	c2 := computeFileCID([]byte("file2"))
	c3 := computeFileCID([]byte("file3"))

	root := computeMerkleRoot([]cid.Cid{c1, c2, c3})
	if root == (cid.Cid{}) {
		t.Fatal("expected non-empty merkle root")
	}

	// Same files in different order should produce same root (for unordered)
	root2 := computeMerkleRoot([]cid.Cid{c3, c1, c2})
	if root != root2 {
		t.Fatal("expected same root for same files in different order")
	}
}

func TestDiffCIDs(t *testing.T) {
	c1 := computeFileCID([]byte("a"))
	c2 := computeFileCID([]byte("b"))
	c3 := computeFileCID([]byte("c"))

	a := []cid.Cid{c1, c2, c3}
	b := []cid.Cid{c2}

	diff := diffCIDs(a, b)
	if len(diff) != 2 {
		t.Fatalf("expected 2 diff items, got %d", len(diff))
	}
	if diff[0] != c1 || diff[1] != c3 {
		t.Fatal("diff items mismatch")
	}
}

func TestMerkleRootEmpty(t *testing.T) {
	root := computeMerkleRoot(nil)
	if root != (cid.Cid{}) {
		t.Fatal("expected empty CID for empty input")
	}

	root = computeMerkleRoot([]cid.Cid{})
	if root != (cid.Cid{}) {
		t.Fatal("expected empty CID for empty input")
	}
}

func TestMerkleRootSingle(t *testing.T) {
	c := computeFileCID([]byte("single"))
	root := computeMerkleRoot([]cid.Cid{c})
	if root != c {
		t.Fatal("expected root to equal single input CID")
	}
}
