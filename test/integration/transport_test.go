//go:build integration

package integration

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/mrityunjay/LocalWEB/pkg/dht"
	"github.com/mrityunjay/LocalWEB/pkg/transport"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Frame encode / decode round-trip
// ---------------------------------------------------------------------------

func TestFrameEncodeDecode(t *testing.T) {
	payload := []byte("hello transport")
	frame := transport.EncodeFrameBare(transport.MsgPing, payload)

	decoded, err := transport.DecodeFrameBare(frame)
	require.NoError(t, err)
	require.Equal(t, transport.MsgPing, decoded.Type)
	require.Equal(t, uint32(len(payload)), decoded.Length)
	require.Equal(t, payload, decoded.Payload)
}

func TestFrameDecodeTooShort(t *testing.T) {
	_, err := transport.DecodeFrameBare([]byte{1, 2, 3})
	require.Error(t, err)
}

func TestFrameDecodeIncomplete(t *testing.T) {
	buf := make([]byte, 5)
	buf[0] = byte(transport.MsgPong)
	binary.BigEndian.PutUint32(buf[1:5], 100) // claims 100 bytes but only 5 provided
	_, err := transport.DecodeFrameBare(buf)
	require.Error(t, err)
}

func TestFrameTooLarge(t *testing.T) {
	buf := make([]byte, 5)
	buf[0] = byte(transport.MsgAnnounce)
	binary.BigEndian.PutUint32(buf[1:5], transport.MaxFrameSize+1)
	_, err := transport.DecodeFrameBare(buf)
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// TCP server integration – accept and respond (using dht wire protocol)
// ---------------------------------------------------------------------------

func TestTCPServerAcceptAndRespond(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	var receivedType dht.MessageType
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
		receivedType = dht.MessageType(hdr[0])

		var lenBuf [4]byte
		if _, err := io.ReadFull(conn, lenBuf[:]); err != nil {
			return
		}
		plLen := binary.BigEndian.Uint32(lenBuf[:])
		pl := make([]byte, plLen)
		if _, err := io.ReadFull(conn, pl); err != nil {
			return
		}

		resp := dht.Message{
			Type:    dht.MsgPong,
			Src:     dht.NodeID{},
			Dst:     dht.NodeID{},
			Payload: []byte("pong-response"),
		}
		var out [1 + 32 + 32]byte
		out[0] = byte(resp.Type)
		copy(out[1:33], resp.Src[:])
		copy(out[33:65], resp.Dst[:])
		var rl [4]byte
		binary.BigEndian.PutUint32(rl[:], uint32(len(resp.Payload)))
		conn.Write(out[:])
		conn.Write(rl[:])
		conn.Write(resp.Payload)
	}()

	client := dht.NewRPCClient(func(ctx context.Context, addr string) (net.Conn, error) {
		return net.Dial("tcp", addr)
	})
	msg := dht.Message{
		Type:    dht.MsgPing,
		Src:     [32]byte{1},
		Dst:     [32]byte{2},
		Payload: []byte("ping-payload"),
	}
	resp, err := client.Call(context.Background(), ln.Addr().String(), msg)
	require.NoError(t, err)
	require.Equal(t, dht.MsgPong, resp.Type)
	require.Equal(t, dht.MsgPing, receivedType)
}

// ---------------------------------------------------------------------------
// Concurrent RPC calls
// ---------------------------------------------------------------------------

func TestTCPConcurrentRPCCalls(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	var mu sync.Mutex
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
				mu.Lock()
				callCount++
				mu.Unlock()

				resp := dht.Message{Type: dht.MsgPong, Payload: []byte("concurrent-pong")}
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

	const workers = 10
	done := make(chan struct{}, workers)
	for i := 0; i < workers; i++ {
		go func() {
			_, err := client.Call(context.Background(), ln.Addr().String(), dht.Message{Type: dht.MsgFindNode})
			if err != nil {
				t.Logf("concurrent call error: %v", err)
			}
			done <- struct{}{}
		}()
	}
	for i := 0; i < workers; i++ {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("timeout waiting for concurrent RPC calls")
		}
	}
	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, workers, callCount, "all concurrent calls should be served")
}

// ---------------------------------------------------------------------------
// Relay wire protocol encode/decode
// ---------------------------------------------------------------------------

func TestRelayExtendEncodeDecode(t *testing.T) {
	circuitID := "circuit-abc-123"
	var target [32]byte
	for i := range target {
		target[i] = byte(i)
	}

	data := transport.EncodeRelayExtend(circuitID, target)
	decodedID, decodedTarget, err := transport.DecodeRelayExtend(data)
	require.NoError(t, err)
	require.Equal(t, circuitID, decodedID)
	require.Equal(t, target, decodedTarget)
}

func TestRelayExtendDecodeTooShort(t *testing.T) {
	_, _, err := transport.DecodeRelayExtend([]byte{1, 2, 3})
	require.Error(t, err)
}

func TestRelayExtendDecodeInvalid(t *testing.T) {
	buf := make([]byte, 4)
	binary.BigEndian.PutUint32(buf[0:4], 100) // claims 100 bytes for ID
	_, _, err := transport.DecodeRelayExtend(buf)
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// Flow control defaults
// ---------------------------------------------------------------------------

func TestDefaultFlowControl(t *testing.T) {
	fc := transport.DefaultFlowControl()
	_ = fc // just verify it constructs without panic
}

// ---------------------------------------------------------------------------
// ServiceID constants
// ---------------------------------------------------------------------------

func TestServiceIDConstants(t *testing.T) {
	require.Equal(t, transport.ServiceID('C'), transport.ServiceControl)
	require.Equal(t, transport.ServiceID('D'), transport.ServiceDNS)
	require.Equal(t, transport.ServiceID('H'), transport.ServiceHTTP)
	require.Equal(t, transport.ServiceID('M'), transport.ServiceMsg)
	require.Equal(t, transport.ServiceID('F'), transport.ServiceFS)
	require.Equal(t, transport.ServiceID('R'), transport.ServiceRelay)
	require.Equal(t, transport.ServiceID('V'), transport.ServiceVoice)
}

// ---------------------------------------------------------------------------
// MessageType constants
// ---------------------------------------------------------------------------

func TestTransportMessageTypeConstants(t *testing.T) {
	require.Equal(t, transport.MessageType(0x01), transport.MsgPing)
	require.Equal(t, transport.MessageType(0x02), transport.MsgPong)
	require.Equal(t, transport.MessageType(0x03), transport.MsgFindNode)
	require.Equal(t, transport.MessageType(0x04), transport.MsgFoundNode)
	require.Equal(t, transport.MessageType(0x05), transport.MsgStore)
	require.Equal(t, transport.MessageType(0x06), transport.MsgFindValue)
}
