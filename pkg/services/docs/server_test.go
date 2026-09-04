package docs

import (
	"context"
	"sync"
	"testing"

	"github.com/mrityunjay/LocalWEB/pkg/discovery"
	"github.com/mrityunjay/LocalWEB/pkg/transport"
)

// mockNotifier captures broadcast calls for testing.
type mockNotifier struct {
	mu          sync.Mutex
	broadcasts  []BroadcastRecord
	peerCount   int
}

type BroadcastRecord struct {
	DocID  string
	Exclude [32]byte
	Msg    *DocMessage
}

func newMockNotifier() *mockNotifier {
	return &mockNotifier{broadcasts: make([]BroadcastRecord, 0, 32)}
}

func (m *mockNotifier) Broadcast(ctx context.Context, docID string, exclude [32]byte, msg *DocMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.broadcasts = append(m.broadcasts, BroadcastRecord{DocID: docID, Exclude: exclude, Msg: msg})
	return nil
}

func (m *mockNotifier) Peers() []discovery.PeerInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	return make([]discovery.PeerInfo, m.peerCount)
}

func (m *mockNotifier) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.broadcasts)
}

func TestNewService(t *testing.T) {
	svc := NewService(ServiceConfig{NodeID: "node-1"})
	_ = svc.eventCh // event loop runs in background
	if svc.NodeID() != "node-1" {
		t.Fatalf("expected nodeID 'node-1', got %q", svc.NodeID())
	}
}

func TestServiceCreateDocument(t *testing.T) {
	svc := NewService(ServiceConfig{NodeID: "node-1"})
	_ = svc.eventCh

	doc := svc.CreateDocument("doc-1", "Test Doc")
	if doc == nil {
		t.Fatal("expected document to be created")
	}
	if doc.Title() != "Test Doc" {
		t.Fatalf("expected title 'Test Doc', got %q", doc.Title())
	}

	// Create same doc again — should return existing
	doc2 := svc.CreateDocument("doc-1", "Other Title")
	if doc2 != doc {
		t.Fatal("expected same document instance")
	}
}

func TestServiceGetDocument(t *testing.T) {
	svc := NewService(ServiceConfig{NodeID: "node-1"})
	_ = svc.eventCh
	svc.CreateDocument("doc-1", "Doc 1")

	doc := svc.GetDocument("doc-1")
	if doc == nil {
		t.Fatal("expected document to be found")
	}

	missing := svc.GetDocument("nonexistent")
	if missing != nil {
		t.Fatal("expected nil for missing document")
	}
}

func TestServiceGetOrCreateDocument(t *testing.T) {
	svc := NewService(ServiceConfig{NodeID: "node-1"})
	_ = svc.eventCh

	doc := svc.GetOrCreateDocument("doc-1", "Title A")
	if doc.Title() != "Title A" {
		t.Fatalf("expected 'Title A', got %q", doc.Title())
	}

	doc2 := svc.GetOrCreateDocument("doc-1", "Title B")
	if doc2.Title() != "Title A" {
		t.Fatalf("expected existing title preserved, got %q", doc2.Title())
	}
}

func TestServiceDeleteDocument(t *testing.T) {
	svc := NewService(ServiceConfig{NodeID: "node-1"})
	_ = svc.eventCh
	svc.CreateDocument("doc-1", "Doc 1")

	err := svc.DeleteDocument("doc-1")
	if err != nil {
		t.Fatalf("delete failed: %v", err)
	}
	if svc.GetDocument("doc-1") != nil {
		t.Fatal("expected document to be deleted")
	}

	err = svc.DeleteDocument("doc-1")
	if err == nil {
		t.Fatal("expected error deleting nonexistent document")
	}
}

func TestServiceApplyLocalOp(t *testing.T) {
	svc := NewService(ServiceConfig{NodeID: "node-1"})
	_ = svc.eventCh
	svc.CreateDocument("doc-1", "Doc")

	authorID := strID(t,"author-1")
	doc, result, err := svc.ApplyLocalOp(context.Background(), "doc-1", NewInsertOp("doc-1", authorID, 0, "hello", 1))
	if err != nil {
		t.Fatalf("apply local op failed: %v", err)
	}
	if doc == nil {
		t.Fatal("expected document back")
	}
	if !result.Applied {
		t.Fatalf("expected op applied, got error: %v", result.Error)
	}

	// Apply to nonexistent doc
	_, _, err = svc.ApplyLocalOp(context.Background(), "nonexistent", NewInsertOp("nonexistent", authorID, 0, "x", 1))
	if err == nil {
		t.Fatal("expected error for nonexistent doc")
	}
}

func TestServiceInsertText(t *testing.T) {
	svc := NewService(ServiceConfig{NodeID: "node-1"})
	_ = svc.eventCh
	svc.CreateDocument("doc-1", "Doc")

	authorID := strID(t,"author")
	doc, result, err := svc.InsertText(context.Background(), "doc-1", authorID, 0, "hello")
	if err != nil {
		t.Fatalf("insert text failed: %v", err)
	}
	if !result.Applied {
		t.Fatalf("expected insert applied, got error: %v", result.Error)
	}
	if doc.Text() != "hello" {
		t.Fatalf("expected 'hello', got %q", doc.Text())
	}
}

func TestServiceDeleteLine(t *testing.T) {
	svc := NewService(ServiceConfig{NodeID: "node-1"})
	_ = svc.eventCh
	svc.CreateDocument("doc-1", "Doc")

	authorID := strID(t,"author")
	svc.InsertText(context.Background(), "doc-1", authorID, 0, "hello")
	svc.InsertText(context.Background(), "doc-1", authorID, 1, "world")

	_, result, err := svc.DeleteLine(context.Background(), "doc-1", authorID, 0)
	if err != nil {
		t.Fatalf("delete line failed: %v", err)
	}
	if !result.Applied {
		t.Fatalf("expected delete applied: %v", result.Error)
	}
	doc := svc.GetDocument("doc-1")
	if doc.Text() != "world" {
		t.Fatalf("expected 'world', got %q", doc.Text())
	}
}

func TestServiceFormatBlock(t *testing.T) {
	svc := NewService(ServiceConfig{NodeID: "node-1"})
	_ = svc.eventCh
	svc.CreateDocument("doc-1", "Doc")

	authorID := strID(t,"author")
	svc.InsertText(context.Background(), "doc-1", authorID, 0, "heading text")

	_, result, err := svc.FormatBlock(context.Background(), "doc-1", authorID, 0, BlockHeading1)
	if err != nil {
		t.Fatalf("format block failed: %v", err)
	}
	if !result.Applied {
		t.Fatalf("expected format applied: %v", result.Error)
	}
	doc := svc.GetDocument("doc-1")
	btypes := doc.BlockTypes()
	if len(btypes) != 1 || btypes[0] != BlockHeading1 {
		t.Fatalf("expected [heading1], got %v", btypes)
	}
}

func TestServiceAllDocuments(t *testing.T) {
	svc := NewService(ServiceConfig{NodeID: "node-1"})
	_ = svc.eventCh
	svc.CreateDocument("doc-a", "A")
	svc.CreateDocument("doc-b", "B")
	svc.CreateDocument("doc-c", "C")

	docs := svc.AllDocuments()
	if len(docs) != 3 {
		t.Fatalf("expected 3 documents, got %d", len(docs))
	}
}

func TestServiceStats(t *testing.T) {
	svc := NewService(ServiceConfig{NodeID: "node-1"})
	_ = svc.eventCh
	svc.CreateDocument("doc-1", "Doc")
	authorID := strID(t,"author")
	svc.InsertText(context.Background(), "doc-1", authorID, 0, "x")

	stats := svc.Stats()
	if stats.TotalDocs != 1 {
		t.Fatalf("expected 1 total doc, got %d", stats.TotalDocs)
	}
	if stats.ActiveDocs != 1 {
		t.Fatalf("expected 1 active doc, got %d", stats.ActiveDocs)
	}
	if stats.TotalOps != 1 {
		t.Fatalf("expected 1 total op, got %d", stats.TotalOps)
	}
}

func TestServiceSetNotifier(t *testing.T) {
	svc := NewService(ServiceConfig{NodeID: "node-1"})
	_ = svc.eventCh
	mock := newMockNotifier()
	svc.SetNotifier(mock)
	// Should not panic
	if svc.notifier == nil {
		t.Fatal("expected notifier to be set")
	}
}

func TestServiceUpdatePresence(t *testing.T) {
	svc := NewService(ServiceConfig{NodeID: "node-1"})
	_ = svc.eventCh
	svc.CreateDocument("doc-1", "Doc")

	peerID := strID(t,"peer-1")
	svc.UpdatePresence(context.Background(), "doc-1", "Alice", peerID, 5, 10)

	presences := svc.GetDocPresence("doc-1")
	if len(presences) != 1 {
		t.Fatalf("expected 1 presence, got %d", len(presences))
	}
	if presences[0].Cursor.Line != 5 || presences[0].Cursor.Column != 10 {
		t.Fatalf("expected cursor (5,10), got (%d,%d)", presences[0].Cursor.Line, presences[0].Cursor.Column)
	}
}

func TestServiceUpdatePresenceNoDoc(t *testing.T) {
	svc := NewService(ServiceConfig{NodeID: "node-1"})
	_ = svc.eventCh
	// Should not panic when doc doesn't exist
	svc.UpdatePresence(context.Background(), "nonexistent", "Alice", strID(t,"p1"), 0, 0)
}

func TestServiceMarshalUnmarshal(t *testing.T) {
	svc := NewService(ServiceConfig{NodeID: "node-1"})
	_ = svc.eventCh
	svc.CreateDocument("doc-1", "Title 1")
	svc.CreateDocument("doc-2", "Title 2")

	data := svc.Marshal()
	if len(data) != 2 {
		t.Fatalf("expected 2 serialized docs, got %d", len(data))
	}

	svc2 := NewService(ServiceConfig{NodeID: "node-2"})
	_ = svc2.eventCh
	if err := svc2.Unmarshal(data); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if svc2.GetDocument("doc-1") == nil {
		t.Fatal("expected doc-1 after unmarshal")
	}
	if svc2.GetDocument("doc-2") == nil {
		t.Fatal("expected doc-2 after unmarshal")
	}
	stats := svc2.Stats()
	if stats.TotalDocs != 2 {
		t.Fatalf("expected 2 docs, got %d", stats.TotalDocs)
	}
}

func TestServiceHandleOperation(t *testing.T) {
	svc := NewService(ServiceConfig{NodeID: "node-1"})
	_ = svc.eventCh
	svc.CreateDocument("doc-1", "Doc")

	peerID := strID(t,"peer-a")
	op := NewInsertOp("doc-1", peerID, 0, "remote", 42)
	payload, _ := encodeOpPayload(op, "doc-1")
	msgBytes, _ := encodeDocMessage(&DocMessage{
		Type: DocMsgOperation, DocID: "doc-1",
		AuthorID: peerID, Payload: payload, Timestamp: 42,
	})
	msg, _ := decodeDocMessage(msgBytes)

	svc.handleMessage(context.Background(), msg)
	doc := svc.GetDocument("doc-1")
	if doc == nil {
		t.Fatal("document should still exist")
	}
	_ = doc
}

func TestServiceHandleOperationUnknownDoc(t *testing.T) {
	svc := NewService(ServiceConfig{NodeID: "node-1"})
	_ = svc.eventCh

	peerID := strID(t,"peer-a")
	op := NewInsertOp("nonexistent", peerID, 0, "x", 1)
	payload, _ := encodeOpPayload(op, "nonexistent")
	msgBytes, _ := encodeDocMessage(&DocMessage{
		Type: DocMsgOperation, DocID: "nonexistent",
		AuthorID: peerID, Payload: payload, Timestamp: 1,
	})
	msg, _ := decodeDocMessage(msgBytes)

	// Should not panic
	svc.handleMessage(context.Background(), msg)
}

func TestServiceHandleSyncRequest(t *testing.T) {
	svc := NewService(ServiceConfig{NodeID: "node-1"})
	_ = svc.eventCh
	svc.CreateDocument("doc-1", "Sync Doc")

	msgBytes, _ := encodeDocMessage(&DocMessage{Type: DocMsgSyncReq, DocID: "doc-1"})
	msg, _ := decodeDocMessage(msgBytes)
	svc.handleMessage(context.Background(), msg)
	// Should not panic
}

func TestServiceHandleSyncRequestNoDoc(t *testing.T) {
	svc := NewService(ServiceConfig{NodeID: "node-1"})
	_ = svc.eventCh

	msgBytes, _ := encodeDocMessage(&DocMessage{Type: DocMsgSyncReq, DocID: "nonexistent"})
	msg, _ := decodeDocMessage(msgBytes)
	svc.handleMessage(context.Background(), msg)
	// Should not panic
}

func TestServiceHandlePresence(t *testing.T) {
	svc := NewService(ServiceConfig{NodeID: "node-1"})
	_ = svc.eventCh
	svc.CreateDocument("doc-1", "Doc")

	peerID := strID(t,"peer-1")
	payload := marshalPresenceUpdate("doc-1", peerID, "Alice", 3, 7)
	msgBytes, _ := encodeDocMessage(&DocMessage{Type: DocMsgPresence, DocID: "doc-1", Payload: payload})
	msg, _ := decodeDocMessage(msgBytes)

	svc.handleMessage(context.Background(), msg)
	presences := svc.GetDocPresence("doc-1")
	if len(presences) != 1 {
		t.Fatalf("expected 1 presence, got %d", len(presences))
	}
	if presences[0].Cursor.Line != 3 || presences[0].Cursor.Column != 7 {
		t.Fatalf("expected cursor (3,7), got (%d,%d)", presences[0].Cursor.Line, presences[0].Cursor.Column)
	}
}

func TestServiceHandleFullState(t *testing.T) {
	svc := NewService(ServiceConfig{NodeID: "node-1"})
	_ = svc.eventCh

	state := DocumentState{DocID: "new-doc", Title: "From State", Content: []string{"line1"}}
	stateBytes := marshalDocumentState(state)
	msgBytes, _ := encodeDocMessage(&DocMessage{Type: DocMsgFullState, DocID: "new-doc", Payload: stateBytes})
	msg, _ := decodeDocMessage(msgBytes)

	svc.handleMessage(context.Background(), msg)
	doc := svc.GetDocument("new-doc")
	if doc == nil {
		t.Fatal("expected document to be created from full state")
	}
}

func TestServiceNodeID(t *testing.T) {
	svc := NewService(ServiceConfig{NodeID: "my-node"})
	_ = svc.eventCh
	if svc.NodeID() != "my-node" {
		t.Fatalf("expected 'my-node', got %q", svc.NodeID())
	}
}

func TestServiceStatsInitiallyZero(t *testing.T) {
	svc := NewService(ServiceConfig{NodeID: "node-1"})
	_ = svc.eventCh
	stats := svc.Stats()
	if stats.TotalDocs != 0 || stats.ActiveDocs != 0 || stats.TotalOps != 0 {
		t.Fatalf("expected zero stats initially, got %+v", stats)
	}
}

func TestServiceBroadcastOnLocalOp(t *testing.T) {
	mock := newMockNotifier()
	svc := NewService(ServiceConfig{NodeID: "node-1", Notifier: mock})
	_ = svc.eventCh
	svc.CreateDocument("doc-1", "Doc")
	authorID := strID(t,"author")
	svc.InsertText(context.Background(), "doc-1", authorID, 0, "broadcast test")

	count := mock.count()
	if count != 1 {
		t.Fatalf("expected 1 broadcast, got %d", count)
	}
}

func TestServiceGetPresence(t *testing.T) {
	svc := NewService(ServiceConfig{NodeID: "node-1"})
	_ = svc.eventCh
	svc.CreateDocument("doc-1", "Doc")

	pres := svc.GetPresence("doc-1")
	if pres == nil {
		t.Fatal("expected presence service for doc-1")
	}
	missing := svc.GetPresence("nonexistent")
	if missing != nil {
		t.Fatal("expected nil presence for nonexistent doc")
	}
}

func TestServicePresenceBroadcast(t *testing.T) {
	mock := newMockNotifier()
	svc := NewService(ServiceConfig{NodeID: "node-1", Notifier: mock})
	_ = svc.eventCh
	svc.CreateDocument("doc-1", "Doc")

	peerID := strID(t,"peer-1")
	svc.UpdatePresence(context.Background(), "doc-1", "Alice", peerID, 2, 3)

	count := mock.count()
	if count != 1 {
		t.Fatalf("expected 1 presence broadcast, got %d", count)
	}
}

func TestDocMessageRoundTrip(t *testing.T) {
	original := &DocMessage{
		Type:      DocMsgOperation,
		DocID:     "test-doc",
		AuthorID:  strID(t,"author"),
		Timestamp: 1234567890,
		Payload:   []byte("test payload"),
	}
	data, err := encodeDocMessage(original)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, err := decodeDocMessage(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.Type != original.Type {
		t.Fatalf("type mismatch")
	}
	if decoded.DocID != original.DocID {
		t.Fatalf("docID mismatch: %q != %q", decoded.DocID, original.DocID)
	}
	if decoded.AuthorID != original.AuthorID {
		t.Fatalf("authorID mismatch")
	}
	if decoded.Timestamp != original.Timestamp {
		t.Fatalf("timestamp mismatch: %d != %d", decoded.Timestamp, original.Timestamp)
	}
	if string(decoded.Payload) != string(original.Payload) {
		t.Fatalf("payload mismatch")
	}
}

func TestDocMessageTooShort(t *testing.T) {
	_, err := decodeDocMessage([]byte{0x01})
	if err == nil {
		t.Fatal("expected error for too-short message")
	}
}

func TestOpRoundTrip(t *testing.T) {
	original := NewInsertOp("doc-1", strID(t,"author"), 5, "hello world", 9999)
	payload := marshalOp(original)
	decoded, err := unmarshalOp(payload)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Type != OpInsert {
		t.Fatalf("type mismatch: %d", decoded.Type)
	}
	if decoded.Position != 5 {
		t.Fatalf("position mismatch: %d", decoded.Position)
	}
	if decoded.Value != "hello world" {
		t.Fatalf("value mismatch: %q", decoded.Value)
	}
	if decoded.Timestamp != 9999 {
		t.Fatalf("timestamp mismatch: %d", decoded.Timestamp)
	}
}

func TestOpPayloadRoundTrip(t *testing.T) {
	op := NewFormatBlockOp("doc-1", strID(t,"a"), 2, BlockHeading2, 5000)
	data, err := encodeOpPayload(op, "doc-1")
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, docID, err := decodeOpPayload(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if docID != "doc-1" {
		t.Fatalf("docID mismatch: %q", docID)
	}
	if decoded.Type != OpFormatBlock {
		t.Fatalf("type mismatch")
	}
	if decoded.BlockType != BlockHeading2 {
		t.Fatalf("block type mismatch: %v", decoded.BlockType)
	}
	if decoded.Position != 2 {
		t.Fatalf("position mismatch: %d", decoded.Position)
	}
}

func TestPresenceUpdateRoundTrip(t *testing.T) {
	docID := "doc-pres"
	peerID := strID(t,"peer-x")
	peerName := "Charlie"
	line, col := 7, 12

	data := marshalPresenceUpdate(docID, peerID, peerName, line, col)
	gotDocID, gotPeerID, gotName, gotLine, gotCol, err := unmarshalPresenceUpdate(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if gotDocID != docID {
		t.Fatalf("docID mismatch: %q != %q", gotDocID, docID)
	}
	if gotPeerID != peerID {
		t.Fatalf("peerID mismatch")
	}
	if gotName != peerName {
		t.Fatalf("name mismatch: %q != %q", gotName, peerName)
	}
	if gotLine != line || gotCol != col {
		t.Fatalf("position mismatch: (%d,%d) != (%d,%d)", gotLine, gotCol, line, col)
	}
}

func TestDocumentStateRoundTrip(t *testing.T) {
	state := DocumentState{
		DocID:      "doc-st",
		Title:      "State Test",
		Content:    []string{"line1", "line2", "line3"},
		BlockTypes: []BlockType{BlockHeading1, BlockParagraph, BlockBulletList},
		Version:    42,
	}
	data := marshalDocumentState(state)
	got, err := unmarshalDocumentState(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.DocID != state.DocID {
		t.Fatalf("docID mismatch")
	}
	if got.Title != state.Title {
		t.Fatalf("title mismatch: %q != %q", got.Title, state.Title)
	}
	if len(got.Content) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(got.Content))
	}
	if got.Version != 42 {
		t.Fatalf("version mismatch: %d != %d", got.Version, state.Version)
	}
}

func TestServiceConcurrentAccess(t *testing.T) {
	svc := NewService(ServiceConfig{NodeID: "node-1"})
	_ = svc.eventCh
	svc.CreateDocument("doc-1", "Doc")

	authorID := strID(t,"author")
	var wg sync.WaitGroup
	wg.Add(20)
	for i := 0; i < 20; i++ {
		go func(idx int) {
			defer wg.Done()
			svc.InsertText(context.Background(), "doc-1", authorID, idx, "x")
			_ = svc.AllDocuments()
			_ = svc.Stats()
		}(i)
	}
	wg.Wait()
}

func TestBlockTypeStringRoundTrip(t *testing.T) {
	for _, expected := range []BlockType{BlockParagraph, BlockHeading1, BlockHeading2, BlockHeading3, BlockBulletList, BlockNumberedList, BlockCodeBlock, BlockQuote} {
		s := expected.String()
		got := ParseBlockType(s)
		if got != expected {
			t.Fatalf("round-trip failed for %v: got %v", expected, got)
		}
	}
}

func TestOpTypeString(t *testing.T) {
	if OpInsert.String() != "insert" {
		t.Fatalf("expected 'insert'")
	}
	if OpDelete.String() != "delete" {
		t.Fatalf("expected 'delete'")
	}
	if OpFormatBlock.String() != "format_block" {
		t.Fatalf("expected 'format_block'")
	}
}

func TestServerOptions(t *testing.T) {
	opts := ServerOptions()
	if !opts[transport.ServiceDocs] {
		t.Fatal("expected ServiceDocs in options")
	}
}

func TestGenerateNodeID(t *testing.T) {
	var pubKey [32]byte
	for i := range pubKey {
		pubKey[i] = byte(i)
	}
	id := GenerateNodeID(pubKey)
	if id == pubKey {
		t.Fatal("expected GenerateNodeID to hash the key, not return it directly")
	}
}

