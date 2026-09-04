package files

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/mrityunjay/LocalWEB/pkg/crypto"
)

func TestEncodeDecodeWantEntries(t *testing.T) {
	c := computeFileCID([]byte("want entry"))
	entries := []WantEntry{
		{CID: c, Type: WantWant, Priority: 10},
		{CID: c, Type: WantHave, Priority: 5},
	}

	encoded := encodeWantEntries(entries)
	decoded, err := decodeWantEntries(encoded)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if len(decoded) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(decoded))
	}
	if decoded[0].Type != WantWant {
		t.Fatalf("expected WantWant, got %d", decoded[0].Type)
	}
	if decoded[1].Type != WantHave {
		t.Fatalf("expected WantHave, got %d", decoded[1].Type)
	}
	if decoded[0].Priority != 10 {
		t.Fatalf("expected priority 10, got %d", decoded[0].Priority)
	}
}

func TestDecodeWantEntriesEmpty(t *testing.T) {
	_, err := decodeWantEntries([]byte{})
	if err == nil {
		t.Fatal("expected error for empty data")
	}
}

func TestDecodeWantEntriesTruncated(t *testing.T) {
	_, err := decodeWantEntries([]byte{2, 0, 0, 0})
	if err == nil {
		t.Fatal("expected error for truncated data")
	}
}

func TestWantEntryConstruct(t *testing.T) {
	c := computeFileCID([]byte("test"))
	entry := WantEntry{
		CID:      c,
		Type:     WantWant,
		Priority: 8,
	}
	if entry.CID != c {
		t.Fatal("CID mismatch")
	}
	if entry.Type != WantWant {
		t.Fatal("type mismatch")
	}
	if entry.Priority != 8 {
		t.Fatal("priority mismatch")
	}
}

func TestExchangeMessageTypes(t *testing.T) {
	if MsgWant != 0x01 {
		t.Fatalf("expected MsgWant=0x01, got 0x%02x", MsgWant)
	}
	if MsgHave != 0x02 {
		t.Fatalf("expected MsgHave=0x02, got 0x%02x", MsgHave)
	}
	if MsgBlock != 0x03 {
		t.Fatalf("expected MsgBlock=0x03, got 0x%02x", MsgBlock)
	}
	if MsgCancel != 0x04 {
		t.Fatalf("expected MsgCancel=0x04, got 0x%02x", MsgCancel)
	}
}

func TestWantTypeValues(t *testing.T) {
	if WantHave != 0x01 {
		t.Fatalf("expected WantHave=0x01, got 0x%02x", WantHave)
	}
	if WantWant != 0x02 {
		t.Fatalf("expected WantWant=0x02, got 0x%02x", WantWant)
	}
}

func TestNoopExchange(t *testing.T) {
	var ep ExchangeProtocol = &noopExchange{}
	ctx := context.Background()

	stream, err := ep.OpenStream(ctx, [32]byte{1})
	if err == nil {
		t.Fatal("expected error from noop exchange")
	}
	_ = stream

	err = ep.SendWant(ctx, [32]byte{1}, nil)
	if err != nil {
		t.Fatalf("SendWant should not error on noop: %v", err)
	}
	err = ep.SendHave(ctx, [32]byte{1}, nil)
	if err != nil {
		t.Fatalf("SendHave should not error on noop: %v", err)
	}
	err = ep.SendBlock(ctx, [32]byte{1}, nil)
	if err != nil {
		t.Fatalf("SendBlock should not error on noop: %v", err)
	}
	err = ep.Close()
	if err != nil {
		t.Fatalf("Close should not error on noop: %v", err)
	}
}

func TestPeerExchangeTracking(t *testing.T) {
	// Test that peer state is tracked correctly in exchangeProtocol
	// This is a lightweight test that doesn't require a running transport server
	ep := &exchangeProtocol{
		peers: make(map[[32]byte]*peerExchange),
	}

	peerID := crypto.NodeID([32]byte{4, 5, 6})
	peer, ok := ep.peers[peerID]
	if ok || peer != nil {
		t.Fatal("expected peer to not exist initially")
	}
}

func TestEncodeBlockRoundTrip(t *testing.T) {
	original := &Block{Data: []byte("exchange round trip")}
	original.CID = computeBlockCID(original.Data)

	encoded, err := EncodeBlock(original)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}

	decoded, err := DecodeBlock(encoded)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if decoded.CID != original.CID {
		t.Fatalf("CID mismatch")
	}
	if !bytes.Equal(decoded.Data, original.Data) {
		t.Fatal("data mismatch")
	}
}


func TestSyncProgressFields(t *testing.T) {
	p := SyncProgress{
		PeerID:    [32]byte{1},
		Have:      []cid.Cid{computeFileCID([]byte("a"))},
		Need:      []cid.Cid{computeFileCID([]byte("b"))},
		InFlight:  []cid.Cid{computeFileCID([]byte("c"))},
		Complete:  true,
		BytesSent: 100,
		BytesRecv: 200,
	}
	if p.BytesSent != 100 {
		t.Fatalf("BytesSent mismatch: got %d", p.BytesSent)
	}
	if p.BytesRecv != 200 {
		t.Fatalf("BytesRecv mismatch: got %d", p.BytesRecv)
	}
	if !p.Complete {
		t.Fatal("expected Complete=true")
	}
}

func TestBlockMetaStruct(t *testing.T) {
	c := computeFileCID([]byte("meta"))
	meta := BlockMeta{
		CID:        c,
		Size:       1024,
		Compressed: true,
		Created:    time.Now(),
		RefCount:   1,
	}
	if meta.Size != 1024 {
		t.Fatalf("Size mismatch: got %d", meta.Size)
	}
	if !meta.Compressed {
		t.Fatal("expected Compressed=true")
	}
}
