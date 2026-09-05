//go:build integration

package integration

import (
	"context"
	"encoding/binary"
	"net"
	"testing"
	"time"

	"github.com/ram1234598766-dotcom/Local-WEB/pkg/discovery"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/services/dns"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// DNS message encode/decode round-trip
// ---------------------------------------------------------------------------

func TestDNSParseAndSerialize(t *testing.T) {
	original := &dns.DNSMessage{
		Header: dns.DNSHeader{ID: 0x1234, Flags: 0x8000, QDCOUNT: 1, ANCOUNT: 1},
		Questions: []dns.DNSQuestion{
			{Name: "test.localweb", Type: dns.TypeA, Class: 1},
		},
		Answers: []dns.DNSRecord{
			{Name: "test.localweb", Type: dns.TypeA, Class: 1, TTL: 300, RData: []byte{10, 0, 0, 1}},
		},
	}

	data, err := dns.SerializeMessage(original)
	require.NoError(t, err)

	parsed, err := dns.ParseMessage(data)
	require.NoError(t, err)
	require.Equal(t, original.Header.ID, parsed.Header.ID)
	require.Equal(t, uint16(1), parsed.Header.QDCOUNT)
	require.Equal(t, uint16(1), parsed.Header.ANCOUNT)
	require.Len(t, parsed.Questions, 1)
	require.Equal(t, "test.localweb", parsed.Questions[0].Name)
}

// ---------------------------------------------------------------------------
// DNS server: resolve A records from a zone
// ---------------------------------------------------------------------------

func TestDNSServerResolveA(t *testing.T) {
	zone := &dns.Zone{
		SOA: dns.SOARecord{
			MName:   "ns1.localweb",
			RName:   "admin.localweb",
			Serial:  1,
			Refresh: 300,
			Retry:   60,
			Expire:  86400,
			Minimum: 60,
		},
		Records: map[string][]dns.ResourceRecord{
			"host1.localweb": {
				{Record: dns.DNSRecord{Name: "host1.localweb", Type: dns.TypeA, Class: 1, TTL: 300}, Data: []byte{192, 168, 1, 10}},
			},
			"host2.localweb": {
				{Record: dns.DNSRecord{Name: "host2.localweb", Type: dns.TypeA, Class: 1, TTL: 300}, Data: []byte{192, 168, 1, 20}},
			},
		},
	}

	peers := discovery.NewPeerDatabase()
	srv := dns.NewServer(zone, peers)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Bind test client to random port, query server on dynamically assigned port
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	require.NoError(t, err)
	defer conn.Close()

	require.NoError(t, srv.Start(ctx, "127.0.0.1:0"))
	serverPort := portFromAddr(srv.Addr())

	// Build a DNS query for host1.localweb A record
	query := buildDNSQuery(0xABCD, "host1.localweb", dns.TypeA)
	_, err = conn.WriteToUDP(query, &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: serverPort})
	require.NoError(t, err)

	buf := make([]byte, dns.MaxMsgSize)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _, err := conn.ReadFromUDP(buf)
	require.NoError(t, err)

	resp, err := dns.ParseMessage(buf[:n])
	require.NoError(t, err)
	require.True(t, resp.Header.Flags&0x8000 != 0, "should be a response")
	require.Equal(t, uint16(1), resp.Header.ANCOUNT, "should have 1 answer")
	require.Len(t, resp.Answers, 1)
}

func TestDNSServerResolvePTR(t *testing.T) {
	zone := &dns.Zone{
		SOA: dns.SOARecord{MName: "ns.localweb", RName: "admin.localweb", Serial: 1, Refresh: 300, Retry: 60, Expire: 86400, Minimum: 60},
		Records: map[string][]dns.ResourceRecord{
			"1.0.0.127.in-addr.arpa": {
				{Record: dns.DNSRecord{Name: "1.0.0.127.in-addr.arpa", Type: dns.TypePTR, Class: 1, TTL: 300}, Data: []byte("host.localweb")},
			},
		},
	}
	peers := discovery.NewPeerDatabase()
	srv := dns.NewServer(zone, peers)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	require.NoError(t, err)
	defer conn.Close()

	require.NoError(t, srv.Start(ctx, "127.0.0.1:0"))
	serverPort := portFromAddr(srv.Addr())

	query := buildDNSQuery(0x1111, "1.0.0.127.in-addr.arpa", dns.TypePTR)
	_, err = conn.WriteToUDP(query, &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: serverPort})
	require.NoError(t, err)

	buf := make([]byte, dns.MaxMsgSize)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _, err := conn.ReadFromUDP(buf)
	require.NoError(t, err)

	resp, err := dns.ParseMessage(buf[:n])
	require.NoError(t, err)
	require.True(t, resp.Header.Flags&0x8000 != 0)
	require.Equal(t, uint16(1), resp.Header.ANCOUNT)
}

func TestDNSServerNXDomain(t *testing.T) {
	zone := &dns.Zone{
		SOA:     dns.SOARecord{MName: "ns.localweb", RName: "admin.localweb", Serial: 1, Refresh: 300, Retry: 60, Expire: 86400, Minimum: 60},
		Records: map[string][]dns.ResourceRecord{},
	}
	peers := discovery.NewPeerDatabase()
	srv := dns.NewServer(zone, peers)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	require.NoError(t, err)
	defer conn.Close()

	require.NoError(t, srv.Start(ctx, "127.0.0.1:0"))
	serverPort := portFromAddr(srv.Addr())

	query := buildDNSQuery(0x2222, "nonexistent.localweb", dns.TypeA)
	_, err = conn.WriteToUDP(query, &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: serverPort})
	require.NoError(t, err)

	buf := make([]byte, dns.MaxMsgSize)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _, err := conn.ReadFromUDP(buf)
	require.NoError(t, err)

	resp, err := dns.ParseMessage(buf[:n])
	require.NoError(t, err)
	require.Equal(t, uint16(dns.RNXDomain), resp.Header.Flags>>3&0xF, "should return NXDomain")
}

// ---------------------------------------------------------------------------
// End-to-end DNS resolution
// ---------------------------------------------------------------------------

func TestDNSEndToEndResolution(t *testing.T) {
	zone := &dns.Zone{
		SOA: dns.SOARecord{MName: "ns1.localweb", RName: "admin.localweb", Serial: 1, Refresh: 300, Retry: 60, Expire: 86400, Minimum: 60},
		Records: map[string][]dns.ResourceRecord{
			"web1.localweb": {{Record: dns.DNSRecord{Name: "web1.localweb", Type: dns.TypeA, Class: 1, TTL: 300}, Data: []byte{10, 0, 0, 1}}},
			"web2.localweb": {{Record: dns.DNSRecord{Name: "web2.localweb", Type: dns.TypeA, Class: 1, TTL: 300}, Data: []byte{10, 0, 0, 2}}},
			"mail.localweb": {{Record: dns.DNSRecord{Name: "mail.localweb", Type: dns.TypeA, Class: 1, TTL: 300}, Data: []byte{10, 0, 0, 3}}},
			"srv1.localweb": {{Record: dns.DNSRecord{Name: "srv1.localweb", Type: dns.TypeSRV, Class: 1, TTL: 300}, Data: []byte{0, 0, 0, 0, 0, 0x11, 0x94}}},
		},
	}

	peers := discovery.NewPeerDatabase()
	srv := dns.NewServer(zone, peers)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	require.NoError(t, err)
	defer conn.Close()

	require.NoError(t, srv.Start(ctx, "127.0.0.1:0"))
	serverPort := portFromAddr(srv.Addr())

	testCases := []struct {
		name    string
		qName   string
		qType   dns.RecordType
		wantAns bool
	}{
		{"resolve_web1", "web1.localweb", dns.TypeA, true},
		{"resolve_web2", "web2.localweb", dns.TypeA, true},
		{"resolve_mail", "mail.localweb", dns.TypeA, true},
		{"missing_nx", "ghost.localweb", dns.TypeA, false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			query := buildDNSQuery(uint16(t.Name()[0]), tc.qName, tc.qType)
			_, err := conn.WriteToUDP(query, &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: serverPort})
			require.NoError(t, err)

			buf := make([]byte, dns.MaxMsgSize)
			conn.SetReadDeadline(time.Now().Add(2 * time.Second))
			n, _, err := conn.ReadFromUDP(buf)
			require.NoError(t, err)

			resp, err := dns.ParseMessage(buf[:n])
			require.NoError(t, err)
			isResponse := resp.Header.Flags&0x8000 != 0
			require.True(t, isResponse)

			if tc.wantAns {
				require.Equal(t, uint16(1), resp.Header.ANCOUNT, "should have answer for %s", tc.qName)
				require.NotEmpty(t, resp.Answers)
			} else {
				require.Equal(t, uint16(dns.RNXDomain), resp.Header.Flags>>3&0xF)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// DNS caching integration
// ---------------------------------------------------------------------------

func TestDNSCacheIntegration(t *testing.T) {
	zone := &dns.Zone{
		SOA: dns.SOARecord{MName: "ns.localweb", RName: "admin.localweb", Serial: 1, Refresh: 300, Retry: 60, Expire: 86400, Minimum: 60},
		Records: map[string][]dns.ResourceRecord{
			"cached.localweb": {{Record: dns.DNSRecord{Name: "cached.localweb", Type: dns.TypeA, Class: 1, TTL: 300}, Data: []byte{10, 0, 0, 42}}},
		},
	}
	peers := discovery.NewPeerDatabase()
	srv := dns.NewServer(zone, peers)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	require.NoError(t, err)
	defer conn.Close()
	require.NoError(t, srv.Start(ctx, "127.0.0.1:0"))
	serverPort := portFromAddr(srv.Addr())

	// First query – cache miss
	query1 := buildDNSQuery(0x3001, "cached.localweb", dns.TypeA)
	_, err = conn.WriteToUDP(query1, &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: serverPort})
	require.NoError(t, err)
	buf1 := make([]byte, dns.MaxMsgSize)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _, _ := conn.ReadFromUDP(buf1)
	resp1, _ := dns.ParseMessage(buf1[:n])
	require.NotNil(t, resp1, "response should not be nil")
	require.Equal(t, uint16(1), resp1.Header.ANCOUNT)

	// Second query – cache hit (within TTL)
	query2 := buildDNSQuery(0x3002, "cached.localweb", dns.TypeA)
	_, err = conn.WriteToUDP(query2, &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: serverPort})
	require.NoError(t, err)
	buf2 := make([]byte, dns.MaxMsgSize)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n2, _, _ := conn.ReadFromUDP(buf2)
	resp2, _ := dns.ParseMessage(buf2[:n2])
	require.NotNil(t, resp2, "response should not be nil")
	require.Equal(t, uint16(1), resp2.Header.ANCOUNT, "cached response should have answer")
}

// ---------------------------------------------------------------------------
// Helper: build a minimal DNS query packet
// ---------------------------------------------------------------------------

func buildDNSQuery(id uint16, name string, qType dns.RecordType) []byte {
	buf := make([]byte, 12)
	binary.BigEndian.PutUint16(buf[0:2], id)
	binary.BigEndian.PutUint16(buf[2:4], 0) // flags: standard query
	binary.BigEndian.PutUint16(buf[4:6], 1) // 1 question
	binary.BigEndian.PutUint16(buf[6:8], 0) // 0 answers
	binary.BigEndian.PutUint16(buf[8:10], 0)
	binary.BigEndian.PutUint16(buf[10:12], 0)

	nameStr := name
	if len(nameStr) > 0 && nameStr[len(nameStr)-1] == '.' {
		nameStr = nameStr[:len(nameStr)-1]
	}
	i := 0
	for i < len(nameStr) {
		dot := indexOfByte(nameStr[i:], '.')
		if dot < 0 {
			label := nameStr[i:]
			if len(label) > 0 {
				buf = append(buf, byte(len(label)))
				buf = append(buf, []byte(label)...)
			}
			break
		}
		label := nameStr[i : i+dot]
		buf = append(buf, byte(len(label)))
		buf = append(buf, []byte(label)...)
		i += dot + 1
	}
	buf = append(buf, 0) // root label

	buf = append(buf, 0, byte(qType))
	buf = append(buf, 0, 0, 0, 1) // CLASS IN
	return buf
}

func indexOfByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

// portFromAddr extracts the UDP port from a "host:port" address string.
func portFromAddr(addr string) int {
	if addr == "" {
		return dns.DefaultPort
	}
	_, p, err := net.SplitHostPort(addr)
	if err != nil {
		return dns.DefaultPort
	}
	port, err := net.LookupPort("udp", p)
	if err != nil {
		return dns.DefaultPort
	}
	return port
}
