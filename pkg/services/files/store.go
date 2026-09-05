package files

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/klauspost/compress/zstd"
	mh "github.com/multiformats/go-multihash"
)

// memoryStore is a simple in-memory block store for testing and lightweight use.
type memoryStore struct {
	mu      sync.RWMutex
	blocks  map[cid.Cid]*Block
	meta    map[cid.Cid]*BlockMeta
	baseDir string
}

// NewMemoryStore creates an in-memory block store.
func NewMemoryStore() BlockStore {
	return &memoryStore{
		blocks: make(map[cid.Cid]*Block),
		meta:   make(map[cid.Cid]*BlockMeta),
	}
}

// NewFileStore creates a file-backed block store.
func NewFileStore(baseDir string) (BlockStore, error) {
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, fmt.Errorf("create store dir: %w", err)
	}
	s := &memoryStore{
		blocks:  make(map[cid.Cid]*Block),
		meta:    make(map[cid.Cid]*BlockMeta),
		baseDir: baseDir,
	}
	if err := s.loadExisting(); err != nil {
		return nil, fmt.Errorf("load existing blocks: %w", err)
	}
	return s, nil
}

func (s *memoryStore) loadExisting() error {
	if s.baseDir == "" {
		return nil
	}
	entries, err := os.ReadDir(s.baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".block" {
			continue
		}
		c, err := cid.Decode(entry.Name()[:len(entry.Name())-6])
		if err != nil {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.baseDir, entry.Name()))
		if err != nil {
			continue
		}
		s.blocks[c] = &Block{CID: c, Data: data}
		s.meta[c] = &BlockMeta{
			CID:     c,
			Size:    int64(len(data)),
			Created: time.Now(),
		}
	}
	return nil
}

func (s *memoryStore) Put(ctx context.Context, block *Block) error {
	if block == nil || len(block.Data) == 0 {
		return errors.New("block is empty")
	}
	if block.Data == nil || len(block.Data) == 0 {
		return errors.New("block data is empty")
	}
	expectedCID := computeBlockCID(block.Data)
	if expectedCID != block.CID {
		return fmt.Errorf("CID mismatch: expected %s, got %s", expectedCID, block.CID)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if meta, exists := s.meta[block.CID]; exists {
		meta.RefCount++
		return nil
	}

	meta := &BlockMeta{
		CID:        block.CID,
		Size:       int64(len(block.Data)),
		Compressed: isZstd(block.Data),
		Created:    time.Now(),
		RefCount:   1,
	}
	s.blocks[block.CID] = &Block{CID: block.CID, Data: append([]byte{}, block.Data...)}
	s.meta[block.CID] = meta

	if s.baseDir != "" {
		path := filepath.Join(s.baseDir, block.CID.String()+".block")
		if err := os.WriteFile(path, block.Data, 0644); err != nil {
			delete(s.blocks, block.CID)
			delete(s.meta, block.CID)
			return fmt.Errorf("write block to disk: %w", err)
		}
	}
	return nil
}

func (s *memoryStore) Get(ctx context.Context, c cid.Cid) (*Block, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	block, ok := s.blocks[c]
	if !ok {
		return nil, fmt.Errorf("block not found: %s", c)
	}
	return &Block{CID: c, Data: append([]byte{}, block.Data...)}, nil
}

func (s *memoryStore) Has(ctx context.Context, c cid.Cid) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.blocks[c]
	return ok
}

func (s *memoryStore) Delete(ctx context.Context, c cid.Cid) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if meta, ok := s.meta[c]; ok {
		meta.RefCount--
		if meta.RefCount > 0 {
			return nil
		}
	}
	delete(s.blocks, c)
	delete(s.meta, c)
	if s.baseDir != "" {
		path := filepath.Join(s.baseDir, c.String()+".block")
		os.Remove(path)
	}
	return nil
}

func (s *memoryStore) List(ctx context.Context) ([]cid.Cid, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]cid.Cid, 0, len(s.blocks))
	for c := range s.blocks {
		out = append(out, c)
	}
	return out, nil
}

func (s *memoryStore) Size(ctx context.Context, c cid.Cid) (int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	meta, ok := s.meta[c]
	if !ok {
		return 0, fmt.Errorf("block not found: %s", c)
	}
	return meta.Size, nil
}

func (s *memoryStore) Close() error {
	return nil
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

// compressBlock compresses data with Zstd.
func compressBlock(data []byte) ([]byte, error) {
	encoder, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.EncoderLevelFromZstd(CompressionLevel)))
	if err != nil {
		return nil, fmt.Errorf("create zstd encoder: %w", err)
	}
	defer encoder.Close()
	return encoder.EncodeAll(data, nil), nil
}

// decompressBlock decompresses Zstd data.
func decompressBlock(data []byte) ([]byte, error) {
	decoder, err := zstd.NewReader(nil)
	if err != nil {
		return nil, fmt.Errorf("create zstd decoder: %w", err)
	}
	defer decoder.Close()
	out, err := decoder.DecodeAll(data, nil)
	if err != nil {
		return nil, fmt.Errorf("decompress block: %w", err)
	}
	return out, nil
}

// FileStore is the default file metadata store.
type fileStore struct {
	mu       sync.RWMutex
	files    map[cid.Cid]*FileMeta
	versions map[cid.Cid][]*FileMeta
}

// NewFileStore creates a new file metadata store.
func NewFileMetadataStore() FileStore {
	return &fileStore{
		files:    make(map[cid.Cid]*FileMeta),
		versions: make(map[cid.Cid][]*FileMeta),
	}
}

func (f *fileStore) PutFile(ctx context.Context, meta *FileMeta, data []byte) error {
	if meta == nil {
		return errors.New("metadata is nil")
	}
	if meta.CID == (cid.Cid{}) {
		return errors.New("CID is empty")
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	if existing, ok := f.files[meta.CID]; ok && existing.Version >= meta.Version {
		return nil
	}

	f.files[meta.CID] = &FileMeta{
		CID:       meta.CID,
		Name:      meta.Name,
		Size:      meta.Size,
		MimeType:  meta.MimeType,
		Modified:  time.Now(),
		Created:   meta.Created,
		ACL:       append([]ACLEntry{}, meta.ACL...),
		Version:   meta.Version,
		ParentCID: meta.ParentCID,
	}
	if meta.ParentCID != (cid.Cid{}) {
		f.versions[meta.ParentCID] = append(f.versions[meta.ParentCID], f.files[meta.CID])
	}
	return nil
}

func (f *fileStore) GetFile(ctx context.Context, c cid.Cid) (*FileMeta, []byte, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	meta, ok := f.files[c]
	if !ok {
		return nil, nil, fmt.Errorf("file not found: %s", c)
	}
	m := *meta
	return &m, nil, nil
}

func (f *fileStore) ListFiles(ctx context.Context) ([]*FileMeta, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	out := make([]*FileMeta, 0, len(f.files))
	for _, m := range f.files {
		cp := *m
		out = append(out, &cp)
	}
	return out, nil
}

func (f *fileStore) StatFile(ctx context.Context, c cid.Cid) (*FileMeta, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	meta, ok := f.files[c]
	if !ok {
		return nil, fmt.Errorf("file not found: %s", c)
	}
	cp := *meta
	return &cp, nil
}

func (f *fileStore) DeleteFile(ctx context.Context, c cid.Cid) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.files[c]; !ok {
		return fmt.Errorf("file not found: %s", c)
	}
	delete(f.files, c)
	return nil
}

func (f *fileStore) VersionFile(ctx context.Context, c cid.Cid) (*FileMeta, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	versions, ok := f.versions[c]
	if !ok || len(versions) == 0 {
		return nil, fmt.Errorf("no versions for: %s", c)
	}
	latest := versions[len(versions)-1]
	cp := *latest
	return &cp, nil
}

// EncodeBlock serializes a block for wire transfer with a 4-byte length prefix.
func EncodeBlock(block *Block) ([]byte, error) {
	if block == nil {
		return nil, errors.New("block is nil")
	}
	buf := make([]byte, 4+len(block.Data))
	binary.BigEndian.PutUint32(buf[:4], uint32(len(block.Data)))
	copy(buf[4:], block.Data)
	return buf, nil
}

// DecodeBlock deserializes a block from wire format.
func DecodeBlock(data []byte) (*Block, error) {
	if len(data) < 4 {
		return nil, errors.New("data too short for block header")
	}
	length := binary.BigEndian.Uint32(data[:4])
	if uint64(len(data)) < 4+uint64(length) {
		return nil, fmt.Errorf("data truncated: expected %d, got %d", 4+length, len(data))
	}
	blockData := data[4 : 4+length]
	c := computeBlockCID(blockData)
	return &Block{CID: c, Data: blockData}, nil
}

// ReadFull reads exactly len(buf) bytes from r.
func ReadFull(r io.Reader, buf []byte) error {
	_, err := io.ReadFull(r, buf)
	return err
}
