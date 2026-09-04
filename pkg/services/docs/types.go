package docs

import (
	"context"
	"fmt"
	"time"

	"github.com/mrityunjay/LocalWEB/pkg/discovery"
	"github.com/mrityunjay/LocalWEB/pkg/transport"
)

// BlockType identifies a rich-text block in a document.
type BlockType uint8

const (
	BlockParagraph   BlockType = iota // Plain text paragraph
	BlockHeading1                      // # Heading
	BlockHeading2                      // ## Heading
	BlockHeading3                      // ### Heading
	BlockBulletList                    // - Bullet item
	BlockNumberedList                  // 1. Numbered item
	BlockCodeBlock                     // ``` code block ```
	BlockQuote                         // > Block quote
)

func (b BlockType) String() string {
	switch b {
	case BlockHeading1:
		return "heading1"
	case BlockHeading2:
		return "heading2"
	case BlockHeading3:
		return "heading3"
	case BlockBulletList:
		return "bullet_list"
	case BlockNumberedList:
		return "numbered_list"
	case BlockCodeBlock:
		return "code_block"
	case BlockQuote:
		return "quote"
	default:
		return "paragraph"
	}
}

// ParseBlockType converts a string to BlockType.
func ParseBlockType(s string) BlockType {
	switch s {
	case "heading1":
		return BlockHeading1
	case "heading2":
		return BlockHeading2
	case "heading3":
		return BlockHeading3
	case "bullet_list":
		return BlockBulletList
	case "numbered_list":
		return BlockNumberedList
	case "code_block":
		return BlockCodeBlock
	case "quote":
		return BlockQuote
	default:
		return BlockParagraph
	}
}

// OpType identifies a document operation.
type OpType uint8

const (
	OpInsert OpType = iota // Insert text at a position
	OpDelete               // Delete text at a position
	OpFormatBlock          // Change a block's formatting
	OpTitleChange          // Change the document title
)

func (o OpType) String() string {
	switch o {
	case OpDelete:
		return "delete"
	case OpFormatBlock:
		return "format_block"
	case OpTitleChange:
		return "title_change"
	default:
		return "insert"
	}
}

// Op represents a single document operation.
type Op struct {
	Type      OpType
	DocID     string
	AuthorID  [32]byte
	Position  int
	Value     string
	BlockType BlockType
	Title     string
	Timestamp int64
}

// DocMessageType identifies a wire message for the docs service.
type DocMessageType uint8

const (
	DocMsgOperation DocMessageType = 0x01 // Operation broadcast
	DocMsgPresence  DocMessageType = 0x02 // Presence update
	DocMsgSyncReq   DocMessageType = 0x03 // Request full state
	DocMsgSyncResp  DocMessageType = 0x04 // Full state response
	DocMsgFullState DocMessageType = 0x05 // Full state push
	DocMsgAck       DocMessageType = 0x06 // Acknowledge operation
)

// DocMessage is the envelope for docs wire messages.
type DocMessage struct {
	Type      DocMessageType
	DocID     string
	AuthorID  [32]byte
	Payload   []byte
	Timestamp int64
}

// DocServiceID is the ServiceID constant for collaborative docs.
const DocServiceID = transport.ServiceDocs

// Wire-level message type for docs ops.
const MsgDocsOp = transport.MessageType(0x30)

// OpResult describes the outcome of applying an operation.
type OpResult struct {
	Applied  bool
	Position int
	Error    error
}

// DocEvent is emitted on document changes for observers.
type DocEvent struct {
	DocID  string
	Op     Op
	Result OpResult
}

// DocEventHandler handles DocEvent callbacks.
type DocEventHandler func(event DocEvent)

// BroadcastFunc sends a message to a set of peers.
type BroadcastFunc func(ctx context.Context, docID string, exclude [32]byte, msg *DocMessage) error

// PeerNotifier abstracts peer broadcast for the docs service.
type PeerNotifier interface {
	Broadcast(ctx context.Context, docID string, exclude [32]byte, msg *DocMessage) error
	Peers() []discovery.PeerInfo
}

// quicBroadcast adapts a transport.Server to PeerNotifier.
type quicBroadcast struct {
	server *transport.Server
}

func newQuicBroadcast(s *transport.Server) *quicBroadcast {
	return &quicBroadcast{server: s}
}

func (b *quicBroadcast) Broadcast(ctx context.Context, docID string, exclude [32]byte, msg *DocMessage) error {
	peers := b.server.Peers()
	var lastErr error
	for _, p := range peers {
		if p.ID == exclude {
			continue
		}
		payload, err := encodeDocMessage(msg)
		if err != nil {
			lastErr = err
			continue
		}
		if err := b.server.SendTo(ctx, p.ID, transport.ServiceDocs, MsgDocsOp, payload); err != nil {
			lastErr = err
		}
	}
	return lastErr
}

func (b *quicBroadcast) Peers() []discovery.PeerInfo {
	transportPeers := b.server.Peers()
	out := make([]discovery.PeerInfo, len(transportPeers))
	for i, p := range transportPeers {
		out[i] = discovery.PeerInfo{
			ID:       p.ID,
			Name:     "",
			Addrs:    []string{p.Addr},
			Source:   "quic",
			LastSeen: p.LastPong,
		}
	}
	return out
}

// encodeDocMessage serializes a DocMessage to bytes.
func encodeDocMessage(msg *DocMessage) ([]byte, error) {
	buf := make([]byte, 0, 1+4+len(msg.DocID)+32+8+len(msg.Payload))
	buf = append(buf, byte(msg.Type))
	buf = append(buf, encodeUint32(uint32(len(msg.DocID)))...)
	buf = append(buf, msg.DocID...)
	buf = append(buf, msg.AuthorID[:]...)
	buf = append(buf, encodeInt64(msg.Timestamp)...)
	buf = append(buf, msg.Payload...)
	return buf, nil
}

// decodeDocMessage deserializes bytes to a DocMessage.
func decodeDocMessage(data []byte) (*DocMessage, error) {
	if len(data) < 1+4+32+8 {
		return nil, fmt.Errorf("doc message too short: %d", len(data))
	}
	msg := &DocMessage{}
	msg.Type = DocMessageType(data[0])
	offset := 1
	nameLen := int(decodeUint32(data[offset : offset+4]))
	offset += 4
	if len(data) < offset+nameLen+32+8 {
		return nil, fmt.Errorf("doc message truncated")
	}
	msg.DocID = string(data[offset : offset+nameLen])
	offset += nameLen
	copy(msg.AuthorID[:], data[offset:offset+32])
	offset += 32
	msg.Timestamp = decodeInt64(data[offset : offset+8])
	offset += 8
	msg.Payload = data[offset:]
	return msg, nil
}

// encodeOpPayload serializes an Op with its DocID into a DocMessage.
func encodeOpPayload(op Op, docID string) ([]byte, error) {
	return encodeDocMessage(&DocMessage{
		Type:    DocMsgOperation,
		DocID:   docID,
		Payload: marshalOp(op),
	})
}

// decodeOpPayload deserializes an Op and docID from payload bytes.
func decodeOpPayload(data []byte) (Op, string, error) {
	msg, err := decodeDocMessage(data)
	if err != nil {
		return Op{}, "", err
	}
	op, err := unmarshalOp(msg.Payload)
	if err != nil {
		return Op{}, "", err
	}
	return op, msg.DocID, nil
}

// marshalOp serializes an Op to binary.
func marshalOp(op Op) []byte {
	buf := make([]byte, 0, 1+32+8+4+4+4+len(op.Value)+4+4+len(op.Title))
	buf = append(buf, byte(op.Type))
	buf = append(buf, op.AuthorID[:]...)
	buf = append(buf, encodeInt64(op.Timestamp)...)
	buf = append(buf, encodeUint32(uint32(op.Position))...)
	buf = append(buf, encodeUint32(uint32(len(op.Value)))...)
	buf = append(buf, op.Value...)
	buf = append(buf, encodeUint32(uint32(op.BlockType))...)
	buf = append(buf, encodeUint32(uint32(len(op.Title)))...)
	buf = append(buf, op.Title...)
	return buf
}

// unmarshalOp deserializes bytes into an Op.
func unmarshalOp(data []byte) (Op, error) {
	var op Op
	if len(data) < 1+32+8+4+4 {
		return op, fmt.Errorf("op payload too short: %d", len(data))
	}
	offset := 0
	op.Type = OpType(data[0])
	offset++
	copy(op.AuthorID[:], data[offset:offset+32])
	offset += 32
	op.Timestamp = decodeInt64(data[offset : offset+8])
	offset += 8
	op.Position = int(decodeUint32(data[offset : offset+4]))
	offset += 4
	valLen := int(decodeUint32(data[offset : offset+4]))
	offset += 4
	if len(data) < offset+valLen {
		return op, fmt.Errorf("op value truncated")
	}
	op.Value = string(data[offset : offset+valLen])
	offset += valLen
	if len(data) < offset+4 {
		op.BlockType = BlockParagraph
		return op, nil
	}
	op.BlockType = BlockType(decodeUint32(data[offset : offset+4]))
	offset += 4
	if len(data) < offset+4 {
		op.Title = ""
		return op, nil
	}
	titleLen := int(decodeUint32(data[offset : offset+4]))
	offset += 4
	if len(data) < offset+titleLen {
		op.Title = ""
		return op, nil
	}
	op.Title = string(data[offset : offset+titleLen])
	return op, nil
}

// encodeUint32 writes v in big-endian order into a 4-byte slice.
func encodeUint32(v uint32) []byte {
	return []byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)}
}

// decodeUint32 reads a big-endian uint32 from b.
func decodeUint32(b []byte) uint32 {
	return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
}

// encodeInt64 writes v in big-endian order into an 8-byte slice.
func encodeInt64(v int64) []byte {
	uv := uint64(v)
	return []byte{
		byte(uv >> 56), byte(uv >> 48), byte(uv >> 40), byte(uv >> 32),
		byte(uv >> 24), byte(uv >> 16), byte(uv >> 8), byte(uv),
	}
}

// decodeInt64 reads a big-endian int64 from b.
func decodeInt64(b []byte) int64 {
	uv := uint64(b[0])<<56 | uint64(b[1])<<48 | uint64(b[2])<<40 | uint64(b[3])<<32 |
		uint64(b[4])<<24 | uint64(b[5])<<16 | uint64(b[6])<<8 | uint64(b[7])
	return int64(uv)
}

// Op constructors.
func NewInsertOp(docID string, author [32]byte, position int, value string, ts int64) Op {
	return Op{Type: OpInsert, DocID: docID, AuthorID: author, Position: position, Value: value, Timestamp: ts}
}

func NewDeleteOp(docID string, author [32]byte, position int, ts int64) Op {
	return Op{Type: OpDelete, DocID: docID, AuthorID: author, Position: position, Timestamp: ts}
}

func NewFormatBlockOp(docID string, author [32]byte, position int, bt BlockType, ts int64) Op {
	return Op{Type: OpFormatBlock, DocID: docID, AuthorID: author, Position: position, BlockType: bt, Timestamp: ts}
}

// CurrentTimestamp returns the current time in nanoseconds.
func CurrentTimestamp() int64 {
	return time.Now().UnixNano()
}
