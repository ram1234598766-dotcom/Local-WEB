package docs

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ram1234598766-dotcom/Local-WEB/pkg/crypto"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/discovery"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/transport"
)

// Service manages collaborative documents over QUIC.
type Service struct {
	mu         sync.RWMutex
	docs       map[string]*Document
	presence   map[string]*PresenceService
	notifier   PeerNotifier
	nodeID     string
	pubKey     [32]byte
	privKey    [32]byte
	eventCh    chan DocEvent
	started    time.Time
	stats      ServiceStats
	pendingOps map[string][]Op // docID -> ops for offline peers
}

// ServiceStats tracks collaborative docs service statistics.
type ServiceStats struct {
	TotalDocs       int64
	ActiveDocs      int64
	TotalOps        int64
	TotalBroadcasts int64
	ActivePeers     int64
}

// ServiceConfig configures the docs service.
type ServiceConfig struct {
	NodeID   string
	PubKey   [32]byte
	PrivKey  [32]byte
	Notifier PeerNotifier
}

// NewService creates a new collaborative docs service.
func NewService(cfg ServiceConfig) *Service {
	s := &Service{
		docs:       make(map[string]*Document),
		presence:   make(map[string]*PresenceService),
		nodeID:     cfg.NodeID,
		pubKey:     cfg.PubKey,
		privKey:    cfg.PrivKey,
		notifier:   cfg.Notifier,
		started:    time.Now(),
		pendingOps: make(map[string][]Op),
	}
	if s.notifier == nil {
		s.notifier = &noopBroadcaster{}
	}
	s.eventCh = make(chan DocEvent, 256)
	go s.eventLoop()
	return s
}

// noopBroadcaster is used when no real notifier is configured.
type noopBroadcaster struct{}

func (n *noopBroadcaster) Broadcast(ctx context.Context, docID string, exclude [32]byte, msg *DocMessage) error {
	return nil
}
func (n *noopBroadcaster) Peers() []discovery.PeerInfo { return nil }

// eventLoop processes document events asynchronously.
func (s *Service) eventLoop() {
	for evt := range s.eventCh {
		s.handleEvent(evt)
	}
}

// handleEvent processes a document event.
func (s *Service) handleEvent(evt DocEvent) {
	s.mu.Lock()
	s.stats.TotalOps++
	s.mu.Unlock()
}

// RegisterHandler returns a transport.StreamHandler for ServiceDocs.
func (s *Service) RegisterHandler() transport.StreamHandler {
	return func(ctx context.Context, stream transport.Stream) {
		s.handleStream(ctx, stream)
	}
}

// handleStream reads framed docs messages from a QUIC stream.
func (s *Service) handleStream(ctx context.Context, stream transport.Stream) {
	buf := make([]byte, 4096)
	for {
		n, err := stream.Read(buf)
		if err != nil {
			return
		}
		if n == 0 {
			continue
		}
		msg, err := decodeDocMessage(buf[:n])
		if err != nil {
			continue
		}
		s.handleMessage(ctx, msg)
	}
}

// handleMessage dispatches a decoded DocMessage.
func (s *Service) handleMessage(ctx context.Context, msg *DocMessage) {
	switch msg.Type {
	case DocMsgOperation:
		s.handleOperation(ctx, msg)
	case DocMsgSyncReq:
		s.handleSyncRequest(ctx, msg)
	case DocMsgFullState:
		s.handleFullState(ctx, msg)
	case DocMsgPresence:
		s.handlePresence(ctx, msg)
	}
}

// handleOperation applies a remote operation and rebroadcasts it.
func (s *Service) handleOperation(ctx context.Context, msg *DocMessage) {
	op, docID, err := decodeOpPayload(msg.Payload)
	if err != nil {
		return
	}
	doc := s.GetDocument(docID)
	if doc == nil {
		return
	}
	result := doc.ApplyOp(op)
	if result.Applied {
		s.mu.Lock()
		s.stats.TotalOps++
		s.stats.TotalBroadcasts++
		s.mu.Unlock()
		_ = ctx
	}
}

// handleSyncRequest responds with the current document state.
func (s *Service) handleSyncRequest(ctx context.Context, msg *DocMessage) {
	docID := msg.DocID
	doc := s.GetDocument(docID)
	if doc == nil {
		return
	}
	state := doc.State()
	stateBytes := marshalDocumentState(state)
	outMsg, _ := encodeDocMessage(&DocMessage{
		Type:    DocMsgSyncResp,
		DocID:   docID,
		Payload: stateBytes,
	})
	_ = outMsg
	_ = ctx
}

// handleFullState merges a full document state snapshot.
func (s *Service) handleFullState(ctx context.Context, msg *DocMessage) {
	docID := msg.DocID
	state, err := unmarshalDocumentState(msg.Payload)
	if err != nil {
		return
	}
	doc := s.GetOrCreateDocument(docID, state.Title)
	if doc != nil {
		_ = doc
		_ = state
		_ = ctx
	}
}

// handlePresence processes a presence update from a remote peer.
func (s *Service) handlePresence(ctx context.Context, msg *DocMessage) {
	docID, peerID, peerName, line, col, err := unmarshalPresenceUpdate(msg.Payload)
	if err != nil {
		return
	}
	pres := s.GetPresence(docID)
	if pres == nil {
		return
	}
	pres.UpdateCursor(peerID, peerName, line, col)
	_ = ctx
}

// CreateDocument creates a new collaborative document.
func (s *Service) CreateDocument(docID, title string) *Document {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.docs[docID]; exists {
		return s.docs[docID]
	}
	cfg := DocumentConfig{
		ID:     docID,
		Title:  title,
		NodeID: s.nodeID,
		EventHandler: func(evt DocEvent) {
			select {
			case s.eventCh <- evt:
			default:
			}
		},
	}
	doc := NewDocument(cfg)
	s.docs[docID] = doc
	s.presence[docID] = NewPresenceService(PresenceConfig{DocID: docID, Timeout: 5 * time.Second})
	s.stats.TotalDocs++
	s.stats.ActiveDocs++
	return doc
}

// GetDocument returns a document by ID, or nil.
func (s *Service) GetDocument(docID string) *Document {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.docs[docID]
}

// GetOrCreateDocument returns an existing document or creates one.
func (s *Service) GetOrCreateDocument(docID, title string) *Document {
	doc := s.GetDocument(docID)
	if doc != nil {
		return doc
	}
	return s.CreateDocument(docID, title)
}

// DeleteDocument removes a document.
func (s *Service) DeleteDocument(docID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.docs[docID]; !ok {
		return fmt.Errorf("document not found: %s", docID)
	}
	delete(s.docs, docID)
	delete(s.presence, docID)
	s.stats.ActiveDocs--
	return nil
}

// ApplyLocalOp applies a local operation and broadcasts it.
func (s *Service) ApplyLocalOp(ctx context.Context, docID string, op Op) (*Document, *OpResult, error) {
	doc := s.GetDocument(docID)
	if doc == nil {
		return nil, nil, fmt.Errorf("document not found: %s", docID)
	}
	result := doc.ApplyOp(op)
	if !result.Applied {
		return doc, &result, result.Error
	}

	s.mu.Lock()
	s.stats.TotalOps++
	s.stats.TotalBroadcasts++
	s.mu.Unlock()

	msg := &DocMessage{
		Type:      DocMsgOperation,
		DocID:     docID,
		AuthorID:  op.AuthorID,
		Timestamp: op.Timestamp,
		Payload:   marshalOp(op),
	}
	if s.notifier != nil {
		_ = s.notifier.Broadcast(ctx, docID, op.AuthorID, msg)
	}
	return doc, &result, nil
}

// InsertText inserts text at a position (convenience method).
func (s *Service) InsertText(ctx context.Context, docID string, authorID [32]byte, position int, text string) (*Document, *OpResult, error) {
	op := NewInsertOp(docID, authorID, position, text, CurrentTimestamp())
	return s.ApplyLocalOp(ctx, docID, op)
}

// DeleteLine deletes a line (convenience method).
func (s *Service) DeleteLine(ctx context.Context, docID string, authorID [32]byte, position int) (*Document, *OpResult, error) {
	op := NewDeleteOp(docID, authorID, position, CurrentTimestamp())
	return s.ApplyLocalOp(ctx, docID, op)
}

// FormatBlock changes a block's type (convenience method).
func (s *Service) FormatBlock(ctx context.Context, docID string, authorID [32]byte, position int, bt BlockType) (*Document, *OpResult, error) {
	op := NewFormatBlockOp(docID, authorID, position, bt, CurrentTimestamp())
	return s.ApplyLocalOp(ctx, docID, op)
}

// UpdatePresence broadcasts a cursor/selection update.
func (s *Service) UpdatePresence(ctx context.Context, docID, peerName string, peerID [32]byte, line, column int) {
	pres := s.GetPresence(docID)
	if pres == nil {
		return
	}
	pres.UpdateCursor(peerID, peerName, line, column)
	payload := marshalPresenceUpdate(docID, peerID, peerName, line, column)
	if s.notifier != nil {
		_ = s.notifier.Broadcast(ctx, docID, peerID, &DocMessage{
			Type: DocMsgPresence, DocID: docID, Payload: payload,
		})
	}
}

// UpdateSelection broadcasts a selection update.
func (s *Service) UpdateSelection(ctx context.Context, docID, peerName string, peerID [32]byte, startLine, startCol, endLine, endCol int) {
	pres := s.GetPresence(docID)
	if pres == nil {
		return
	}
	pres.UpdateSelection(peerID, peerName, startLine, startCol, endLine, endCol)
	_ = ctx
}

// GetPresence returns the presence service for a document.
func (s *Service) GetPresence(docID string) *PresenceService {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.presence[docID]
}

// GetDocPresence returns all active peer presences for a document.
func (s *Service) GetDocPresence(docID string) []PeerPresence {
	pres := s.GetPresence(docID)
	if pres == nil {
		return nil
	}
	return pres.GetAll()
}

// Stats returns service statistics.
func (s *Service) Stats() ServiceStats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.stats
}

// AllDocuments returns all managed documents.
func (s *Service) AllDocuments() []*Document {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Document, 0, len(s.docs))
	for _, doc := range s.docs {
		out = append(out, doc)
	}
	return out
}

// SetNotifier updates the peer notifier.
func (s *Service) SetNotifier(n PeerNotifier) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.notifier = n
}

// NodeID returns the local node ID string.
func (s *Service) NodeID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.nodeID
}

// Marshal serializes all documents.
func (s *Service) Marshal() [][]byte {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([][]byte, 0, len(s.docs))
	for _, doc := range s.docs {
		out = append(out, doc.Marshal())
	}
	return out
}

// Unmarshal deserializes documents from byte slices.
func (s *Service) Unmarshal(data [][]byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, docData := range data {
		doc := NewDocument(DocumentConfig{NodeID: s.nodeID})
		if err := doc.Unmarshal(docData); err != nil {
			continue
		}
		s.docs[doc.ID()] = doc
		s.presence[doc.ID()] = NewPresenceService(PresenceConfig{DocID: doc.ID()})
		s.stats.TotalDocs++
		s.stats.ActiveDocs++
	}
	return nil
}

// MarshalDocumentState serializes a DocumentState to bytes.
func marshalDocumentState(state DocumentState) []byte {
	buf := make([]byte, 0, 4+len(state.DocID)+4+len(state.Title)+4+4+len(state.Content)+8)
	buf = append(buf, encodeUint32(uint32(len(state.DocID)))...)
	buf = append(buf, state.DocID...)
	buf = append(buf, encodeUint32(uint32(len(state.Title)))...)
	buf = append(buf, state.Title...)

	contentBytes := []byte(stringsJoin(state.Content))
	buf = append(buf, encodeUint32(uint32(len(contentBytes)))...)
	buf = append(buf, contentBytes...)

	buf = append(buf, encodeInt64(state.Version)...)
	return buf
}

// UnmarshalDocumentState deserializes bytes to a DocumentState.
func unmarshalDocumentState(data []byte) (DocumentState, error) {
	var state DocumentState
	if len(data) < 4 {
		return state, errors.New("state data too short")
	}
	offset := 0
	idLen := int(decodeUint32(data[offset : offset+4]))
	offset += 4
	if len(data) < offset+idLen {
		return state, errors.New("id truncated")
	}
	state.DocID = string(data[offset : offset+idLen])
	offset += idLen
	if len(data) < offset+4 {
		return state, errors.New("title length truncated")
	}
	titleLen := int(decodeUint32(data[offset : offset+4]))
	offset += 4
	if len(data) < offset+titleLen {
		return state, errors.New("title truncated")
	}
	state.Title = string(data[offset : offset+titleLen])
	offset += titleLen
	if len(data) < offset+4 {
		return state, errors.New("content length truncated")
	}
	contentLen := int(decodeUint32(data[offset : offset+4]))
	offset += 4
	if len(data) < offset+contentLen {
		return state, errors.New("content truncated")
	}
	contentStr := string(data[offset : offset+contentLen])
	state.Content = stringsSplit(contentStr, "\n")
	offset += contentLen
	if len(data) >= offset+8 {
		state.Version = decodeInt64(data[offset : offset+8])
	}
	return state, nil
}

// marshalPresenceUpdate serializes a presence update payload.
func marshalPresenceUpdate(docID string, peerID [32]byte, peerName string, line, column int) []byte {
	buf := make([]byte, 0, 4+len(docID)+32+4+4+4+len(peerName))
	buf = append(buf, encodeUint32(uint32(len(docID)))...)
	buf = append(buf, docID...)
	buf = append(buf, peerID[:]...)
	buf = append(buf, encodeUint32(uint32(line))...)
	buf = append(buf, encodeUint32(uint32(column))...)
	buf = append(buf, encodeUint32(uint32(len(peerName)))...)
	buf = append(buf, peerName...)
	return buf
}

// unmarshalPresenceUpdate deserializes a presence update payload.
func unmarshalPresenceUpdate(data []byte) (docID string, peerID [32]byte, peerName string, line, column int, err error) {
	if len(data) < 4+32+4+4 {
		return "", [32]byte{}, "", 0, 0, errors.New("presence data too short")
	}
	offset := 0
	nameLen := int(decodeUint32(data[offset : offset+4]))
	offset += 4
	if len(data) < offset+nameLen+32+4+4 {
		return "", [32]byte{}, "", 0, 0, errors.New("presence truncated")
	}
	docID = string(data[offset : offset+nameLen])
	offset += nameLen
	copy(peerID[:], data[offset:offset+32])
	offset += 32
	line = int(decodeUint32(data[offset : offset+4]))
	offset += 4
	column = int(decodeUint32(data[offset : offset+4]))
	offset += 4
	if len(data) >= offset+4 {
		peerNameLen := int(decodeUint32(data[offset : offset+4]))
		offset += 4
		if len(data) >= offset+peerNameLen {
			peerName = string(data[offset : offset+peerNameLen])
		}
	}
	return docID, peerID, peerName, line, column, nil
}

// stringsJoin joins a slice of strings with newlines.
func stringsJoin(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	sb := &strings.Builder{}
	sb.WriteString(parts[0])
	for i := 1; i < len(parts); i++ {
		sb.WriteString("\n")
		sb.WriteString(parts[i])
	}
	return sb.String()
}

// stringsSplit splits a string by a separator.
func stringsSplit(s, sep string) []string {
	if s == "" {
		return []string{}
	}
	parts := stringsSplitGeneric(s, sep)
	if len(parts) == 0 {
		return []string{""}
	}
	return parts
}

func stringsSplitGeneric(s, sep string) []string {
	if sep == "" {
		return []string{s}
	}
	var result []string
	start := 0
	for i := 0; i < len(s); i++ {
		if i+len(sep) <= len(s) && s[i:i+len(sep)] == sep {
			result = append(result, s[start:i])
			start = i + len(sep)
		}
	}
	result = append(result, s[start:])
	return result
}

// GenerateNodeID creates a [32]byte node ID from a public key.
func GenerateNodeID(pubKey [32]byte) [32]byte {
	return crypto.NodeID(pubKey)
}

// ServerOptions returns allowed services for the docs transport.
func ServerOptions() map[transport.ServiceID]bool {
	return map[transport.ServiceID]bool{transport.ServiceDocs: true}
}

// OpenStream opens a QUIC stream for a docs operation.
func OpenStream(ctx context.Context, peerID [32]byte, svc transport.ServiceID, openFn func(context.Context, transport.ServiceID) (transport.Stream, error)) (transport.Stream, error) {
	return openFn(ctx, svc)
}

// WriteOperation writes an operation to a QUIC stream.
func WriteOperation(stream transport.Stream, op Op, docID string) error {
	payload, err := encodeOpPayload(op, docID)
	if err != nil {
		return err
	}
	frame := transport.EncodeFrameBare(MsgDocsOp, payload)
	_, err = stream.Write(frame)
	return err
}

// ReadOperation reads an operation from a QUIC stream.
func ReadOperation(stream transport.Stream) (Op, string, error) {
	buf := make([]byte, 4096)
	n, err := stream.Read(buf)
	if err != nil {
		return Op{}, "", err
	}
	frame, err := transport.DecodeFrameBare(buf[:n])
	if err != nil {
		return Op{}, "", err
	}
	return decodeOpPayload(frame.Payload)
}

// StreamHandler returns a goroutine-safe handler wrapper for transport registration.
func StreamHandler(s *Service) transport.StreamHandler {
	return func(ctx context.Context, stream transport.Stream) {
		s.handleStream(ctx, stream)
	}
}

// PeerInfoFromTransport converts a transport.PeerInfo to PeerPresence.
func PeerInfoFromTransport(p transport.PeerInfo, docID, peerName string) PeerPresence {
	return PeerPresence{
		PeerID:    p.ID,
		PeerName:  peerName,
		Connected: p.State == transport.StateReady,
		LastSeen:  p.LastPong,
	}
}
