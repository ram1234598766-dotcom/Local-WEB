package files

import (
	"bytes"
	"context"
	"testing"

	"github.com/ipfs/go-cid"
)

func TestMemoryStorePutGet(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	block := &Block{
		Data: []byte("hello world"),
	}
	block.CID = computeBlockCID(block.Data)

	if err := store.Put(ctx, block); err != nil {
		t.Fatalf("put failed: %v", err)
	}

	got, err := store.Get(ctx, block.CID)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if !bytes.Equal(got.Data, block.Data) {
		t.Fatalf("data mismatch: got %q, want %q", got.Data, block.Data)
	}
}

func TestMemoryStoreHas(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	block := &Block{Data: []byte("test data")}
	block.CID = computeBlockCID(block.Data)
	store.Put(ctx, block)

	if !store.Has(ctx, block.CID) {
		t.Fatal("expected block to exist")
	}
	if store.Has(ctx, cid.Cid{}) {
		t.Fatal("expected empty CID to not exist")
	}
}

func TestMemoryStoreDelete(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	block := &Block{Data: []byte("delete me")}
	block.CID = computeBlockCID(block.Data)
	store.Put(ctx, block)

	if err := store.Delete(ctx, block.CID); err != nil {
		t.Fatalf("delete failed: %v", err)
	}
	if store.Has(ctx, block.CID) {
		t.Fatal("expected block to be deleted")
	}
}

func TestMemoryStoreList(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	blocks := []*Block{
		{Data: []byte("block1")},
		{Data: []byte("block2")},
		{Data: []byte("block3")},
	}
	for _, b := range blocks {
		b.CID = computeBlockCID(b.Data)
		store.Put(ctx, b)
	}

	cids, err := store.List(ctx)
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(cids) != 3 {
		t.Fatalf("expected 3 CIDs, got %d", len(cids))
	}
}

func TestMemoryStoreRefCount(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	block := &Block{Data: []byte("refcount")}
	block.CID = computeBlockCID(block.Data)

	store.Put(ctx, block)
	store.Put(ctx, block)

	if err := store.Delete(ctx, block.CID); err != nil {
		t.Fatalf("delete failed: %v", err)
	}
	if !store.Has(ctx, block.CID) {
		t.Fatal("expected block to still exist (refcount > 0)")
	}

	if err := store.Delete(ctx, block.CID); err != nil {
		t.Fatalf("second delete failed: %v", err)
	}
	if store.Has(ctx, block.CID) {
		t.Fatal("expected block to be deleted")
	}
}

func TestMemoryStoreSize(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	block := &Block{Data: []byte("size test")}
	block.CID = computeBlockCID(block.Data)
	store.Put(ctx, block)

	size, err := store.Size(ctx, block.CID)
	if err != nil {
		t.Fatalf("size failed: %v", err)
	}
	if size != int64(len(block.Data)) {
		t.Fatalf("size mismatch: got %d, want %d", size, len(block.Data))
	}
}

func TestMemoryStoreEmptyBlock(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	err := store.Put(ctx, &Block{})
	if err == nil {
		t.Fatal("expected error for empty block")
	}
}

func TestFileStorePutGet(t *testing.T) {
	fs := NewFileMetadataStore()
	ctx := context.Background()

	meta := &FileMeta{
		Name:     "test.txt",
		Size:     11,
		MimeType: "text/plain",
		ACL:      []ACLEntry{{PubKey: [32]byte{1}, Read: true}},
	}
	meta.CID = computeFileCID([]byte("hello world"))

	if err := fs.PutFile(ctx, meta, []byte("hello world")); err != nil {
		t.Fatalf("put file failed: %v", err)
	}

	gotMeta, _, err := fs.GetFile(ctx, meta.CID)
	if err != nil {
		t.Fatalf("get file failed: %v", err)
	}
	if gotMeta.Name != meta.Name {
		t.Fatalf("name mismatch: got %q, want %q", gotMeta.Name, meta.Name)
	}
}

func TestFileStoreList(t *testing.T) {
	fs := NewFileMetadataStore()
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		meta := &FileMeta{
			Name:     string(rune('a' + i)),
			MimeType: "text/plain",
		}
		meta.CID = computeFileCID([]byte(meta.Name))
		fs.PutFile(ctx, meta, []byte(meta.Name))
	}

	files, err := fs.ListFiles(ctx)
	if err != nil {
		t.Fatalf("list files failed: %v", err)
	}
	if len(files) != 3 {
		t.Fatalf("expected 3 files, got %d", len(files))
	}
}

func TestFileStoreDelete(t *testing.T) {
	fs := NewFileMetadataStore()
	ctx := context.Background()

	meta := &FileMeta{Name: "delete_me"}
	meta.CID = computeFileCID([]byte("delete_me"))
	fs.PutFile(ctx, meta, nil)

	if err := fs.DeleteFile(ctx, meta.CID); err != nil {
		t.Fatalf("delete failed: %v", err)
	}
	if _, _, err := fs.GetFile(ctx, meta.CID); err == nil {
		t.Fatal("expected file to be deleted")
	}
}

func TestFileStoreVersion(t *testing.T) {
	fs := NewFileMetadataStore()
	ctx := context.Background()

	parentCID := computeFileCID([]byte("parent"))
	v1 := &FileMeta{Name: "v1", ParentCID: parentCID, Version: 1}
	v1.CID = computeFileCID([]byte("v1 data"))
	v2 := &FileMeta{Name: "v2", ParentCID: parentCID, Version: 2}
	v2.CID = computeFileCID([]byte("v2 data"))

	fs.PutFile(ctx, v1, nil)
	fs.PutFile(ctx, v2, nil)

	latest, err := fs.VersionFile(ctx, parentCID)
	if err != nil {
		t.Fatalf("version failed: %v", err)
	}
	if latest.Name != "v2" {
		t.Fatalf("expected latest version v2, got %s", latest.Name)
	}
}

func TestEncodeDecodeBlock(t *testing.T) {
	original := &Block{Data: []byte("encode test data")}
	original.CID = computeBlockCID(original.Data)

	encoded, err := EncodeBlock(original)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}

	decoded, err := DecodeBlock(encoded)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if decoded.CID != original.CID {
		t.Fatalf("CID mismatch: got %s, want %s", decoded.CID, original.CID)
	}
	if !bytes.Equal(decoded.Data, original.Data) {
		t.Fatalf("data mismatch")
	}
}

func TestCompressDecompressBlock(t *testing.T) {
	data := bytes.Repeat([]byte("compressible data "), 1000)

	compressed, err := compressBlock(data)
	if err != nil {
		t.Fatalf("compress failed: %v", err)
	}

	decompressed, err := decompressBlock(compressed)
	if err != nil {
		t.Fatalf("decompress failed: %v", err)
	}

	if !bytes.Equal(decompressed, data) {
		t.Fatal("decompressed data does not match original")
	}
}

func TestIsZstd(t *testing.T) {
	if !isZstd([]byte{0x28, 0xB5, 0x2F, 0xFD, 0x00}) {
		t.Fatal("expected zstd magic bytes to be detected")
	}
	if isZstd([]byte{0x00, 0x00, 0x00, 0x00, 0x00}) {
		t.Fatal("expected non-zstd bytes to not be detected")
	}
}

func TestComputeBlockCID(t *testing.T) {
	data := []byte("test cid")
	c := computeBlockCID(data)
	if c == (cid.Cid{}) {
		t.Fatal("expected non-empty CID")
	}
}

func TestFileStoreStat(t *testing.T) {
	fs := NewFileMetadataStore()
	ctx := context.Background()

	meta := &FileMeta{Name: "stat_test"}
	meta.CID = computeFileCID([]byte("stat_test"))
	fs.PutFile(ctx, meta, nil)

	got, err := fs.StatFile(ctx, meta.CID)
	if err != nil {
		t.Fatalf("stat failed: %v", err)
	}
	if got.Name != meta.Name {
		t.Fatalf("name mismatch: got %q, want %q", got.Name, meta.Name)
	}
}
