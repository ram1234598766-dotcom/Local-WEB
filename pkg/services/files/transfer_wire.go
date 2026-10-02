package files

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"time"

	"github.com/ipfs/go-cid"
	mh "github.com/multiformats/go-multihash"
)

// Wire framing for a WebRTC data-channel transfer.
//
// SCTP data channels deliver bytes, not messages the Files service understands.
// These frames are what actually goes over the channel, and they are the thing
// that makes the transfer resumable: the receiver reports the CIDs it already
// holds and the sender skips them, so a transfer interrupted half way does not
// start over, and a file that mostly matches an existing one only sends what
// differs.
type wireMessageType uint8

const (
	wireHello    wireMessageType = 1 // receiver -> sender: {version, cids[]}
	wireManifest wireMessageType = 2 // sender -> receiver: {total, cids[]}
	wireBlock    wireMessageType = 3 // sender -> receiver: {cid, data}
	wireComplete wireMessageType = 4 // sender -> receiver: {}
	wireAbort    wireMessageType = 5 // either direction: {reason}
)

const (
	transferVersion   = 1
	transferLabel     = "files"
	transferChunkSize = 16 * 1024
	// maxFrameBytes bounds a single frame so a malformed length cannot make the
	// receiver allocate unbounded memory.
	maxFrameBytes = 4 << 20
)

// Content-defined chunking parameters.
//
// A fixed-size chunker would resend everything when a file is edited near the
// start. A rolling hash over a window makes the boundaries depend on the content
// rather than the offset, so an insertion only disturbs the chunks around it.
const (
	cdcMinSize = 2 * 1024
	cdcMaxSize = 64 * 1024
	cdcMask    = 0x1FFF // average ~8 KB
)

// ContentDefinedChunker splits data at content-determined boundaries.
type ContentDefinedChunker struct {
	window []byte
	pos    int
}

// NewContentDefinedChunker creates a chunker with an empty window.
func NewContentDefinedChunker() *ContentDefinedChunker {
	return &ContentDefinedChunker{window: make([]byte, 64)}
}

// Feed consumes input and invokes emit for each complete chunk.
//
// A chunk is emitted when the rolling hash hits the mask and the chunk has
// reached cdcMinSize, or unconditionally at cdcMaxSize so a run of bytes that
// never matches still makes progress.
func (c *ContentDefinedChunker) Feed(data []byte, emit func(chunk []byte) error) error {
	start := 0
	for i := 0; i < len(data); i++ {
		// Roll the byte in and out of the window.
		out := c.window[c.pos]
		c.window[c.pos] = data[i]
		c.pos = (c.pos + 1) % len(c.window)

		size := i - start + 1
		sum := crc32.NewIEEE()
		sum.Write(c.window[:])
		sum.Write(data[start : i+1])

		if (size >= cdcMinSize && sum.Sum32()&cdcMask == 0) || size >= cdcMaxSize {
			if err := emit(data[start : i+1]); err != nil {
				return err
			}
			start = i + 1
		}
		_ = out
	}
	if start < len(data) {
		return emit(data[start:])
	}
	return nil
}

// Chunk splits data into content-defined chunks and returns them with their CIDs.
func Chunk(data []byte) ([]*Block, error) {
	var blocks []*Block
	c := NewContentDefinedChunker()
	err := c.Feed(data, func(chunk []byte) error {
		c, err := CidFor(chunk)
		if err != nil {
			return err
		}
		cp := make([]byte, len(chunk))
		copy(cp, chunk)
		blocks = append(blocks, &Block{CID: c, Data: cp})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return blocks, nil
}

// CidFor computes the content identifier of a block.
//
// This goes through the store's own helper on purpose. The store refuses any
// block whose CID does not match computeBlockCID, so a second, subtly different
// prefix here would make every block the transfer produced unstoreable.
func CidFor(data []byte) (cid.Cid, error) {
	if len(data) == 0 {
		return cid.Cid{}, fmt.Errorf("cannot identify an empty block")
	}
	return computeBlockCID(data), nil
}

// TransferStats reports what a transfer actually cost.
//
// The sender fills Sent* and the receiver fills Received*; SkippedBlocks and
// BytesSaved mean the same thing on both sides, namely blocks that were not
// moved because the far end already had them.
type TransferStats struct {
	TotalBlocks    int
	SentBlocks     int
	ReceivedBlocks int
	SkippedBlocks  int
	BytesSent      int64
	BytesReceived  int64
	BytesSaved     int64
	Duration       time.Duration
	ResumedFrom    int
}

// SavedFraction reports the share of the file that was not sent because the
// receiver already had it. It is the number that says whether the transfer is
// genuinely incremental.
func (s TransferStats) SavedFraction() float64 {
	if s.TotalBlocks == 0 {
		return 0
	}
	return float64(s.SkippedBlocks) / float64(s.TotalBlocks)
}

// reAssembler rebuilds the original file from received blocks.
type reAssembler struct {
	order    []cid.Cid
	received map[cid.Cid][]byte
}

func newReAssembler(order []cid.Cid) *reAssembler {
	return &reAssembler{order: order, received: make(map[cid.Cid][]byte, len(order))}
}

// wants reports whether a CID is part of this transfer at all.
func (r *reAssembler) wants(c cid.Cid) bool {
	for _, want := range r.order {
		if want == c {
			return true
		}
	}
	return false
}

// Bytes returns the reassembled file, or an error if any block is missing.
func (r *reAssembler) Bytes() ([]byte, error) {
	var out []byte
	for i, c := range r.order {
		data, ok := r.received[c]
		if !ok {
			return nil, fmt.Errorf("block %d (%s) never arrived", i, c)
		}
		out = append(out, data...)
	}
	return out, nil
}

// frame is one wire message.
type frame struct {
	Type   wireMessageType
	CIDs   []cid.Cid
	Total  uint32
	CID    cid.Cid
	Data   []byte
	Reason string
}

// encodeFrame serialises a frame. The CIDs are length-prefixed so a count can
// never disagree with the data behind it.
func encodeFrame(f frame) ([]byte, error) {
	var out []byte
	out = append(out, byte(f.Type))
	var scratch [4]byte

	binary.BigEndian.PutUint32(scratch[:], f.Total)
	out = append(out, scratch[:]...)

	binary.BigEndian.PutUint32(scratch[:], uint32(len(f.CIDs)))
	out = append(out, scratch[:]...)
	for _, c := range f.CIDs {
		raw := c.Bytes()
		binary.BigEndian.PutUint32(scratch[:], uint32(len(raw)))
		out = append(out, scratch[:]...)
		out = append(out, raw...)
	}

	cidLen := len(f.CID.Bytes())
	binary.BigEndian.PutUint32(scratch[:], uint32(cidLen))
	out = append(out, scratch[:]...)
	if cidLen > 0 {
		out = append(out, f.CID.Bytes()...)
	}

	binary.BigEndian.PutUint32(scratch[:], uint32(len(f.Data)))
	out = append(out, scratch[:]...)
	out = append(out, f.Data...)

	binary.BigEndian.PutUint32(scratch[:], uint32(len(f.Reason)))
	out = append(out, scratch[:]...)
	out = append(out, f.Reason...)
	return out, nil
}

// decodeFrame parses a frame, refusing any length that would exceed the cap.
//
// The layout interleaves fixed-width integers with length-prefixed blobs, so the
// two are read by different helpers. Conflating them shifts every subsequent
// offset, which turns a valid frame into garbage and a short buffer into a
// panic.
func decodeFrame(b []byte) (frame, error) {
	var f frame
	if len(b) < 5 {
		return f, fmt.Errorf("frame too short: %d bytes", len(b))
	}
	f.Type = wireMessageType(b[0])
	f.Total = binary.BigEndian.Uint32(b[1:5])
	read := 5

	// readU32 reads a bare 4-byte integer at the cursor.
	readU32 := func(what string) (uint32, error) {
		if read+4 > len(b) {
			return 0, fmt.Errorf("frame truncated before %s at offset %d", what, read)
		}
		v := binary.BigEndian.Uint32(b[read : read+4])
		read += 4
		return v, nil
	}

	// readBlob reads a length-prefixed byte slice.
	readBlob := func(what string) ([]byte, error) {
		n, err := readU32(what + " length")
		if err != nil {
			return nil, err
		}
		if n > maxFrameBytes {
			return nil, fmt.Errorf("%s of %d bytes exceeds the %d byte limit", what, n, maxFrameBytes)
		}
		if read+int(n) > len(b) {
			return nil, fmt.Errorf("%s claims %d bytes but only %d remain", what, n, len(b)-read)
		}
		out := b[read : read+int(n)]
		read += int(n)
		return out, nil
	}

	count, err := readU32("cid count")
	if err != nil {
		return f, err
	}
	if count > 0 {
		if int(count) > len(b) {
			return f, fmt.Errorf("cid count %d cannot fit in a %d byte frame", count, len(b))
		}
		f.CIDs = make([]cid.Cid, 0, count)
	}
	for i := uint32(0); i < count; i++ {
		raw, err := readBlob(fmt.Sprintf("cid %d", i))
		if err != nil {
			return f, err
		}
		c, err := cid.Cast(raw)
		if err != nil {
			return f, fmt.Errorf("cid %d is malformed: %w", i, err)
		}
		f.CIDs = append(f.CIDs, c)
	}

	cidRaw, err := readBlob("cid")
	if err != nil {
		return f, err
	}
	if len(cidRaw) > 0 {
		c, err := cid.Cast(cidRaw)
		if err != nil {
			return f, fmt.Errorf("cid is malformed: %w", err)
		}
		f.CID = c
	}

	data, err := readBlob("payload")
	if err != nil {
		return f, err
	}
	f.Data = data

	reason, err := readBlob("reason")
	if err != nil {
		return f, err
	}
	f.Reason = string(reason)
	return f, nil
}

// mhExists keeps the multihash package referenced for CidFor's explicit use.
var _ = mh.SHA2_256

// digestOf is the integrity check applied to a reassembled file.
func digestOf(data []byte) [32]byte { return sha256.Sum256(data) }
