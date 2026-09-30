package dht

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"
)

func TestSolveAndVerifyPoW(t *testing.T) {
	data := []byte("test data")
	nonce, err := SolvePoW(data, MinPoWDifficulty)
	if err != nil {
		t.Fatalf("SolvePoW failed: %v", err)
	}
	if !VerifyPoW(data, nonce, MinPoWDifficulty) {
		t.Fatal("VerifyPoW failed for valid nonce")
	}
	if VerifyPoW(data, []byte("badnonce"), MinPoWDifficulty) {
		t.Fatal("VerifyPoW passed for invalid nonce")
	}
}

func TestSolveAndVerifyPoWAtMaxDifficulty(t *testing.T) {
	data := []byte("max difficulty")
	start := time.Now()
	nonce, err := SolvePoW(data, MaxPoWDifficulty)
	if err != nil {
		t.Fatalf("SolvePoW failed: %v", err)
	}
	if !VerifyPoW(data, nonce, MaxPoWDifficulty) {
		t.Fatal("VerifyPoW failed for valid nonce")
	}
	t.Logf("difficulty %d solved in %v", MaxPoWDifficulty, time.Since(start))
}

// A difficulty outside the supported range is either unsolvable or a free
// denial of service against everyone trying to join, so it must be refused
// rather than searched for.
func TestPoWRejectsOutOfRangeDifficulty(t *testing.T) {
	for _, d := range []int{0, 1, 7, MaxPoWDifficulty + 1, 128, 255} {
		if _, err := SolvePoW([]byte("x"), d); err == nil {
			t.Errorf("SolvePoW accepted out-of-range difficulty %d", d)
		}
		if VerifyPoW([]byte("x"), make([]byte, 8), d) {
			t.Errorf("VerifyPoW accepted out-of-range difficulty %d", d)
		}
	}
}

func TestVerifyPoWRejectsWrongNonceLength(t *testing.T) {
	for _, n := range [][]byte{nil, {}, make([]byte, 7), make([]byte, 9)} {
		if VerifyPoW([]byte("x"), n, MinPoWDifficulty) {
			t.Errorf("VerifyPoW accepted nonce of length %d", len(n))
		}
	}
}

func TestVerifyPoWBindsToChallengePreimage(t *testing.T) {
	nonce, err := SolvePoW(registrationChallenge([32]byte{1}, "node-a"), MinPoWDifficulty)
	if err != nil {
		t.Fatalf("solve: %v", err)
	}
	// The same nonce must not validate for a different identity or name.
	if VerifyPoW(registrationChallenge([32]byte{2}, "node-a"), nonce, MinPoWDifficulty) {
		t.Fatal("nonce verified against a different public key")
	}
	if VerifyPoW(registrationChallenge([32]byte{1}, "node-b"), nonce, MinPoWDifficulty) {
		t.Fatal("nonce verified against a different name")
	}
}

func TestKBucketAddAndGetClosest(t *testing.T) {
	b := NewKBucket()
	target := NodeIDFromPub([32]byte{1: 0xff})
	p1 := &Peer{Info: PeerInfo{ID: NodeIDFromPub([32]byte{1: 0x01}), Name: "p1", Addrs: []string{"addr1"}}}
	p2 := &Peer{Info: PeerInfo{ID: NodeIDFromPub([32]byte{1: 0x02}), Name: "p2", Addrs: []string{"addr2"}}}
	p3 := &Peer{Info: PeerInfo{ID: NodeIDFromPub([32]byte{1: 0x03}), Name: "p3", Addrs: []string{"addr3"}}}
	b.Add(p1)
	b.Add(p2)
	b.Add(p3)
	closest := b.GetClosest(2, target)
	if len(closest) != 2 {
		t.Fatalf("expected 2 closest, got %d", len(closest))
	}
}

// nodeIDFromByte builds a distinct NodeID for a given seed byte. Array
// literals cannot take a computed index, so tests use this instead.
func nodeIDFromByte(b byte) NodeID {
	var id NodeID
	id[0] = b
	id[1] = b ^ 0xff
	return id
}

func TestKBucketEvictsOnFull(t *testing.T) {
	b := NewKBucket()
	for i := 0; i < KBucketSize; i++ {
		if !b.Add(&Peer{Info: PeerInfo{ID: nodeIDFromByte(byte(i + 1))}}) {
			t.Fatalf("add %d rejected below capacity", i)
		}
	}
	if b.Add(&Peer{Info: PeerInfo{ID: nodeIDFromByte(200)}}) {
		t.Fatal("expected add to be rejected when bucket is full")
	}
	if b.Len() != KBucketSize {
		t.Fatalf("bucket holds %d, want %d", b.Len(), KBucketSize)
	}
	// Re-adding an existing peer refreshes it rather than growing the bucket.
	existing := nodeIDFromByte(1)
	if b.Add(&Peer{Info: PeerInfo{ID: existing}}) {
		t.Fatal("expected duplicate add to report no change")
	}
	if b.Len() != KBucketSize {
		t.Fatalf("duplicate add changed bucket size to %d", b.Len())
	}
}

func TestPrefixLenFindsFirstSetBit(t *testing.T) {
	cases := []struct {
		id   NodeID
		want int
	}{
		{NodeID{}, 256},
		{NodeID{0: 0x80}, 0},
		{NodeID{0: 0x40}, 1},
		{NodeID{0: 0x01}, 7},
		{NodeID{0: 0x00, 1: 0x80}, 8},
		{NodeID{2: 0x10}, 16 + 3},
	}
	for i, tc := range cases {
		if got := tc.id.PrefixLen(); got != tc.want {
			t.Errorf("case %d: got %d, want %d", i, got, tc.want)
		}
	}
}

// The bucket index used to be id.PrefixLen() computed as id XOR id, which is
// always zero. Every peer therefore landed in bucket 0 and the routing table
// filled to 20 and then rejected the entire network.
func TestRoutingTableDistributesPeersAcrossBuckets(t *testing.T) {
	rt := NewRoutingTable(NodeID{})

	ids := []NodeID{
		{0: 0x80}, // prefix 0
		{0: 0x40}, // prefix 1
		{0: 0x20}, // prefix 2
		{0: 0x10}, // prefix 3
		{0: 0x08}, // prefix 4
		{0: 0x04}, // prefix 5
		{0: 0x02}, // prefix 6
		{0: 0x01}, // prefix 7
		{1: 0x80}, // prefix 8
	}
	for _, id := range ids {
		if !rt.Add(&Peer{Info: PeerInfo{ID: id}}) {
			t.Fatalf("add for id %x rejected", id[:2])
		}
	}

	used := map[int]bool{}
	for _, id := range ids {
		used[rt.BucketIndex(id)] = true
	}
	if len(used) != len(ids) {
		t.Fatalf("expected %d distinct buckets, got %d: %v", len(ids), len(used), used)
	}
	if rt.Len() != len(ids) {
		t.Fatalf("routing table holds %d peers, want %d", rt.Len(), len(ids))
	}
}

// xorDist previously truncated the 256-bit distance to its low 64 bits, so two
// IDs that differed only in their leading bytes compared as identical.
func TestXorDistUsesFullKeyspace(t *testing.T) {
	// Two IDs whose low bytes are equal and whose leading bytes differ. Under
	// the old low-64-bit truncation these compared as equal.
	high := NodeID{0: 0xff, 31: 0x00}
	low := NodeID{0: 0x00, 31: 0xff}

	dHigh := xorDist(high, NodeID{})
	dLow := xorDist(low, NodeID{})
	if compareDist(dHigh, dLow) <= 0 {
		t.Fatal("distance ordering did not follow the most significant byte")
	}
	// IDs differing only in the last byte must also be distinguishable.
	a := NodeID{31: 0x01}
	b := NodeID{31: 0x02}
	if compareDist(xorDist(a, NodeID{}), xorDist(b, NodeID{})) >= 0 {
		t.Fatal("distance ordering ignored the least significant byte")
	}
	// Distances must be symmetric and reflexive.
	if compareDist(xorDist(high, low), xorDist(low, high)) != 0 {
		t.Fatal("xorDist is not symmetric")
	}
	if compareDist(xorDist(high, high), NodeID{}) != 0 {
		t.Fatal("xorDist of an ID with itself is not zero")
	}
}

// FindClosest previously concatenated per-bucket results in bucket order and
// truncated, so a distant peer in a low-numbered bucket could displace a
// nearer peer in a high-numbered one.
func TestFindClosestSortsGlobally(t *testing.T) {
	rt := NewRoutingTable(NodeID{})

	// target shares its first byte with near but differs on the second,
	// which is what decides distance here.
	target := NodeID{0: 0x10, 1: 0x00}
	near := NodeID{0: 0x10, 1: 0x01} // differs in the low bit of byte 1
	far := NodeID{31: 0xff}          // differs in the last byte only

	rt.Add(&Peer{Info: PeerInfo{ID: far}})
	rt.Add(&Peer{Info: PeerInfo{ID: near}})

	closest := rt.FindClosest(target, 2)
	if len(closest) != 2 {
		t.Fatalf("expected 2 peers, got %d", len(closest))
	}
	if closest[0].Info.ID != near {
		t.Fatalf("expected the near peer first, got %x", closest[0].Info.ID[:4])
	}
	if compareDist(xorDist(closest[0].Info.ID, target), xorDist(closest[1].Info.ID, target)) > 0 {
		t.Fatal("FindClosest returned results out of distance order")
	}
}

func TestFindClosestRespectsLimit(t *testing.T) {
	rt := NewRoutingTable(NodeID{})
	for i := 0; i < 40; i++ {
		rt.Add(&Peer{Info: PeerInfo{ID: nodeIDFromByte(byte(i + 1))}})
	}
	if got := len(rt.FindClosest(NodeID{0: 0x01}, 5)); got != 5 {
		t.Fatalf("FindClosest returned %d peers, want 5", got)
	}
}

// A full bucket must not permanently block the table from learning new peers:
// stale entries have to be reapable.
func TestPruneStaleRemovesDepartedPeers(t *testing.T) {
	rt := NewRoutingTable(NodeID{})
	id := NodeID{0: 0x80}
	rt.Add(&Peer{Info: PeerInfo{ID: id}})

	if got := rt.PruneStale(time.Hour); got != 0 {
		t.Fatalf("pruned %d fresh peers, want 0", got)
	}
	// Backdate the peer past the TTL.
	rt.mu.Lock()
	rt.buckets[rt.BucketIndex(id)].peers[0].Info.LastSeen = time.Now().Add(-2 * time.Hour)
	rt.mu.Unlock()

	if got := rt.PruneStale(time.Hour); got != 1 {
		t.Fatalf("pruned %d stale peers, want 1", got)
	}
	if rt.Len() != 0 {
		t.Fatalf("routing table still holds %d peers after prune", rt.Len())
	}
	// With the slot free the table can learn again.
	if !rt.Add(&Peer{Info: PeerInfo{ID: id}}) {
		t.Fatal("expected add to succeed after pruning")
	}
}

func TestRoutingTableAddAndFindClosest(t *testing.T) {
	localID := NodeIDFromPub([32]byte{0: 0xaa})
	rt := NewRoutingTable(localID)
	p1 := &Peer{Info: PeerInfo{ID: NodeIDFromPub([32]byte{1: 0x01})}}
	p2 := &Peer{Info: PeerInfo{ID: NodeIDFromPub([32]byte{1: 0x02})}}
	rt.Add(p1)
	rt.Add(p2)
	target := NodeIDFromPub([32]byte{1: 0x01})
	closest := rt.FindClosest(target, 1)
	if len(closest) != 1 || closest[0].Info.ID != p1.Info.ID {
		t.Fatalf("unexpected closest: %v", closest)
	}
}

func TestEncodeDecodeStore(t *testing.T) {
	key := "mykey"
	value := []byte("myvalue")
	data := encodeStore(key, value)
	k, v := decodeStore(data)
	if k != key || string(v) != string(value) {
		t.Fatalf("decode mismatch: got %q=%q", k, v)
	}
}

// decodeStore reads a length-prefixed key, so a truncated buffer is
// attacker-controlled input on the wire. It must never panic, and must never
// hand back more than the buffer actually contains.
func TestDecodeStoreRejectsTruncatedInput(t *testing.T) {
	full := encodeStore("mykey", []byte("myvalue"))
	for i := 0; i < len(full); i++ {
		truncated := full[:i]
		k, v := decodeStore(truncated)
		if len(v) > 0 && len(k)+2+len(v) > len(truncated) {
			t.Fatalf("truncation to %d bytes returned %d bytes of data",
				i, len(k)+2+len(v))
		}
	}
	// A declared key length that overruns the buffer yields nothing at all.
	k, v := decodeStore(encodeStore("ab", []byte("x"))[:3])
	if k != "" || v != nil {
		t.Fatalf("overrunning key length returned key %q value %q", k, v)
	}
	// A key length header larger than the buffer is rejected outright.
	over := make([]byte, 2)
	over[0], over[1] = 0xff, 0xff
	if k, v := decodeStore(over); k != "" || v != nil {
		t.Fatalf("oversized key length returned key %q value %q", k, v)
	}
}

func TestEncodeDecodeRegister(t *testing.T) {
	pi := PeerInfo{
		ID:        NodeIDFromPub([32]byte{1: 1}),
		PublicKey: [32]byte{1: 1},
		Name:      "node",
	}
	nonce := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	data := encodeRegister(pi, nonce, 20)
	pubKey, n, diff, name, ok := decodeRegister(data)
	if !ok {
		t.Fatal("decodeRegister rejected a well-formed payload")
	}
	if pubKey != pi.PublicKey || string(n) != string(nonce) || diff != 20 || name != pi.Name {
		t.Fatalf("register decode mismatch")
	}
}

// The name is part of the proof-of-work pre-image, so it has to survive the
// wire encoding or the receiver cannot recompute the challenge.
func TestEncodeDecodeRegisterRoundTripsName(t *testing.T) {
	for _, name := range []string{"", "n", "node-with-a-longer-name", "unicode-\u00e9\u00e8\u00ea"} {
		pi := PeerInfo{PublicKey: [32]byte{7}, Name: name}
		_, _, _, got, ok := decodeRegister(encodeRegister(pi, make([]byte, 8), 12))
		if !ok {
			t.Fatalf("decode rejected payload for name %q", name)
		}
		if got != name {
			t.Fatalf("name round-trip: got %q, want %q", got, name)
		}
	}
}

func TestDecodeRegisterRejectsTruncatedInput(t *testing.T) {
	pi := PeerInfo{PublicKey: [32]byte{1}, Name: "node"}
	full := encodeRegister(pi, make([]byte, 8), 12)
	for i := 0; i < len(full); i++ {
		if _, _, _, _, ok := decodeRegister(full[:i]); ok {
			t.Fatalf("truncation to %d bytes still decoded", i)
		}
	}
}

func TestRPCClientRoundTrip(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	go func() {
		conn, _ := ln.Accept()
		if conn == nil {
			return
		}
		defer conn.Close()
		var hdr [65]byte
		if _, err := io.ReadFull(conn, hdr[:]); err != nil {
			return
		}
		var lenBuf [4]byte
		if _, err := io.ReadFull(conn, lenBuf[:]); err != nil {
			return
		}
		plLen := binary.BigEndian.Uint32(lenBuf[:])
		pl := make([]byte, plLen)
		if _, err := io.ReadFull(conn, pl); err != nil {
			return
		}
		resp := Message{Type: MsgFoundNode, Src: NodeID{}, Dst: NodeID{}, Payload: []byte("ok")}
		var out [1 + 32 + 32]byte
		out[0] = byte(MsgFoundNode)
		copy(out[1:33], resp.Src[:])
		copy(out[33:65], resp.Dst[:])
		var rl [4]byte
		binary.BigEndian.PutUint32(rl[:], uint32(len(resp.Payload)))
		conn.Write(out[:])
		conn.Write(rl[:])
		conn.Write(resp.Payload)
	}()

	client := NewRPCClient(func(ctx context.Context, addr string) (net.Conn, error) {
		return net.Dial("tcp", addr)
	})
	msg := Message{Type: MsgFindNode, Src: NodeID{}, Dst: NodeID{}, Payload: []byte("target")}
	resp, err := client.Call(context.Background(), ln.Addr().String(), msg)
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}
	if resp.Type != MsgFoundNode {
		t.Fatalf("unexpected response type: %v", resp.Type)
	}
}

// newTestNode builds a bare Node suitable for exercising handleMessage.
func newTestNode() *Node {
	localID := NodeID{0: 0xff}
	n := &Node{
		id:         localID,
		table:      NewRoutingTable(localID),
		peers:      make(map[NodeID]*Peer),
		store:      make(map[string][]byte),
		listenAddr: "127.0.0.1:0",
	}
	return n
}

// MsgRegisterNode previously fell through to the default branch, so the
// proof of work a registering node paid was never checked and Sybil
// registration was free. These tests pin the gate in place.
func TestHandleRegisterNodeAcceptsValidProofOfWork(t *testing.T) {
	n := newTestNode()
	pubKey := [32]byte{0: 0x42}
	pi := PeerInfo{PublicKey: pubKey, Name: "honest-node"}
	nonce, err := SolvePoW(registrationChallenge(pubKey, pi.Name), MinPoWDifficulty)
	if err != nil {
		t.Fatalf("solve: %v", err)
	}
	id := NodeIDFromPub(pubKey)
	msg := Message{
		Type:    MsgRegisterNode,
		Src:     id,
		Payload: encodeRegister(pi, nonce, MinPoWDifficulty),
	}
	n.handleMessage(msg)

	if _, ok := n.peers[id]; !ok {
		t.Fatal("valid registration was not admitted to the peer set")
	}
	if n.table.Len() != 1 {
		t.Fatalf("routing table holds %d peers, want 1", n.table.Len())
	}
}

func TestHandleRegisterNodeRejectsInvalidProofOfWork(t *testing.T) {
	pubKey := [32]byte{0: 0x42}
	id := NodeIDFromPub(pubKey)

	cases := map[string]func(pubKey [32]byte, name string) (PeerInfo, []byte){
		// Never solved at all: all-zero nonce.
		"no work": func(pk [32]byte, name string) (PeerInfo, []byte) {
			return PeerInfo{PublicKey: pk, Name: name}, make([]byte, 8)
		},
		// Solved for a different identity, then replayed here.
		"work for another key": func(pk [32]byte, name string) (PeerInfo, []byte) {
			n2, err := SolvePoW(registrationChallenge([32]byte{9: 9}, name), MinPoWDifficulty)
			if err != nil {
				t.Fatalf("solve: %v", err)
			}
			return PeerInfo{PublicKey: pk, Name: name}, n2
		},
		// Solved for a different name, then replayed here.
		"work for another name": func(pk [32]byte, name string) (PeerInfo, []byte) {
			n2, err := SolvePoW(registrationChallenge(pk, "someone-else"), MinPoWDifficulty)
			if err != nil {
				t.Fatalf("solve: %v", err)
			}
			return PeerInfo{PublicKey: pk, Name: name}, n2
		},
		// Valid work, but the sender is not the identity that did the work.
		"identity mismatch": func(pk [32]byte, name string) (PeerInfo, []byte) {
			n2, err := SolvePoW(registrationChallenge(pk, name), MinPoWDifficulty)
			if err != nil {
				t.Fatalf("solve: %v", err)
			}
			return PeerInfo{PublicKey: pk, Name: name}, n2
		},
		// A difficulty the system never issues.
		"impossible difficulty": func(pk [32]byte, name string) (PeerInfo, []byte) {
			n2, err := SolvePoW(registrationChallenge(pk, name), MinPoWDifficulty)
			if err != nil {
				t.Fatalf("solve: %v", err)
			}
			return PeerInfo{PublicKey: pk, Name: name}, n2
		},
	}

	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			n := newTestNode()
			pi, nonce := build(pubKey, "sybil-node")

			difficulty := MinPoWDifficulty
			sender := NodeIDFromPub(pubKey)
			switch name {
			case "identity mismatch":
				sender = NodeIDFromPub([32]byte{0xaa})
			case "impossible difficulty":
				// Same valid work, but advertised at a difficulty it does not meet.
				difficulty = MaxPoWDifficulty
			}

			n.handleMessage(Message{
				Type:    MsgRegisterNode,
				Src:     sender,
				Payload: encodeRegister(pi, nonce, difficulty),
			})

			if _, ok := n.peers[id]; ok {
				t.Fatal("registration with an invalid proof of work was admitted")
			}
			if n.table.Len() != 0 {
				t.Fatalf("routing table holds %d peers, want 0", n.table.Len())
			}
		})
	}
}
func TestHandleRegisterNodeRejectsMalformedPayload(t *testing.T) {
	n := newTestNode()
	for _, payload := range [][]byte{nil, {}, make([]byte, 10), make([]byte, 41)} {
		n.handleMessage(Message{Type: MsgRegisterNode, Src: NodeID{0: 1}, Payload: payload})
	}
	if n.table.Len() != 0 {
		t.Fatalf("routing table holds %d peers after malformed payloads", n.table.Len())
	}
}

// The shortlist is what bounds an iterative lookup. It must stay sorted by
// distance and capped, rather than growing with every hop.
func TestDedupeAndPruneSortsDeduplicatesAndCaps(t *testing.T) {
	target := NodeID{0: 0x10}
	mk := func(b ...byte) *Peer {
		var id NodeID
		copy(id[:], b)
		return &Peer{Info: PeerInfo{ID: id}}
	}
	near := mk(0x10, 0x01)
	mid := mk(0x10, 0x02)
	far := mk(0x20, 0x03)
	dup := mk(0x10, 0x01) // same ID as near

	got := dedupeAndPrune([]*Peer{far, mid, near, dup}, target, 3)
	if len(got) != 3 {
		t.Fatalf("expected 3 peers after prune, got %d", len(got))
	}
	seen := map[NodeID]bool{}
	for i, p := range got {
		if seen[p.Info.ID] {
			t.Fatalf("duplicate ID survived pruning at index %d", i)
		}
		seen[p.Info.ID] = true
		if i > 0 {
			prev := xorDist(got[i-1].Info.ID, target)
			cur := xorDist(p.Info.ID, target)
			if compareDist(prev, cur) > 0 {
				t.Fatal("shortlist is not sorted by distance")
			}
		}
	}
	if got[0].Info.ID != near.Info.ID {
		t.Fatal("nearest peer is not first in the shortlist")
	}
}

func TestDedupeAndPruneRespectsLimit(t *testing.T) {
	var peers []*Peer
	for i := 1; i <= 50; i++ {
		var id NodeID
		id[0] = byte(255 - i)
		peers = append(peers, &Peer{Info: PeerInfo{ID: id}})
	}
	if got := len(dedupeAndPrune(peers, NodeID{}, KBucketSize)); got != KBucketSize {
		t.Fatalf("prune returned %d peers, want %d", got, KBucketSize)
	}
}
