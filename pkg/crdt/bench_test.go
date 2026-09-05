package crdt

import (
	"testing"
)

func BenchmarkORSetAdd(b *testing.B) {
	s := NewORSet()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Add("key")
	}
}

func BenchmarkORSetRemove(b *testing.B) {
	s := NewORSet()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Add("key")
		s.Remove("key")
	}
}

func BenchmarkORSetMerge(b *testing.B) {
	a := NewORSet()
	c := NewORSet()
	for i := 0; i < 50; i++ {
		a.Add("keyA")
		c.Add("keyB")
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		a.Merge(c)
	}
}

func BenchmarkORSetMarshal(b *testing.B) {
	s := NewORSet()
	for i := 0; i < 100; i++ {
		s.Add("key")
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = s.Marshal()
	}
}

func BenchmarkRGAInsert(b *testing.B) {
	list := NewRGA("node1")
	list.Insert("", "initial")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		list.Insert("initial", "value")
	}
}

func BenchmarkRGAMerge(b *testing.B) {
	a := NewRGA("nodeA")
	a.Insert("", "initial")
	c := NewRGA("nodeB")
	c.Insert("", "initial")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		a.Merge(c)
	}
}

func BenchmarkLWWRegisterSet(b *testing.B) {
	r := NewLWWRegister("node1")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Set([]byte("value"))
	}
}

func BenchmarkLWWRegisterMerge(b *testing.B) {
	a := NewLWWRegister("nodeA")
	a.Set([]byte("value1"))
	c := NewLWWRegister("nodeB")
	c.Set([]byte("value2"))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		a.Merge(c)
	}
}
