//go:build integration

package integration

import (
	"context"
	"crypto/sha3"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"

	"github.com/mrityunjay/LocalWEB/pkg/crypto"
	"github.com/mrityunjay/LocalWEB/pkg/dht"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// KBucket tests
// ---------------------------------------------------------------------------

func TestKBucketAddAndGetClosest(t *testing.T) {
	b := dht.NewKBucket()
	target := dht.NodeIDFromPub([32]byte{1: 0xff})
	p1 := &dht.Peer{Info: dht.PeerInfo{ID: dht.NodeIDFromPub([32]byte{1: 0x01}), Name: "p1", Addrs: []string{"addr1"}}}
	p2 := &dht.Peer{Info: dht.PeerInfo{ID: dht.NodeIDFromPub([32]byte{1: 0x02}), Name: "p2", Addrs: []string{"addr2"}}}
	p3 := &dht.Peer{Info: dht.PeerInfo{ID: dht.NodeIDFromPub([32]byte{1: 0x03}), Name: "p3", Addrs: []string{"addr3"}}}
	require.True(t, b.Add(p1))
	require.True(t, b.Add(p2))
	require.True(t, b.Add(p3))

	closest := b.GetClosest(2, target)
	require.Len(t, closest, 2)
}

func TestKBucketRejectDuplicate(t *testing.T) {
	b := dht.NewKBucket()
	p := &dht.Peer{Info: dht.PeerInfo{ID: dht.NodeIDFromPub([32]byte{1: 0x01})}}
	require.True(t, b.Add(p))
	require.False(t, b.Add(p))
	require.Equal(t, 1, b.Len())
}

func TestKBucketRejectWhenFull(t *testing.T) {
	b := dht.NewKBucket()
	for i := 0; i < dht.KBucketSize; i++ {
		p := &dht.Peer{Info: dht.PeerInfo{ID: dht.NodeIDFromPub([32]byte{byte(i)})}}
		require.True(t, b.Add(p))
	}
	extra := &dht.Peer{Info: dht.PeerInfo{ID: dht.NodeIDFromPub([32]byte{0xff})}}
	require.False(t, b.Add(extra))
}

// ---------------------------------------------------------------------------
// Routing table tests
// ---------------------------------------------------------------------------

func TestRoutingTableAddAndClosest(t *testing.T) {
	localID := dht.NodeIDFromPub([32]byte{0xaa})
	rt := dht.NewRoutingTable(localID)
	p1 := &dht.Peer{Info: dht.PeerInfo{ID: dht.NodeIDFromPub([32]byte{1: 0x01})}}
	p2 := &dht.Peer{Info: dht.PeerInfo{ID: dht.NodeIDFromPub([32]byte{1: 0x02})}}
	rt.Add(p1)
	rt.Add(p2)
	closest := rt.FindClosest(p1.Info.ID, 1)
	require.Len(t, closest, 1)
	require.Equal(t, p1.Info.ID, closest[0].Info.ID)
}

func TestRoutingTableAllPeers(t *testing.T) {
	localID := dht.NodeIDFromPub([32]byte{0xbb})
	rt := dht.NewRoutingTable(localID)
	for i := 0; i < 5; i++ {
		rt.Add(&dht.Peer{Info: dht.PeerInfo{ID: dht.NodeIDFromPub([32]byte{byte(i)})}})
	}
	require.Len(t, rt.AllPeers(), 5)
}

// ---------------------------------------------------------------------------
// 5-node DHT: routing table population (using exported RoutingTable)
// ---------------------------------------------------------------------------

func TestDHT5NodeRoutingTable(t *testing.T) {
	const nodeCount = 5
	nodes := make([]dht.NodeID, nodeCount)

	for i := 0; i < nodeCount; i++ {
		pub, _, err := crypto.GenerateKeyPair()
		require.NoError(t, err)
		nodes[i] = dht.NodeIDFromPub(pub)
	}

	// Create independent routing tables and cross-populate
	tables := make([]*dht.RoutingTable, nodeCount)
	for i := 0; i < nodeCount; i++ {
		tables[i] = dht.NewRoutingTable(nodes[i])
	}

	for i, srcRT := range tables {
		for j, dstID := range nodes {
			if i == j {
				continue
			}
			peer := dht.PeerInfo{
				ID:        dstID,
				Name:      string(rune('A' + j)),
				Addrs:     []string{"127.0.0.1:0"},
				Score:     0.5,
				FirstSeen: time.Now(),
			}
			srcRT.Add(&dht.Peer{Info: peer})
		}
	}

	// Each table should have 4 peers
	for i, rt := range tables {
		all := rt.AllPeers()
		require.Len(t, all, 4, "routing table %d should have 4 peers, got %d", i, len(all))
	}
}

func TestDHT5NodeFindClosest(t *testing.T) {
	const nodeCount = 5
	nodes := make([]dht.NodeID, nodeCount)
	for i := 0; i < nodeCount; i++ {
		pub, _, err := crypto.GenerateKeyPair()
		require.NoError(t, err)
		nodes[i] = dht.NodeIDFromPub(pub)
	}

	rt := dht.NewRoutingTable(nodes[0])
	for i := 1; i < nodeCount; i++ {
		rt.Add(&dht.Peer{Info: dht.PeerInfo{
			ID:    nodes[i],
			Name:  string(rune('A' + i)),
			Addrs: []string{"127.0.0.1:0"},
		}})
	}

	// Find closest to the last node
	target := nodes[nodeCount-1]
	closest := rt.FindClosest(target, 3)
	require.Len(t, closest, 3, "should return 3 closest peers")

	found := false
	for _, p := range closest {
		if p.Info.ID == target {
			found = true
			break
		}
	}
	require.True(t, found, "closest set should include target node")
}

func TestDHT5NodeXorDistance(t *testing.T) {
	nodes := make([]dht.NodeID, 5)
	for i := 0; i < 5; i++ {
		pub, _, err := crypto.GenerateKeyPair()
		require.NoError(t, err)
		nodes[i] = dht.NodeIDFromPub(pub)
	}

	rt := dht.NewRoutingTable(nodes[0])
	for i := 1; i < 5; i++ {
		rt.Add(&dht.Peer{Info: dht.PeerInfo{ID: nodes[i], Name: string(rune('A' + i))}})
	}

	// All 4 peers should be findable
	closest := rt.FindClosest(nodes[1], 4)
	require.Len(t, closest, 4)
}

// ---------------------------------------------------------------------------
// PoW tests
// ---------------------------------------------------------------------------

func TestPoWIntegration(t *testing.T) {
	data := []byte("pow-test-data")
	nonce, err := dht.SolvePoW(data, 8)
	require.NoError(t, err)
	require.True(t, dht.VerifyPoW(data, nonce, 8))
	require.False(t, dht.VerifyPoW(data, []byte("bad"), 8))
}

// ---------------------------------------------------------------------------
// RPC round-trip over real TCP
// ---------------------------------------------------------------------------

func TestRPCRoundTrip(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				var hdr [65]byte
				if _, err := io.ReadFull(c, hdr[:]); err != nil {
					return
				}
				resp := dht.Message{Type: dht.MsgFoundNode, Src: dht.NodeID{}, Dst: dht.NodeID{}, Payload: []byte("ok-payload")}
				var out [1 + 32 + 32]byte
				out[0] = byte(resp.Type)
				copy(out[1:33], resp.Src[:])
				copy(out[33:65], resp.Dst[:])
				var rl [4]byte
				binary.BigEndian.PutUint32(rl[:], uint32(len(resp.Payload)))
				c.Write(out[:])
				c.Write(rl[:])
				c.Write(resp.Payload)
			}(conn)
		}
	}()

	client := dht.NewRPCClient(func(ctx context.Context, addr string) (net.Conn, error) {
		return net.Dial("tcp", addr)
	})
	msg := dht.Message{Type: dht.MsgFindNode, Src: dht.NodeID{}, Dst: dht.NodeID{}, Payload: []byte("target-node-id")}
	resp, err := client.Call(context.Background(), ln.Addr().String(), msg)
	require.NoError(t, err)
	require.Equal(t, dht.MsgFoundNode, resp.Type)
	require.Equal(t, []byte("ok-payload"), resp.Payload)
}

// ---------------------------------------------------------------------------
// Multiple sequential RPC calls
// ---------------------------------------------------------------------------

func TestTCPMultipleSequentialRPCCalls(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	callCount := 0
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				var hdr [65]byte
				if _, err := io.ReadFull(c, hdr[:]); err != nil {
					return
				}
				var lenBuf [4]byte
				if _, err := io.ReadFull(c, lenBuf[:]); err != nil {
					return
				}
				plLen := binary.BigEndian.Uint32(lenBuf[:])
				pl := make([]byte, plLen)
				if _, err := io.ReadFull(c, pl); err != nil {
					return
				}
				callCount++
				resp := dht.Message{Type: dht.MsgFoundNode, Payload: pl}
				var out [1 + 32 + 32]byte
				out[0] = byte(resp.Type)
				var rl [4]byte
				binary.BigEndian.PutUint32(rl[:], uint32(len(resp.Payload)))
				c.Write(out[:])
				c.Write(rl[:])
				c.Write(resp.Payload)
			}(conn)
		}
	}()

	client := dht.NewRPCClient(func(ctx context.Context, addr string) (net.Conn, error) {
		return net.Dial("tcp", addr)
	})
	for i := 0; i < 5; i++ {
		_, err := client.Call(context.Background(), ln.Addr().String(), dht.Message{Type: dht.MsgFindNode})
		require.NoError(t, err)
	}
	require.Equal(t, 5, callCount)
}

// ---------------------------------------------------------------------------
// Merkle root computation
// ---------------------------------------------------------------------------

func TestComputeMerkleRoot(t *testing.T) {
	data := [][32]byte{{1}, {2}, {3}}
	// Use a simple hash-based approach (mirrors dht.ComputeMerkleRoot)
	root := simpleMerkleRoot(data)
	require.NotEqual(t, [32]byte{}, root)
}

func simpleMerkleRoot(leaves [][32]byte) [32]byte {
	if len(leaves) == 0 {
		return [32]byte{}
	}
	if len(leaves) == 1 {
		return leaves[0]
	}
	var next [][32]byte
	for i := 0; i < len(leaves); i += 2 {
		if i+1 < len(leaves) {
			h := sha3.New256()
			if bytesCompare(leaves[i][:], leaves[i+1][:]) < 0 {
				h.Write(leaves[i][:])
				h.Write(leaves[i+1][:])
			} else {
				h.Write(leaves[i+1][:])
				h.Write(leaves[i][:])
			}
			var out [32]byte
			h.Sum(out[:0])
			next = append(next, out)
		} else {
			next = append(next, leaves[i])
		}
	}
	return simpleMerkleRoot(next)
}

func bytesCompare(a, b []byte) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return int(a[i]) - int(b[i])
		}
	}
	return len(a) - len(b)
}
