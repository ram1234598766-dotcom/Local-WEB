package crdt

import (
	"bytes"
	"crypto/sha3"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mrityunjay/LocalWEB/pkg/crypto"
)

// OR-Set (Observed-Remove Set) - add-wins semantics
type ORSet struct {
	mu      sync.RWMutex
	adds    map[string]map[string]bool // element -> set of unique tags
	removes map[string]bool            // set of tombstones (tags)
}

var tagCounter uint32

func NewORSet() *ORSet {
	return &ORSet{
		adds:    make(map[string]map[string]bool),
		removes: make(map[string]bool),
	}
}

func (s *ORSet) Add(elem string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tag := uniqueTag()
	if _, ok := s.adds[elem]; !ok {
		s.adds[elem] = make(map[string]bool)
	}
	s.adds[elem][tag] = true
}

func (s *ORSet) Remove(elem string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if tags, ok := s.adds[elem]; ok {
		for tag := range tags {
			s.removes[tag] = true
		}
	}
}

func (s *ORSet) Contains(elem string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	tags, ok := s.adds[elem]
	if !ok {
		return false
	}
	for tag := range tags {
		if !s.removes[tag] {
			return true
		}
	}
	return false
}

func (s *ORSet) Items() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []string
	for elem, tags := range s.adds {
		for tag := range tags {
			if !s.removes[tag] {
				out = append(out, elem)
				break
			}
		}
	}
	sort.Strings(out)
	return out
}

func (s *ORSet) Merge(other *ORSet) {
	s.mu.Lock()
	defer s.mu.Unlock()
	other.mu.RLock()
	defer other.mu.RUnlock()

	for elem, tags := range other.adds {
		if _, ok := s.adds[elem]; !ok {
			s.adds[elem] = make(map[string]bool)
		}
		for tag := range tags {
			s.adds[elem][tag] = true
		}
	}
	for tag := range other.removes {
		s.removes[tag] = true
	}
}

func (s *ORSet) Marshal() []byte {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var entries []struct {
		Elem string
		Tags []string
	}
	for elem, tags := range s.adds {
		var tagList []string
		for tag := range tags {
			tagList = append(tagList, tag)
		}
		entries = append(entries, struct {
			Elem string
			Tags []string
		}{Elem: elem, Tags: tagList})
	}
	out, _ := encodeEntries(entries, s.removes)
	return out
}

func (s *ORSet) Unmarshal(data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, removes, err := decodeEntries(data)
	if err != nil {
		return err
	}
	s.adds = make(map[string]map[string]bool)
	s.removes = removes
	for _, e := range entries {
		s.adds[e.Elem] = make(map[string]bool)
		for _, tag := range e.Tags {
			s.adds[e.Elem][tag] = true
		}
	}
	return nil
}

type RGANode struct {
	ID        string
	Value     string
	Timestamp int64
	Author    string
	Deleted   bool
	Next      *RGANode
	Prev      *RGANode
}

type RGA struct {
	mu       sync.RWMutex
	head     *RGANode
	tail     *RGANode
	length   int
	clock    int64
	nodeID   string
}

func NewRGA(nodeID string) *RGA {
	rga := &RGA{nodeID: nodeID}
	rga.head = &RGANode{ID: "head", Value: "", Timestamp: 0, Author: ""}
	rga.tail = &RGANode{ID: "tail", Value: "", Timestamp: math.MaxInt64, Author: ""}
	rga.head.Next = rga.tail
	rga.tail.Prev = rga.head
	return rga
}

func (r *RGA) Insert(afterID, value string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.clock++
	id := fmt.Sprintf("%d:%s:%s", r.clock, r.nodeID, value)

	afterNode := r.findNode(afterID)
	if afterNode == nil {
		afterNode = r.tail.Prev
	}

	node := &RGANode{
		ID:        id,
		Value:     value,
		Timestamp: r.clock,
		Author:    r.nodeID,
		Next:      afterNode.Next,
		Prev:      afterNode,
	}
	if afterNode.Next != nil {
		afterNode.Next.Prev = node
	}
	afterNode.Next = node
	r.length++
}

func (r *RGA) Delete(nodeID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	node := r.findNode(nodeID)
	if node != nil {
		node.Deleted = true
	}
}

func (r *RGA) Get(index int) (string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if index < 0 || index >= r.length {
		return "", errors.New("index out of bounds")
	}
	curr := r.head.Next
	for i := 0; i < index && curr != nil; i++ {
		curr = curr.Next
	}
	if curr == nil || curr == r.tail {
		return "", errors.New("index out of bounds")
	}
	return curr.Value, nil
}

func (r *RGA) Length() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.length
}

func (r *RGA) Marshal() []byte {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var nodes []RGANode
	curr := r.head.Next
	for curr != nil && curr != r.tail {
		nodes = append(nodes, *curr)
		curr = curr.Next
	}
	out, _ := encodeRGAList(nodes)
	return out
}

func (r *RGA) Unmarshal(data []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	nodes, err := decodeRGAList(data)
	if err != nil {
		return err
	}
	r.head.Next = r.tail
	r.tail.Prev = r.head
	r.length = 0
	prev := r.head
	for _, n := range nodes {
		node := &RGANode{
			ID:        n.ID,
			Value:     n.Value,
			Timestamp: n.Timestamp,
			Author:    n.Author,
			Deleted:   n.Deleted,
			Prev:      prev,
		}
		prev.Next = node
		node.Next = r.tail
		r.tail.Prev = node
		prev = node
		if !n.Deleted {
			r.length++
		}
	}
	return nil
}

func (r *RGA) Merge(other *RGA) {
	r.mu.Lock()
	defer r.mu.Unlock()
	other.mu.RLock()
	defer other.mu.RUnlock()
	// Simplified merge: append missing nodes in causal order
	otherNodes := make(map[string]*RGANode)
	curr := other.head.Next
	for curr != nil && curr != other.tail {
		otherNodes[curr.ID] = curr
		curr = curr.Next
	}
	curr = r.head.Next
	for curr != nil && curr != r.tail {
		delete(otherNodes, curr.ID)
		curr = curr.Next
	}
	var toInsert []*RGANode
	for _, n := range otherNodes {
		cp := *n
		toInsert = append(toInsert, &cp)
	}
	sort.Slice(toInsert, func(i, j int) bool {
		if toInsert[i].Timestamp != toInsert[j].Timestamp {
			return toInsert[i].Timestamp < toInsert[j].Timestamp
		}
		return toInsert[i].Author < toInsert[j].Author
	})
	insertAfter := r.tail.Prev
	for _, n := range toInsert {
		node := &RGANode{
			ID:        n.ID,
			Value:     n.Value,
			Timestamp: n.Timestamp,
			Author:    n.Author,
			Deleted:   n.Deleted,
			Prev:      insertAfter,
			Next:      r.tail,
		}
		insertAfter.Next = node
		r.tail.Prev = node
		insertAfter = node
		if !n.Deleted {
			r.length++
		}
	}
}

func (r *RGA) findNode(id string) *RGANode {
	curr := r.head.Next
	for curr != nil && curr != r.tail {
		if curr.ID == id {
			return curr
		}
		curr = curr.Next
	}
	return nil
}

// LWW-Register (Last-Writer-Wins)
type LWWRegister struct {
	mu        sync.RWMutex
	Value     []byte
	Timestamp int64
	Author    string
}

func NewLWWRegister(author string) *LWWRegister {
	return &LWWRegister{Author: author}
}

func (r *LWWRegister) Set(value []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Value = append([]byte{}, value...)
	r.Timestamp = time.Now().UnixNano()
}

func (r *LWWRegister) Get() ([]byte, int64, string) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]byte{}, r.Value...), r.Timestamp, r.Author
}

func (r *LWWRegister) Merge(other *LWWRegister) {
	r.mu.Lock()
	defer r.mu.Unlock()
	other.mu.RLock()
	defer other.mu.RUnlock()
	if other.Timestamp > r.Timestamp || (other.Timestamp == r.Timestamp && other.Author > r.Author) {
		r.Value = append([]byte{}, other.Value...)
		r.Timestamp = other.Timestamp
		r.Author = other.Author
	}
}

func (r *LWWRegister) Marshal() []byte {
	r.mu.RLock()
	defer r.mu.RUnlock()
	buf := make([]byte, 8+4+len(r.Value))
	binary.BigEndian.PutUint64(buf[:8], uint64(r.Timestamp))
	binary.BigEndian.PutUint32(buf[8:12], uint32(len(r.Value)))
	copy(buf[12:], r.Value)
	return buf
}

func (r *LWWRegister) Unmarshal(data []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(data) < 12 {
		return errors.New("data too short")
	}
	r.Timestamp = int64(binary.BigEndian.Uint64(data[:8]))
	vLen := int(binary.BigEndian.Uint32(data[8:12]))
	if len(data) < 12+vLen {
		return errors.New("data too short")
	}
	r.Value = append([]byte{}, data[12:12+vLen]...)
	return nil
}

func uniqueTag() string {
	h := sha3.New256()
	b := make([]byte, 16)
	binary.LittleEndian.PutUint64(b[:8], uint64(time.Now().UnixNano()))
	binary.LittleEndian.PutUint32(b[8:12], atomic.AddUint32(&tagCounter, 1))
	h.Write(b[:12])
	var out [32]byte
	h.Sum(out[:0])
	return string(out[:])
}

func encodeEntries(entries []struct {
	Elem string
	Tags []string
}, removes map[string]bool) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte(byte(len(entries)))
	for _, e := range entries {
		binary.Write(&buf, binary.BigEndian, uint16(len(e.Elem)))
		buf.WriteString(e.Elem)
		binary.Write(&buf, binary.BigEndian, uint16(len(e.Tags)))
		for _, t := range e.Tags {
			binary.Write(&buf, binary.BigEndian, uint16(len(t)))
			buf.WriteString(t)
		}
	}
	buf.WriteByte(byte(len(removes)))
	for tag := range removes {
		binary.Write(&buf, binary.BigEndian, uint16(len(tag)))
		buf.WriteString(tag)
	}
	return buf.Bytes(), nil
}

func decodeEntries(data []byte) ([]struct {
	Elem string
	Tags []string
}, map[string]bool, error) {
	var entries []struct {
		Elem string
		Tags []string
	}
	removes := make(map[string]bool)
	buf := bytes.NewBuffer(data)
	elemCount, _ := buf.ReadByte()
	for i := 0; i < int(elemCount); i++ {
		var kLen uint16
		if err := binary.Read(buf, binary.BigEndian, &kLen); err != nil {
			return nil, nil, err
		}
		elem := make([]byte, kLen)
		if _, err := buf.Read(elem); err != nil {
			return nil, nil, err
		}
		var tCount uint16
		if err := binary.Read(buf, binary.BigEndian, &tCount); err != nil {
			return nil, nil, err
		}
		var tags []string
		for j := 0; j < int(tCount); j++ {
			var tLen uint16
			if err := binary.Read(buf, binary.BigEndian, &tLen); err != nil {
				return nil, nil, err
			}
			tag := make([]byte, tLen)
			if _, err := buf.Read(tag); err != nil {
				return nil, nil, err
			}
			tags = append(tags, string(tag))
		}
		entries = append(entries, struct {
			Elem  string
			Tags  []string
		}{Elem: string(elem), Tags: tags})
	}
	rmCount, _ := buf.ReadByte()
	for i := 0; i < int(rmCount); i++ {
		var tLen uint16
		if err := binary.Read(buf, binary.BigEndian, &tLen); err != nil {
			return nil, nil, err
		}
		tag := make([]byte, tLen)
		if _, err := buf.Read(tag); err != nil {
			return nil, nil, err
		}
		removes[string(tag)] = true
	}
	return entries, removes, nil
}

func encodeRGAList(nodes []RGANode) ([]byte, error) {
	var buf bytes.Buffer
	binary.Write(&buf, binary.BigEndian, uint16(len(nodes)))
	for _, n := range nodes {
		binary.Write(&buf, binary.BigEndian, uint16(len(n.ID)))
		buf.WriteString(n.ID)
		binary.Write(&buf, binary.BigEndian, uint16(len(n.Value)))
		buf.WriteString(n.Value)
		binary.Write(&buf, binary.BigEndian, n.Timestamp)
		binary.Write(&buf, binary.BigEndian, uint16(len(n.Author)))
		buf.WriteString(n.Author)
		deleted := byte(0)
		if n.Deleted {
			deleted = 1
		}
		buf.WriteByte(deleted)
	}
	return buf.Bytes(), nil
}

func decodeRGAList(data []byte) ([]RGANode, error) {
	var nodes []RGANode
	buf := bytes.NewBuffer(data)
	var count uint16
	if err := binary.Read(buf, binary.BigEndian, &count); err != nil {
		return nil, err
	}
	for i := 0; i < int(count); i++ {
		var idLen, valLen, authLen uint16
		if err := binary.Read(buf, binary.BigEndian, &idLen); err != nil {
			return nil, err
		}
		id := make([]byte, idLen)
		if _, err := buf.Read(id); err != nil {
			return nil, err
		}
		if err := binary.Read(buf, binary.BigEndian, &valLen); err != nil {
			return nil, err
		}
		val := make([]byte, valLen)
		if _, err := buf.Read(val); err != nil {
			return nil, err
		}
		var ts int64
		if err := binary.Read(buf, binary.BigEndian, &ts); err != nil {
			return nil, err
		}
		if err := binary.Read(buf, binary.BigEndian, &authLen); err != nil {
			return nil, err
		}
		auth := make([]byte, authLen)
		if _, err := buf.Read(auth); err != nil {
			return nil, err
		}
		deleted, _ := buf.ReadByte()
		nodes = append(nodes, RGANode{
			ID:        string(id),
			Value:     string(val),
			Timestamp: ts,
			Author:    string(auth),
			Deleted:   deleted == 1,
		})
	}
	return nodes, nil
}

// MerkleTree for anti-entropy
type MerkleTree struct {
	Leaves [][32]byte
	Root   [32]byte
}

func NewMerkleTree(data []string) *MerkleTree {
	leaves := make([][32]byte, len(data))
	for i, d := range data {
		leaves[i] = crypto.SHA3Hash([]byte(d))
	}
	root := computeRoot(leaves)
	return &MerkleTree{Leaves: leaves, Root: root}
}

func computeRoot(leaves [][32]byte) [32]byte {
	if len(leaves) == 0 {
		return [32]byte{}
	}
	if len(leaves) == 1 {
		return leaves[0]
	}
	var next [][32]byte
	for i := 0; i < len(leaves); i += 2 {
		if i+1 < len(leaves) {
			next = append(next, hashPair(leaves[i], leaves[i+1]))
		} else {
			next = append(next, leaves[i])
		}
	}
	return computeRoot(next)
}

func hashPair(a, b [32]byte) [32]byte {
	h := sha3.New256()
	if bytes.Compare(a[:], b[:]) < 0 {
		h.Write(a[:])
		h.Write(b[:])
	} else {
		h.Write(b[:])
		h.Write(a[:])
	}
	var out [32]byte
	h.Sum(out[:0])
	return out
}

func DiffMerkle(a, b *MerkleTree) ([][32]byte, [][32]byte) {
	if a.Root == b.Root {
		return nil, nil
	}
	var onlyA, onlyB [][32]byte
	seen := make(map[[32]byte]bool)
	for _, leaf := range b.Leaves {
		seen[leaf] = true
	}
	for _, leaf := range a.Leaves {
		if !seen[leaf] {
			onlyA = append(onlyA, leaf)
		}
	}
	seen = make(map[[32]byte]bool)
	for _, leaf := range a.Leaves {
		seen[leaf] = true
	}
	for _, leaf := range b.Leaves {
		if !seen[leaf] {
			onlyB = append(onlyB, leaf)
		}
	}
	return onlyA, onlyB
}

