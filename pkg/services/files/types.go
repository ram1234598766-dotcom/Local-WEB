package files

import (
	"context"
	"time"

	"github.com/ipfs/go-cid"
)

// BlockSize is the fixed block size for content-addressed chunking (4 MiB).
const BlockSize = 4 << 20

// CompressionLevel is the Zstd compression level (3 = balanced speed/ratio).
const CompressionLevel = 3

// Block is the fundamental unit of content-addressed storage.
type Block struct {
	CID  cid.Cid
	Data []byte
}

// BlockMeta contains metadata about a stored block.
type BlockMeta struct {
	CID        cid.Cid
	Size       int64
	Compressed bool
	Created    time.Time
	RefCount   int64
}

// FileMeta contains metadata about a file in the store.
type FileMeta struct {
	CID       cid.Cid
	Name      string
	Size      int64
	MimeType  string
	Modified  time.Time
	Created   time.Time
	ACL       []ACLEntry
	Version   int64
	ParentCID cid.Cid
}

// ACLEntry defines access control for a file by public key.
type ACLEntry struct {
	PubKey [32]byte
	Read   bool
	Write  bool
	Admin  bool
}

// MerkleNode represents a node in the file Merkle DAG.
type MerkleNode struct {
	CID      cid.Cid
	Parent   cid.Cid
	Children []cid.Cid
	Height   int
	FileCIDs []cid.Cid
}

// SyncProgress tracks synchronization progress with a peer.
type SyncProgress struct {
	PeerID    [32]byte
	Have      []cid.Cid
	Need      []cid.Cid
	InFlight  []cid.Cid
	Complete  bool
	BytesSent uint64
	BytesRecv uint64
}

// WantType indicates whether a peer has or wants a block.
type WantType uint8

const (
	WantHave WantType = 0x01
	WantWant WantType = 0x02
)

// WantEntry is a want/have advertisement in the exchange protocol.
type WantEntry struct {
	CID      cid.Cid
	Type     WantType
	Priority uint8
}

// BlockStore persists blocks on disk with CID addressing.
type BlockStore interface {
	Put(ctx context.Context, block *Block) error
	Get(ctx context.Context, cid cid.Cid) (*Block, error)
	Has(ctx context.Context, cid cid.Cid) bool
	Delete(ctx context.Context, cid cid.Cid) error
	List(ctx context.Context) ([]cid.Cid, error)
	Size(ctx context.Context, cid cid.Cid) (int64, error)
	Close() error
}

// FileStore manages file metadata and content.
type FileStore interface {
	PutFile(ctx context.Context, meta *FileMeta, data []byte) error
	GetFile(ctx context.Context, cid cid.Cid) (*FileMeta, []byte, error)
	ListFiles(ctx context.Context) ([]*FileMeta, error)
	StatFile(ctx context.Context, cid cid.Cid) (*FileMeta, error)
	DeleteFile(ctx context.Context, cid cid.Cid) error
	VersionFile(ctx context.Context, cid cid.Cid) (*FileMeta, error)
}

// SyncEngine handles file synchronization with peers.
type SyncEngine interface {
	Sync(ctx context.Context, peerID [32]byte) error
	WantList(ctx context.Context, peerID [32]byte) ([]cid.Cid, error)
	ReceivedBlock(ctx context.Context, block *Block) error
	Start(ctx context.Context) error
	Stop() error
	Peers() []PeerInfo
	Stats() SyncStats
}

// SyncStats tracks synchronization statistics.
type SyncStats struct {
	BlocksSent  uint64
	BlocksRecv  uint64
	BytesSent   uint64
	BytesRecv   uint64
	ActiveSyncs int
	TotalSyncs  uint64
	FailedSyncs uint64
}

// ExchangeProtocol handles block exchange between peers.
type ExchangeProtocol interface {
	OpenStream(ctx context.Context, peerID [32]byte) (ExchangeStream, error)
	SendWant(ctx context.Context, peerID [32]byte, entries []WantEntry) error
	SendHave(ctx context.Context, peerID [32]byte, entries []WantEntry) error
	SendBlock(ctx context.Context, peerID [32]byte, block *Block) error
	Close() error
}

// ExchangeStream is a bidirectional exchange stream.
type ExchangeStream interface {
	Read(p []byte) (n int, err error)
	Write(p []byte) (n int, err error)
	Close() error
	PeerID() [32]byte
}

// PeerInfo holds information about a sync peer.
type PeerInfo struct {
	ID    [32]byte
	State string
}

// FS provides a filesystem mount interface.
type FS interface {
	Mount(ctx context.Context, path string) error
	Unmount(ctx context.Context) error
	Mounted() bool
	GetMountInfo() MountInfo
}
