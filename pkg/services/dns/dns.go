package dns

import (
	"context"
	"encoding/binary"
	"errors"
	"net"
	"sync"
	"time"

	"github.com/mrityunjay/LocalWEB/pkg/discovery"
)

const (
	TLD           = ".localweb"
	DefaultPort   = 5353
	CacheTTL      = 5 * time.Minute
	MaxMsgSize    = 512
)

type RecordType uint16

const (
	TypeA     RecordType = 1
	TypeNS    RecordType = 2
	TypeCNAME RecordType = 5
	TypePTR   RecordType = 12
	TypeMX    RecordType = 15
	TypeTXT   RecordType = 16
	TypeAAAA  RecordType = 28
	TypeSRV   RecordType = 33
	TypeHTTPS RecordType = 65
	TypeSVCB  RecordType = 64
)

type OpCode uint8

const (
	OpQuery OpCode = 0
)

type RCode uint8

const (
	RNoError  RCode = 0
	RFormErr  RCode = 1
	RNXDomain RCode = 3
	RNotImp   RCode = 4
	RRefused  RCode = 5
)

type DNSHeader struct {
	ID      uint16
	Flags   uint16
	QDCOUNT uint16
	ANCOUNT uint16
	NSCOUNT uint16
	ARCOUNT uint16
}

type DNSQuestion struct {
	Name  string
	Type  RecordType
	Class uint16
}

type DNSRecord struct {
	Name     string
	Type     RecordType
	Class    uint16
	TTL      uint32
	RDLength uint16
	RData    []byte
}

type DNSMessage struct {
	Header      DNSHeader
	Questions   []DNSQuestion
	Answers     []DNSRecord
	Authorities []DNSRecord
	Additionals []DNSRecord
}

type Zone struct {
	SOA       SOARecord
	Records   map[string][]ResourceRecord
	Transfers map[[32]byte]bool
	SignedAt  time.Time
	Sig       [64]byte
}

type SOARecord struct {
	MName   string
	RName   string
	Serial  uint32
	Refresh uint32
	Retry   uint32
	Expire  uint32
	Minimum uint32
}

type ResourceRecord struct {
	Record DNSRecord
	Data   []byte
}

type Server struct {
	zone      *Zone
	peers     *discovery.PeerDatabase
	mu        sync.RWMutex
	cache     map[string]cacheEntry
}

type cacheEntry struct {
	record  DNSRecord
	expires time.Time
}

func NewServer(zone *Zone, peers *discovery.PeerDatabase) *Server {
	return &Server{
		zone:  zone,
		peers: peers,
		cache: make(map[string]cacheEntry),
	}
}

func (s *Server) Start(ctx context.Context, addr string) error {
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP(addr), Port: DefaultPort})
	if err != nil {
		return err
	}
	go s.readLoop(ctx, conn)
	return nil
}

func (s *Server) readLoop(ctx context.Context, conn *net.UDPConn) {
	buf := make([]byte, MaxMsgSize)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		n, remote, err := conn.ReadFromUDP(buf)
		if err != nil {
			continue
		}
		go s.handleQuery(ctx, conn, remote, buf[:n])
	}
}

func (s *Server) handleQuery(ctx context.Context, conn *net.UDPConn, remote *net.UDPAddr, data []byte) {
	msg, err := ParseMessage(data)
	if err != nil {
		return
	}

	if len(msg.Questions) == 0 {
		return
	}
	q := msg.Questions[0]
	response := &DNSMessage{
		Header: DNSHeader{
			ID:      msg.Header.ID,
			Flags:   0x8000,
			QDCOUNT: 1,
		},
		Questions: []DNSQuestion{q},
	}

	key := q.Name
	s.mu.RLock()
	if entry, ok := s.cache[key]; ok && time.Now().Before(entry.expires) {
		s.mu.RUnlock()
		response.Answers = append(response.Answers, entry.record)
	} else {
		s.mu.RUnlock()
		record, err := s.resolve(q)
		if err != nil {
			response.Header.Flags |= uint16(RNXDomain) << 3
		} else {
			response.Answers = append(response.Answers, record)
			s.mu.Lock()
			s.cache[key] = cacheEntry{record: record, expires: time.Now().Add(CacheTTL)}
			s.mu.Unlock()
		}
	}

	respData, _ := SerializeMessage(response)
	conn.WriteToUDP(respData, remote)
}

func (s *Server) resolve(q DNSQuestion) (DNSRecord, error) {
	name := q.Name
	if q.Type == TypePTR {
		name = reverseName(name)
	}
	records, ok := s.zone.Records[name]
	if !ok {
		return DNSRecord{}, errors.New("not found")
	}
	for _, rr := range records {
		if rr.Record.Type == q.Type {
			return rr.Record, nil
		}
	}
	if len(records) > 0 {
		return records[0].Record, nil
	}
	return DNSRecord{}, errors.New("not found")
}

func reverseName(addr string) string {
	ip := net.ParseIP(addr)
	if ip == nil {
		return addr
	}
	ip = ip.To4()
	if ip == nil {
		return addr
	}
	_ = ip
	return "in-addr.arpa"
}

func (s *Server) Advertise(ctx context.Context) error {
	return nil
}

func (s *Server) Stop() error {
	return nil
}

func ParseMessage(data []byte) (*DNSMessage, error) {
	if len(data) < 12 {
		return nil, errors.New("dns message too short")
	}
	msg := &DNSMessage{
		Header: DNSHeader{
			ID:      binary.BigEndian.Uint16(data[0:2]),
			Flags:   binary.BigEndian.Uint16(data[2:4]),
			QDCOUNT: binary.BigEndian.Uint16(data[4:6]),
			ANCOUNT: binary.BigEndian.Uint16(data[6:8]),
			NSCOUNT: binary.BigEndian.Uint16(data[8:10]),
			ARCOUNT: binary.BigEndian.Uint16(data[10:12]),
		},
	}
	offset := 12
	for i := 0; i < int(msg.Header.QDCOUNT); i++ {
		name, off, err := readName(data, offset)
		if err != nil {
			return nil, err
		}
		offset = off
		if offset+4 > len(data) {
			return nil, errors.New("dns question truncated")
		}
		msg.Questions = append(msg.Questions, DNSQuestion{
			Name:  name,
			Type:  RecordType(binary.BigEndian.Uint16(data[offset : offset+2])),
			Class: binary.BigEndian.Uint16(data[offset+2 : offset+4]),
		})
		offset += 4
	}
	return msg, nil
}

func SerializeMessage(msg *DNSMessage) ([]byte, error) {
	buf := make([]byte, 12)
	binary.BigEndian.PutUint16(buf[0:2], msg.Header.ID)
	binary.BigEndian.PutUint16(buf[2:4], msg.Header.Flags)
	binary.BigEndian.PutUint16(buf[4:6], msg.Header.QDCOUNT)
	binary.BigEndian.PutUint16(buf[6:8], msg.Header.ANCOUNT)
	binary.BigEndian.PutUint16(buf[8:10], msg.Header.NSCOUNT)
	binary.BigEndian.PutUint16(buf[10:12], msg.Header.ARCOUNT)
	for _, q := range msg.Questions {
		buf = append(buf, encodeName(q.Name)...)
		buf = append(buf, byte(q.Type>>8), byte(q.Type), byte(q.Class>>8), byte(q.Class))
	}
	for _, r := range msg.Answers {
		buf = append(buf, encodeName(r.Name)...)
		buf = append(buf, byte(r.Type>>8), byte(r.Type), byte(r.Class>>8), byte(r.Class))
		buf = append(buf, byte(r.TTL>>24), byte(r.TTL>>16), byte(r.TTL>>8), byte(r.TTL))
		buf = append(buf, byte(len(r.RData)>>8), byte(len(r.RData)))
		buf = append(buf, r.RData...)
	}
	return buf, nil
}

func readName(data []byte, offset int) (string, int, error) {
	var parts []string
	for offset < len(data) {
		length := int(data[offset])
		if length == 0 {
			offset++
			break
		}
		if length&0xC0 == 0xC0 {
			if offset+1 >= len(data) {
				return "", offset, errors.New("truncated pointer")
			}
			ptr := int(binary.BigEndian.Uint16(data[offset:offset+2]) & 0x3FFF)
			name, _, err := readName(data, ptr)
			if err != nil {
				return "", offset, err
			}
			parts = append(parts, name)
			offset += 2
			break
		}
		offset++
		if offset+length > len(data) {
			return "", offset, errors.New("truncated name")
		}
		parts = append(parts, string(data[offset:offset+length]))
		offset += length
	}
	return joinName(parts), offset, nil
}

func encodeName(name string) []byte {
	var buf []byte
	if len(name) > 0 && name[len(name)-1] == '.' {
		name = name[:len(name)-1]
	}
	parts := splitName(name)
	for _, p := range parts {
		buf = append(buf, byte(len(p)))
		buf = append(buf, p...)
	}
	buf = append(buf, 0)
	return buf
}

func splitName(name string) []string {
	var parts []string
	start := 0
	for i := 0; i < len(name); i++ {
		if name[i] == '.' {
			parts = append(parts, name[start:i])
			start = i + 1
		}
	}
	if start < len(name) {
		parts = append(parts, name[start:])
	}
	return parts
}

func joinName(parts []string) string {
	out := ""
	for i, p := range parts {
		out += p
		if i < len(parts)-1 {
			out += "."
		}
	}
	return out
}
