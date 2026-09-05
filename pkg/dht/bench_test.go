package dht

import (
	"testing"
)

func BenchmarkNodeIDXor(b *testing.B) {
	a := NodeID{0xAA, 0xBB, 0xCC}
	c := NodeID{0x11, 0x22, 0x33}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = a.Xor(c)
	}
}

func BenchmarkRoutingTableAdd(b *testing.B) {
	rt := NewRoutingTable(NodeID{0x42})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rt.Add(&Peer{
			Info: PeerInfo{
				ID:   NodeID{byte(i % 256)},
				Name: "test",
			},
		})
	}
}

func BenchmarkRoutingTableFindClosest(b *testing.B) {
	rt := NewRoutingTable(NodeID{0x42})
	for i := 0; i < 100; i++ {
		rt.Add(&Peer{
			Info: PeerInfo{
				ID:   NodeID{byte(i)},
				Name: "test",
			},
		})
	}
	target := NodeID{0xFF}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = rt.FindClosest(target, 20)
	}
}

func BenchmarkRaftPropose(b *testing.B) {
	node := NewRaft(NodeID{0x01}, nil, nil)
	node.mu.Lock()
	node.state = StateLeader
	node.term = 1
	node.mu.Unlock()
	defer node.Stop()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = node.Propose([]byte("benchmark-key=value"))
	}
}

func BenchmarkRaftLeaderElection(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		node := NewRaft(NodeID{byte(i % 256)}, nil, nil)
		node.Start()
		b.StartTimer()

		node.BecomeCandidate()

		b.StopTimer()
		node.Stop()
		b.StartTimer()
	}
}
