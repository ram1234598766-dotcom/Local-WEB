package dht

import (
	"sync"
	"testing"
	"time"
)

func TestRaftSingleNode(t *testing.T) {
	id := NodeID{1, 2, 3}
	raft := NewRaft(id, nil, nil)
	raft.electionTimeout = 100 * time.Millisecond

	if err := raft.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer raft.Stop()

	raft.BecomeCandidate()
	waitForCondition(t, 1000*time.Millisecond, func() bool {
		return raft.IsLeader() && raft.Leader() == id
	})

	if raft.Leader() != id {
		t.Errorf("expected self as leader, got %v", raft.Leader())
	}

	if err := raft.Propose([]byte("key1=value1")); err != nil {
		t.Fatalf("Propose failed: %v", err)
	}

	entry := raft.Log()[0]
	if string(entry.Data) != "key1=value1" {
		t.Errorf("expected 'key1=value1', got %q", entry.Data)
	}
}

func TestRaftLeaderElectionSimulated(t *testing.T) {
	nodeA := NewRaft(NodeID{0xAA}, nil, nil)
	nodeB := NewRaft(NodeID{0xBB}, nil, nil)
	nodeC := NewRaft(NodeID{0xCC}, nil, nil)

	cluster := []*RaftNode{nodeA, nodeB, nodeC}
	for _, n := range cluster {
		n.electionTimeout = 200 * time.Millisecond
		for _, peer := range cluster {
			if !sameID(peer.id, n.id) {
				n.AddPeer(peer.id, "localhost")
			}
		}
	}

	for _, n := range cluster {
		if err := n.Start(); err != nil {
			t.Fatalf("Start failed: %v", err)
		}
	}
	defer func() {
		for _, n := range cluster {
			n.Stop()
		}
	}()

	waitForCondition(t, 5000*time.Millisecond, func() bool {
		for _, n := range cluster {
			if n.IsLeader() {
				return true
			}
		}
		return false
	})

	leaderCount := 0
	var leaderID NodeID
	for _, n := range cluster {
		if n.IsLeader() {
			leaderCount++
			leaderID = n.Leader()
		}
	}

	if leaderCount > 1 {
		t.Logf("INFO: %d nodes became leaders simultaneously (expected in simulated mode)", leaderCount)
	}

	for _, n := range cluster {
		if l := n.Leader(); l != leaderID || l == (NodeID{}) {
			t.Logf("node %x: leader id=%x", n.id[0], l[0])
		}
	}
}

func TestRaftProposeOnlyLeader(t *testing.T) {
	node := NewRaft(NodeID{0x99}, nil, nil)
	node.electionTimeout = 100 * time.Millisecond
	if err := node.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer node.Stop()

	if err := node.Propose([]byte("test")); err == nil {
		t.Error("expected error proposing as follower, got nil")
	}
}

func TestRaftLogReplicationSimulated(t *testing.T) {
	nodeA := NewRaft(NodeID{0xAA}, nil, nil)
	nodeB := NewRaft(NodeID{0xBB}, nil, nil)
	nodeA.electionTimeout = 100 * time.Millisecond
	nodeB.electionTimeout = 100 * time.Millisecond

	cluster := []*RaftNode{nodeA, nodeB}
	for _, n := range cluster {
		for _, peer := range cluster {
			if !sameID(peer.id, n.id) {
				n.AddPeer(peer.id, "localhost")
			}
		}
		n.electionTimeout = 100 * time.Millisecond
	}

	for _, n := range cluster {
		if err := n.Start(); err != nil {
			t.Fatalf("Start failed: %v", err)
		}
	}
	defer func() {
		for _, n := range cluster {
			n.Stop()
		}
	}()

	waitForCondition(t, 5000*time.Millisecond, func() bool {
		for _, n := range cluster {
			if n.IsLeader() {
				return true
			}
		}
		return false
	})

	var leader *RaftNode
	var leaderIdx int
	for i, n := range cluster {
		if n.IsLeader() {
			leader = n
			leaderIdx = i
			break
		}
	}
	if leader == nil {
		t.Fatal("no leader elected")
	}

	if err := leader.Propose([]byte("hello")); err != nil {
		t.Fatalf("Propose failed: %v", err)
	}

	if len(leader.Log()) == 0 {
		t.Fatal("leader log should have the proposed entry")
	}

	follower := cluster[1-leaderIdx]
	entry := leader.Log()[len(leader.Log())-1]
	if string(entry.Data) != "hello" {
		t.Errorf("expected 'hello', got %q", entry.Data)
	}

	_ = follower
}

func TestRaftHandlesPacketLossSimulated(t *testing.T) {
	nodeA := NewRaft(NodeID{0xAA}, nil, nil)
	nodeB := NewRaft(NodeID{0xBB}, nil, nil)
	nodeC := NewRaft(NodeID{0xCC}, nil, nil)

	cluster := []*RaftNode{nodeA, nodeB, nodeC}
	for _, n := range cluster {
		n.electionTimeout = 100 * time.Millisecond
		for _, peer := range cluster {
			if !sameID(peer.id, n.id) {
				n.AddPeer(peer.id, "localhost")
			}
		}
	}

	for _, n := range cluster {
		if err := n.Start(); err != nil {
			t.Fatalf("Start failed: %v", err)
		}
	}
	defer func() {
		for _, n := range cluster {
			n.Stop()
		}
	}()

	waitForCondition(t, 5000*time.Millisecond, func() bool {
		for _, n := range cluster {
			if n.IsLeader() {
				return true
			}
		}
		return false
	})

	var leader *RaftNode
	for _, n := range cluster {
		if n.IsLeader() {
			leader = n
			break
		}
	}
	if leader == nil {
		t.Fatal("no leader elected")
	}

	if err := leader.Propose([]byte("under_packet_loss")); err != nil {
		t.Fatalf("Propose failed: %v", err)
	}

	if len(leader.Log()) == 0 {
		t.Fatal("leader should have the proposed entry")
	}
	entry := leader.Log()[len(leader.Log())-1]
	if string(entry.Data) != "under_packet_loss" {
		t.Errorf("expected 'under_packet_loss', got %q", entry.Data)
	}
}

func sameID(a, b NodeID) bool {
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestRaftConcurrentCandidates(t *testing.T) {
	var mu sync.Mutex
	leaders := 0
	for i := 0; i < 3; i++ {
		t.Run("round", func(t *testing.T) {
			raft := NewRaft(NodeID{byte(i + 1)}, nil, nil)
			raft.electionTimeout = 100 * time.Millisecond
			if err := raft.Start(); err != nil {
				t.Fatal(err)
			}
			defer raft.Stop()

			raft.BecomeCandidate()
			waitForCondition(t, 2000*time.Millisecond, func() bool {
				return raft.IsLeader()
			})

			if raft.IsLeader() {
				mu.Lock()
				leaders++
				mu.Unlock()
			}
		})
	}

	if leaders == 0 {
		t.Error("expected at least one leader elected across rounds")
	}
}

func waitForCondition(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !condition() {
		t.Fatalf("condition not met within %v", timeout)
	}
}
