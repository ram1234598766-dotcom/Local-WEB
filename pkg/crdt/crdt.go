package crdt

import (
	"bytes"
	"crypto/sha3"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/rs/zerolog/log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ram1234598766-dotcom/Local-WEB/pkg/crypto"
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

	// OriginID is the node this one was causally inserted after, and it never
	// changes.
	//
	// Prev cannot serve this purpose: splicing a node in after the sibling
	// ahead of it makes that sibling its structural predecessor, so a later
	// insert would mistake a concurrent sibling for a descendant and order the
	// text differently depending on arrival. OriginID keeps the causal edge, so
	// "is this node a child of X" stays answerable after splicing.
	OriginID string
}

type RGA struct {
	mu     sync.RWMutex
	head   *RGANode
	tail   *RGANode
	length int
	clock  int64
	nodeID string
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

	r.insertOrdered(afterNode, &RGANode{
		ID:        id,
		Value:     value,
		Timestamp: r.clock,
		Author:    r.nodeID,
		OriginID:  afterNode.ID,
	})
	r.length++
}

// beforeNode reports whether a should sort ahead of b in an RGA sibling run.
//
// Siblings are the nodes inserted after the same predecessor, including the
// subtrees that hang off them. Ordering them by (Timestamp, Author) rather than
// by arrival is what makes the sequence a function of the operation set: two
// replicas that received the same concurrent inserts in opposite orders still
// produce identical text.
func beforeNode(a, b *RGANode) bool {
	if a.Timestamp != b.Timestamp {
		return a.Timestamp < b.Timestamp
	}
	return a.Author < b.Author
}

// insertOrdered splices node into r among afterNode's causal children, in the
// position its (Timestamp, Author) gives it, and links it in both directions.
//
// Only concurrent siblings are skipped, and each is skipped together with its
// entire subtree. Comparing against every later node instead would displace a
// node's own causal child, since a child necessarily has a higher timestamp than
// its parent, and the two replicas would then disagree about the text.
//
// Sibling membership is read from OriginID, not from Prev: Prev is whoever
// happens to sit immediately ahead in the list, which for a sibling is another
// sibling rather than the shared parent.
//
// This is the single place a node enters the list, so a local insert and a
// merged insert cannot disagree about where a node belongs.
func (r *RGA) insertOrdered(afterNode, node *RGANode) {
	insertAfter := afterNode
	curr := afterNode.Next

	for curr != nil && curr != r.tail {
		// Only nodes causally inserted after afterNode are candidates. Reaching
		// anything else means the run of siblings to compare is finished.
		if curr.OriginID != afterNode.ID {
			break
		}
		// This sibling sorts at or after the new node, so the new node goes
		// ahead of it.
		if !beforeNode(curr, node) {
			break
		}

		// Skip this sibling's whole subtree: keep going while the next node is
		// not itself a child of afterNode, since that makes it a descendant.
		for curr.Next != nil && curr.Next != r.tail && curr.Next.OriginID != afterNode.ID {
			curr = curr.Next
		}
		// The splice point is the last node of the skipped subtree, not the
		// sibling itself: a new sibling belongs after everything that sibling
		// brought with it, so splicing at the sibling would place it between a
		// node and its own descendants.
		insertAfter = curr
		curr = curr.Next
	}

	node.Prev = insertAfter
	node.Next = insertAfter.Next
	insertAfter.Next = node
	if node.Next != nil {
		node.Next.Prev = node
	}
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

// String renders the live (non-deleted) characters in order.
func (r *RGA) String() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var sb strings.Builder
	for curr := r.head.Next; curr != nil && curr != r.tail; curr = curr.Next {
		if curr.Deleted {
			continue
		}
		sb.WriteString(curr.Value)
	}
	return sb.String()
}

// HeadNextID returns the ID of the first node after the head sentinel, or the
// empty string when the document is empty. Callers need it to express an
// insert relative to the start of the document, since "head" is the sentinel
// they would otherwise have to guess at.
func (r *RGA) HeadNextID() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.head.Next == nil || r.head.Next == r.tail {
		return ""
	}
	return r.head.Next.ID
}

// IDs returns the IDs of all live nodes in order, which is what a caller
// needs to address a position for a subsequent Insert.
func (r *RGA) IDs() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []string
	for curr := r.head.Next; curr != nil && curr != r.tail; curr = curr.Next {
		if curr.Deleted {
			continue
		}
		out = append(out, curr.ID)
	}
	return out
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
		// Payloads written before OriginID existed carry no causal edge. The
		// serialised list is in document order, so the previous node is the best
		// available origin and keeps such a payload usable for merge.
		origin := n.OriginID
		if origin == "" {
			origin = prev.ID
		}
		node := &RGANode{
			ID:        n.ID,
			Value:     n.Value,
			Timestamp: n.Timestamp,
			Author:    n.Author,
			Deleted:   n.Deleted,
			Prev:      prev,
			OriginID:  origin,
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

// Merge folds the operations r is missing from other into r.
//
// It previously appended every unknown node at the tail in (Timestamp, Author)
// order, ignoring the predecessor each node was actually inserted after. That is
// the one thing an RGA must not lose: a node's position is defined by what it
// followed, not by when it was learned. With tail-append, two replicas that
// applied the same operations in different orders ended up with different text
// and no amount of further merging could reconcile them.
//
// Nodes are now placed after their recorded predecessor, and concurrent
// siblings are ordered by (Timestamp, Author) so the result depends only on the
// set of operations, not on arrival order.
func (r *RGA) Merge(other *RGA) {
	if other == nil || other == r {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	other.mu.RLock()
	defer other.mu.RUnlock()

	// Index what this replica already has, so a node is never inserted twice and
	// a tombstone arriving for a node already present can still be applied.
	local := r.indexIDs()

	type pending struct {
		node   RGANode
		prevID string
	}
	var missing []pending

	for curr := other.head.Next; curr != nil && curr != other.tail; curr = curr.Next {
		if local[curr.ID] {
			// The node is known here but other has it tombstoned: a delete must
			// propagate, otherwise one replica shows text the other has erased.
			if curr.Deleted && !r.isDeleted(curr.ID) {
				r.applyDeleteLocked(curr.ID)
			}
			continue
		}
		prevID := r.head.ID
		if curr.Prev != nil && curr.Prev.ID != "" {
			prevID = curr.Prev.ID
		}
		cp := *curr
		cp.Prev = nil
		cp.Next = nil
		missing = append(missing, pending{node: cp, prevID: prevID})
	}
	if len(missing) == 0 {
		return
	}

	// A node can arrive before the node it was inserted after, so keep sweeping
	// the unplaceable ones until a pass makes no progress.
	placed := make(map[string]bool, len(missing))
	remaining := missing
	for {
		var deferred []pending
		progress := false
		for _, p := range remaining {
			if placed[p.node.ID] {
				continue
			}
			after := r.findNode(p.prevID)
			if after == nil {
				// Predecessor has not been seen. Leave it for a later merge
				// rather than guessing a position, which would diverge.
				deferred = append(deferred, p)
				continue
			}
			node := p.node
			// The causal edge is the node's recorded origin, not wherever it ends
			// up being spliced.
			node.OriginID = p.prevID
			r.insertOrdered(after, &node)
			// Length counts every node in the list, tombstoned ones included,
			// which is what Delete and Insert both assume. String is the live
			// text.
			r.length++
			placed[p.node.ID] = true
			progress = true
		}
		if len(deferred) == 0 {
			return
		}
		if !progress {
			// Nothing more can be anchored. Report the orphans rather than
			// appending them somewhere arbitrary.
			for _, p := range deferred {
				log.Warn().
					Str("node", p.node.ID).
					Str("missing_predecessor", p.prevID).
					Msg("rga: cannot merge node whose predecessor has never been seen")
			}
			return
		}
		remaining = deferred
	}
}

// indexIDs returns the ids of every node in the list, including the sentinels.
// The caller must hold at least the read lock.
func (r *RGA) indexIDs() map[string]bool {
	ids := make(map[string]bool)
	for curr := r.head; curr != nil; curr = curr.Next {
		ids[curr.ID] = true
		if curr == r.tail {
			break
		}
	}
	return ids
}

// isDeleted reports whether a node is tombstoned. The caller must hold the lock.
func (r *RGA) isDeleted(id string) bool {
	n := r.findNode(id)
	return n != nil && n.Deleted
}

// applyDeleteLocked tombstones a node.
//
// Length deliberately does not change: it counts every node in the list,
// tombstoned ones included, which is the contract Insert and Delete already
// follow. String is the live text.
//
// The caller must hold the write lock.
func (r *RGA) applyDeleteLocked(id string) {
	n := r.findNode(id)
	if n == nil || n.Deleted {
		return
	}
	n.Deleted = true
}

// findNode locates a node by ID, including the head sentinel.
//
// The head check matters: callers legitimately insert relative to "head" to
// prepend to the document. Without it, findNode returned nil for "head",
// Insert fell back to tail.Prev, and every insert silently became an append —
// so the document could never be built in the intended order.
func (r *RGA) findNode(id string) *RGANode {
	if id == r.head.ID {
		return r.head
	}
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
	binary.Write(&buf, binary.BigEndian, uint32(len(entries)))
	for _, e := range entries {
		binary.Write(&buf, binary.BigEndian, uint16(len(e.Elem)))
		buf.WriteString(e.Elem)
		binary.Write(&buf, binary.BigEndian, uint16(len(e.Tags)))
		for _, t := range e.Tags {
			binary.Write(&buf, binary.BigEndian, uint16(len(t)))
			buf.WriteString(t)
		}
	}
	binary.Write(&buf, binary.BigEndian, uint32(len(removes)))
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
	var elemCount uint32
	if err := binary.Read(buf, binary.BigEndian, &elemCount); err != nil {
		return nil, nil, err
	}
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
			Elem string
			Tags []string
		}{Elem: string(elem), Tags: tags})
	}
	var rmCount uint32
	if err := binary.Read(buf, binary.BigEndian, &rmCount); err != nil {
		return nil, nil, err
	}
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

// encodeRGAList serialises the node list.
//
// The origin is written last and as an optional trailing field, so a payload
// produced before OriginID existed still decodes: the reader stops when the
// bytes run out and fills the origin from document order.
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
		if n.OriginID != "" {
			binary.Write(&buf, binary.BigEndian, uint16(len(n.OriginID)))
			buf.WriteString(n.OriginID)
		}
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
		// The origin is optional and trailing: a payload written before it
		// existed simply runs out of bytes here, and the caller fills it in from
		// document order.
		origin := ""
		if buf.Len() > 0 {
			var originLen uint16
			if err := binary.Read(buf, binary.BigEndian, &originLen); err == nil {
				if originLen > 0 {
					ob := make([]byte, originLen)
					if _, err := buf.Read(ob); err == nil {
						origin = string(ob)
					}
				}
			}
		}
		nodes = append(nodes, RGANode{
			ID:        string(id),
			Value:     string(val),
			Timestamp: ts,
			Author:    string(auth),
			Deleted:   deleted == 1,
			OriginID:  origin,
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
	leavesA := make(map[[32]byte]bool, len(a.Leaves))
	for _, leaf := range a.Leaves {
		leavesA[leaf] = true
	}
	leavesB := make(map[[32]byte]bool, len(b.Leaves))
	for _, leaf := range b.Leaves {
		leavesB[leaf] = true
	}
	for leaf := range leavesA {
		if !leavesB[leaf] {
			onlyA = append(onlyA, leaf)
		}
	}
	for leaf := range leavesB {
		if !leavesA[leaf] {
			onlyB = append(onlyB, leaf)
		}
	}
	return onlyA, onlyB
}
