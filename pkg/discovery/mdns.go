package discovery

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

const (
	mdnsAddr     = "224.0.0.251:5353"
	serviceType  = "_localweb._tcp.local"
	announceInt  = 30 * time.Second
	mdnsTTL      = 120 // seconds
)

// MDNSDiscovery implements DiscoveryMode via multicast DNS.
type MDNSDiscovery struct {
	nodeID    [32]byte
	name      string
	conn      *net.UDPConn
	peers     map[string]*mdnsPeer
	ctx       context.Context
	cancel    context.CancelFunc
}

type mdnsPeer struct {
	info    PeerInfo
	answers []dnsAnswer
}

// dnsAnswer represents a DNS resource record.
type dnsAnswer struct {
	Name  string
	Type  uint16
	Class uint16
	TTL   uint32
	Data  []byte
}

func NewMDNSDiscovery() *MDNSDiscovery {
	return &MDNSDiscovery{
		peers: make(map[string]*mdnsPeer),
	}
}

func (m *MDNSDiscovery) Name() string          { return "mdns" }
func (m *MDNSDiscovery) RequiresWiFi() bool    { return true }

func (m *MDNSDiscovery) Start(ctx context.Context, nodeID [32]byte, name string) (<-chan PeerEvent, error) {
	m.nodeID = nodeID
	m.name = name
	m.ctx, m.cancel = context.WithCancel(ctx)

	events := make(chan PeerEvent, 16)

	// Join multicast group
	addr, err := net.ResolveUDPAddr("udp4", mdnsAddr)
	if err != nil {
		return nil, fmt.Errorf("resolve mDNS addr: %w", err)
	}

	iface, err := pickMulticastInterface()
	if err != nil {
		return nil, fmt.Errorf("no multicast interface: %w", err)
	}

	conn, err := net.ListenMulticastUDP("udp4", iface, addr)
	if err != nil {
		return nil, fmt.Errorf("listen multicast: %w", err)
	}
	m.conn = conn

	// Start announce loop
	go m.announceLoop()

	// Start listen loop
	go m.listenLoop(events)

	return events, nil
}

func (m *MDNSDiscovery) Advertise(info PeerInfo) error {
	pkt := m.buildAnnounce(info)
	_, err := m.conn.WriteToUDP(pkt, &net.UDPAddr{
		IP:   net.ParseIP("224.0.0.251"),
		Port: 5353,
	})
	return err
}

func (m *MDNSDiscovery) Stop() error {
	m.cancel()
	if m.conn != nil {
		m.conn.Close()
	}
	return nil
}

// announceLoop periodically sends mDNS announcements.
func (m *MDNSDiscovery) announceLoop() {
	ticker := time.NewTicker(announceInt)
	defer ticker.Stop()

	// Send initial announcement
	m.sendAnnounce()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.sendAnnounce()
		}
	}
}

func (m *MDNSDiscovery) sendAnnounce() {
	info := PeerInfo{
		ID:    m.nodeID,
		Name:  m.name,
		Addrs: []string{m.getLocalAddr()},
		Services: []ServiceInfo{
			{Name: "dns", Port: 5353},
			{Name: "http", Port: 8080},
			{Name: "messaging", Port: 9090},
		},
	}

	pkt := m.buildAnnounce(info)
	_, err := m.conn.WriteToUDP(pkt, &net.UDPAddr{
		IP:   net.ParseIP("224.0.0.251"),
		Port: 5353,
	})
	if err != nil {
		log.Debug().Err(err).Msg("mDNS announce failed")
	}
}

// buildAnnounce constructs an mDNS announcement packet.
func (m *MDNSDiscovery) buildAnnounce(info PeerInfo) []byte {
	var pkt []byte

	// DNS Header
	header := make([]byte, 12)
	binary.BigEndian.PutUint16(header[0:2], uint16(time.Now().UnixNano()%65535)) // Transaction ID
	binary.BigEndian.PutUint16(header[2:4], 0x8400)                              // Flags: response, authoritative
	binary.BigEndian.PutUint16(header[4:6], 0)                                   // Questions
	binary.BigEndian.PutUint16(header[6:8], 3)                                   // Answers: SRV + A + TXT
	binary.BigEndian.PutUint16(header[8:10], 0)                                  // Authority
	binary.BigEndian.PutUint16(header[10:12], 0)                                 // Additional
	pkt = append(pkt, header...)

	// Answer 1: SRV record
	srvName := encodeDNSName(serviceType)
	srvData := make([]byte, 6)
	binary.BigEndian.PutUint16(srvData[0:2], 0)   // Priority
	binary.BigEndian.PutUint16(srvData[2:4], 0)   // Weight
	binary.BigEndian.PutUint16(srvData[4:6], 4443) // Port
	srvData = append(srvData, encodeDNSName(info.Name+".local")...)
	pkt = appendDNSAnswer(pkt, srvName, 33, mdnsTTL, srvData) // Type SRV

	// Answer 2: A record
	aName := encodeDNSName(info.Name + ".local")
	ip := net.ParseIP(info.Addrs[0])
	if ip4 := ip.To4(); ip4 != nil {
		pkt = appendDNSAnswer(pkt, aName, 1, mdnsTTL, ip4) // Type A
	}

	// Answer 3: TXT record
	txtName := encodeDNSName(info.Name + ".local")
	txtData := buildTXTRecord(info)
	pkt = appendDNSAnswer(pkt, txtName, 16, mdnsTTL, txtData) // Type TXT

	return pkt
}

// listenLoop processes incoming mDNS packets.
func (m *MDNSDiscovery) listenLoop(events chan<- PeerEvent) {
	buf := make([]byte, 4096)

	for {
		select {
		case <-m.ctx.Done():
			return
		default:
		}

		m.conn.SetReadDeadline(time.Now().Add(time.Second))
		n, src, err := m.conn.ReadFromUDP(buf)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			return
		}

		peer := m.parseMDNSResponse(buf[:n], src)
		if peer != nil && peer.ID != m.nodeID {
			events <- PeerEvent{
				Type: PeerFound,
				Peer: *peer,
				Time: time.Now(),
			}
		}
	}
}

// parseMDNSResponse parses an mDNS response packet.
func (m *MDNSDiscovery) parseMDNSResponse(data []byte, src *net.UDPAddr) *PeerInfo {
	if len(data) < 12 {
		return nil
	}

	// Parse header
	flags := binary.BigEndian.Uint16(data[2:4])
	isResponse := flags&0x8000 != 0
	if !isResponse {
		return nil // Only process responses
	}

	answerCount := int(binary.BigEndian.Uint16(data[6:8]))
	offset := 12

	peer := &PeerInfo{
		Addrs:    []string{fmt.Sprintf("%s:4443", src.IP.String())},
		Source:   "mdns",
		LastSeen: time.Now(),
	}

	// Skip questions
	questionCount := int(binary.BigEndian.Uint16(data[4:6]))
	for i := 0; i < questionCount && offset < len(data); i++ {
		offset = skipDNSName(data, offset)
		offset += 4 // QTYPE + QCLASS
	}

	// Parse answers
	for i := 0; i < answerCount && offset < len(data); i++ {
		nameEnd := skipDNSName(data, offset)
		if nameEnd+10 > len(data) {
			break
		}

		recType := binary.BigEndian.Uint16(data[nameEnd : nameEnd+2])
		// recClass := binary.BigEndian.Uint16(data[nameEnd+2 : nameEnd+4])
		recTTL := binary.BigEndian.Uint32(data[nameEnd+4 : nameEnd+8])
		rdLen := int(binary.BigEndian.Uint16(data[nameEnd+8 : nameEnd+10]))
		rdData := data[nameEnd+10 : nameEnd+10+rdLen]

		offset = nameEnd + 10 + rdLen

		switch recType {
		case 33: // SRV
			if rdLen > 6 {
				name := parseDNSName(data, nameEnd+10+6)
				peer.Name = strings.TrimSuffix(name, ".local")
			}
		case 1: // A
			if rdLen == 4 {
				ip := net.IP(rdData)
				peer.Addrs = []string{fmt.Sprintf("%s:4443", ip.String())}
			}
		case 16: // TXT
			peer.Services = parseTXTRecord(rdData)
		case 28: // AAAA
			if rdLen == 16 {
				ip := net.IP(rdData)
				peer.Addrs = []string{fmt.Sprintf("[%s]:4443", ip.String())}
			}
		}

		_ = recTTL
	}

	if peer.Name == "" {
		return nil
	}

	return peer
}

// --- DNS Encoding Helpers ---

func encodeDNSName(name string) []byte {
	var buf []byte
	parts := strings.Split(name, ".")
	for _, part := range parts {
		buf = append(buf, byte(len(part)))
		buf = append(buf, []byte(part)...)
	}
	buf = append(buf, 0) // Root label
	return buf
}

func parseDNSName(data []byte, offset int) string {
	var name string
	for offset < len(data) {
		length := int(data[offset])
		offset++
		if length == 0 {
			break
		}
		// Handle compression pointer
		if length&0xC0 == 0xC0 {
			break
		}
		if offset+length > len(data) {
			break
		}
		if name != "" {
			name += "."
		}
		name += string(data[offset : offset+length])
		offset += length
	}
	return name
}

func skipDNSName(data []byte, offset int) int {
	for offset < len(data) {
		length := int(data[offset])
		offset++
		if length == 0 {
			return offset
		}
		if length&0xC0 == 0xC0 {
			return offset + 1 // Compression pointer
		}
		offset += length
	}
	return offset
}

func appendDNSAnswer(pkt []byte, name []byte, recType uint16, ttl uint32, data []byte) []byte {
	pkt = append(pkt, name...)
	answer := make([]byte, 10)
	binary.BigEndian.PutUint16(answer[0:2], recType)
	binary.BigEndian.PutUint16(answer[2:4], 1) // Class: IN (with cache-flush bit)
	binary.BigEndian.PutUint32(answer[4:8], ttl)
	binary.BigEndian.PutUint16(answer[8:10], uint16(len(data)))
	pkt = append(pkt, answer...)
	pkt = append(pkt, data...)
	return pkt
}

func buildTXTRecord(info PeerInfo) []byte {
	var data []byte
	data = append(data, 13) // Length
	data = append(data, []byte(fmt.Sprintf("id=%x", info.ID[:8]))...)

	data = append(data, 4) // Length
	data = append(data, []byte("ver=1")...)

	if len(info.Services) > 0 {
		svcs := ""
		for i, s := range info.Services {
			if i > 0 {
				svcs += ","
			}
			svcs += s.Name
		}
		svcBytes := []byte(fmt.Sprintf("svc=%s", svcs))
		data = append(data, byte(len(svcBytes)))
		data = append(data, svcBytes...)
	}

	return data
}

func parseTXTRecord(data []byte) []ServiceInfo {
	var services []ServiceInfo
	offset := 0

	for offset < len(data) {
		length := int(data[offset])
		offset++
		if offset+length > len(data) {
			break
		}
		entry := string(data[offset : offset+length])
		offset += length

		if strings.HasPrefix(entry, "svc=") {
			svcNames := strings.Split(strings.TrimPrefix(entry, "svc="), ",")
			for _, name := range svcNames {
				services = append(services, ServiceInfo{Name: name})
			}
		}
	}

	return services
}

func (m *MDNSDiscovery) getLocalAddr() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "0.0.0.0"
	}

	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			if ipNet, ok := addr.(*net.IPNet); ok && ipNet.IP.To4() != nil && !ipNet.IP.IsLoopback() {
				return ipNet.IP.String()
			}
		}
	}

	return "0.0.0.0"
}

func pickMulticastInterface() (*net.Interface, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}

	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagMulticast == 0 {
			continue
		}
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, a := range addrs {
			if ipNet, ok := a.(*net.IPNet); ok && ipNet.IP.To4() != nil && !ipNet.IP.IsLoopback() {
				return &iface, nil
			}
		}
	}

	return nil, fmt.Errorf("no multicast interface")
}
