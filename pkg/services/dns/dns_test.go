package dns

import (
	"testing"
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
