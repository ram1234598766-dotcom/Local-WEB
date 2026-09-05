//go:build integration

package integration

import (
	"context"
	"net"
	"sync"
	"time"

	"github.com/ipfs/go-cid"
	mh "github.com/multiformats/go-multihash"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/discovery"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/link"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/services/voice"
)

// ---------------------------------------------------------------------------
// tcpTransport: QUICTransport backed by real TCP connections
// ---------------------------------------------------------------------------

type tcpTransport struct {
	listener net.Listener
}

func (t *tcpTransport) Dial(ctx context.Context, addr string) (net.Conn, error) {
	d := net.Dialer{Timeout: 2 * time.Second}
	return d.DialContext(ctx, "tcp", addr)
}

func (t *tcpTransport) Listen(addr string) (net.Listener, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	t.listener = ln
	return ln, nil
}

// ---------------------------------------------------------------------------
// fakeDiscoveryMode: a controllable DiscoveryMode for testing
// ---------------------------------------------------------------------------

type fakeDiscoveryMode struct {
	name         string
	requiresWiFi bool
	events       chan discovery.PeerEvent
	mu           sync.Mutex
	started      bool
}

func newFakeDiscoveryMode(name string, requiresWiFi bool) *fakeDiscoveryMode {
	return &fakeDiscoveryMode{
		name:         name,
		requiresWiFi: requiresWiFi,
		events:       make(chan discovery.PeerEvent, 16),
	}
}

func (m *fakeDiscoveryMode) Name() string { return m.name }

func (m *fakeDiscoveryMode) RequiresWiFi() bool { return m.requiresWiFi }

func (m *fakeDiscoveryMode) Advertise(info discovery.PeerInfo) error {
	return nil
}

func (m *fakeDiscoveryMode) Start(ctx context.Context, nodeID [32]byte, name string) (<-chan discovery.PeerEvent, error) {
	m.mu.Lock()
	m.started = true
	m.mu.Unlock()
	return m.events, nil
}

func (m *fakeDiscoveryMode) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.started = false
	return nil
}

// ---------------------------------------------------------------------------
// Voice test helpers
// ---------------------------------------------------------------------------

func makePeerID(i int) voice.PeerID {
	return voice.PeerID{byte(i)}
}

func makeCallConfig(caller, callee voice.PeerID, channel string) voice.CallConfig {
	return voice.CallConfig{
		Caller:     caller,
		Callee:     callee,
		ChannelID:  channel,
		AudioCodec: voice.CodecOpus,
		VideoCodec: voice.CodecVP9,
	}
}

// ---------------------------------------------------------------------------
// File sync test helpers
// ---------------------------------------------------------------------------

func computeTestBlockCID(data []byte) cid.Cid {
	prefix := cid.NewPrefixV1(cid.Raw, mh.SHA2_256)
	c, _ := prefix.Sum(data)
	return c
}

// ---------------------------------------------------------------------------
// Link mode test helpers
// ---------------------------------------------------------------------------

var allLinkModes = []link.LinkMode{
	link.ModeWiFiStation,
	link.ModeWiFiDirect,
	link.ModeAdHocWiFi,
	link.ModeUSBTether,
	link.ModeBLE,
	link.ModeAcoustic,
}
