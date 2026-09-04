package proto

import (
	"time"

	"github.com/ipfs/go-cid"
	"github.com/mrityunjay/LocalWEB/pkg/dht"
	"github.com/mrityunjay/LocalWEB/pkg/services/dns"
	"github.com/mrityunjay/LocalWEB/pkg/services/files"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Marshal serializes any protobuf message to bytes.
func Marshal(msg proto.Message) ([]byte, error) {
	return proto.Marshal(msg)
}

// Unmarshal deserializes bytes into any protobuf message.
func Unmarshal(data []byte, msg proto.Message) error {
	return proto.Unmarshal(data, msg)
}

// --- DHT types ---

// PeerInfoToProto converts dht.PeerInfo to proto.PeerInfo.
func PeerInfoToProto(p dht.PeerInfo) *PeerInfo {
	return &PeerInfo{
		Id:        p.ID[:],
		PublicKey: p.PublicKey[:],
		Name:      p.Name,
		Addrs:     p.Addrs,
		Services:  p.Services,
		Score:     p.Score,
		LastSeen:  p.LastSeen.UnixNano(),
		FirstSeen: p.FirstSeen.UnixNano(),
		Version:   p.Version,
	}
}

// PeerInfoFromProto converts proto.PeerInfo to dht.PeerInfo.
func PeerInfoFromProto(p *PeerInfo) dht.PeerInfo {
	var id dht.NodeID
	copy(id[:], p.Id)
	var pk [32]byte
	copy(pk[:], p.PublicKey)
	return dht.PeerInfo{
		ID:        id,
		PublicKey: pk,
		Name:      p.Name,
		Addrs:     p.Addrs,
		Services:  p.Services,
		Score:     p.Score,
		LastSeen:  time.Unix(0, p.LastSeen),
		FirstSeen: time.Unix(0, p.FirstSeen),
		Version:   p.Version,
	}
}

// --- DHT Message types ---

// MessageToProto converts dht.Message to proto.Message.
func MessageToProto(m dht.Message) *Message {
	var mt MessageType
	switch m.Type {
	case dht.MsgPing:
		mt = MessageType_MSG_PING
	case dht.MsgPong:
		mt = MessageType_MSG_PONG
	case dht.MsgFindNode:
		mt = MessageType_MSG_FIND_NODE
	case dht.MsgFoundNode:
		mt = MessageType_MSG_FOUND_NODE
	case dht.MsgStore:
		mt = MessageType_MSG_STORE
	case dht.MsgFindValue:
		mt = MessageType_MSG_FIND_VALUE
	case dht.MsgFoundValue:
		mt = MessageType_MSG_FOUND_VALUE
	case dht.MsgRegisterNode:
		mt = MessageType_MSG_REGISTER_NODE
	default:
		mt = MessageType_MSG_PING
	}
	var src, dst dht.NodeID
	copy(src[:], m.Src[:])
	copy(dst[:], m.Dst[:])
	return &Message{
		Type:    mt,
		Src:     src[:],
		Dst:     dst[:],
		Payload: m.Payload,
	}
}

// MessageFromProto converts proto.Message to dht.Message.
func MessageFromProto(m *Message) dht.Message {
	var mt dht.MessageType
	switch m.Type {
	case MessageType_MSG_PING:
		mt = dht.MsgPing
	case MessageType_MSG_PONG:
		mt = dht.MsgPong
	case MessageType_MSG_FIND_NODE:
		mt = dht.MsgFindNode
	case MessageType_MSG_FOUND_NODE:
		mt = dht.MsgFoundNode
	case MessageType_MSG_STORE:
		mt = dht.MsgStore
	case MessageType_MSG_FIND_VALUE:
		mt = dht.MsgFindValue
	case MessageType_MSG_FOUND_VALUE:
		mt = dht.MsgFoundValue
	case MessageType_MSG_REGISTER_NODE:
		mt = dht.MsgRegisterNode
	default:
		mt = dht.MsgPing
	}
	var src, dst dht.NodeID
	copy(src[:], m.Src)
	copy(dst[:], m.Dst)
	return dht.Message{
		Type:    mt,
		Src:     src,
		Dst:     dst,
		Payload: m.Payload,
	}
}

// --- DNS types ---

// RecordTypeToProto converts dns.RecordType to proto.RecordType.
func RecordTypeToProto(r dns.RecordType) RecordType {
	switch r {
	case dns.TypeA:
		return RecordType_TYPE_A
	case dns.TypeNS:
		return RecordType_TYPE_NS
	case dns.TypeCNAME:
		return RecordType_TYPE_CNAME
	case dns.TypePTR:
		return RecordType_TYPE_PTR
	case dns.TypeMX:
		return RecordType_TYPE_MX
	case dns.TypeTXT:
		return RecordType_TYPE_TXT
	case dns.TypeAAAA:
		return RecordType_TYPE_AAAA
	case dns.TypeSRV:
		return RecordType_TYPE_SRV
	case dns.TypeHTTPS:
		return RecordType_TYPE_HTTPS
	case dns.TypeSVCB:
		return RecordType_TYPE_SVCB
	default:
		return RecordType_RECORD_RESERVED
	}
}

// RecordTypeFromProto converts proto.RecordType to dns.RecordType.
func RecordTypeFromProto(r RecordType) dns.RecordType {
	switch r {
	case RecordType_TYPE_A:
		return dns.TypeA
	case RecordType_TYPE_NS:
		return dns.TypeNS
	case RecordType_TYPE_CNAME:
		return dns.TypeCNAME
	case RecordType_TYPE_PTR:
		return dns.TypePTR
	case RecordType_TYPE_MX:
		return dns.TypeMX
	case RecordType_TYPE_TXT:
		return dns.TypeTXT
	case RecordType_TYPE_AAAA:
		return dns.TypeAAAA
	case RecordType_TYPE_SRV:
		return dns.TypeSRV
	case RecordType_TYPE_HTTPS:
		return dns.TypeHTTPS
	case RecordType_TYPE_SVCB:
		return dns.TypeSVCB
	default:
		return dns.TypeA
	}
}

// DNSRecordToProto converts dns.DNSRecord to proto.DNSRecord.
func DNSRecordToProto(r dns.DNSRecord) *DNSRecord {
	return &DNSRecord{
		Name:     r.Name,
		Type:     RecordTypeToProto(r.Type),
		Class_:   uint32(r.Class),
		Ttl:      r.TTL,
		RdLength: uint32(r.RDLength),
		Rdata:    r.RData,
	}
}

// DNSRecordFromProto converts proto.DNSRecord to dns.DNSRecord.
func DNSRecordFromProto(r *DNSRecord) dns.DNSRecord {
	return dns.DNSRecord{
		Name:     r.Name,
		Type:     RecordTypeFromProto(r.Type),
		Class:    uint16(r.Class_),
		TTL:      r.Ttl,
		RDLength: uint16(r.RdLength),
		RData:    r.Rdata,
	}
}

// --- File types ---

// BlockToProto converts files.Block to proto.Block.
func BlockToProto(b files.Block) *Block {
	return &Block{
		Cid:  b.CID.Bytes(),
		Data: b.Data,
	}
}

// BlockFromProto converts proto.Block to files.Block.
func BlockFromProto(b *Block) (files.Block, error) {
	if len(b.Cid) == 0 {
		return files.Block{Data: b.Data}, nil
	}
	c, err := cid.Cast(b.Cid)
	if err != nil {
		return files.Block{}, err
	}
	return files.Block{CID: c, Data: b.Data}, nil
}

// BlockMetaToProto converts files.BlockMeta to proto.BlockMeta.
func BlockMetaToProto(m files.BlockMeta) *BlockMeta {
	var ts *timestamppb.Timestamp
	if !m.Created.IsZero() {
		ts = timestamppb.New(m.Created)
	}
	return &BlockMeta{
		Cid:        m.CID.Bytes(),
		Size:       m.Size,
		Compressed: m.Compressed,
		Created:    ts,
		RefCount:   m.RefCount,
	}
}

// BlockMetaFromProto converts proto.BlockMeta to files.BlockMeta.
func BlockMetaFromProto(m *BlockMeta) (files.BlockMeta, error) {
	var c cid.Cid
	if len(m.Cid) > 0 {
		var err error
		c, err = cid.Cast(m.Cid)
		if err != nil {
			return files.BlockMeta{}, err
		}
	}
	var created time.Time
	if m.Created != nil {
		created = m.Created.AsTime()
	}
	return files.BlockMeta{
		CID:        c,
		Size:       m.Size,
		Compressed: m.Compressed,
		Created:    created,
		RefCount:   m.RefCount,
	}, nil
}

// FileMetaToProto converts files.FileMeta to proto.FileMeta.
func FileMetaToProto(m files.FileMeta) *FileMeta {
	var modified, created *timestamppb.Timestamp
	if !m.Modified.IsZero() {
		modified = timestamppb.New(m.Modified)
	}
	if !m.Created.IsZero() {
		created = timestamppb.New(m.Created)
	}
	acl := make([]*ACLEntry, len(m.ACL))
	for i, a := range m.ACL {
		var pk [32]byte
		copy(pk[:], a.PubKey[:])
		acl[i] = &ACLEntry{
			PubKey: pk[:],
			Read:   a.Read,
			Write:  a.Write,
			Admin:  a.Admin,
		}
	}
	var parent []byte
	if m.ParentCID.Defined() {
		parent = m.ParentCID.Bytes()
	}
	return &FileMeta{
		Cid:       m.CID.Bytes(),
		Name:      m.Name,
		Size:      m.Size,
		MimeType:  m.MimeType,
		Modified:  modified,
		Created:   created,
		Acl:       acl,
		Version:   m.Version,
		ParentCid: parent,
	}
}

// FileMetaFromProto converts proto.FileMeta to files.FileMeta.
func FileMetaFromProto(m *FileMeta) (files.FileMeta, error) {
	var c cid.Cid
	if len(m.Cid) > 0 {
		var err error
		c, err = cid.Cast(m.Cid)
		if err != nil {
			return files.FileMeta{}, err
		}
	}
	var modified, created time.Time
	if m.Modified != nil {
		modified = m.Modified.AsTime()
	}
	if m.Created != nil {
		created = m.Created.AsTime()
	}
	var parent cid.Cid
	if len(m.ParentCid) > 0 {
		var err error
		parent, err = cid.Cast(m.ParentCid)
		if err != nil {
			return files.FileMeta{}, err
		}
	}
	acl := make([]files.ACLEntry, len(m.Acl))
	for i, a := range m.Acl {
		var pk [32]byte
		copy(pk[:], a.PubKey)
		acl[i] = files.ACLEntry{
			PubKey: pk,
			Read:   a.Read,
			Write:  a.Write,
			Admin:  a.Admin,
		}
	}
	return files.FileMeta{
		CID:       c,
		Name:      m.Name,
		Size:      m.Size,
		MimeType:  m.MimeType,
		Modified:  modified,
		Created:   created,
		ACL:       acl,
		Version:   m.Version,
		ParentCID: parent,
	}, nil
}

// --- Marshal/Unmarshal convenience helpers ---

// MarshalPeer serializes a dht.PeerInfo to bytes.
func MarshalPeer(p dht.PeerInfo) ([]byte, error) {
	return Marshal(PeerInfoToProto(p))
}

// UnmarshalPeer deserializes bytes into a dht.PeerInfo.
func UnmarshalPeer(data []byte) (dht.PeerInfo, error) {
	var p PeerInfo
	if err := Unmarshal(data, &p); err != nil {
		return dht.PeerInfo{}, err
	}
	return PeerInfoFromProto(&p), nil
}

// MarshalMessage serializes a dht.Message to bytes.
func MarshalMessage(m dht.Message) ([]byte, error) {
	return Marshal(MessageToProto(m))
}

// UnmarshalMessage deserializes bytes into a dht.Message.
func UnmarshalMessage(data []byte) (dht.Message, error) {
	var m Message
	if err := Unmarshal(data, &m); err != nil {
		return dht.Message{}, err
	}
	return MessageFromProto(&m), nil
}

// MarshalDNSRecord serializes a dns.DNSRecord to bytes.
func MarshalDNSRecord(r dns.DNSRecord) ([]byte, error) {
	return Marshal(DNSRecordToProto(r))
}

// UnmarshalDNSRecord deserializes bytes into a dns.DNSRecord.
func UnmarshalDNSRecord(data []byte) (dns.DNSRecord, error) {
	var r DNSRecord
	if err := Unmarshal(data, &r); err != nil {
		return dns.DNSRecord{}, err
	}
	return DNSRecordFromProto(&r), nil
}

// MarshalBlock serializes a files.Block to bytes.
func MarshalBlock(b files.Block) ([]byte, error) {
	return Marshal(BlockToProto(b))
}

// UnmarshalBlock deserializes bytes into a files.Block.
func UnmarshalBlock(data []byte) (files.Block, error) {
	var b Block
	if err := Unmarshal(data, &b); err != nil {
		return files.Block{}, err
	}
	return BlockFromProto(&b)
}

// MarshalBlockMeta serializes a files.BlockMeta to bytes.
func MarshalBlockMeta(m files.BlockMeta) ([]byte, error) {
	return Marshal(BlockMetaToProto(m))
}

// UnmarshalBlockMeta deserializes bytes into a files.BlockMeta.
func UnmarshalBlockMeta(data []byte) (files.BlockMeta, error) {
	var m BlockMeta
	if err := Unmarshal(data, &m); err != nil {
		return files.BlockMeta{}, err
	}
	return BlockMetaFromProto(&m)
}

// MarshalFileMeta serializes a files.FileMeta to bytes.
func MarshalFileMeta(m files.FileMeta) ([]byte, error) {
	return Marshal(FileMetaToProto(m))
}

// UnmarshalFileMeta deserializes bytes into a files.FileMeta.
func UnmarshalFileMeta(data []byte) (files.FileMeta, error) {
	var m FileMeta
	if err := Unmarshal(data, &m); err != nil {
		return files.FileMeta{}, err
	}
	return FileMetaFromProto(&m)
}
