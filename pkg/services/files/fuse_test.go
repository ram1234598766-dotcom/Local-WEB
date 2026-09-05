package files

import (
	"context"
	"testing"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/mrityunjay/LocalWEB/pkg/crypto"
)

func TestFuseMount(t *testing.T) {
	fs := NewFS(NewMemoryStore(), NewFileMetadataStore())
	ctx := context.Background()

	if err := fs.Mount(ctx, "/tmp/localweb-test"); err != nil {
		t.Fatalf("mount failed: %v", err)
	}
	if !fs.Mounted() {
		t.Fatal("expected fs to be mounted")
	}

	if err := fs.Unmount(ctx); err != nil {
		t.Fatalf("unmount failed: %v", err)
	}
	if fs.Mounted() {
		t.Fatal("expected fs to be unmounted")
	}
}

func TestFuseDoubleMount(t *testing.T) {
	fs := NewFS(NewMemoryStore(), NewFileMetadataStore())
	ctx := context.Background()

	if err := fs.Mount(ctx, "/tmp/localweb-test"); err != nil {
		t.Fatalf("first mount failed: %v", err)
	}
	err := fs.Mount(ctx, "/tmp/localweb-test2")
	if err == nil {
		t.Fatal("expected error on double mount")
	}

	fs.Unmount(ctx)
}

func TestFuseUnmountWithoutMount(t *testing.T) {
	fs := NewFS(NewMemoryStore(), NewFileMetadataStore())
	ctx := context.Background()

	err := fs.Unmount(ctx)
	if err != nil {
		t.Fatalf("unmount without mount should not error: %v", err)
	}
}

func TestMountInfo(t *testing.T) {
	store := NewMemoryStore()
	fileStore := NewFileMetadataStore()
	fs := NewFS(store, fileStore)
	ctx := context.Background()

	fs.Mount(ctx, "/tmp/localweb-test")
	info := fs.GetMountInfo()

	if !info.Mounted {
		t.Fatal("expected mounted=true in info")
	}
	if info.Path != "/tmp/localweb-test" {
		t.Fatalf("path mismatch: got %s", info.Path)
	}
	if info.Store != store {
		t.Fatal("store mismatch")
	}
	if info.FileStore != fileStore {
		t.Fatal("file store mismatch")
	}
	if info.Uptime == 0 {
		time.Sleep(10 * time.Millisecond)
		info = fs.GetMountInfo()
		if info.Uptime == 0 {
			t.Fatal("expected non-zero uptime")
		}
	}

	fs.Unmount(ctx)
	info = fs.GetMountInfo()
	if info.Mounted {
		t.Fatal("expected mounted=false after unmount")
	}
}

func TestFuseMountInfoPeerID(t *testing.T) {
	store := NewMemoryStore()
	fileStore := NewFileMetadataStore()
	fs := NewFS(store, fileStore)
	ctx := context.Background()

	fs.Mount(ctx, "/tmp/localweb-test")
	info := fs.GetMountInfo()

	if info.PeerID.ID == ([32]byte{}) {
		t.Fatal("expected non-zero peer ID in info")
	}

	fs.Unmount(ctx)
}

func TestFuseInterfaceCompliance(t *testing.T) {
	var fs FS = NewFS(NewMemoryStore(), NewFileMetadataStore())
	ctx := context.Background()

	if err := fs.Mount(ctx, "/tmp/test"); err != nil {
		t.Fatalf("mount failed: %v", err)
	}
	if !fs.Mounted() {
		t.Fatal("expected mounted")
	}
	if err := fs.Unmount(ctx); err != nil {
		t.Fatalf("unmount failed: %v", err)
	}
}

func TestSyncProgress(t *testing.T) {
	p := SyncProgress{
		PeerID:   crypto.NodeID([32]byte{10}),
		Complete: false,
	}
	if p.Complete {
		t.Fatal("expected Complete=false")
	}
	p.Complete = true
	if !p.Complete {
		t.Fatal("expected Complete=true")
	}
}

func TestFileMetaACL(t *testing.T) {
	meta := FileMeta{
		Name: "secret.txt",
		ACL: []ACLEntry{
			{PubKey: [32]byte{1}, Read: true, Write: false, Admin: false},
			{PubKey: [32]byte{2}, Read: true, Write: true, Admin: true},
		},
	}
	if !meta.ACL[0].Read {
		t.Fatal("expected Read=true")
	}
	if meta.ACL[0].Write {
		t.Fatal("expected Write=false")
	}
	if !meta.ACL[1].Admin {
		t.Fatal("expected Admin=true")
	}
}

func TestMerkleNodeFields(t *testing.T) {
	c := computeFileCID([]byte("node"))
	node := MerkleNode{
		CID:      c,
		Parent:   c,
		Height:   3,
		FileCIDs: []cid.Cid{c},
	}
	if node.Height != 3 {
		t.Fatalf("Height mismatch: got %d", node.Height)
	}
	if len(node.FileCIDs) != 1 {
		t.Fatalf("expected 1 file CID, got %d", len(node.FileCIDs))
	}
}

func TestBlockSizeConstants(t *testing.T) {
	if BlockSize != (4 << 20) {
		t.Fatalf("BlockSize should be 4MiB, got %d", BlockSize)
	}
	if CompressionLevel != 3 {
		t.Fatalf("CompressionLevel should be 3, got %d", CompressionLevel)
	}
}

func TestFuseMountedInitiallyFalse(t *testing.T) {
	fs := NewFS(NewMemoryStore(), NewFileMetadataStore())
	if fs.Mounted() {
		t.Fatal("expected not mounted initially")
	}
}

func TestMountInfoBeforeMount(t *testing.T) {
	fs := NewFS(NewMemoryStore(), NewFileMetadataStore())
	info := fs.GetMountInfo()
	if info.Mounted {
		t.Fatal("expected not mounted before Mount()")
	}
	if info.Path != "" {
		t.Fatal("expected empty path before Mount()")
	}
}

func TestFSClose(t *testing.T) {
	fs := NewFS(NewMemoryStore(), NewFileMetadataStore())
	ctx := context.Background()
	fs.Mount(ctx, "/tmp/test")
	fs.Unmount(ctx)

	// Verify clean unmount
	if fs.Mounted() {
		t.Fatal("expected unmounted after Unmount()")
	}
}

func TestBlockStoreClose(t *testing.T) {
	store := NewMemoryStore()
	err := store.Close()
	if err != nil {
		t.Fatalf("Close should not error: %v", err)
	}
}
