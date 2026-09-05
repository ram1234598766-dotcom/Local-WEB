package proto_test

import (
	"testing"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/dht"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/proto"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/services/dns"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/services/files"
	"github.com/stretchr/testify/require"
)

func TestPeerInfoRoundTrip(t *testing.T) {
	orig := dht.PeerInfo{
		ID:        dht.NodeID{1, 2, 3},
		PublicKey: [32]byte{4, 5, 6},
		Name:      "test-peer",
		Addrs:     []string{"127.0.0.1:4040", "10.0.0.1:4040"},
		Services:  []string{"dns", "fs"},
		Score:     0.75,
		LastSeen:  time.Now(),
		FirstSeen: time.Now().Add(-time.Hour),
		Version:   "v0.1.0",
	}

	data, err := proto.MarshalPeer(orig)
	require.NoError(t, err)

	got, err := proto.UnmarshalPeer(data)
	require.NoError(t, err)

	require.Equal(t, orig.ID, got.ID)
	require.Equal(t, orig.PublicKey, got.PublicKey)
	require.Equal(t, orig.Name, got.Name)
	require.Equal(t, orig.Addrs, got.Addrs)
	require.Equal(t, orig.Services, got.Services)
	require.Equal(t, orig.Score, got.Score)
	require.Equal(t, orig.Version, got.Version)
	require.Equal(t, orig.LastSeen.UnixNano(), got.LastSeen.UnixNano())
	require.Equal(t, orig.FirstSeen.UnixNano(), got.FirstSeen.UnixNano())
}

func TestPeerInfoRoundTrip_Empty(t *testing.T) {
	orig := dht.PeerInfo{}

	data, err := proto.MarshalPeer(orig)
	require.NoError(t, err)

	got, err := proto.UnmarshalPeer(data)
	require.NoError(t, err)

	require.Equal(t, orig.Name, got.Name)
	require.Equal(t, orig.Score, got.Score)
	require.Equal(t, orig.Version, got.Version)
}

func TestMessageRoundTrip(t *testing.T) {
	orig := dht.Message{
		Type:    dht.MsgFindNode,
		Src:     dht.NodeID{1, 2, 3},
		Dst:     dht.NodeID{4, 5, 6},
		Payload: []byte("hello payload"),
	}

	data, err := proto.MarshalMessage(orig)
	require.NoError(t, err)

	got, err := proto.UnmarshalMessage(data)
	require.NoError(t, err)

	require.Equal(t, orig.Type, got.Type)
	require.Equal(t, orig.Src, got.Src)
	require.Equal(t, orig.Dst, got.Dst)
	require.Equal(t, orig.Payload, got.Payload)
}

func TestMessageAllTypes(t *testing.T) {
	types := []dht.MessageType{
		dht.MsgPing,
		dht.MsgPong,
		dht.MsgFindNode,
		dht.MsgFoundNode,
		dht.MsgStore,
		dht.MsgFindValue,
		dht.MsgFoundValue,
		dht.MsgRegisterNode,
	}

	for _, mt := range types {
		orig := dht.Message{
			Type:    mt,
			Src:     dht.NodeID{10},
			Dst:     dht.NodeID{20},
			Payload: []byte{0xDE, 0xAD, 0xBE, 0xEF},
		}

		data, err := proto.MarshalMessage(orig)
		require.NoError(t, err, "type=%d", mt)

		got, err := proto.UnmarshalMessage(data)
		require.NoError(t, err, "type=%d", mt)
		require.Equal(t, orig.Type, got.Type, "type=%d", mt)
		require.Equal(t, orig.Src, got.Src, "type=%d", mt)
		require.Equal(t, orig.Dst, got.Dst, "type=%d", mt)
		require.Equal(t, orig.Payload, got.Payload, "type=%d", mt)
	}
}

func TestDNSRecordRoundTrip(t *testing.T) {
	orig := dns.DNSRecord{
		Name:     "host.localweb",
		Type:     dns.TypeA,
		Class:    1,
		TTL:      300,
		RDLength: 4,
		RData:    []byte{192, 168, 1, 1},
	}

	data, err := proto.MarshalDNSRecord(orig)
	require.NoError(t, err)

	got, err := proto.UnmarshalDNSRecord(data)
	require.NoError(t, err)

	require.Equal(t, orig.Name, got.Name)
	require.Equal(t, orig.Type, got.Type)
	require.Equal(t, orig.Class, got.Class)
	require.Equal(t, orig.TTL, got.TTL)
	require.Equal(t, orig.RData, got.RData)
}

func TestDNSRecordAllTypes(t *testing.T) {
	cases := []dns.RecordType{
		dns.TypeA, dns.TypeNS, dns.TypeCNAME, dns.TypePTR,
		dns.TypeMX, dns.TypeTXT, dns.TypeAAAA, dns.TypeSRV,
		dns.TypeHTTPS, dns.TypeSVCB,
	}

	for _, rt := range cases {
		orig := dns.DNSRecord{
			Name:     "test.localweb",
			Type:     rt,
			Class:    1,
			TTL:      60,
			RDLength: 4,
			RData:    []byte{10, 0, 0, 1},
		}

		data, err := proto.MarshalDNSRecord(orig)
		require.NoError(t, err, "type=%d", rt)

		got, err := proto.UnmarshalDNSRecord(data)
		require.NoError(t, err, "type=%d", rt)
		require.Equal(t, orig.Name, got.Name, "type=%d", rt)
		require.Equal(t, orig.Type, got.Type, "type=%d", rt)
		require.Equal(t, orig.RData, got.RData, "type=%d", rt)
	}
}

func TestBlockRoundTrip(t *testing.T) {
	c, err := cid.Decode("QmUNLLsPACCz1vLxQVkXqqLX5R1X345qqfHbsf67hvA3Nn")
	require.NoError(t, err)

	orig := files.Block{
		CID:  c,
		Data: []byte("block data content"),
	}

	data, err := proto.MarshalBlock(orig)
	require.NoError(t, err)

	got, err := proto.UnmarshalBlock(data)
	require.NoError(t, err)

	require.True(t, orig.CID.Equals(got.CID))
	require.Equal(t, orig.Data, got.Data)
}

func TestBlockMetaRoundTrip(t *testing.T) {
	c, err := cid.Decode("QmUNLLsPACCz1vLxQVkXqqLX5R1X345qqfHbsf67hvA3Nn")
	require.NoError(t, err)

	created := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	orig := files.BlockMeta{
		CID:        c,
		Size:       4096,
		Compressed: true,
		Created:    created,
		RefCount:   3,
	}

	data, err := proto.MarshalBlockMeta(orig)
	require.NoError(t, err)

	got, err := proto.UnmarshalBlockMeta(data)
	require.NoError(t, err)

	require.True(t, orig.CID.Equals(got.CID))
	require.Equal(t, orig.Size, got.Size)
	require.Equal(t, orig.Compressed, got.Compressed)
	require.Equal(t, orig.RefCount, got.RefCount)
	require.Equal(t, created.UnixNano(), got.Created.UnixNano())
}

func TestFileMetaRoundTrip(t *testing.T) {
	c, err := cid.Decode("QmUNLLsPACCz1vLxQVkXqqLX5R1X345qqfHbsf67hvA3Nn")
	require.NoError(t, err)
	parent, err := cid.Decode("QmXgZ9bvpBRDFhzKzPkhbnyKzQqHkcKjd6cXow7zMUP6Rd")
	require.NoError(t, err)

	modified := time.Date(2024, 6, 1, 8, 0, 0, 0, time.UTC)
	created := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	orig := files.FileMeta{
		CID:       c,
		Name:      "report.pdf",
		Size:      1048576,
		MimeType:  "application/pdf",
		Modified:  modified,
		Created:   created,
		Version:   2,
		ParentCID: parent,
		ACL: []files.ACLEntry{
			{PubKey: [32]byte{1}, Read: true, Write: false, Admin: false},
			{PubKey: [32]byte{2}, Read: true, Write: true, Admin: true},
		},
	}

	data, err := proto.MarshalFileMeta(orig)
	require.NoError(t, err)

	got, err := proto.UnmarshalFileMeta(data)
	require.NoError(t, err)

	require.True(t, orig.CID.Equals(got.CID))
	require.Equal(t, orig.Name, got.Name)
	require.Equal(t, orig.Size, got.Size)
	require.Equal(t, orig.MimeType, got.MimeType)
	require.Equal(t, orig.Version, got.Version)
	require.True(t, orig.ParentCID.Equals(got.ParentCID))
	require.Equal(t, orig.ACL, got.ACL)
	require.Equal(t, modified.UnixNano(), got.Modified.UnixNano())
	require.Equal(t, created.UnixNano(), got.Created.UnixNano())
}

func TestFileMetaRoundTrip_Empty(t *testing.T) {
	orig := files.FileMeta{}

	data, err := proto.MarshalFileMeta(orig)
	require.NoError(t, err)

	got, err := proto.UnmarshalFileMeta(data)
	require.NoError(t, err)

	require.Equal(t, orig.Name, got.Name)
	require.Equal(t, orig.Size, got.Size)
	require.Equal(t, orig.Version, got.Version)
}

func TestPeerInfoToFromProto(t *testing.T) {
	p := dht.NodeID{255, 254, 253}
	orig := dht.PeerInfo{
		ID:        p,
		PublicKey: [32]byte{0xAA},
		Name:      "alice",
		Addrs:     []string{"addr1", "addr2"},
		Services:  []string{"dns"},
		Score:     0.9,
		Version:   "v1",
		LastSeen:  time.Now(),
		FirstSeen: time.Now(),
	}

	pb := proto.PeerInfoToProto(orig)
	require.Equal(t, "alice", pb.Name)
	require.Equal(t, float64(0.9), pb.Score)
	require.Equal(t, "v1", pb.Version)
	require.Len(t, pb.Addrs, 2)
	require.Len(t, pb.Services, 1)

	back := proto.PeerInfoFromProto(pb)
	require.Equal(t, orig.Name, back.Name)
	require.Equal(t, orig.Score, back.Score)
	require.Equal(t, orig.Version, back.Version)
	require.Equal(t, orig.Addrs, back.Addrs)
	require.Equal(t, orig.Services, back.Services)
}

func TestMessageToFromProto(t *testing.T) {
	orig := dht.Message{
		Type:    dht.MsgPing,
		Src:     dht.NodeID{1},
		Dst:     dht.NodeID{2},
		Payload: []byte("ping"),
	}

	pb := proto.MessageToProto(orig)
	require.Equal(t, proto.MessageType_MSG_PING, pb.Type)

	back := proto.MessageFromProto(pb)
	require.Equal(t, dht.MsgPing, back.Type)
	require.Equal(t, orig.Payload, back.Payload)
}

func TestDNSRecordToFromProto(t *testing.T) {
	orig := dns.DNSRecord{
		Name:     "host.localweb",
		Type:     dns.TypeAAAA,
		Class:    1,
		TTL:      300,
		RDLength: 16,
		RData:    []byte{0x20, 0x01, 0x0D, 0xB8, 0x85, 0xA3, 0x0, 0x0, 0x0, 0x0, 0x8, 0x20, 0x0, 0x0, 0x0, 0x1},
	}

	pb := proto.DNSRecordToProto(orig)
	require.Equal(t, proto.RecordType_TYPE_AAAA, pb.Type)
	require.Equal(t, uint32(16), pb.RdLength)

	back := proto.DNSRecordFromProto(pb)
	require.Equal(t, dns.TypeAAAA, back.Type)
	require.Equal(t, orig.RData, back.RData)
}

func TestBlockToFromProto(t *testing.T) {
	c, err := cid.Decode("QmUNLLsPACCz1vLxQVkXqqLX5R1X345qqfHbsf67hvA3Nn")
	require.NoError(t, err)

	orig := files.Block{CID: c, Data: []byte("data")}

	pb := proto.BlockToProto(orig)
	require.NotNil(t, pb.Cid)

	back, err := proto.BlockFromProto(pb)
	require.NoError(t, err)
	require.True(t, orig.CID.Equals(back.CID))
}

func TestFileMetaACL(t *testing.T) {
	acl := []files.ACLEntry{
		{PubKey: [32]byte{1}, Read: true},
		{PubKey: [32]byte{2}, Read: true, Write: true},
		{PubKey: [32]byte{3}, Read: true, Write: true, Admin: true},
	}

	fm := files.FileMeta{ACL: acl}
	pb := proto.FileMetaToProto(fm)
	require.Len(t, pb.Acl, 3)
	require.True(t, pb.Acl[0].Read)
	require.False(t, pb.Acl[0].Write)
	require.True(t, pb.Acl[2].Admin)

	back, err := proto.FileMetaFromProto(pb)
	require.NoError(t, err)
	require.Len(t, back.ACL, 3)
	require.True(t, back.ACL[2].Admin)
	require.False(t, back.ACL[0].Write)
}

func TestZeroTimeHandling(t *testing.T) {
	fm := files.FileMeta{
		Name:    "test",
		Size:    0,
		Version: 0,
	}

	pb := proto.FileMetaToProto(fm)
	require.Nil(t, pb.Created)
	require.Nil(t, pb.Modified)

	back, err := proto.FileMetaFromProto(pb)
	require.NoError(t, err)
	require.True(t, back.Created.IsZero())
	require.True(t, back.Modified.IsZero())
	require.Equal(t, int64(0), back.Size)
}

func TestGenericMarshalUnmarshal(t *testing.T) {
	pb := &proto.PeerInfo{
		Id:      []byte{1, 2, 3},
		Name:    "generic-test",
		Addrs:   []string{"a:1"},
		Score:   0.5,
		Version: "v0",
	}

	data, err := proto.Marshal(pb)
	require.NoError(t, err)

	var got proto.PeerInfo
	err = proto.Unmarshal(data, &got)
	require.NoError(t, err)
	require.Equal(t, "generic-test", got.Name)
	require.Equal(t, float64(0.5), got.Score)
}
