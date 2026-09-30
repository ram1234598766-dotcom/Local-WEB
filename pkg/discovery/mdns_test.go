package discovery

import (
	"context"
	"encoding/binary"
	"net"
	"testing"
)

// mdnsSrc is a stand-in for the UDP source address of a received mDNS packet.
func mdnsSrc(ip string) *net.UDPAddr {
	return &net.UDPAddr{IP: net.ParseIP(ip), Port: 5353}
}

// dnsHeader builds a 12 byte DNS header with the given flags and section counts.
func dnsHeader(flags uint16, questions, answers uint16) []byte {
	h := make([]byte, 12)
	binary.BigEndian.PutUint16(h[0:2], 0x1234)
	binary.BigEndian.PutUint16(h[2:4], flags)
	binary.BigEndian.PutUint16(h[4:6], questions)
	binary.BigEndian.PutUint16(h[6:8], answers)
	return h
}

func TestNewMDNSDiscovery(t *testing.T) {
	m := NewMDNSDiscovery()
	if m == nil {
		t.Fatal("expected a non-nil discovery instance")
	}
	if m.peers == nil {
		t.Error("expected the peer cache to be initialised")
	}
	if len(m.peers) != 0 {
		t.Errorf("expected an empty peer cache, got %d entries", len(m.peers))
	}
	if m.Name() != "mdns" {
		t.Errorf("expected the mode name %q, got %q", "mdns", m.Name())
	}
	if !m.RequiresWiFi() {
		t.Error("expected mDNS to require WiFi")
	}
	if m.conn != nil {
		t.Error("expected no socket before Start")
	}
}

func TestMDNSDiscoveryImplementsDiscoveryMode(t *testing.T) {
	var mode DiscoveryMode = NewMDNSDiscovery()
	if mode.Name() != "mdns" {
		t.Errorf("expected the mode name %q, got %q", "mdns", mode.Name())
	}
	if !mode.RequiresWiFi() {
		t.Error("expected the mDNS mode to require WiFi")
	}
}

// The orchestrator stops every mode it holds, including ones whose Start failed
// or was never reached, so Stop must never dereference the absent cancel func.
func TestMDNSDiscoveryStopWithoutStart(t *testing.T) {
	t.Run("never started", func(t *testing.T) {
		m := NewMDNSDiscovery()

		if err := m.Stop(); err != nil {
			t.Errorf("expected Stop without Start to succeed, got %v", err)
		}
		if err := m.Stop(); err != nil {
			t.Errorf("expected a repeated Stop to succeed, got %v", err)
		}
	})

	t.Run("start failed after assigning the context", func(t *testing.T) {
		m := NewMDNSDiscovery()
		// Start sets the cancel func before it can fail on interface or socket
		// setup, so a failed start leaves a live context and no connection.
		m.ctx, m.cancel = context.WithCancel(context.Background())

		if err := m.Stop(); err != nil {
			t.Errorf("expected Stop after a failed Start to succeed, got %v", err)
		}
		if err := m.Stop(); err != nil {
			t.Errorf("expected a repeated Stop to succeed, got %v", err)
		}
		select {
		case <-m.ctx.Done():
		default:
			t.Error("expected Stop to cancel the context")
		}
	})
}

func TestEncodeDNSName(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []byte
	}{
		{"single label", "host", []byte{4, 'h', 'o', 's', 't', 0}},
		{"two labels", "host.local", []byte{4, 'h', 'o', 's', 't', 5, 'l', 'o', 'c', 'a', 'l', 0}},
		{"three labels", "_srv._tcp.local", []byte{4, '_', 's', 'r', 'v', 4, '_', 't', 'c', 'p', 5, 'l', 'o', 'c', 'a', 'l', 0}},
		{"empty label still terminates", "", []byte{0, 0}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := encodeDNSName(tt.input)
			if len(got) != len(tt.want) {
				t.Fatalf("expected %d bytes %v, got %d bytes %v", len(tt.want), tt.want, len(got), got)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("byte %d: expected %#x, got %#x", i, tt.want[i], got[i])
				}
			}
		})
	}
}

func TestEncodeParseDNSNameRoundTrip(t *testing.T) {
	names := []string{
		"host",
		"host.local",
		"_localweb._tcp.local",
		"peer-with-a-longer-name.local",
	}

	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			encoded := encodeDNSName(name)
			// Trailing garbage makes sure parsing stops at the root label.
			parsed := parseDNSName(append(encoded, 0xFF, 0xFF, 0xFF), 0)
			if parsed != name {
				t.Errorf("expected %q, got %q", name, parsed)
			}
			if end := skipDNSName(encoded, 0); end != len(encoded) {
				t.Errorf("expected skipDNSName to land on %d, got %d", len(encoded), end)
			}
		})
	}
}

func TestParseDNSNameStopsAtCompressionPointer(t *testing.T) {
	// "ab" followed by a pointer (0xC0 0x0C). The parser does not follow the
	// pointer, so it must return only the labels decoded before it.
	data := []byte{2, 'a', 'b', 0xC0, 0x0C, 0, 0}

	if got := parseDNSName(data, 0); got != "ab" {
		t.Errorf("expected the pointer to stop parsing with %q, got %q", "ab", got)
	}
	if got := skipDNSName(data, 0); got != 5 {
		t.Errorf("expected skipDNSName to consume the 2 byte pointer and return 5, got %d", got)
	}
}

func TestParseDNSNameTruncatedLabel(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"label longer than the buffer", []byte{9, 'a', 'b'}},
		{"truncated mid label", []byte{4, 'a', 'b'}},
		{"no data", []byte{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Must not panic and must not invent labels.
			if got := parseDNSName(tt.data, 0); got != "" && got != "a" && got != "ab" {
				t.Errorf("unexpected parse result %q", got)
			}
			skipDNSName(tt.data, 0)
		})
	}
}

func TestSkipDNSNameRoot(t *testing.T) {
	if got := skipDNSName([]byte{0}, 0); got != 1 {
		t.Errorf("expected a bare root label to end at 1, got %d", got)
	}
}

// A caller slices with the returned offset, so it must never point past the
// buffer even when the name is truncated mid label or mid pointer.
func TestSkipDNSNameNeverExceedsBuffer(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"label longer than the buffer", []byte{9, 'a', 'b'}},
		{"truncated mid label", []byte{4, 'a', 'b'}},
		{"bare compression pointer", []byte{0xC0}},
		{"pointer as the last byte", []byte{2, 'a', 'b', 0xC0}},
		{"no data", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := skipDNSName(tt.data, 0); got > len(tt.data) {
				t.Errorf("expected an offset within the %d byte buffer, got %d", len(tt.data), got)
			}
		})
	}
}

func TestAppendDNSAnswerLayout(t *testing.T) {
	name := encodeDNSName("host.local")
	payload := []byte{1, 2, 3, 4}

	pkt := appendDNSAnswer(nil, name, 1, 120, payload)

	if len(pkt) != len(name)+10+len(payload) {
		t.Fatalf("expected a %d byte record, got %d", len(name)+10+len(payload), len(pkt))
	}
	if string(pkt[:len(name)]) != string(name) {
		t.Error("expected the encoded name to lead the record")
	}
	rec := pkt[len(name):]
	if got := binary.BigEndian.Uint16(rec[0:2]); got != 1 {
		t.Errorf("expected rtype 1, got %d", got)
	}
	if got := binary.BigEndian.Uint16(rec[2:4]); got != 1 {
		t.Errorf("expected class IN (1), got %d", got)
	}
	if got := binary.BigEndian.Uint32(rec[4:8]); got != 120 {
		t.Errorf("expected TTL 120, got %d", got)
	}
	if got := binary.BigEndian.Uint16(rec[8:10]); got != uint16(len(payload)) {
		t.Errorf("expected rdlength %d, got %d", len(payload), got)
	}
	if string(rec[10:]) != string(payload) {
		t.Errorf("expected payload %v, got %v", payload, rec[10:])
	}
}

func TestAppendDNSAnswerZeroLengthData(t *testing.T) {
	pkt := appendDNSAnswer(nil, encodeDNSName("x"), 16, 0, nil)
	if len(pkt) != 3+10 {
		t.Fatalf("expected 13 bytes for an empty TXT record, got %d", len(pkt))
	}
}

func TestBuildAnnounceHeader(t *testing.T) {
	m := NewMDNSDiscovery()
	pkt := m.buildAnnounce(PeerInfo{ID: peerID(1), Name: "alpha", Addrs: []string{"10.0.0.5"}})

	if len(pkt) < 12 {
		t.Fatalf("packet is shorter than a DNS header: %d bytes", len(pkt))
	}
	if flags := binary.BigEndian.Uint16(pkt[2:4]); flags != 0x8400 {
		t.Errorf("expected the response+authoritative flags 0x8400, got 0x%04x", flags)
	}
	if q := binary.BigEndian.Uint16(pkt[4:6]); q != 0 {
		t.Errorf("expected 0 questions, got %d", q)
	}
	if a := binary.BigEndian.Uint16(pkt[6:8]); a != 3 {
		t.Errorf("expected 3 answers (SRV + A + TXT), got %d", a)
	}
	if auth := binary.BigEndian.Uint16(pkt[8:10]); auth != 0 {
		t.Errorf("expected 0 authority records, got %d", auth)
	}
	if add := binary.BigEndian.Uint16(pkt[10:12]); add != 0 {
		t.Errorf("expected 0 additional records, got %d", add)
	}
}

func TestBuildAnnounceRecordTypes(t *testing.T) {
	m := NewMDNSDiscovery()
	pkt := m.buildAnnounce(PeerInfo{ID: peerID(1), Name: "alpha", Addrs: []string{"10.0.0.5"}})

	offset := 12
	types := make([]uint16, 0, 3)
	for i := 0; i < 3 && offset < len(pkt); i++ {
		offset = skipDNSName(pkt, offset)
		if offset+10 > len(pkt) {
			t.Fatalf("record %d header runs past the packet", i)
		}
		types = append(types, binary.BigEndian.Uint16(pkt[offset:offset+2]))
		ttl := binary.BigEndian.Uint32(pkt[offset+4 : offset+8])
		if ttl != mdnsTTL {
			t.Errorf("record %d: expected TTL %d, got %d", i, mdnsTTL, ttl)
		}
		rdLen := int(binary.BigEndian.Uint16(pkt[offset+8 : offset+10]))
		offset += 10 + rdLen
	}

	want := []uint16{33, 1, 16} // SRV, A, TXT
	if len(types) != len(want) {
		t.Fatalf("expected %d records, got %d (%v)", len(want), len(types), types)
	}
	for i := range want {
		if types[i] != want[i] {
			t.Errorf("record %d: expected type %d, got %d", i, want[i], types[i])
		}
	}
}

func TestBuildAnnounceRoundTrip(t *testing.T) {
	m := NewMDNSDiscovery()
	info := PeerInfo{ID: peerID(3), Name: "alpha", Addrs: []string{"10.0.0.5"}}
	pkt := m.buildAnnounce(info)

	peer := m.parseMDNSResponse(pkt, mdnsSrc("10.0.0.5"))
	if peer == nil {
		t.Fatal("expected the parser to accept a locally built announcement")
	}
	if peer.Name != "alpha" {
		t.Errorf("expected the name to survive the round trip as %q, got %q", "alpha", peer.Name)
	}
	if len(peer.Addrs) != 1 {
		t.Fatalf("expected 1 address, got %v", peer.Addrs)
	}
	// The parser normalises the address by appending the mDNS service port.
	if peer.Addrs[0] != "10.0.0.5:4443" {
		t.Errorf("expected the A record to become 10.0.0.5:4443, got %q", peer.Addrs[0])
	}
	if peer.Source != "mdns" {
		t.Errorf("expected the source to be stamped as mdns, got %q", peer.Source)
	}
	if peer.LastSeen.IsZero() {
		t.Error("expected LastSeen to be stamped by the parser")
	}
}

func TestBuildAnnounceRoundTripNameAndAddressOnly(t *testing.T) {
	m := NewMDNSDiscovery()
	names := []string{"alpha", "peer-01", "A_B.C"}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			pkt := m.buildAnnounce(PeerInfo{ID: peerID(3), Name: name, Addrs: []string{"192.168.10.20"}})
			peer := m.parseMDNSResponse(pkt, mdnsSrc("192.168.10.20"))
			if peer == nil {
				t.Fatal("expected the packet to parse")
			}
			if peer.Name != name {
				t.Errorf("expected %q, got %q", name, peer.Name)
			}
			if peer.Addrs[0] != "192.168.10.20:4443" {
				t.Errorf("expected 192.168.10.20:4443, got %q", peer.Addrs[0])
			}
		})
	}
}

func TestBuildAnnounceNonIPv4SkipsARecord(t *testing.T) {
	m := NewMDNSDiscovery()
	pkt := m.buildAnnounce(PeerInfo{ID: peerID(3), Name: "alpha", Addrs: []string{"2001:db8::1"}})

	// The A record is skipped for a v6 address, so only SRV and TXT are written
	// and the advertised answer count must say exactly that.
	if a := binary.BigEndian.Uint16(pkt[6:8]); a != 2 {
		t.Errorf("expected the answer count to match the 2 records written, got %d", a)
	}
	peer := m.parseMDNSResponse(pkt, mdnsSrc("2001:db8::1"))
	if peer == nil {
		t.Fatal("expected the packet to still parse")
	}
	if len(peer.Addrs) != 1 {
		t.Errorf("expected the source address fallback to be used, got %v", peer.Addrs)
	}
}

// The A record is the only conditional one, so any absent or unparsable
// address must not be indexed into or advertised as an answer.
func TestBuildAnnounceWithoutAddrs(t *testing.T) {
	inputs := []struct {
		name  string
		addrs []string
	}{
		{"nil address list", nil},
		{"empty address list", []string{}},
		{"empty string address", []string{""}},
		{"unparsable address", []string{"not-an-ip"}},
		{"ipv6 address", []string{"2001:db8::1"}},
	}

	for _, in := range inputs {
		t.Run(in.name, func(t *testing.T) {
			m := NewMDNSDiscovery()
			pkt := m.buildAnnounce(PeerInfo{ID: peerID(3), Name: "alpha", Addrs: in.addrs})

			if a := binary.BigEndian.Uint16(pkt[6:8]); a != 2 {
				t.Errorf("expected SRV + TXT only, got an answer count of %d", a)
			}
			peer := m.parseMDNSResponse(pkt, mdnsSrc("10.0.0.7"))
			if peer == nil {
				t.Fatal("expected the packet to still parse")
			}
			if peer.Name != "alpha" {
				t.Errorf("expected name %q, got %q", "alpha", peer.Name)
			}
			if peer.Addrs[0] != "10.0.0.7:4443" {
				t.Errorf("expected the source address fallback, got %q", peer.Addrs[0])
			}
		})
	}
}

// The declared answer count drives how far a conformant parser walks the
// packet, so it has to equal the records actually written.
func TestBuildAnnounceAnswerCountMatchesRecords(t *testing.T) {
	tests := []struct {
		name      string
		addrs     []string
		wantCount uint16
	}{
		{"ipv4 writes SRV, A and TXT", []string{"10.0.0.5"}, 3},
		{"ipv6 skips the A record", []string{"2001:db8::1"}, 2},
		{"no address skips the A record", nil, 2},
		{"unparsable address skips the A record", []string{"not-an-ip"}, 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewMDNSDiscovery()
			pkt := m.buildAnnounce(PeerInfo{ID: peerID(3), Name: "alpha", Addrs: tt.addrs})

			declared := binary.BigEndian.Uint16(pkt[6:8])
			if declared != tt.wantCount {
				t.Errorf("expected the header to declare %d answers, got %d", tt.wantCount, declared)
			}

			records := 0
			offset := 12
			for offset < len(pkt) {
				offset = skipDNSName(pkt, offset)
				if offset+10 > len(pkt) {
					t.Fatalf("record %d header runs past the packet", records)
				}
				records++
				offset += 10 + int(binary.BigEndian.Uint16(pkt[offset+8:offset+10]))
			}
			if records != int(declared) {
				t.Errorf("expected the declared count %d to match the %d records present", declared, records)
			}
		})
	}
}

// The identity is what deduplicates peers across transports; if the parser drops
// it every discovered node collapses onto the zero ID.
func TestBuildAnnounceRoundTripPreservesPeerID(t *testing.T) {
	speckled := [32]byte{}
	copy(speckled[:], []byte{0xDE, 0xAD, 0xBE, 0xEF, 0x01, 0x23, 0x45, 0x67})

	tests := []struct {
		name string
		id   [32]byte
	}{
		{"single byte identity", peerID(0xAB)},
		{"speckled identity", speckled},
		{"zero identity", [32]byte{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewMDNSDiscovery()
			info := PeerInfo{
				ID:       tt.id,
				Name:     "alpha",
				Addrs:    []string{"192.168.1.50"},
				Services: []ServiceInfo{{Name: "dns"}, {Name: "http"}, {Name: "messaging"}},
			}

			peer := m.parseMDNSResponse(m.buildAnnounce(info), mdnsSrc("192.168.1.50"))
			if peer == nil {
				t.Fatal("expected the announcement to parse")
			}
			if peer.ID != info.ID {
				t.Errorf("expected the identity %x to survive the round trip, got %x", info.ID[:8], peer.ID[:8])
			}
			if len(peer.Services) != 3 {
				t.Errorf("expected the 3 advertised services, got %+v", peer.Services)
			}
		})
	}
}

func TestParseMDNSResponseRejectsShortPacket(t *testing.T) {
	m := NewMDNSDiscovery()
	for _, n := range []int{0, 1, 11} {
		if got := m.parseMDNSResponse(make([]byte, n), mdnsSrc("10.0.0.5")); got != nil {
			t.Errorf("expected a %d byte packet to be rejected, got %+v", n, got)
		}
	}
}

func TestParseMDNSResponseRejectsQuery(t *testing.T) {
	m := NewMDNSDiscovery()

	// Flags without 0x8000 are a query, not a response.
	pkt := append(dnsHeader(0x0000, 0, 0), encodeDNSName("_localweb._tcp.local")...)
	pkt = append(pkt, 0, 12, 0, 1)

	if got := m.parseMDNSResponse(pkt, mdnsSrc("10.0.0.5")); got != nil {
		t.Errorf("expected a query to be ignored, got %+v", got)
	}
}

func TestParseMDNSResponseRejectsAnswersWithoutName(t *testing.T) {
	m := NewMDNSDiscovery()

	// One TXT answer and nothing that carries a host name.
	pkt := dnsHeader(0x8400, 0, 1)
	pkt = append(pkt, encodeDNSName("x.local")...)
	pkt = appendDNSAnswer(pkt, encodeDNSName("x.local"), 16, 120, []byte{4, 'v', 'e', 'r', '1'})

	if got := m.parseMDNSResponse(pkt, mdnsSrc("10.0.0.5")); got != nil {
		t.Errorf("expected a nameless response to be dropped, got %+v", got)
	}
}

func TestParseMDNSResponseRejectsEmptyAnswerSection(t *testing.T) {
	m := NewMDNSDiscovery()
	if got := m.parseMDNSResponse(dnsHeader(0x8400, 0, 0), mdnsSrc("10.0.0.5")); got != nil {
		t.Errorf("expected a response with no answers to be dropped, got %+v", got)
	}
}

func TestParseMDNSResponseSkipsQuestions(t *testing.T) {
	m := NewMDNSDiscovery()

	pkt := dnsHeader(0x8400, 1, 1)
	// One question, as a real mDNS query-with-known-answer would carry.
	pkt = append(pkt, encodeDNSName("_localweb._tcp.local")...)
	pkt = append(pkt, 0, 12, 0, 1) // QTYPE=SRV, QCLASS=IN
	pkt = appendDNSAnswer(pkt, encodeDNSName("_localweb._tcp.local"), 33, 120, srvRecordData("beta"))

	peer := m.parseMDNSResponse(pkt, mdnsSrc("10.0.0.9"))
	if peer == nil {
		t.Fatal("expected the answer to be found past the question")
	}
	if peer.Name != "beta" {
		t.Errorf("expected the SRV target name %q, got %q", "beta", peer.Name)
	}
	if peer.Addrs[0] != "10.0.0.9:4443" {
		t.Errorf("expected the source address to be used when there is no A record, got %q", peer.Addrs[0])
	}
}

func TestParseMDNSResponseHandlesAllRecordTypes(t *testing.T) {
	m := NewMDNSDiscovery()

	tests := []struct {
		name     string
		rtype    uint16
		data     []byte
		wantName string
		wantAddr string
	}{
		{"srv sets the name", 33, srvRecordData("gamma"), "gamma", "10.0.0.1:4443"},
		{"a overrides the address", 1, []byte{10, 0, 0, 77}, "gamma", "10.0.0.77:4443"},
		{"aaaa overrides the address", 28, net.ParseIP("2001:db8::5").To16(), "gamma", "[2001:db8::5]:4443"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Every packet carries an SRV record, because the parser derives the
			// peer name from it and drops any response without a name.
			pkt := dnsHeader(0x8400, 0, 2)
			pkt = appendDNSAnswer(pkt, encodeDNSName("_localweb._tcp.local"), 33, 120, srvRecordData("gamma"))
			pkt = appendDNSAnswer(pkt, encodeDNSName("gamma.local"), tt.rtype, 120, tt.data)

			peer := m.parseMDNSResponse(pkt, mdnsSrc("10.0.0.1"))
			if peer == nil {
				t.Fatal("expected the packet to parse")
			}
			if peer.Name != tt.wantName {
				t.Errorf("expected name %q, got %q", tt.wantName, peer.Name)
			}
			if peer.Addrs[0] != tt.wantAddr {
				t.Errorf("expected address %q, got %q", tt.wantAddr, peer.Addrs[0])
			}
		})
	}
}

func TestParseMDNSResponseWithoutNameIsDropped(t *testing.T) {
	m := NewMDNSDiscovery()

	// A and TXT records alone never populate peer.Name, so the whole response
	// is discarded even though usable data was present.
	pkt := dnsHeader(0x8400, 0, 2)
	pkt = appendDNSAnswer(pkt, encodeDNSName("delta.local"), 1, 120, []byte{10, 0, 0, 9})
	pkt = appendDNSAnswer(pkt, encodeDNSName("delta.local"), 16, 120, buildTxtEntry("svc=dns"))

	if got := m.parseMDNSResponse(pkt, mdnsSrc("10.0.0.1")); got != nil {
		t.Errorf("expected a nameless response to be dropped, got %+v", got)
	}
}

func TestParseMDNSResponseTXTRecord(t *testing.T) {
	m := NewMDNSDiscovery()
	pkt := dnsHeader(0x8400, 0, 2)
	pkt = appendDNSAnswer(pkt, encodeDNSName("_localweb._tcp.local"), 33, 120, srvRecordData("delta"))
	pkt = appendDNSAnswer(pkt, encodeDNSName("delta.local"), 16, 120, buildTxtEntry("svc=dns,http,messaging"))

	peer := m.parseMDNSResponse(pkt, mdnsSrc("10.0.0.1"))
	if peer == nil {
		t.Fatal("expected the packet to parse")
	}
	if len(peer.Services) != 3 {
		t.Fatalf("expected 3 services from the TXT record, got %d (%+v)", len(peer.Services), peer.Services)
	}
	wantNames := []string{"dns", "http", "messaging"}
	for i, want := range wantNames {
		if peer.Services[i].Name != want {
			t.Errorf("service %d: expected %q, got %q", i, want, peer.Services[i].Name)
		}
	}
}

// A responder may split its advertisement over several TXT answers; the service
// lists have to be unioned rather than overwritten by the last record.
func TestParseMDNSResponseUnionsMultipleTXTRecords(t *testing.T) {
	m := NewMDNSDiscovery()

	pkt := dnsHeader(0x8400, 0, 3)
	pkt = appendDNSAnswer(pkt, encodeDNSName("_localweb._tcp.local"), 33, 120, srvRecordData("epsilon"))
	pkt = appendDNSAnswer(pkt, encodeDNSName("epsilon.local"), 16, 120,
		concatTxt(buildTxtEntry("id=deadbeefdeadbeef"), buildTxtEntry("svc=dns")))
	pkt = appendDNSAnswer(pkt, encodeDNSName("epsilon.local"), 16, 120, buildTxtEntry("svc=http,messaging"))

	peer := m.parseMDNSResponse(pkt, mdnsSrc("10.0.0.1"))
	if peer == nil {
		t.Fatal("expected the packet to parse")
	}
	if peer.Name != "epsilon" {
		t.Errorf("expected name %q, got %q", "epsilon", peer.Name)
	}
	if len(peer.Services) != 3 {
		t.Fatalf("expected the service lists to be unioned into 3 entries, got %+v", peer.Services)
	}
	wantNames := []string{"dns", "http", "messaging"}
	for i, want := range wantNames {
		if peer.Services[i].Name != want {
			t.Errorf("service %d: expected %q, got %q", i, want, peer.Services[i].Name)
		}
	}
}

func TestParseMDNSResponseStopsWhenAnswerCountExceedsData(t *testing.T) {
	m := NewMDNSDiscovery()

	// The header claims two answers but only one is present.
	pkt := dnsHeader(0x8400, 0, 2)
	pkt = appendDNSAnswer(pkt, encodeDNSName("epsilon.local"), 33, 120, srvRecordData("epsilon"))

	peer := m.parseMDNSResponse(pkt, mdnsSrc("10.0.0.1"))
	if peer == nil {
		t.Fatal("expected the present answer to still be parsed")
	}
	if peer.Name != "epsilon" {
		t.Errorf("expected name %q, got %q", "epsilon", peer.Name)
	}
}

// The receive buffer is filled straight off the wire, so a datagram whose
// rdata length field overruns the packet must be dropped rather than sliced.
func TestParseMDNSResponseTruncatedRecordData(t *testing.T) {
	t.Run("rdata cut short by truncation", func(t *testing.T) {
		m := NewMDNSDiscovery()

		// The header claims two answers; the second record keeps its 10 byte
		// header but loses the tail of its rdata.
		pkt := dnsHeader(0x8400, 0, 2)
		pkt = appendDNSAnswer(pkt, encodeDNSName("_localweb._tcp.local"), 33, 120, srvRecordData("gamma"))
		pkt = appendDNSAnswer(pkt, encodeDNSName("gamma.local"), 16, 120, buildTxtEntry("svc=dns"))
		pkt = pkt[:len(pkt)-3]

		peer := m.parseMDNSResponse(pkt, mdnsSrc("10.0.0.1"))
		if peer == nil {
			t.Fatal("expected the intact record to still parse")
		}
		if peer.Name != "gamma" {
			t.Errorf("expected name %q, got %q", "gamma", peer.Name)
		}
		if len(peer.Services) != 0 {
			t.Errorf("expected the truncated record to be skipped, got %+v", peer.Services)
		}
	})

	t.Run("rdata length overruns the datagram", func(t *testing.T) {
		m := NewMDNSDiscovery()

		// Assembled by hand because a well formed helper cannot emit a record
		// whose length field disagrees with the bytes that follow it.
		pkt := dnsHeader(0x8400, 0, 1)
		pkt = append(pkt, encodeDNSName("gamma.local")...)
		rec := make([]byte, 10)
		binary.BigEndian.PutUint16(rec[0:2], 16)      // TXT
		binary.BigEndian.PutUint16(rec[2:4], 1)       // Class IN
		binary.BigEndian.PutUint32(rec[4:8], mdnsTTL) // TTL
		binary.BigEndian.PutUint16(rec[8:10], 0xFFFF) // Hostile rdata length
		pkt = append(pkt, rec...)
		pkt = append(pkt, []byte{4, 'v', 'e', 'r', '1'}...)

		if got := m.parseMDNSResponse(pkt, mdnsSrc("10.0.0.1")); got != nil {
			t.Errorf("expected a record that overruns the packet to be dropped, got %+v", got)
		}
	})
}

func TestParseTXTRecord(t *testing.T) {
	tests := []struct {
		name  string
		data  []byte
		want  []string
		count int
	}{
		{
			name:  "single service",
			data:  buildTxtEntry("svc=dns"),
			want:  []string{"dns"},
			count: 1,
		},
		{
			name:  "comma separated services",
			data:  buildTxtEntry("svc=dns,http,messaging"),
			want:  []string{"dns", "http", "messaging"},
			count: 3,
		},
		{
			name:  "several entries, only svc is surfaced",
			data:  concatTxt(buildTxtEntry("id=deadbeefdeadbeef"), buildTxtEntry("ver=1"), buildTxtEntry("svc=docs")),
			want:  []string{"docs"},
			count: 1,
		},
		{
			name:  "entry without the svc prefix is ignored",
			data:  buildTxtEntry("other=1"),
			count: 0,
		},
		{
			name:  "length prefix past the end stops parsing",
			data:  []byte{40, 's', 'v', 'c', '='},
			count: 0,
		},
		{
			name:  "empty record",
			data:  []byte{},
			count: 0,
		},
		{
			name:  "zero length entry is skipped",
			data:  []byte{0, 3, 'v', 'e', 'r'},
			count: 0,
		},
		{
			name:  "empty service list yields one empty entry",
			data:  buildTxtEntry("svc="),
			want:  []string{""},
			count: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseTXTRecord(tt.data)
			if len(got) != tt.count {
				t.Fatalf("expected %d services, got %d (%+v)", tt.count, len(got), got)
			}
			for i, want := range tt.want {
				if got[i].Name != want {
					t.Errorf("service %d: expected %q, got %q", i, want, got[i].Name)
				}
			}
		})
	}
}

func TestBuildTXTRecordContainsExpectedKeys(t *testing.T) {
	data := string(buildTXTRecord(PeerInfo{ID: peerID(1), Name: "alpha"}))

	for _, want := range []string{"id=", "ver=1"} {
		if !containsString(data, want) {
			t.Errorf("expected %q in the encoded TXT record, got %q", want, data)
		}
	}
	// The id is the hex form of the first 8 identity bytes.
	if want := "id=0100000000000000"; !containsString(data, want) {
		t.Errorf("expected %q in the encoded TXT record, got %q", want, data)
	}
}

func TestBuildTXTRecordWithServices(t *testing.T) {
	data := buildTXTRecord(PeerInfo{
		ID:       peerID(1),
		Name:     "alpha",
		Services: []ServiceInfo{{Name: "dns", Port: 5353}, {Name: "http", Port: 8080}},
	})

	want := "svc=dns,http"
	if !containsString(string(data), want) {
		t.Errorf("expected %q in the encoded TXT record, got %q", want, string(data))
	}
}

// Every TXT string is length prefixed. A prefix that disagrees with the bytes
// that follow makes the whole record unparseable, which silently costs the
// receiver every advertised service.
func TestBuildTXTRecordLengthPrefixMatchesPayload(t *testing.T) {
	data := buildTXTRecord(PeerInfo{
		ID:       peerID(1),
		Name:     "alpha",
		Services: []ServiceInfo{{Name: "dns"}, {Name: "http"}, {Name: "messaging"}},
	})

	var entries []string
	for offset := 0; offset < len(data); {
		length := int(data[offset])
		offset++
		if offset+length > len(data) {
			t.Fatalf("entry %d declares %d bytes but only %d remain", len(entries), length, len(data)-offset)
		}
		entries = append(entries, string(data[offset:offset+length]))
		offset += length
	}

	want := []string{"id=0100000000000000", "ver=1", "svc=dns,http,messaging"}
	if len(entries) != len(want) {
		t.Fatalf("expected %d well framed entries, got %d (%q)", len(want), len(entries), entries)
	}
	for i := range want {
		if entries[i] != want[i] {
			t.Errorf("entry %d: expected %q, got %q", i, want[i], entries[i])
		}
	}

	// The record has to survive the trip through the parser too.
	services := parseTXTRecord(data)
	if len(services) != 3 {
		t.Fatalf("expected the 3 advertised services to round trip, got %+v", services)
	}
	for i, name := range []string{"dns", "http", "messaging"} {
		if services[i].Name != name {
			t.Errorf("service %d: expected %q, got %q", i, name, services[i].Name)
		}
	}
	if got := parseTXTValue(data, "id"); got != "0100000000000000" {
		t.Errorf("expected the id to be readable back out, got %q", got)
	}
}

func TestParseTXTValue(t *testing.T) {
	data := concatTxt(
		buildTxtEntry("id=deadbeefdeadbeef"),
		buildTxtEntry("ver=1"),
		buildTxtEntry("svc=dns"),
	)

	if got := parseTXTValue(data, "id"); got != "deadbeefdeadbeef" {
		t.Errorf("expected the id value, got %q", got)
	}
	if got := parseTXTValue(data, "ver"); got != "1" {
		t.Errorf("expected the ver value, got %q", got)
	}
	if got := parseTXTValue(data, "absent"); got != "" {
		t.Errorf("expected an absent key to yield an empty string, got %q", got)
	}
	if got := parseTXTValue([]byte{40, 'i', 'd'}, "id"); got != "" {
		t.Errorf("expected a malformed record to yield an empty string, got %q", got)
	}
}

func TestPeerIDFromHex(t *testing.T) {
	want := [32]byte{}
	copy(want[:], []byte{0xDE, 0xAD, 0xBE, 0xEF, 0x01, 0x23, 0x45, 0x67})

	if got := peerIDFromHex("deadbeef01234567"); got != want {
		t.Errorf("expected %x, got %x", want[:8], got[:8])
	}
	if got := peerIDFromHex("0000000000000000"); got != ([32]byte{}) {
		t.Errorf("expected the zero identity, got %x", got[:8])
	}
	for _, bad := range []string{"", "nothex", "deadbeef0123456", "deadbeef01234567890"} {
		if got := peerIDFromHex(bad); got != ([32]byte{}) {
			t.Errorf("peerIDFromHex(%q): expected the zero identity, got %x", bad, got[:8])
		}
	}
}

func TestMDNSDiscoveryGetLocalAddr(t *testing.T) {
	m := NewMDNSDiscovery()
	addr := m.getLocalAddr()

	if addr == "" {
		t.Fatal("expected a non-empty local address")
	}
	if addr != "0.0.0.0" && net.ParseIP(addr) == nil {
		t.Errorf("expected an IP literal or the 0.0.0.0 fallback, got %q", addr)
	}
}

func TestPickMulticastInterfaceIsSelfConsistent(t *testing.T) {
	iface, err := pickMulticastInterface()
	switch {
	case err != nil:
		// No usable interface on this host; the error must still be descriptive.
		if err.Error() == "" {
			t.Error("expected a non-empty error message")
		}
	case iface == nil:
		t.Error("expected a non-nil interface when no error is returned")
	default:
		if iface.Flags&net.FlagUp == 0 {
			t.Error("expected the returned interface to be up")
		}
		if iface.Flags&net.FlagMulticast == 0 {
			t.Error("expected the returned interface to support multicast")
		}
	}
}

// srvRecordData builds the 6 byte SRV preamble plus an encoded target name.
func srvRecordData(name string) []byte {
	data := make([]byte, 6)
	binary.BigEndian.PutUint16(data[2:4], 0)    // weight
	binary.BigEndian.PutUint16(data[4:6], 4443) // port
	return append(data, encodeDNSName(name+".local")...)
}

// buildTxtEntry frames a single correctly length-prefixed TXT string.
func buildTxtEntry(s string) []byte {
	return append([]byte{byte(len(s))}, []byte(s)...)
}

func concatTxt(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

func containsString(haystack, needle string) bool {
	return len(needle) == 0 || len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
