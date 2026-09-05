package store

import (
	"context"
	"fmt"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/mrityunjay/LocalWEB/pkg/services/files"
	mh "github.com/multiformats/go-multihash"
)

// BadgerBlockStore implements files.BlockStore using BadgerDB.
// Blocks are content-addressed by their CID.
type BadgerBlockStore struct {
	store *Store
}

// NewBadgerBlockStore creates a block store backed by the given Store.
func NewBadgerBlockStore(store *Store) *BadgerBlockStore {
	return &BadgerBlockStore{store: store}
}

const (
	blockPrefix = "block"
	metaPrefix  = "meta"
)

// Put stores a block. The block's CID must match its data (content-addressed).
func (bs *BadgerBlockStore) Put(ctx context.Context, block *files.Block) error {
	if block == nil || len(block.Data) == 0 {
		return fmt.Errorf("block is empty")
	}

	expectedCID := computeBlockCID(block.Data)
	if expectedCID != block.CID {
		return fmt.Errorf("CID mismatch: expected %s, got %s", expectedCID, block.CID)
	}

	blockKey := bs.store.nsKey(blockPrefix, block.CID.String())
	metaKey := bs.store.nsKey(metaPrefix, block.CID.String())

	meta := files.BlockMeta{
		CID:        block.CID,
		Size:       int64(len(block.Data)),
		Compressed: isZstd(block.Data),
		Created:    time.Now(),
		RefCount:   1,
	}

	metaData, err := encodeGob(meta)
	if err != nil {
		return fmt.Errorf("encode block meta: %w", err)
	}

	entries := []Entry{
		{Key: blockKey, Value: block.Data},
		{Key: metaKey, Value: metaData},
	}

	return bs.store.Batch(ctx, entries)
}

// Get retrieves a block by CID.
func (bs *BadgerBlockStore) Get(ctx context.Context, c cid.Cid) (*files.Block, error) {
	blockKey := bs.store.nsKey(blockPrefix, c.String())
	data, err := bs.store.Get(ctx, blockKey)
	if err != nil {
		return nil, fmt.Errorf("block not found: %s", c)
	}

	expectedCID := computeBlockCID(data)
	if expectedCID != c {
		return nil, fmt.Errorf("stored data CID mismatch for %s", c)
	}

	return &files.Block{CID: c, Data: data}, nil
}

// Has checks if a block exists.
func (bs *BadgerBlockStore) Has(ctx context.Context, c cid.Cid) bool {
	blockKey := bs.store.nsKey(blockPrefix, c.String())
	exists, _ := bs.store.Has(ctx, blockKey)
	return exists
}

// Delete removes a block and its metadata.
func (bs *BadgerBlockStore) Delete(ctx context.Context, c cid.Cid) error {
	blockKey := bs.store.nsKey(blockPrefix, c.String())
	metaKey := bs.store.nsKey(metaPrefix, c.String())

	entries := []Entry{
		{Key: blockKey},
		{Key: metaKey},
	}
	return bs.store.Batch(ctx, entries)
}

// List returns all stored CIDs.
func (bs *BadgerBlockStore) List(ctx context.Context) ([]cid.Cid, error) {
	prefix := bs.store.nsKey(blockPrefix)
	var cids []cid.Cid

	err := bs.store.Iterator(ctx, prefix, func(k, v []byte) error {
		c, err := cid.Decode(string(k[len(prefix)+1:])) // skip "LWS:block:"
		if err != nil {
			return fmt.Errorf("decode CID from key: %w", err)
		}
		cids = append(cids, c)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list blocks: %w", err)
	}
	return cids, nil
}

// Size returns the size of a block by CID.
func (bs *BadgerBlockStore) Size(ctx context.Context, c cid.Cid) (int64, error) {
	metaKey := bs.store.nsKey(metaPrefix, c.String())
	data, err := bs.store.Get(ctx, metaKey)
	if err != nil {
		return 0, fmt.Errorf("block not found: %s", c)
	}

	var meta files.BlockMeta
	if err := decodeGob(data, &meta); err != nil {
		return 0, fmt.Errorf("decode block meta: %w", err)
	}
	return meta.Size, nil
}

// Close closes the underlying store.
func (bs *BadgerBlockStore) Close() error {
	return bs.store.Close()
}

// computeBlockCID computes the CID for raw block data.
func computeBlockCID(data []byte) cid.Cid {
	prefix := cid.NewPrefixV1(cid.Raw, mh.SHA2_256)
	c, _ := prefix.Sum(data)
	return c
}

// isZstd detects Zstd compression magic bytes.
func isZstd(data []byte) bool {
	return len(data) >= 4 && data[0] == 0x28 && data[1] == 0xB5 && data[2] == 0x2F && data[3] == 0xFD
}
