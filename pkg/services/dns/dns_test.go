package dns

import (
	"bytes"
	"testing"
	"time"

	"github.com/ram1234598766-dotcom/Local-WEB/pkg/crypto"
)

const classIN uint16 = 1

func TestSerializeParseMessage(t *testing.T) {
	orig := &DNSMessage{
		Header: DNSHeader{ID: 42, QDCOUNT: 1, ANCOUNT: 1},
		Questions: []DNSQuestion{
			{Name: "host.localweb", Type: TypeA, Class: classIN},
		},
		Answers: []DNSRecord{
			{Name: "host.localweb", Type: TypeA, Class: classIN, TTL: 300, RData: []byte{10, 0, 0, 1}},
		},
	}
	buf, err := SerializeMessage(orig)
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}
	if len(buf) < 12 {
		t.Fatalf("expected at least 12 bytes, got %d", len(buf))
	}

	parsed, err := ParseMessage(buf)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if parsed.Header.ID != 42 {
		t.Fatalf("expected ID 42, got %d", parsed.Header.ID)
	}
	if len(parsed.Questions) != 1 {
		t.Fatalf("expected 1 question, got %d", len(parsed.Questions))
	}
	if parsed.Questions[0].Name != "host.localweb" {
		t.Fatalf("expected host.localweb, got %s", parsed.Questions[0].Name)
	}
	if parsed.Questions[0].Type != TypeA {
		t.Fatalf("expected TypeA, got %v", parsed.Questions[0].Type)
	}
}

func TestEncodeName(t *testing.T) {
	encoded := encodeName("host.localweb")
	if len(encoded) == 0 {
		t.Fatal("expected non-empty encoding")
	}
}

func TestZoneRecords(t *testing.T) {
	z := &Zone{
		Records:   make(map[string][]ResourceRecord),
		Transfers: make(map[[32]byte]bool),
	}
	z.Records["host.localweb"] = []ResourceRecord{
		{Record: DNSRecord{Name: "host.localweb", Type: TypeA, Class: classIN, TTL: 300, RData: []byte{10, 0, 0, 1}}},
	}

	recs, ok := z.Records["host.localweb"]
	if !ok {
		t.Fatal("expected records for host.localweb")
	}
	if len(recs) != 1 {
		t.Fatalf("expected 1 record, got %d", len(recs))
	}
	if string(recs[0].Record.RData) != string([]byte{10, 0, 0, 1}) {
		t.Fatalf("RData mismatch")
	}
}

// signedZone builds a server over a small zone signed with a fresh keypair.
func signedZone(t *testing.T) (*Server, [32]byte, [32]byte) {
	t.Helper()
	pub, priv, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("keypair: %v", err)
	}
	srv := NewServer(&Zone{
		Records: map[string][]ResourceRecord{
			"a.localweb": {
				{Record: DNSRecord{Name: "a.localweb", Type: TypeA, Class: classIN, TTL: 300}, Data: []byte{10, 0, 0, 1}},
			},
			"b.localweb": {
				{Record: DNSRecord{Name: "b.localweb", Type: TypeA, Class: classIN, TTL: 300}, Data: []byte{10, 0, 0, 2}},
			},
		},
	}, nil)
	if err := srv.SignZone(pub, priv); err != nil {
		t.Fatalf("sign zone: %v", err)
	}
	return srv, pub, priv
}

// The canonical pre-image used to walk a Go map directly, so its byte order
// changed between calls and no signature could ever verify reliably.
func TestZoneCanonicalIsDeterministic(t *testing.T) {
	srv, _, _ := signedZone(t)
	first := srv.zoneCanonical()
	for i := 0; i < 50; i++ {
		if !bytes.Equal(first, srv.zoneCanonical()) {
			t.Fatalf("zone canonical form changed on call %d", i)
		}
	}
}

func TestZoneCanonicalCoversEveryRecordField(t *testing.T) {
	srv, _, _ := signedZone(t)
	base := srv.zoneCanonical()

	cases := map[string]func(z *Zone){
		"ttl":   func(z *Zone) { z.Records["a.localweb"][0].Record.TTL = 301 },
		"class": func(z *Zone) { z.Records["a.localweb"][0].Record.Class = 3 },
		"type":  func(z *Zone) { z.Records["a.localweb"][0].Record.Type = TypeAAAA },
		"rdata": func(z *Zone) { z.Records["a.localweb"][0].Data = []byte{10, 0, 0, 9} },
		"name":  func(z *Zone) { z.Records["c.localweb"] = z.Records["a.localweb"] },
		"drop":  func(z *Zone) { delete(z.Records, "b.localweb") },
	}
	for name, mutate := range cases {
		// Rebuild so each case starts from the same signed state.
		srv2, _, _ := signedZone(t)
		mutate(srv2.zone)
		if bytes.Equal(base, srv2.zoneCanonical()) {
			t.Errorf("canonical form ignores a change to %s", name)
		}
	}
}

func TestSignedZoneResolves(t *testing.T) {
	srv, _, _ := signedZone(t)
	rec, err := srv.resolve(DNSQuestion{Name: "a.localweb", Type: TypeA, Class: classIN})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !bytes.Equal(rec.RData, []byte{10, 0, 0, 1}) {
		t.Fatalf("unexpected rdata %v", rec.RData)
	}
}

// A forged record must be rejected: the zone signature covers every record, so
// adding one without re-signing invalidates the whole zone.
func TestForgedRecordIsRejected(t *testing.T) {
	srv, _, _ := signedZone(t)
	srv.zone.Records["evil.localweb"] = []ResourceRecord{
		{Record: DNSRecord{Name: "evil.localweb", Type: TypeA, Class: classIN, TTL: 300}, Data: []byte{192, 168, 0, 1}},
	}
	if _, err := srv.resolve(DNSQuestion{Name: "evil.localweb", Type: TypeA, Class: classIN}); err == nil {
		t.Fatal("expected a forged record to be rejected")
	}
}

func TestTamperedZoneIsRejected(t *testing.T) {
	srv, _, _ := signedZone(t)
	// Redirect an existing record; the signature no longer matches.
	srv.zone.Records["a.localweb"][0].Data = []byte{10, 0, 0, 99}
	if _, err := srv.resolve(DNSQuestion{Name: "a.localweb", Type: TypeA, Class: classIN}); err == nil {
		t.Fatal("expected a tampered zone to be rejected")
	}
}

// A signature made with a different key must not validate.
func TestWrongSignerKeyIsRejected(t *testing.T) {
	srv, _, _ := signedZone(t)
	otherPub, _, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("keypair: %v", err)
	}
	srv.zone.Signer = otherPub
	if _, err := srv.resolve(DNSQuestion{Name: "a.localweb", Type: TypeA, Class: classIN}); err == nil {
		t.Fatal("expected a zone signed by another key to be rejected")
	}
}

func TestUnsignedZoneWithSignerSetIsRejected(t *testing.T) {
	srv, pub, _ := signedZone(t)
	srv.zone.Sig = [64]byte{}
	if _, err := srv.resolve(DNSQuestion{Name: "a.localweb", Type: TypeA, Class: classIN}); err == nil {
		t.Fatal("expected a zone with a signer but no signature to be rejected")
	}
	_ = pub
}

// An unsigned zone has no signer and must still resolve: signing is opt-in.
func TestUnsignedZoneResolves(t *testing.T) {
	srv := NewServer(&Zone{
		Records: map[string][]ResourceRecord{
			"a.localweb": {
				{Record: DNSRecord{Name: "a.localweb", Type: TypeA, Class: classIN, TTL: 300}, Data: []byte{10, 0, 0, 1}},
			},
		},
	}, nil)
	if _, err := srv.resolve(DNSQuestion{Name: "a.localweb", Type: TypeA, Class: classIN}); err != nil {
		t.Fatalf("unsigned zone should resolve: %v", err)
	}
}

func TestReSigningClearsCache(t *testing.T) {
	srv, pub, priv := signedZone(t)
	// The packet handler is what fills the cache, so seed it directly: what
	// matters here is that re-signing drops answers resolved under the old
	// signature.
	srv.mu.Lock()
	srv.cache["a.localweb"] = cacheEntry{record: DNSRecord{Name: "a.localweb", Type: TypeA}, expires: time.Now().Add(time.Minute)}
	srv.mu.Unlock()

	if err := srv.SignZone(pub, priv); err != nil {
		t.Fatalf("re-sign: %v", err)
	}
	srv.mu.RLock()
	cached := len(srv.cache)
	srv.mu.RUnlock()
	if cached != 0 {
		t.Fatalf("cache still holds %d entries after re-signing", cached)
	}
}
