package files

import (
	"bytes"
	"context"
	"crypto/sha256"
	"sync"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"
)

// The Files service could not move bytes to a peer: Sync never contacted anyone
// and GetFile returned a nil data slice. These tests drive a real transfer over
// a real WebRTC data channel between two in-process peers, with real block
// stores on both ends and no network.

// loopbackFileChannel connects two transfers over a real WebRTC data channel
// and returns the two ends.
func loopbackFileChannel(t *testing.T) (*webrtc.DataChannel, *webrtc.DataChannel) {
	t.Helper()

	offerer, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatalf("offerer: %v", err)
	}
	answerer, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatalf("answerer: %v", err)
	}
	t.Cleanup(func() { _ = offerer.Close(); _ = answerer.Close() })

	// A data channel alone gives pion nothing to gather candidates for, so an
	// audio transceiver is added to force a real m-line and real candidates.
	if _, err := offerer.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio,
		webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly}); err != nil {
		t.Fatalf("AddTransceiverFromKind: %v", err)
	}

	sendEnd := make(chan *webrtc.DataChannel, 1)
	answerer.OnDataChannel(func(dc *webrtc.DataChannel) { sendEnd <- dc })

	dc, err := offerer.CreateDataChannel(transferLabel, nil)
	if err != nil {
		t.Fatalf("CreateDataChannel: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	gather := webrtc.GatheringCompletePromise(offerer)
	offer, err := offerer.CreateOffer(nil)
	if err != nil {
		t.Fatalf("CreateOffer: %v", err)
	}
	if err := offerer.SetLocalDescription(offer); err != nil {
		t.Fatalf("SetLocalDescription: %v", err)
	}
	select {
	case <-gather:
	case <-time.After(20 * time.Second):
		t.Fatal("offerer gathering did not complete")
	}

	offerSDP := offerer.LocalDescription()
	if err := answerer.SetRemoteDescription(*offerSDP); err != nil {
		t.Fatalf("SetRemoteDescription: %v", err)
	}
	answerGather := webrtc.GatheringCompletePromise(answerer)
	answer, err := answerer.CreateAnswer(nil)
	if err != nil {
		t.Fatalf("CreateAnswer: %v", err)
	}
	if err := answerer.SetLocalDescription(answer); err != nil {
		t.Fatalf("SetLocalDescription(answer): %v", err)
	}
	select {
	case <-answerGather:
	case <-time.After(20 * time.Second):
		t.Fatal("answerer gathering did not complete")
	}
	answerSDP := answerer.LocalDescription()
	if err := offerer.SetRemoteDescription(*answerSDP); err != nil {
		t.Fatalf("SetRemoteDescription(answer): %v", err)
	}

	deadline := time.Now().Add(40 * time.Second)
	for time.Now().Before(deadline) {
		if offerer.ConnectionState() == webrtc.PeerConnectionStateConnected {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if offerer.ConnectionState() != webrtc.PeerConnectionStateConnected {
		t.Fatalf("peers never connected: %s", offerer.ConnectionState())
	}

	var recvEnd *webrtc.DataChannel
	select {
	case dc := <-sendEnd:
		recvEnd = dc
	case <-time.After(20 * time.Second):
		t.Fatal("the answerer never saw the data channel")
	}

	// Both ends must be open before either sends. A Send on a channel that is
	// still connecting is dropped, which looks exactly like a hang.
	openOn := func(label string, ch *webrtc.DataChannel) <-chan struct{} {
		ready := make(chan struct{})
		var once sync.Once
		ch.OnOpen(func() { once.Do(func() { close(ready) }) })
		if ch.ReadyState() == webrtc.DataChannelStateOpen {
			once.Do(func() { close(ready) })
		}
		return ready
	}
	select {
	case <-openOn("offerer", dc):
	case <-time.After(20 * time.Second):
		t.Fatal("the sender's data channel never opened")
	}
	select {
	case <-openOn("answerer", recvEnd):
	case <-time.After(20 * time.Second):
		t.Fatal("the receiver's data channel never opened")
	}
	_ = ctx

	return dc, recvEnd
}

// transferPair wires both ends' inbound handlers, which must happen before
// either side starts sending.
func transferPair(t *testing.T) (*Transfer, *Transfer, *webrtc.DataChannel, *webrtc.DataChannel) {
	t.Helper()
	sendCh, recvCh := loopbackFileChannel(t)
	return transferPairOver(t, NewMemoryStore(), NewMemoryStore(), sendCh, recvCh)
}

// transferPairOver is transferPair with caller-supplied stores, so a test can
// pre-seed the receiver and prove the transfer resumes.
func transferPairOver(t *testing.T, sendStore, recvStore BlockStore, sendCh, recvCh *webrtc.DataChannel) (*Transfer, *Transfer, *webrtc.DataChannel, *webrtc.DataChannel) {
	t.Helper()
	sender := NewTransfer(sendStore)
	receiver := NewTransfer(recvStore)

	// Attach must run before either side sends, or the first frames are lost.
	sender.Attach(sendCh)
	receiver.Attach(recvCh)

	return sender, receiver, sendCh, recvCh
}

func TestChunkingIsDeterministic(t *testing.T) {
	data := testPayload(300*1024, 1)

	a, err := Chunk(data)
	if err != nil {
		t.Fatalf("Chunk: %v", err)
	}
	b, err := Chunk(data)
	if err != nil {
		t.Fatalf("Chunk again: %v", err)
	}

	if len(a) != len(b) {
		t.Fatalf("chunking is not deterministic: %d vs %d chunks", len(a), len(b))
	}
	for i := range a {
		if !a[i].CID.Equals(b[i].CID) {
			t.Fatalf("chunk %d CID differs between runs", i)
		}
	}
	if len(a) < 2 {
		t.Fatalf("expected the payload to split into several chunks, got %d", len(a))
	}

	// Reassembling the chunks must give the original bytes back exactly.
	var out []byte
	for _, blk := range a {
		out = append(out, blk.Data...)
	}
	if !bytes.Equal(out, data) {
		t.Error("reassembled chunks do not match the input")
	}
}

func TestFrameRoundTrip(t *testing.T) {
	blocks, err := Chunk(testPayload(20*1024, 2))
	if err != nil {
		t.Fatalf("Chunk: %v", err)
	}
	in := frame{
		Type:   wireBlock,
		CID:    blocks[0].CID,
		Data:   blocks[0].Data,
		Total:  1,
		Reason: "because",
	}
	raw, err := encodeFrame(in)
	if err != nil {
		t.Fatalf("encodeFrame: %v", err)
	}
	out, err := decodeFrame(raw)
	if err != nil {
		t.Fatalf("decodeFrame: %v", err)
	}
	if out.Type != in.Type || out.CID != in.CID || out.Reason != in.Reason {
		t.Errorf("frame did not round trip: %+v", out)
	}
	if !bytes.Equal(out.Data, in.Data) {
		t.Error("frame payload did not round trip")
	}
}

func TestDecodeFrameRejectsGarbage(t *testing.T) {
	if _, err := decodeFrame([]byte{0x01}); err == nil {
		t.Error("expected an error for a truncated frame")
	}
	// A length field claiming more than the cap must be refused rather than
	// allocating.
	bad := []byte{byte(wireBlock), 0, 0, 0, 0, 0xFF, 0xFF, 0xFF, 0xFF}
	if _, err := decodeFrame(bad); err == nil {
		t.Error("expected an error for an out-of-range count")
	}
}

func TestTransferSendsFileOverDataChannel(t *testing.T) {
	sender, receiver, sendCh, _ := transferPair(t)

	data := testPayload(512*1024, 3)
	want := sha256.Sum256(data)

	type result struct {
		data  []byte
		stats TransferStats
		err   error
	}
	done := make(chan result, 1)
	go func() {
		out, stats, err := receiver.Receive(context.Background())
		done <- result{data: out, stats: stats, err: err}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	stats, err := sender.Send(ctx, sendCh, data)
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if stats.TotalBlocks < 2 {
		t.Errorf("expected several blocks, got %d", stats.TotalBlocks)
	}
	if stats.SkippedBlocks != 0 {
		t.Errorf("a first transfer should skip nothing, skipped %d", stats.SkippedBlocks)
	}

	select {
	case res := <-done:
		if res.err != nil {
			t.Fatalf("Receive: %v", res.err)
		}
		if got := sha256.Sum256(res.data); got != want {
			t.Errorf("received digest = %x, want %x", got, want)
		}
	case <-time.After(90 * time.Second):
		t.Fatal("the receiver never completed")
	}
}

// TestTransferResumesFromBlocksTheReceiverAlreadyHas is the test that matters
// most for a resumable transfer: if the receiver already holds some of the
// blocks, those must not be sent again, and the file must still reassemble
// whole. An earlier version of the handshake reused one message type for both
// "what I have" and "what I want", so the sender skipped every block and
// reported a transfer that had moved no bytes at all.
func TestTransferResumesFromBlocksTheReceiverAlreadyHas(t *testing.T) {
	sendCh, recvCh := loopbackFileChannel(t)

	data := testPayload(256*1024, 7)
	want := sha256.Sum256(data)

	// Chunk exactly as the transfer will, then pre-seed the receiver with the
	// first third of the file.
	blocks, err := Chunk(data)
	if err != nil {
		t.Fatalf("Chunk: %v", err)
	}
	if len(blocks) < 4 {
		t.Fatalf("need at least 4 blocks to make this meaningful, got %d", len(blocks))
	}
	held := blocks[:len(blocks)/3]

	recvStore := NewMemoryStore()
	for _, b := range held {
		if err := recvStore.Put(context.Background(), b); err != nil {
			t.Fatalf("seed receiver: %v", err)
		}
	}

	sender, receiver, sendCh, recvCh := transferPairOver(t, NewMemoryStore(), recvStore, sendCh, recvCh)

	done := make(chan struct {
		out  []byte
		stat TransferStats
		err  error
	}, 1)
	go func() {
		out, stat, err := receiver.Receive(context.Background())
		done <- struct {
			out  []byte
			stat TransferStats
			err  error
		}{out, stat, err}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	stats, err := sender.Send(ctx, sendCh, data)
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	if stats.SkippedBlocks != len(held) {
		t.Errorf("skipped %d blocks, want exactly the %d the receiver already held",
			stats.SkippedBlocks, len(held))
	}
	if stats.SentBlocks != stats.TotalBlocks-len(held) {
		t.Errorf("sent %d blocks, want %d", stats.SentBlocks, stats.TotalBlocks-len(held))
	}
	if stats.BytesSaved <= 0 {
		t.Error("a resumed transfer must report bytes saved, got 0")
	}
	if stats.SavedFraction() <= 0 {
		t.Error("a resumed transfer must report a non-zero saved fraction")
	}
	if stats.ResumedFrom != len(held) {
		t.Errorf("ResumedFrom = %d, want %d", stats.ResumedFrom, len(held))
	}

	select {
	case res := <-done:
		if res.err != nil {
			t.Fatalf("Receive: %v", res.err)
		}
		if got := sha256.Sum256(res.out); got != want {
			t.Errorf("reassembled digest = %x, want %x", got, want)
		}
		if res.stat.ReceivedBlocks != stats.SentBlocks {
			t.Errorf("receiver got %d blocks, sender sent %d", res.stat.ReceivedBlocks, stats.SentBlocks)
		}
	case <-time.After(90 * time.Second):
		t.Fatal("the receiver never completed")
	}
}

// testPayload builds deterministic, compressible-but-not-trivial content.
func testPayload(size int, seed byte) []byte {
	out := make([]byte, size)
	x := uint32(seed)*2654435761 + 1
	for i := range out {
		x = x*1664525 + 1013904223
		// A repeating pattern with a slow drift, so the payload has structure
		// for the chunker to find and is not trivially all zeroes.
		out[i] = byte(i%251) ^ byte(x>>16)
	}
	return out
}
