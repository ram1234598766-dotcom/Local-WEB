package voice

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

var (
	ErrCallNotFound     = errors.New("call not found")
	ErrInvalidTransition = errors.New("invalid state transition")
	ErrCallEnded        = errors.New("call ended")
)

// Call represents a single voice/video call session.
type Call struct {
	mu        sync.RWMutex
	id        CallID
	state     CallState
	caller    PeerID
	callee    PeerID
	channelID string
	config    CallConfig
	tracks    map[string]*TrackInfo
	candidates map[string]ICECandidate
	createdAt time.Time
	endedAt   time.Time
}

// NewCall creates a new call in the idle state.
func NewCall(cfg CallConfig) *Call {
	id := CallID{}
	copy(id[:], fmt.Sprintf("%x-%x", cfg.Caller[:4], cfg.Callee[:4]))
	return &Call{
		id:        id,
		state:     CallStateIdle,
		caller:    cfg.Caller,
		callee:    cfg.Callee,
		channelID: cfg.ChannelID,
		config:    cfg,
		tracks:    make(map[string]*TrackInfo),
		candidates: make(map[string]ICECandidate),
		createdAt: time.Now(),
	}
}

// ID returns the call identifier.
func (c *Call) ID() CallID {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.id
}

// State returns the current call state.
func (c *Call) State() CallState {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.state
}

// Peers returns the caller and callee.
func (c *Call) Peers() (PeerID, PeerID) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.caller, c.callee
}

// ChannelID returns the signaling channel.
func (c *Call) ChannelID() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.channelID
}

// Tracks returns a snapshot of the call's tracks.
func (c *Call) Tracks() []TrackInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]TrackInfo, 0, len(c.tracks))
	for _, t := range c.tracks {
		out = append(out, *t)
	}
	return out
}

// Candidates returns a snapshot of gathered ICE candidates.
func (c *Call) Candidates() []ICECandidate {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]ICECandidate, 0, len(c.candidates))
	for _, c := range c.candidates {
		out = append(out, c)
	}
	return out
}

// AddCandidate stores an ICE candidate.
func (c *Call) AddCandidate(cand ICECandidate) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.candidates[cand.ID] = cand
}

// AddTrack registers a media track on the call.
func (c *Call) AddTrack(t TrackInfo) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state == CallStateEnded {
		return ErrCallEnded
	}
	c.tracks[t.ID] = &t
	return nil
}

// transition moves the call to a new state if the transition is valid.
func (c *Call) transition(newState CallState) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	allowed := map[CallState][]CallState{
		CallStateIdle:     {CallStateCalling},
		CallStateCalling:  {CallStateRinging, CallStateConnected, CallStateEnded},
		CallStateRinging:  {CallStateConnected, CallStateEnded},
		CallStateConnected: {CallStateEnded},
	}

	valid, ok := allowed[c.state]
	if !ok {
		return fmt.Errorf("%w: from %s to %s", ErrInvalidTransition, c.state.String(), newState.String())
	}
	for _, s := range valid {
		if s == newState {
			c.state = newState
			log.Info().
				Str("call_id", fmt.Sprintf("%x", c.id[:8])).
				Str("state", c.state.String()).
				Msg("call state changed")
			return nil
		}
	}
	return fmt.Errorf("%w: from %s to %s", ErrInvalidTransition, c.state.String(), newState.String())
}

// Call initiates the call (idle -> calling).
func (c *Call) Call() error {
	return c.transition(CallStateCalling)
}

// Ringing moves the call to ringing (caller hears ringback).
func (c *Call) Ringing() error {
	return c.transition(CallStateRinging)
}

// Accept answers the call (ringing -> connected).
func (c *Call) Accept() error {
	return c.transition(CallStateConnected)
}

// End terminates the call from any active state.
func (c *Call) End() error {
	c.mu.Lock()
	if c.state == CallStateEnded {
		c.mu.Unlock()
		return ErrCallEnded
	}
	c.mu.Unlock()
	return c.transition(CallStateEnded)
}

// EndedAt returns when the call ended, if applicable.
func (c *Call) EndedAt() (time.Time, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.endedAt.IsZero() {
		return time.Time{}, false
	}
	return c.endedAt, true
}

// CallManager tracks active calls.
type CallManager struct {
	mu      sync.RWMutex
	calls   map[CallID]*Call
	byPeer  map[PeerID]CallID
}

// NewCallManager creates a new CallManager.
func NewCallManager() *CallManager {
	return &CallManager{
		calls:  make(map[CallID]*Call),
		byPeer: make(map[PeerID]CallID),
	}
}

// Create registers a new outgoing call.
func (m *CallManager) Create(cfg CallConfig) *Call {
	m.mu.Lock()
	defer m.mu.Unlock()

	call := NewCall(cfg)
	if err := call.Call(); err != nil {
		log.Error().Err(err).Msg("create call state transition failed")
		return nil
	}
	m.calls[call.id] = call
	m.byPeer[cfg.Caller] = call.id
	m.byPeer[cfg.Callee] = call.id
	return call
}

// Get retrieves a call by ID.
func (m *CallManager) Get(id CallID) (*Call, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	c, ok := m.calls[id]
	if !ok {
		return nil, ErrCallNotFound
	}
	return c, nil
}

// GetByPeer retrieves the active call involving a peer.
func (m *CallManager) GetByPeer(p PeerID) (*Call, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	id, ok := m.byPeer[p]
	if !ok {
		return nil, ErrCallNotFound
	}
	c, ok := m.calls[id]
	if !ok {
		return nil, ErrCallNotFound
	}
	return c, nil
}

// Ring moves an existing idle/calling call to ringing.
func (m *CallManager) Ring(id CallID) error {
	call, err := m.Get(id)
	if err != nil {
		return err
	}
	return call.Ringing()
}

// Accept answers a ringing call.
func (m *CallManager) Accept(id CallID) error {
	call, err := m.Get(id)
	if err != nil {
		return err
	}
	return call.Accept()
}

// End terminates and removes a call.
func (m *CallManager) End(id CallID) error {
	call, err := m.Get(id)
	if err != nil {
		return err
	}
	if err := call.End(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.calls, id)
	delete(m.byPeer, call.caller)
	delete(m.byPeer, call.callee)
	return nil
}

// ActiveCalls returns all non-ended calls.
func (m *CallManager) ActiveCalls() []*Call {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Call, 0, len(m.calls))
	for _, c := range m.calls {
		if s := c.State(); s != CallStateEnded {
			out = append(out, c)
		}
	}
	return out
}

// Stop removes all calls.
func (m *CallManager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = make(map[CallID]*Call)
	m.byPeer = make(map[PeerID]CallID)
}
