package docs

import (
	"sync"
	"testing"
	"time"

	"github.com/mrityunjay/LocalWEB/pkg/discovery"
	"github.com/mrityunjay/LocalWEB/pkg/transport"
)

func TestPresenceCreate(t *testing.T) {
	pres := NewPresenceService(PresenceConfig{DocID: "doc-1"})
	if pres.DocID() != "doc-1" {
		t.Fatalf("expected docID 'doc-1', got %q", pres.DocID())
	}
	if pres.Count() != 0 {
		t.Fatalf("expected 0 peers, got %d", pres.Count())
	}
}

func TestPresenceUpdateCursor(t *testing.T) {
	pres := NewPresenceService(PresenceConfig{DocID: "doc-1"})
	id := strID(t, "peer-1")
	pres.UpdateCursor(id, "Alice", 5, 10)
	pp, ok := pres.GetPeer(id)
	if !ok {
		t.Fatal("expected peer to be present")
	}
	if pp.Cursor.Line != 5 || pp.Cursor.Column != 10 {
		t.Fatalf("expected cursor (5,10), got (%d,%d)", pp.Cursor.Line, pp.Cursor.Column)
	}
	if pp.Cursor.PeerName != "Alice" {
		t.Fatalf("expected peer name 'Alice', got %q", pp.Cursor.PeerName)
	}
	if !pp.Connected {
		t.Fatal("expected peer to be connected")
	}
}

func TestPresenceUpdateSelection(t *testing.T) {
	pres := NewPresenceService(PresenceConfig{DocID: "doc-1"})
	id := strID(t, "peer-1")
	pres.UpdateSelection(id, "Bob", 1, 0, 3, 5)
	pp, ok := pres.GetPeer(id)
	if !ok {
		t.Fatal("expected peer to be present")
	}
	if pp.Selection == nil {
		t.Fatal("expected selection to be set")
	}
	if pp.Selection.StartLine != 1 || pp.Selection.StartCol != 0 {
		t.Fatalf("expected selection (1,0), got (%d,%d)", pp.Selection.StartLine, pp.Selection.StartCol)
	}
	if pp.Selection.EndLine != 3 || pp.Selection.EndCol != 5 {
		t.Fatalf("expected selection end (3,5), got (%d,%d)", pp.Selection.EndLine, pp.Selection.EndCol)
	}
}

func TestPresenceRemovePeer(t *testing.T) {
	pres := NewPresenceService(PresenceConfig{DocID: "doc-1"})
	id := strID(t, "peer-1")
	pres.UpdateCursor(id, "Alice", 0, 0)
	if pres.Count() != 1 {
		t.Fatalf("expected 1 peer, got %d", pres.Count())
	}
	pres.RemovePeer(id)
	if pres.Count() != 0 {
		t.Fatalf("expected 0 peers after removal, got %d", pres.Count())
	}
	_, ok := pres.GetPeer(id)
	if ok {
		t.Fatal("expected peer to be absent after removal")
	}
}

func TestPresenceMultiplePeers(t *testing.T) {
	pres := NewPresenceService(PresenceConfig{DocID: "doc-1"})
	pres.UpdateCursor(strID(t, "peer-1"), "Alice", 0, 0)
	pres.UpdateCursor(strID(t, "peer-2"), "Bob", 3, 5)
	pres.UpdateCursor(strID(t, "peer-3"), "Charlie", 1, 2)

	all := pres.GetAll()
	if len(all) != 3 {
		t.Fatalf("expected 3 peers, got %d", len(all))
	}

	names := make(map[string]bool)
	for _, p := range all {
		names[p.PeerName] = true
	}
	if !names["Alice"] || !names["Bob"] || !names["Charlie"] {
		t.Fatalf("missing expected peers: %v", names)
	}
}

func TestPresenceTimeout(t *testing.T) {
	pres := NewPresenceService(PresenceConfig{DocID: "doc-1", Timeout: 100 * time.Millisecond})
	id := strID(t, "peer-1")
	pres.UpdateCursor(id, "Alice", 0, 0)
	if pres.Count() != 1 {
		t.Fatalf("expected 1 peer, got %d", pres.Count())
	}
	time.Sleep(150 * time.Millisecond)
	all := pres.GetAll()
	if len(all) != 0 {
		t.Fatalf("expected 0 peers after timeout, got %d", len(all))
	}
}

func TestPresenceTouch(t *testing.T) {
	pres := NewPresenceService(PresenceConfig{DocID: "doc-1", Timeout: 200 * time.Millisecond})
	id := strID(t, "peer-1")
	pres.UpdateCursor(id, "Alice", 0, 0)
	time.Sleep(100 * time.Millisecond)
	pres.Touch(id)
	time.Sleep(150 * time.Millisecond)
	if pres.Count() != 1 {
		t.Fatalf("expected peer still present after touch, got count %d", pres.Count())
	}
}

func TestPresenceMarkConnectedDisconnected(t *testing.T) {
	pres := NewPresenceService(PresenceConfig{DocID: "doc-1"})
	id := strID(t, "peer-1")
	pres.MarkConnected(id, "Alice")
	pp, ok := pres.GetPeer(id)
	if !ok || !pp.Connected {
		t.Fatal("expected connected peer")
	}
	pres.MarkDisconnected(id)
	pp, ok = pres.GetPeer(id)
	if !ok || pp.Connected {
		t.Fatal("expected disconnected peer")
	}
}

func TestPresenceFromDiscovery(t *testing.T) {
	pres := NewPresenceService(PresenceConfig{DocID: "doc-1"})
	peers := []discovery.PeerInfo{
		{ID: strID(t, "peer-a"), Name: "Alice", Addrs: []string{"192.168.1.1:1234"}, LastSeen: time.Now()},
		{ID: strID(t, "peer-b"), Name: "Bob", Addrs: []string{"192.168.1.2:1234"}, LastSeen: time.Now()},
	}
	pres.FromDiscovery(peers)
	all := pres.GetAll()
	if len(all) != 2 {
		t.Fatalf("expected 2 peers, got %d", len(all))
	}
}

func TestPresenceMarshalRoundTrip(t *testing.T) {
	pres := NewPresenceService(PresenceConfig{DocID: "doc-1"})
	pres.UpdateCursor(strID(t, "peer-1"), "Alice", 5, 10)
	pres.UpdateCursor(strID(t, "peer-2"), "Bob", 3, 2)

	data := pres.Marshal()
	pres2 := NewPresenceService(PresenceConfig{DocID: "doc-1"})
	pres2.Unmarshal(data)
	all := pres2.GetAll()
	if len(all) != 2 {
		t.Fatalf("expected 2 peers after unmarshal, got %d", len(all))
	}
}

func TestPresenceConcurrentUpdates(t *testing.T) {
	pres := NewPresenceService(PresenceConfig{DocID: "doc-1"})
	var wg sync.WaitGroup
	wg.Add(20)
	for i := 0; i < 20; i++ {
		go func(idx int) {
			defer wg.Done()
			id := strID(t, "peer-"+string(rune('A'+idx%26)))
			pres.UpdateCursor(id, "User", idx, idx*2)
		}(i)
	}
	wg.Wait()
	_ = pres.GetAll()
}

func TestPresencePeerInfoFromTransport(t *testing.T) {
	id := strID(t, "transport-peer")
	p := transport.PeerInfo{ID: id, Addr: "127.0.0.1:9999", State: transport.StateReady, LastPong: time.Now()}
	pp := PeerInfoFromTransport(p, "doc-1", "TransportPeer")
	if pp.PeerID != id {
		t.Fatal("peer ID mismatch")
	}
	if !pp.Connected {
		t.Fatal("expected connected peer")
	}
	if pp.PeerName != "TransportPeer" {
		t.Fatalf("expected peer name 'TransportPeer', got %q", pp.PeerName)
	}
}

func TestPresenceGetPeerNotExists(t *testing.T) {
	pres := NewPresenceService(PresenceConfig{DocID: "doc-1"})
	_, ok := pres.GetPeer(strID(t, "nonexistent"))
	if ok {
		t.Fatal("expected peer to not exist")
	}
}
