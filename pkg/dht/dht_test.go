package dht

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"testing"
)

func TestSolveAndVerifyPoW(t *testing.T) {
	data := []byte("test data")
	nonce, err := SolvePoW(data, 8)
	if err != nil {
		t.Fatalf("SolvePoW failed: %v", err)
	}
	if !VerifyPoW(data, nonce, 8) {
		t.Fatal("VerifyPoW failed for valid nonce")
	}
	if VerifyPoW(data, []byte("badnonce"), 8) {
		t.Fatal("VerifyPoW passed for invalid nonce")
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

func TestEncodeDecodeRegister(t *testing.T) {
	pi := PeerInfo{ID: NodeIDFromPub([32]byte{1: 1}), Name: "node"}
	nonce := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	data := encodeRegister(pi, nonce, 20)
	pubKey, n, diff := decodeRegister(data)
	if pubKey != pi.PublicKey || string(n) != string(nonce) || diff != 20 {
		t.Fatalf("register decode mismatch")
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
