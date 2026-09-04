package files

import (
	"context"
	"errors"
	"sync"
	"time"
)

// fuseMount is a stub FUSE mount for v1.0. Full implementation will use
// go-fuse (github.com/hanwen/go-fuse/v2) on Linux/macOS and Dokany on Windows.
type fuseMount struct {
	mu       sync.RWMutex
	path     string
	mounted  bool
	store    BlockStore
	fileStore FileStore
	started  time.Time
}

// NewFS creates a new FUSE filesystem interface.
func NewFS(store BlockStore, fileStore FileStore) FS {
	return &fuseMount{
		store:    store,
		fileStore: fileStore,
	}
}

func (f *fuseMount) Mount(ctx context.Context, path string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.mounted {
		return errors.New("already mounted")
	}
	f.path = path
	f.mounted = true
	f.started = time.Now()
	return nil
}

func (f *fuseMount) Unmount(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.mounted {
		return nil
	}
	f.mounted = false
	return nil
}

func (f *fuseMount) Mounted() bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.mounted
}

// MountInfo holds information about a mounted filesystem.
type MountInfo struct {
	Path      string
	Mounted   bool
	Store     BlockStore
	FileStore FileStore
	Uptime    time.Duration
	PeerID    PeerInfo
}

// GetMountInfo returns information about the mount.
func (f *fuseMount) GetMountInfo() MountInfo {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return MountInfo{
		Path:      f.path,
		Mounted:   f.mounted,
		Store:     f.store,
		FileStore: f.fileStore,
		Uptime:    time.Since(f.started),
		PeerID:    PeerInfo{ID: [32]byte{1, 2, 3}, State: "stub"},
	}
}
