package messaging

import (
	"context"
	"crypto/sha3"
	"encoding/binary"
	"errors"
	"sync"
	"time"

	"github.com/mrityunjay/LocalWEB/pkg/crypto"
)

type ChannelID [32]byte

type Message struct {
	ID        string
	ChannelID ChannelID
	Sender    [32]byte
	Timestamp int64
	Content   []byte
	Signature []byte
	ParentID  string
	Type      uint8
}

type Store interface {
	Append(channel ChannelID, msg Message) error
	History(channel ChannelID, after string, limit int) ([]Message, error)
}

type memoryStore struct {
	mu      sync.RWMutex
	history map[ChannelID][]Message
}

func newMemoryStore() *memoryStore {
	return &memoryStore{history: make(map[ChannelID][]Message)}
}

func (s *memoryStore) Append(channel ChannelID, msg Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.history[channel] = append(s.history[channel], msg)
	return nil
}

func (s *memoryStore) History(channel ChannelID, after string, limit int) ([]Message, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	all := s.history[channel]
	start := 0
	for i, m := range all {
		if m.ID == after {
			start = i + 1
			break
		}
	}
	if start >= len(all) {
		return nil, nil
	}
	if limit <= 0 || limit > len(all)-start {
		limit = len(all) - start
	}
	out := make([]Message, limit)
	copy(out, all[start:start+limit])
	return out, nil
}

type Service struct {
	mu      sync.RWMutex
	channels map[ChannelID]*Channel
	store    Store
	privKey   [32]byte
}

type Channel struct {
	ID       ChannelID
	Members  map[[32]byte]bool
	Created  time.Time
	LastSeen time.Time
}

func NewService(store Store, privKey [32]byte) *Service {
	if store == nil {
		store = NewMemoryStore()
	}
	return &Service{
		channels: make(map[ChannelID]*Channel),
		store:    store,
		privKey:  privKey,
	}
}

func NewMemoryStore() *memoryStore {
	return &memoryStore{history: make(map[ChannelID][]Message)}
}

func (s *Service) CreateChannel(members [][32]byte) ChannelID {
	s.mu.Lock()
	defer s.mu.Unlock()
	var id ChannelID
	h := sha3.New256()
	for _, m := range members {
		h.Write(m[:])
	}
	h.Sum(id[:0])
	s.channels[id] = &Channel{
		ID:      id,
		Members: make(map[[32]byte]bool),
		Created: time.Now(),
	}
	for _, m := range members {
		s.channels[id].Members[m] = true
	}
	return id
}

func (s *Service) Publish(ctx context.Context, channelID ChannelID, sender [32]byte, content []byte, parentID string) (Message, error) {
	s.mu.RLock()
	ch, ok := s.channels[channelID]
	s.mu.RUnlock()
	if !ok {
		return Message{}, errors.New("channel not found")
	}
	if !ch.Members[sender] {
		return Message{}, errors.New("not a channel member")
	}

	msg := Message{
		ID:        nextID(sender, content),
		ChannelID: channelID,
		Sender:    sender,
		Timestamp: time.Now().UnixNano(),
		Content:   append([]byte{}, content...),
		ParentID:  parentID,
		Type:      0,
	}
	sig, err := crypto.Sign(s.privKey, append([]byte(msg.ID), content...))
	if err != nil {
		return Message{}, err
	}
	msg.Signature = sig
	ch.LastSeen = time.Now()
	if err := s.store.Append(channelID, msg); err != nil {
		return Message{}, err
	}
	return msg, nil
}

func (s *Service) History(channelID ChannelID, after string, limit int) ([]Message, error) {
	return s.store.History(channelID, after, limit)
}

func nextID(sender [32]byte, content []byte) string {
	h := sha3.New256()
	h.Write(sender[:])
	h.Write(content)
	var out [32]byte
	h.Sum(out[:0])
	return string(out[:16])
}

func (s *Service) MarshalChannel(channelID ChannelID) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ch, ok := s.channels[channelID]
	if !ok {
		return nil, errors.New("channel not found")
	}
	buf := make([]byte, 20)
	binary.LittleEndian.PutUint64(buf[0:8], uint64(ch.Created.UnixNano()))
	binary.LittleEndian.PutUint64(buf[8:16], uint64(ch.LastSeen.UnixNano()))
	buf[16] = 0
	buf[17] = 0
	buf[18] = 0
	buf[19] = 0
	return buf, nil
}

func (s *Service) UnmarshalChannel(data []byte) (ChannelID, error) {
	if len(data) < 16 {
		return ChannelID{}, errors.New("data too short")
	}
	return ChannelID{}, nil
}
