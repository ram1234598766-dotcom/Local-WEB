package store

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/gob"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/dgraph-io/badger/v3"
)

// magicHeader is the prefix for all keys in the store to prevent collisions.
const magicHeader = "LWS"

// Entry represents a key-value pair for batch operations.
type Entry struct {
	Key   []byte
	Value []byte
}

// Store is a thread-safe BadgerDB wrapper with optional NaCl secretbox encryption.
// The encryption key is provided at open time and used by Badger's built-in AES-GCM.
type Store struct {
	mu     sync.RWMutex
	db     *badger.DB
	path   string
	key    [32]byte
	closed bool
}
// Open creates or opens a BadgerDB store at the given path with encryption.
func Open(path string, key [32]byte) (*Store, error) {
	if err := os.MkdirAll(path, 0700); err != nil {
		return nil, fmt.Errorf("create store dir: %w", err)
	}

	opts := badger.DefaultOptions(path).
		WithLogger(nil).
		WithEncryptionKey(key[:]).
		WithIndexCacheSize(10 << 20) // 10 MiB index cache

	db, err := badger.Open(opts)
	if err != nil {
		return nil, fmt.Errorf("open badger: %w", err)
	}

	return &Store{
		db:   db,
		path: path,
		key:  key,
	}, nil
}

// OpenInMemory creates an in-memory store for testing.
func OpenInMemory() *Store {
	opts := badger.DefaultOptions("").
		WithLogger(nil).
		WithInMemory(true)

	db, err := badger.Open(opts)
	if err != nil {
		panic(fmt.Sprintf("open in-memory badger: %v", err))
	}

	return &Store{db: db}
}

// Close closes the underlying BadgerDB instance.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	return s.db.Close()
}

// checkClosed returns an error if the store is closed.
func (s *Store) checkClosed() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return fmt.Errorf("store is closed")
	}
	return nil
}

// nsKey returns a namespaced key to prevent collisions.
func (s *Store) nsKey(parts ...string) []byte {
	buf := make([]byte, 0, len(magicHeader)+1+len(parts)*32)
	buf = append(buf, magicHeader...)
	buf = append(buf, ':')
	for i, part := range parts {
		if i > 0 {
			buf = append(buf, ':')
		}
		buf = append(buf, part...)
	}
	return buf
}

// Get retrieves a value by key.
func (s *Store) Get(ctx context.Context, k []byte) ([]byte, error) {
	if err := s.checkClosed(); err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	var val []byte
	err := s.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(k)
		if err != nil {
			return err
		}
		return item.Value(func(v []byte) error {
			val = append([]byte{}, v...)
			return nil
		})
	})
	if err != nil {
		if err == badger.ErrKeyNotFound {
			return nil, fmt.Errorf("key not found")
		}
		return nil, fmt.Errorf("get: %w", err)
	}
	return val, nil
}

// Put stores a key-value pair.
func (s *Store) Put(ctx context.Context, k, v []byte) error {
	if err := s.checkClosed(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	return s.db.Update(func(txn *badger.Txn) error {
		return txn.Set(k, v)
	})
}

// PutWithTTL stores a key-value pair with a time-to-live.
func (s *Store) PutWithTTL(ctx context.Context, k, v []byte, ttl time.Duration) error {
	if err := s.checkClosed(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	e := badger.NewEntry(k, v).WithTTL(ttl)
	return s.db.Update(func(txn *badger.Txn) error {
		return txn.SetEntry(e)
	})
}

// Delete removes a key.
func (s *Store) Delete(ctx context.Context, k []byte) error {
	if err := s.checkClosed(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	return s.db.Update(func(txn *badger.Txn) error {
		return txn.Delete(k)
	})
}

// Has checks if a key exists.
func (s *Store) Has(ctx context.Context, k []byte) (bool, error) {
	if err := s.checkClosed(); err != nil {
		return false, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	err := s.db.View(func(txn *badger.Txn) error {
		_, err := txn.Get(k)
		return err
	})
	if err != nil {
		if err == badger.ErrKeyNotFound {
			return false, nil
		}
		return false, fmt.Errorf("has: %w", err)
	}
	return true, nil
}

// Iterator scans keys with the given prefix and calls fn for each key-value pair.
// Iteration stops if fn returns an error.
func (s *Store) Iterator(ctx context.Context, prefix []byte, fn func(k, v []byte) error) error {
	if err := s.checkClosed(); err != nil {
		return err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = true
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			item := it.Item()
			err := item.Value(func(v []byte) error {
				return fn(item.Key(), v)
			})
			if err != nil {
				return err
			}
		}
		return nil
	})
}

// Batch performs multiple write operations atomically.
func (s *Store) Batch(ctx context.Context, entries []Entry) error {
	if err := s.checkClosed(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	return s.db.Update(func(txn *badger.Txn) error {
		for _, e := range entries {
			if len(e.Value) == 0 {
				if err := txn.Delete(e.Key); err != nil {
					return fmt.Errorf("batch delete: %w", err)
				}
			} else {
				if err := txn.Set(e.Key, e.Value); err != nil {
					return fmt.Errorf("batch set: %w", err)
				}
			}
		}
		return nil
	})
}

// Count returns the number of entries with the given prefix.
func (s *Store) Count(ctx context.Context, prefix []byte) (int, error) {
	if err := s.checkClosed(); err != nil {
		return 0, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	count := 0
	err := s.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			count++
		}
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("count: %w", err)
	}
	return count, nil
}

// Path returns the on-disk path of the store.
func (s *Store) Path() string {
	return s.path
}

// encodeGob serializes a value using encoding/gob.
func encodeGob(v interface{}) ([]byte, error) {
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(v); err != nil {
		return nil, fmt.Errorf("encode gob: %w", err)
	}
	return buf.Bytes(), nil
}

// decodeGob deserializes bytes into the given value.
func decodeGob(data []byte, v interface{}) error {
	if err := gob.NewDecoder(bytes.NewReader(data)).Decode(v); err != nil {
		return fmt.Errorf("decode gob: %w", err)
	}
	return nil
}

// u64ToBytes converts a uint64 to an 8-byte big-endian slice for lexicographic sorting.
func u64ToBytes(v uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, v)
	return b
}

// bytesToU64 converts an 8-byte big-endian slice to uint64.
func bytesToU64(b []byte) uint64 {
	return binary.BigEndian.Uint64(b)
}
