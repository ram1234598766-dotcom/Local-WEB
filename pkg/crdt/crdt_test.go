package crdt

import (
	"testing"
)

func TestORSetAddRemoveContains(t *testing.T) {
	s := NewORSet()
	s.Add("a")
	s.Add("b")
	if !s.Contains("a") {
		t.Fatal("expected 'a' in set")
	}
	s.Remove("a")
	if s.Contains("a") {
		t.Fatal("expected 'a' removed")
	}
	if !s.Contains("b") {
		t.Fatal("expected 'b' still in set")
	}
}

func TestORSetMerge(t *testing.T) {
	a := NewORSet()
	b := NewORSet()
	a.Add("x")
	b.Add("y")
	a.Merge(b)
	if !a.Contains("x") || !a.Contains("y") {
		t.Fatal("merge failed")
	}
}

func TestORSetMarshalRoundTrip(t *testing.T) {
	s := NewORSet()
	s.Add("hello")
	s.Add("world")
	data := s.Marshal()
	s2 := NewORSet()
	if err := s2.Unmarshal(data); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(s2.Items()) != 2 {
		t.Fatalf("expected 2 items, got %d", len(s2.Items()))
	}
}

func TestRGAInsertDelete(t *testing.T) {
	rga := NewRGA("node1")
	rga.Insert("head", "a")
	rga.Insert("a", "b")
	if rga.Length() != 2 {
		t.Fatalf("expected length 2, got %d", rga.Length())
	}
	val, err := rga.Get(0)
	if err != nil || val != "a" {
		t.Fatalf("unexpected get(0): %q, %v", val, err)
	}
	rga.Delete("a")
	if rga.Length() != 2 {
		t.Fatalf("expected length unchanged after delete, got %d", rga.Length())
	}
}

func TestRGAMarshalRoundTrip(t *testing.T) {
	rga := NewRGA("node1")
	rga.Insert("head", "x")
	data := rga.Marshal()
	rga2 := NewRGA("node2")
	if err := rga2.Unmarshal(data); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if rga2.Length() != 1 {
		t.Fatalf("expected length 1, got %d", rga2.Length())
	}
}

func TestLWWRegisterSetMerge(t *testing.T) {
	a := NewLWWRegister("node1")
	b := NewLWWRegister("node2")
	a.Set([]byte("first"))
	b.Set([]byte("second"))
	a.Merge(b)
	got, _, _ := a.Get()
	if string(got) != "second" {
		t.Fatalf("expected 'second', got %q", got)
	}
}

func TestLWWRegisterMarshalRoundTrip(t *testing.T) {
	r := NewLWWRegister("node1")
	r.Set([]byte("data"))
	data := r.Marshal()
	r2 := NewLWWRegister("node2")
	if err := r2.Unmarshal(data); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	got, _, _ := r2.Get()
	if string(got) != "data" {
		t.Fatalf("expected 'data', got %q", got)
	}
}

func TestMerkleTree(t *testing.T) {
	a := NewMerkleTree([]string{"a", "b", "c"})
	b := NewMerkleTree([]string{"a", "b", "d"})
	onlyA, onlyB := DiffMerkle(a, b)
	if len(onlyA) != 1 || len(onlyB) != 1 {
		t.Fatalf("expected 1 diff each, got %d, %d", len(onlyA), len(onlyB))
	}
}
