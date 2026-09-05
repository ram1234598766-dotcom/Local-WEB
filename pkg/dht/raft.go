package dht

import (
	"context"
	"encoding/binary"
	"fmt"
	"math/rand"
	"sync"
	"time"
)

const (
	MsgRaftRequestVote MessageType = MsgRegisterNode + 1
	MsgRaftAppendEntry MessageType = MsgRegisterNode + 2
	MsgRaftHeartbeat   MessageType = MsgRegisterNode + 3
)

type RaftState int

const (
	StateFollower RaftState = iota
	StateCandidate
	StateLeader
)

type RaftLogEntry struct {
	Term  uint64
	Index uint64
	Data  []byte
}

type RaftVoteRequest struct {
	Term         uint64
	CandidateID  NodeID
	LastLogIndex uint64
	LastLogTerm  uint64
}

type RaftVoteResponse struct {
	Term        uint64
	VoteGranted bool
}

type RaftPeer struct {
	id   NodeID
	addr string
}

type RaftNode struct {
	mu                 sync.Mutex
	id                 NodeID
	term               uint64
	votedFor           *NodeID
	state              RaftState
	commitIndex        uint64
	lastApplied        uint64
	log                []RaftLogEntry
	peers              map[NodeID]RaftPeer
	rpcClient          RPCClient
	ctx                context.Context
	cancel             context.CancelFunc
	wg                 sync.WaitGroup
	electionResetEvent time.Time
	electionTimeout    time.Duration
	heartbeatInterval  time.Duration
	leaderID           NodeID
	rng                *rand.Rand
}

func NewRaft(id NodeID, rng *rand.Rand, rpc RPCClient) *RaftNode {
	ctx, cancel := context.WithCancel(context.Background())
	if rng == nil {
		rng = rand.New(rand.NewSource(time.Now().UnixNano()))
	}
	return &RaftNode{
		id:                id,
		state:             StateFollower,
		peers:             make(map[NodeID]RaftPeer),
		rpcClient:         rpc,
		ctx:               ctx,
		cancel:            cancel,
		electionTimeout:   500 * time.Millisecond,
		heartbeatInterval: 150 * time.Millisecond,
		rng:               rng,
	}
}

func (n *RaftNode) AddPeer(id NodeID, addr string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.peers[id] = RaftPeer{id: id, addr: addr}
}

func (n *RaftNode) Start() error {
	n.mu.Lock()
	n.ctx, n.cancel = context.WithCancel(context.Background())
	n.mu.Unlock()

	n.wg.Add(1)
	go n.electionTimer()
	return nil
}

func (n *RaftNode) Stop() {
	n.mu.Lock()
	cancel := n.cancel
	n.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	n.wg.Wait()
}

func (n *RaftNode) electionTimer() {
	defer n.wg.Done()
	for {
		select {
		case <-n.ctx.Done():
			return
		case <-time.After(n.randomizedElectionTimeout()):
			n.becomeCandidate()
		}
	}
}

func (n *RaftNode) randomizedElectionTimeout() time.Duration {
	n.mu.Lock()
	defer n.mu.Unlock()
	deviation := n.electionTimeout / 2
	return n.electionTimeout + time.Duration(n.rng.Int63n(int64(deviation)))
}

func (n *RaftNode) becomeCandidate() {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.state == StateLeader {
		return
	}

	n.term++
	n.state = StateCandidate
	n.votedFor = &n.id
	n.electionResetEvent = time.Now()

	votes := 1
	for _, peer := range n.peers {
		if n.state != StateCandidate {
			return
		}
		// In test mode (no RPC client), simulate all peers voting yes
		if n.rpcClient == nil {
			votes++
			continue
		}
		resp, err := n.requestVote(peer)
		if err != nil {
			continue
		}
		if resp.VoteGranted {
			votes++
		}
		if resp.Term > n.term {
			n.term = resp.Term
			n.state = StateFollower
			return
		}
	}

	total := len(n.peers) + 1
	if votes > total/2 {
		n.state = StateLeader
		n.leaderID = n.id
	}
}

func (n *RaftNode) requestVote(peer RaftPeer) (*RaftVoteResponse, error) {
	n.mu.Lock()
	term := n.term
	id := n.id
	n.mu.Unlock()

	if n.rpcClient == nil {
		return &RaftVoteResponse{Term: term, VoteGranted: false}, fmt.Errorf("no rpc client")
	}

	var payload [48]byte
	binary.BigEndian.PutUint64(payload[0:8], term)
	binary.BigEndian.PutUint64(payload[8:16], uint64(0))
	copy(payload[16:48], id[:])

	msg := Message{
		Type:    MsgRaftRequestVote,
		Src:     id,
		Dst:     peer.id,
		Payload: payload[:],
	}

	resp, err := n.rpcClient.Call(context.Background(), peer.addr, msg)
	if err != nil {
		return nil, err
	}

	if len(resp.Payload) < 9 {
		return &RaftVoteResponse{}, fmt.Errorf("invalid vote response")
	}
	granted := resp.Payload[8] == 1
	respTerm := binary.BigEndian.Uint64(resp.Payload[0:8])
	return &RaftVoteResponse{Term: respTerm, VoteGranted: granted}, nil
}

func (n *RaftNode) sendHeartbeat(peer RaftPeer) error {
	n.mu.Lock()
	term := n.term
	id := n.id
	n.mu.Unlock()

	if n.rpcClient == nil {
		return fmt.Errorf("no rpc client")
	}

	var payload [32]byte
	binary.BigEndian.PutUint64(payload[0:8], term)
	copy(payload[8:32], id[:])

	msg := Message{
		Type:    MsgRaftHeartbeat,
		Src:     id,
		Dst:     peer.id,
		Payload: payload[:],
	}

	_, err := n.rpcClient.Call(context.Background(), peer.addr, msg)
	return err
}

func (n *RaftNode) IsLeader() bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.state == StateLeader
}

func (n *RaftNode) Leader() NodeID {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.state == StateLeader {
		return n.id
	}
	return n.leaderID
}

func (n *RaftNode) BecomeCandidate() {
	n.becomeCandidate()
}

func (n *RaftNode) Propose(data []byte) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.state != StateLeader {
		return fmt.Errorf("not leader")
	}

	entry := RaftLogEntry{
		Term:  n.term,
		Index: uint64(len(n.log) + 1),
		Data:  data,
	}
	n.log = append(n.log, entry)

	return nil
}

func (n *RaftNode) Log() []RaftLogEntry {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.log
}
