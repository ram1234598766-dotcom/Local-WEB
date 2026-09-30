package link

import (
	"bytes"
	"context"
	"encoding/binary"
	"strings"
	"testing"
	"time"
)

func TestNewBLE(t *testing.T) {
	nodeID := testNodeID(1)
	pub := testNodeID(2)

	b, err := NewBLE(nodeID, pub, "node-a", CapDNS|CapHTTP)
	if err != nil {
		t.Fatalf("NewBLE: %v", err)
	}
	if b == nil {
		t.Fatal("NewBLE returned a nil link with a nil error")
	}
	if b.nodeID != nodeID {
		t.Errorf("nodeID = %x, want %x", b.nodeID, nodeID)
	}
	if b.publicKey != pub {
		t.Errorf("publicKey = %x, want %x", b.publicKey, pub)
	}
	if b.name != "node-a" {
		t.Errorf("name = %q, want node-a", b.name)
	}
	if b.capabilities != CapDNS|CapHTTP {
		t.Errorf("capabilities = %b, want %b", b.capabilities, CapDNS|CapHTTP)
	}
	if b.peers == nil {
		t.Error("peer map not initialised")
	}
	if b.adapter == nil {
		t.Fatal("adapter not initialised")
	}
	if !b.IsAvailable(context.Background()) {
		t.Error("IsAvailable() = false, want true from the stub adapter")
	}
	if b.ctx == nil || b.ctx.Err() != nil {
		t.Error("BLE context should start live")
	}
}

func TestBLESurfaceValues(t *testing.T) {
	b, err := NewBLE(testNodeID(1), testNodeID(2), "n", 0)
	if err != nil {
		t.Fatalf("NewBLE: %v", err)
	}
	defer b.Stop()

	if got := b.Name(); got != "ble" {
		t.Errorf("Name() = %q, want ble", got)
	}
	if got := b.Mode(); got != ModeBLE {
		t.Errorf("Mode() = %s, want ble", got)
	}
	if b.RequiresWiFi() {
		t.Error("RequiresWiFi() = true, want false (BLE needs no WiFi radio)")
	}
	if b.RequiresRouter() {
		t.Error("RequiresRouter() = true, want false")
	}
	if got := b.Bandwidth(); got != 1 {
		t.Errorf("Bandwidth() = %d, want 1", got)
	}
	if got := b.MaxPeers(); got != 10 {
		t.Errorf("MaxPeers() = %d, want 10", got)
	}
}

func TestBLECapabilitiesAreDistinctBits(t *testing.T) {
	caps := map[string]uint32{
		"CapDNS": CapDNS, "CapHTTP": CapHTTP, "CapSMTP": CapSMTP,
		"CapIMAP": CapIMAP, "CapMsg": CapMsg, "CapFiles": CapFiles,
		"CapVoice": CapVoice, "CapVPN": CapVPN, "CapDocs": CapDocs,
		"CapRegistry": CapRegistry,
	}
	if CapDNS != 1 {
		t.Errorf("CapDNS = %d, want 1", CapDNS)
	}
	if CapRegistry != 1<<9 {
		t.Errorf("CapRegistry = %d, want %d", CapRegistry, 1<<9)
	}
	seen := make(map[uint32]string, len(caps))
	for name, bit := range caps {
		if bit == 0 || bit&(bit-1) != 0 {
			t.Errorf("%s = %#x is not a single bit", name, bit)
		}
		if other, dup := seen[bit]; dup {
			t.Errorf("%s and %s share bit %#x", name, other, bit)
		}
		seen[bit] = name
	}
}

func TestGATTPropertyFlagsAreDistinctBits(t *testing.T) {
	props := map[string]uint8{
		"GATTRead": GATTRead, "GATTWrite": GATTWrite,
		"GATTWriteWithoutResponse": GATTWriteWithoutResponse,
		"GATTNotify":               GATTNotify, "GATTIndicate": GATTIndicate,
	}
	seen := make(map[uint8]string, len(props))
	for name, bit := range props {
		if bit == 0 || bit&(bit-1) != 0 {
			t.Errorf("%s = %#x is not a single bit", name, bit)
		}
		if other, dup := seen[bit]; dup {
			t.Errorf("%s and %s share bit %#x", name, other, bit)
		}
		seen[bit] = name
	}
	// A characteristic must be able to combine read and notify without the
	// flags colliding.
	combined := GATTRead | GATTNotify
	if combined != GATTRead+GATTNotify {
		t.Errorf("GATTRead|GATTNotify = %#x, want %#x", combined, GATTRead+GATTNotify)
	}
}

func TestParseBLEIdentity(t *testing.T) {
	build := func(node, pub, name []byte, caps uint32, version uint16) []byte {
		buf := make([]byte, 102)
		copy(buf[0:32], node)
		copy(buf[32:64], pub)
		copy(buf[64:96], name)
		binary.LittleEndian.PutUint32(buf[96:100], caps)
		binary.LittleEndian.PutUint16(buf[100:102], version)
		return buf
	}

	t.Run("rejects short payloads", func(t *testing.T) {
		for _, n := range []int{0, 1, 101} {
			if got := parseBLEIdentity(make([]byte, n)); got != nil {
				t.Errorf("parseBLEIdentity(%d bytes) = %+v, want nil", n, got)
			}
		}
	})

	t.Run("parses the fixed-width layout", func(t *testing.T) {
		node := testNodeID(3)
		pub := testNodeID(4)
		got := parseBLEIdentity(build(node[:], pub[:], []byte("peer"), CapDNS|CapMsg, 7))
		if got == nil {
			t.Fatal("parseBLEIdentity returned nil for a valid payload")
		}
		if got.NodeID != node {
			t.Errorf("NodeID = %x, want %x", got.NodeID, node)
		}
		if got.PublicKey != pub {
			t.Errorf("PublicKey = %x, want %x", got.PublicKey, pub)
		}
		if want := [32]byte{'p', 'e', 'e', 'r'}; got.Name != want {
			t.Errorf("Name = %q, want %q (null-padded to 32 bytes)", got.Name, want)
		}
		if got.Capabilities != CapDNS|CapMsg {
			t.Errorf("Capabilities = %b, want %b", got.Capabilities, CapDNS|CapMsg)
		}
		if got.Version != 7 {
			t.Errorf("Version = %d, want 7", got.Version)
		}
	})

	t.Run("ignores trailing bytes", func(t *testing.T) {
		node := testNodeID(5)
		pub := testNodeID(6)
		payload := append(build(node[:], pub[:], []byte("x"), CapHTTP, 1), 0xde, 0xad)
		got := parseBLEIdentity(payload)
		if got == nil {
			t.Fatal("parseBLEIdentity returned nil for a payload with trailing bytes")
		}
		if got.NodeID != node {
			t.Errorf("NodeID = %x, want %x", got.NodeID, node)
		}
	})
}

func TestBLEIdentityRoundTrip(t *testing.T) {
	nodeID := testNodeID(7)
	pub := testNodeID(8)
	b := &BLE{nodeID: nodeID, publicKey: pub, name: "round-trip", capabilities: CapFiles | CapVPN}

	encoded := b.encodeIdentity()
	if len(encoded) != 102 {
		t.Fatalf("encodeIdentity() length = %d, want 102", len(encoded))
	}

	got := parseBLEIdentity(encoded)
	if got == nil {
		t.Fatal("parseBLEIdentity returned nil for our own encoding")
	}
	if got.NodeID != nodeID {
		t.Errorf("NodeID = %x, want %x", got.NodeID, nodeID)
	}
	if got.PublicKey != pub {
		t.Errorf("PublicKey = %x, want %x", got.PublicKey, pub)
	}
	if got.Capabilities != CapFiles|CapVPN {
		t.Errorf("Capabilities = %b, want %b", got.Capabilities, CapFiles|CapVPN)
	}
	if got.Version != 1 {
		t.Errorf("Version = %d, want 1 (the hardcoded protocol version)", got.Version)
	}
	// The name field is a fixed 32-byte array, so a short name is padded.
	if want := [32]byte{'r', 'o', 'u', 'n', 'd', '-', 't', 'r', 'i', 'p'}; got.Name != want {
		t.Errorf("Name = %q, want %q", got.Name, want)
	}
}

func TestBLEEncodeIdentityTruncatesOverlongName(t *testing.T) {
	b := &BLE{name: strings.Repeat("n", 48)}
	encoded := b.encodeIdentity()
	if len(encoded) != 102 {
		t.Fatalf("encodeIdentity() length = %d, want 102", len(encoded))
	}
	got := parseBLEIdentity(encoded)
	if got == nil {
		t.Fatal("parseBLEIdentity returned nil")
	}
	if !bytes.Equal(got.Name[:], bytes.Repeat([]byte("n"), 32)) {
		t.Errorf("Name field = %q, want 32 'n' bytes", got.Name)
	}
}

// adElement is one Advertising Data (AD) structure: a type byte plus payload.
type adElement struct {
	typ     byte
	payload []byte
}

// parseAD walks an advertising payload using the [len][type][len-1 bytes]
// framing, so tests do not depend on hardcoded byte offsets.
func parseAD(t *testing.T, raw []byte) []adElement {
	t.Helper()
	var out []adElement
	for off := 0; off < len(raw); {
		if off+2 > len(raw) {
			t.Fatalf("truncated AD structure at offset %d of %d bytes", off, len(raw))
		}
		length := int(raw[off])
		if length == 0 {
			t.Fatalf("zero-length AD structure at offset %d", off)
		}
		if off+1+length > len(raw) {
			t.Fatalf("AD structure at offset %d claims %d bytes, only %d remain", off, length, len(raw)-off-1)
		}
		out = append(out, adElement{typ: raw[off+1], payload: raw[off+2 : off+1+length]})
		off += 1 + length
	}
	return out
}

func findAD(elems []adElement, typ byte) (adElement, bool) {
	for _, e := range elems {
		if e.typ == typ {
			return e, true
		}
	}
	return adElement{}, false
}

func TestBLEBuildAdvertDataFlags(t *testing.T) {
	data := (&BLE{name: "node-a"}).buildAdvertData()
	elems := parseAD(t, data)

	flags, ok := findAD(elems, 0x01)
	if !ok {
		t.Fatalf("no flags AD structure in % x", data)
	}
	if len(flags.payload) != 1 {
		t.Fatalf("flags payload is %d bytes, want 1", len(flags.payload))
	}
	// Bit 0 LE General Discoverable Mode, bit 1 BR/EDR Not Supported, and no
	// other flag bits may be set.
	const want = 0x06
	if flags.payload[0] != want {
		t.Errorf("flags = %#x, want %#x", flags.payload[0], want)
	}
}

// The 128-bit service UUID AD element is 1 type byte plus 16 UUID bytes, so its
// length prefix must be 17. Copying into a 17-byte buffer dropped the last UUID
// byte and advertised a 15-byte UUID that matches nothing.
func TestBLEBuildAdvertDataServiceUUID(t *testing.T) {
	data := (&BLE{name: "node-a"}).buildAdvertData()
	elems := parseAD(t, data)

	svc, ok := findAD(elems, 0x07)
	if !ok {
		t.Fatalf("no 128-bit service UUID AD structure in % x", data)
	}
	// parseAD slices the payload using the declared length, so a 16-byte payload
	// also proves the prefix claimed 17 rather than 16.
	if len(svc.payload) != len(LocalWEBServiceUUID) {
		t.Fatalf("service UUID payload is %d bytes, want %d: % x", len(svc.payload), len(LocalWEBServiceUUID), svc.payload)
	}
	if !bytes.Equal(svc.payload, LocalWEBServiceUUID[:]) {
		t.Errorf("service UUID = % x, want % x", svc.payload, LocalWEBServiceUUID)
	}
}

func TestBLEBuildAdvertDataNameField(t *testing.T) {
	elems := parseAD(t, (&BLE{name: "node-a"}).buildAdvertData())

	name, ok := findAD(elems, 0x08)
	if !ok {
		t.Fatal("no shortened local name AD structure")
	}
	if got := string(name.payload); got != "node-a" {
		t.Errorf("name = %q, want node-a", got)
	}
}

func TestBLEBuildAdvertDataTruncatesNameToTwentyBytes(t *testing.T) {
	long := strings.Repeat("x", 40)
	elems := parseAD(t, (&BLE{name: long}).buildAdvertData())

	name, ok := findAD(elems, 0x08)
	if !ok {
		t.Fatal("no shortened local name AD structure")
	}
	if len(name.payload) != 20 {
		t.Errorf("name payload is %d bytes, want the 20-byte BLE cap", len(name.payload))
	}
	if got := string(name.payload); got != strings.Repeat("x", 20) {
		t.Errorf("truncated name = %q, want 20 x's", got)
	}
}

func TestBLEBuildAdvertDataWithEmptyName(t *testing.T) {
	elems := parseAD(t, (&BLE{name: ""}).buildAdvertData())

	name, ok := findAD(elems, 0x08)
	if !ok {
		t.Fatal("no shortened local name AD structure for an empty name")
	}
	if len(name.payload) != 0 {
		t.Errorf("name payload is %d bytes, want 0", len(name.payload))
	}
}

func TestBLEServiceDefinition(t *testing.T) {
	b := &BLE{nodeID: testNodeID(9), publicKey: testNodeID(10), name: "svc"}
	chars := b.serviceDefinition()

	if len(chars) != 3 {
		t.Fatalf("service definition has %d characteristics, want 3", len(chars))
	}
	if chars[0].UUID != BLEIdentityUUID {
		t.Errorf("characteristic 0 UUID = % x, want the identity UUID", chars[0].UUID)
	}
	if chars[0].Props != GATTRead|GATTNotify {
		t.Errorf("identity props = %#x, want read|notify", chars[0].Props)
	}
	if len(chars[0].Value) != 102 {
		t.Errorf("identity value is %d bytes, want the 102-byte encoding", len(chars[0].Value))
	}
	if chars[1].UUID != BLEMessagingUUID {
		t.Errorf("characteristic 1 UUID = % x, want the messaging UUID", chars[1].UUID)
	}
	if chars[1].Props != GATTWrite|GATTNotify {
		t.Errorf("messaging props = %#x, want write|notify", chars[1].Props)
	}
	if chars[1].Value != nil {
		t.Error("messaging characteristic should carry no initial value")
	}
	if chars[2].UUID != BLETransferUUID {
		t.Errorf("characteristic 2 UUID = % x, want the transfer UUID", chars[2].UUID)
	}
	if chars[2].Props != GATTWriteWithoutResponse {
		t.Errorf("transfer props = %#x, want write-without-response", chars[2].Props)
	}

	// The three UUIDs must be distinct so a subscriber can tell them apart.
	seen := map[[16]byte]bool{}
	for _, c := range chars {
		if seen[c.UUID] {
			t.Errorf("duplicate characteristic UUID % x", c.UUID)
		}
		seen[c.UUID] = true
	}
}

func TestBLEAdapterIsAnUnconditionalStub(t *testing.T) {
	// Documenting current behaviour: the adapter reports itself powered on
	// every platform and never discovers anything.
	a, err := newBLEAdapter()
	if err != nil {
		t.Fatalf("newBLEAdapter: %v", err)
	}
	if a == nil {
		t.Fatal("newBLEAdapter returned a nil adapter")
	}
	if !a.IsPowered() {
		t.Error("IsPowered() = false, want true (powered is hardcoded)")
	}
	if err := a.StartGATTServer(nil); err != nil {
		t.Errorf("StartGATTServer: %v", err)
	}
	if err := a.StartAdvertising(nil); err != nil {
		t.Errorf("StartAdvertising: %v", err)
	}
	a.StopAdvertising()
	a.StopScanning()
	if got := a.Scan(LocalWEBServiceUUID[:]); got != nil {
		t.Errorf("Scan() = %v results, want nil (no platform backend)", got)
	}
	conn, err := a.Connect("AA:BB:CC:DD:EE:FF")
	if err == nil {
		t.Error("Connect() succeeded, want an error (unimplemented)")
	}
	if conn != nil {
		t.Error("Connect() returned a conn alongside an error")
	}
	if err == nil || !strings.Contains(err.Error(), "not yet implemented") {
		t.Errorf("Connect() error = %v, want it to say not yet implemented", err)
	}
}

func TestBLEScanOnceFindsNothingFromStubAdapter(t *testing.T) {
	b, err := NewBLE(testNodeID(11), testNodeID(12), "n", 0)
	if err != nil {
		t.Fatalf("NewBLE: %v", err)
	}
	defer b.Stop()

	if got := b.scanOnce(); len(got) != 0 {
		t.Errorf("scanOnce() = %d peers, want 0 from the stub adapter", len(got))
	}
}

func TestBLEAdvertiseAndStop(t *testing.T) {
	b, err := NewBLE(testNodeID(13), testNodeID(14), "adv", CapDNS)
	if err != nil {
		t.Fatalf("NewBLE: %v", err)
	}

	if err := b.Advertise(PeerInfo{Name: "peer"}); err != nil {
		t.Errorf("Advertise: %v", err)
	}
	if err := b.Stop(); err != nil {
		t.Errorf("Stop: %v", err)
	}
	if b.ctx.Err() == nil {
		t.Error("Stop did not cancel the link context")
	}
}

func TestBLEDiscoverStartsWithoutHardware(t *testing.T) {
	b, err := NewBLE(testNodeID(15), testNodeID(16), "disc", 0)
	if err != nil {
		t.Fatalf("NewBLE: %v", err)
	}
	defer b.Stop()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events, err := b.Discover(ctx)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if events == nil {
		t.Fatal("Discover returned a nil channel")
	}

	// The stub adapter never produces a peer, so nothing may arrive.
	select {
	case evt := <-events:
		t.Errorf("unexpected event from the stub adapter: %+v", evt)
	case <-time.After(50 * time.Millisecond):
	}

	// Cancelling the caller's context must stop the scan and heartbeat loops.
	cancel()
	if !waitFor(2*time.Second, func() bool { return ctx.Err() != nil }) {
		t.Error("context was not canceled")
	}
}

func TestBLEConnectIsUnimplemented(t *testing.T) {
	b, err := NewBLE(testNodeID(17), testNodeID(18), "c", 0)
	if err != nil {
		t.Fatalf("NewBLE: %v", err)
	}
	defer b.Stop()

	conn, err := b.Connect(context.Background(), "AA:BB:CC:DD:EE:FF")
	if err == nil {
		t.Fatal("Connect succeeded, want an error")
	}
	if conn != nil {
		t.Error("Connect returned a conn alongside an error")
	}
}

func TestBLEServiceUUIDIsStable(t *testing.T) {
	// The UUID is advertised on the air; changing it silently breaks
	// discovery against already-deployed nodes.
	want := [16]byte{
		0x12, 0x34, 0x56, 0x78, 0x9a, 0xbc, 0xde, 0xf0,
		0x12, 0x34, 0x56, 0x78, 0x9a, 0xbc, 0xde, 0xf0,
	}
	if LocalWEBServiceUUID != want {
		t.Errorf("LocalWEBServiceUUID = % x, want % x", LocalWEBServiceUUID, want)
	}
	if BLEIdentityUUID[0] != 0x01 {
		t.Errorf("BLEIdentityUUID first byte = %#x, want 0x01", BLEIdentityUUID[0])
	}
	if BLEMessagingUUID[0] != 0x02 {
		t.Errorf("BLEMessagingUUID first byte = %#x, want 0x02", BLEMessagingUUID[0])
	}
	if BLETransferUUID[0] != 0x03 {
		t.Errorf("BLETransferUUID first byte = %#x, want 0x03", BLETransferUUID[0])
	}
}
