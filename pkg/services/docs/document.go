package docs

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/mrityunjay/LocalWEB/pkg/crdt"
)

// DocumentState tracks a document's full state for sync.
type DocumentState struct {
	DocID      string
	Title      string
	Content    []string
	BlockTypes []BlockType
	Length     int
	Version    int64
	UpdatedAt  time.Time
}

// Document is an RGA-backed collaborative text document.
//
// The `lines` slice is the authoritative source for the document's visible
// content. The embedded RGA tracks per-character state for CRDT merge. After
// any remote merge, `lines` is rebuilt from the RGA node list.
type Document struct {
	mu         sync.RWMutex
	id         string
	title      string
	rga        *crdt.RGA
	blockTypes []BlockType // block type for each line
	lines      []string    // visible lines
	history    []Op        // applied operations for undo/redo
	historyIdx int         // current position in undo history
	version    int64
	updatedAt  time.Time
	eventCh    chan DocEvent
	nodeID     string
}

// DocumentConfig configures a new Document.
type DocumentConfig struct {
	ID           string
	Title        string
	NodeID       string
	EventHandler DocEventHandler
}

// NewDocument creates a new collaborative document.
func NewDocument(cfg DocumentConfig) *Document {
	doc := &Document{
		id:      cfg.ID,
		title:   cfg.Title,
		rga:     crdt.NewRGA(cfg.NodeID),
		lines:   []string{},
		history: make([]Op, 0, 256),
		version: 0,
		nodeID:  cfg.NodeID,
	}
	if cfg.EventHandler != nil {
		doc.eventCh = make(chan DocEvent, 64)
		go doc.eventLoop(cfg.EventHandler)
	}
	return doc
}

// eventLoop forwards events to the handler.
func (d *Document) eventLoop(handler DocEventHandler) {
	for evt := range d.eventCh {
		handler(evt)
	}
}

// emitEvent sends an event to the handler channel.
func (d *Document) emitEvent(evt DocEvent) {
	if d.eventCh != nil {
		select {
		case d.eventCh <- evt:
		default:
		}
	}
}

// ID returns the document ID.
func (d *Document) ID() string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.id
}

// Title returns the current title.
func (d *Document) Title() string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.title
}

// Text returns the full document text.
func (d *Document) Text() string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return joinLines(d.lines)
}

// Lines returns a copy of the document lines.
func (d *Document) Lines() []string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	out := make([]string, len(d.lines))
	copy(out, d.lines)
	return out
}

// BlockTypes returns a copy of the block types.
func (d *Document) BlockTypes() []BlockType {
	d.mu.RLock()
	defer d.mu.RUnlock()
	out := make([]BlockType, len(d.blockTypes))
	copy(out, d.blockTypes)
	return out
}

// Length returns the current RGA length (character count).
func (d *Document) Length() int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.rga.Length()
}

// LineCount returns the number of lines.
func (d *Document) LineCount() int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return len(d.lines)
}

// Version returns the document version counter.
func (d *Document) Version() int64 {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.version
}

// State returns a snapshot of the document.
func (d *Document) State() DocumentState {
	d.mu.RLock()
	defer d.mu.RUnlock()
	lines := make([]string, len(d.lines))
	copy(lines, d.lines)
	btypes := make([]BlockType, len(d.blockTypes))
	copy(btypes, d.blockTypes)
	return DocumentState{
		DocID:      d.id,
		Title:      d.title,
		Content:    lines,
		BlockTypes: btypes,
		Length:     d.rga.Length(),
		Version:    d.version,
		UpdatedAt:  d.updatedAt,
	}
}

// ApplyOp applies a local or remote operation.
func (d *Document) ApplyOp(op Op) OpResult {
	d.mu.Lock()
	defer d.mu.Unlock()

	switch op.Type {
	case OpInsert:
		return d.applyInsert(op)
	case OpDelete:
		return d.applyDelete(op)
	case OpFormatBlock:
		return d.applyFormatBlock(op)
	case OpTitleChange:
		return d.applyTitleChange(op)
	default:
		return OpResult{Applied: false, Error: fmt.Errorf("unknown op type: %d", op.Type)}
	}
}

// applyInsert inserts a new line at the given position (line-level).
func (d *Document) applyInsert(op Op) OpResult {
	if op.Position < 0 || op.Position > len(d.lines) {
		return OpResult{Applied: false, Error: fmt.Errorf("insert position %d out of range [0,%d]", op.Position, len(d.lines))}
	}
	// Insert into lines/blockTypes
	if op.Position == len(d.lines) {
		d.lines = append(d.lines, op.Value)
		d.blockTypes = append(d.blockTypes, BlockParagraph)
	} else {
		d.lines = insertSlice(d.lines, op.Position, op.Value)
		d.blockTypes = insertSliceBlock(d.blockTypes, op.Position, BlockParagraph)
	}
	// Push characters into RGA for CRDT sync
	d.pushLineToRGA(op.Position, op.Value)
	d.version++
	d.updatedAt = time.Now()
	d.history = append(d.history, op)
	d.historyIdx = len(d.history)
	return OpResult{Applied: true, Position: op.Position}
}

// pushLineToRGA pushes a line's characters into the RGA.
// Characters are appended at the end of the RGA sequence.
func (d *Document) pushLineToRGA(linePos int, lineText string) {
	_ = linePos
	// Append all characters at the end of the RGA
	lastCharID := "head"
	if d.rga.Length() > 0 {
		lastCh, err := d.rga.Get(d.rga.Length() - 1)
		if err == nil && lastCh != "" {
			lastCharID = fmt.Sprintf("%d:%s:%s", 0, d.nodeID, lastCh)
		}
	}
	// Add newline if not the first line
	if linePos < d.rga.Length() {
		d.rga.Insert(lastCharID, "\n")
		lastCharID = ""
	}
	for _, ch := range lineText {
		d.rga.Insert(lastCharID, string(ch))
		lastCharID = ""
	}
}

// applyDelete removes a line at the given position.
func (d *Document) applyDelete(op Op) OpResult {
	if op.Position < 0 || op.Position >= len(d.lines) {
		return OpResult{Applied: false, Error: fmt.Errorf("delete position %d out of range", op.Position)}
	}
	_ = d.lines[op.Position]
	d.lines = append(d.lines[:op.Position], d.lines[op.Position+1:]...)
	d.blockTypes = append(d.blockTypes[:op.Position], d.blockTypes[op.Position+1:]...)
	d.version++
	d.updatedAt = time.Now()
	delOp := Op{Type: OpDelete, DocID: d.id, AuthorID: op.AuthorID, Position: op.Position, Timestamp: op.Timestamp}
	d.history = append(d.history, delOp)
	d.historyIdx = len(d.history)
	return OpResult{Applied: true, Position: op.Position}
}

// applyFormatBlock changes a block's type.
func (d *Document) applyFormatBlock(op Op) OpResult {
	if op.Position < 0 || op.Position >= len(d.blockTypes) {
		return OpResult{Applied: false, Error: fmt.Errorf("format position %d out of range", op.Position)}
	}
	d.blockTypes[op.Position] = op.BlockType
	d.version++
	d.updatedAt = time.Now()
	fmtOp := Op{Type: OpFormatBlock, DocID: d.id, AuthorID: op.AuthorID, Position: op.Position, BlockType: op.BlockType, Timestamp: op.Timestamp}
	d.history = append(d.history, fmtOp)
	d.historyIdx = len(d.history)
	return OpResult{Applied: true, Position: op.Position}
}

// applyTitleChange updates the title.
func (d *Document) applyTitleChange(op Op) OpResult {
	d.title = op.Title
	d.version++
	d.updatedAt = time.Now()
	return OpResult{Applied: true, Position: 0}
}

// rebuildFromRGA rebuilds lines and blockTypes from the RGA node list.
// Used after remote merges to resync local state.
func (d *Document) rebuildFromRGA() {
	var lines []string
	var blockTypes []BlockType
	var current string
	for i := 0; i < d.rga.Length(); i++ {
		ch, err := d.rga.Get(i)
		if err != nil {
			break
		}
		if ch == "\n" {
			lines = append(lines, current)
			blockTypes = append(blockTypes, BlockParagraph)
			current = ""
		} else {
			current += ch
		}
	}
	lines = append(lines, current)
	for len(blockTypes) < len(lines) {
		blockTypes = append(blockTypes, BlockParagraph)
	}
	// Preserve existing block types where possible
	for i := 0; i < len(blockTypes) && i < len(d.blockTypes); i++ {
		if i < len(lines) && i < len(blockTypes) {
			blockTypes[i] = d.blockTypes[i]
		}
	}
	d.lines = lines
	d.blockTypes = blockTypes
}

// Insert inserts a new line at the given position (local edit).
func (d *Document) Insert(position int, text string) OpResult {
	op := NewInsertOp(d.id, stringToID(d.nodeID), position, text, CurrentTimestamp())
	result := d.ApplyOp(op)
	if result.Applied {
		d.emitEvent(DocEvent{DocID: d.id, Op: op, Result: result})
	}
	return result
}

// Delete removes a line at the given position (local edit).
func (d *Document) Delete(position int) OpResult {
	op := NewDeleteOp(d.id, stringToID(d.nodeID), position, CurrentTimestamp())
	result := d.ApplyOp(op)
	if result.Applied {
		d.emitEvent(DocEvent{DocID: d.id, Op: op, Result: result})
	}
	return result
}

// FormatBlock changes the block type at a position (local edit).
func (d *Document) FormatBlock(position int, bt BlockType) OpResult {
	op := NewFormatBlockOp(d.id, stringToID(d.nodeID), position, bt, CurrentTimestamp())
	result := d.ApplyOp(op)
	if result.Applied {
		d.emitEvent(DocEvent{DocID: d.id, Op: op, Result: result})
	}
	return result
}

// Merge applies a batch of remote operations (from another replica).
// After merging, lines are rebuilt from the RGA.
func (d *Document) Merge(ops []Op) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, op := range ops {
		switch op.Type {
		case OpInsert:
			_ = d.applyInsert(op)
		case OpDelete:
			_ = d.applyDelete(op)
		case OpFormatBlock:
			_ = d.applyFormatBlock(op)
		case OpTitleChange:
			_ = d.applyTitleChange(op)
		}
	}
	// Rebuild lines from RGA after merge
	d.rebuildFromRGA()
	return nil
}

// Undo undoes the last operation.
func (d *Document) Undo() *OpResult {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.historyIdx <= 0 {
		return &OpResult{Applied: false, Error: errors.New("nothing to undo")}
	}
	d.historyIdx--
	op := d.history[d.historyIdx]
	var result OpResult
	switch op.Type {
	case OpInsert:
		if op.Position < len(d.lines) {
			d.lines = append(d.lines[:op.Position], d.lines[op.Position+1:]...)
			d.blockTypes = append(d.blockTypes[:op.Position], d.blockTypes[op.Position+1:]...)
			result = OpResult{Applied: true, Position: op.Position}
		}
	case OpDelete:
		if op.Position <= len(d.lines) {
			d.lines = insertSlice(d.lines, op.Position, op.Value)
			d.blockTypes = insertSliceBlock(d.blockTypes, op.Position, BlockParagraph)
			result = OpResult{Applied: true, Position: op.Position}
		}
	case OpFormatBlock:
		if op.Position < len(d.blockTypes) {
			d.blockTypes[op.Position] = BlockParagraph
			result = OpResult{Applied: true, Position: op.Position}
		}
	case OpTitleChange:
		d.title = ""
		result = OpResult{Applied: true}
	}
	d.version++
	d.updatedAt = time.Now()
	return &result
}

// Redo redoes the last undone operation.
func (d *Document) Redo() *OpResult {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.historyIdx >= len(d.history) {
		return &OpResult{Applied: false, Error: errors.New("nothing to redo")}
	}
	op := d.history[d.historyIdx]
	d.historyIdx++
	var result OpResult
	switch op.Type {
	case OpInsert:
		if op.Position <= len(d.lines) {
			d.lines = insertSlice(d.lines, op.Position, op.Value)
			d.blockTypes = insertSliceBlock(d.blockTypes, op.Position, BlockParagraph)
			result = OpResult{Applied: true, Position: op.Position}
		}
	case OpDelete:
		if op.Position < len(d.lines) {
			d.lines = append(d.lines[:op.Position], d.lines[op.Position+1:]...)
			d.blockTypes = append(d.blockTypes[:op.Position], d.blockTypes[op.Position+1:]...)
			result = OpResult{Applied: true, Position: op.Position}
		}
	case OpFormatBlock:
		if op.Position < len(d.blockTypes) {
			d.blockTypes[op.Position] = op.BlockType
			result = OpResult{Applied: true, Position: op.Position}
		}
	case OpTitleChange:
		d.title = op.Title
		result = OpResult{Applied: true}
	}
	d.version++
	d.updatedAt = time.Now()
	return &result
}

// CanUndo returns whether undo is available.
func (d *Document) CanUndo() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.historyIdx > 0
}

// CanRedo returns whether redo is available.
func (d *Document) CanRedo() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.historyIdx < len(d.history)
}

// Marshal serializes the document state.
func (d *Document) Marshal() []byte {
	d.mu.RLock()
	defer d.mu.RUnlock()
	rgaData := d.rga.Marshal()
	buf := make([]byte, 0, 4+len(d.id)+4+len(d.title)+4+len(rgaData)+8+4+len(d.blockTypes)+4+len(d.lines))
	buf = append(buf, encodeUint32(uint32(len(d.id)))...)
	buf = append(buf, d.id...)
	buf = append(buf, encodeUint32(uint32(len(d.title)))...)
	buf = append(buf, d.title...)
	buf = append(buf, encodeUint32(uint32(len(rgaData)))...)
	buf = append(buf, rgaData...)
	buf = append(buf, encodeInt64(d.version)...)
	// Serialize block types
	buf = append(buf, encodeUint32(uint32(len(d.blockTypes)))...)
	for _, bt := range d.blockTypes {
		buf = append(buf, byte(bt))
	}
	// Serialize lines
	buf = append(buf, encodeUint32(uint32(len(d.lines)))...)
	for _, line := range d.lines {
		buf = append(buf, encodeUint32(uint32(len(line)))...)
		buf = append(buf, line...)
	}
	return buf
}

// Unmarshal deserializes document state.
func (d *Document) Unmarshal(data []byte) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(data) < 4 {
		return errors.New("document data too short")
	}
	offset := 0

	// DocID
	idLen := int(decodeUint32(data[offset : offset+4]))
	offset += 4
	if len(data) < offset+idLen {
		return errors.New("document id truncated")
	}
	d.id = string(data[offset : offset+idLen])
	offset += idLen

	// Title
	if len(data) < offset+4 {
		return errors.New("title length truncated")
	}
	titleLen := int(decodeUint32(data[offset : offset+4]))
	offset += 4
	if len(data) < offset+titleLen {
		return errors.New("title truncated")
	}
	d.title = string(data[offset : offset+titleLen])
	offset += titleLen

	// RGA data
	if len(data) < offset+4 {
		return errors.New("rga length truncated")
	}
	rgaLen := int(decodeUint32(data[offset : offset+4]))
	offset += 4
	if len(data) < offset+rgaLen {
		return errors.New("rga data truncated")
	}
	if err := d.rga.Unmarshal(data[offset : offset+rgaLen]); err != nil {
		return fmt.Errorf("rga unmarshal: %w", err)
	}
	offset += rgaLen

	// Version
	if len(data) < offset+8 {
		d.blockTypes = []BlockType{}
		d.lines = rebuildLinesFromRGARaw(d.rga)
		return nil
	}
	d.version = decodeInt64(data[offset : offset+8])
	offset += 8

	// Block types
	if len(data) < offset+4 {
		d.blockTypes = []BlockType{}
		d.lines = rebuildLinesFromRGARaw(d.rga)
		return nil
	}
	btLen := int(decodeUint32(data[offset : offset+4]))
	offset += 4
	if len(data) < offset+btLen {
		d.blockTypes = []BlockType{}
		d.lines = rebuildLinesFromRGARaw(d.rga)
		return nil
	}
	d.blockTypes = make([]BlockType, btLen)
	for i := 0; i < btLen; i++ {
		btVal := BlockType(data[offset+i])
		if int(btVal) < len(_blockTypeStrings) {
			d.blockTypes[i] = btVal
		} else {
			d.blockTypes[i] = BlockParagraph
		}
	}
	offset += btLen

	// Lines
	if len(data) < offset+4 {
		d.lines = rebuildLinesFromRGARaw(d.rga)
		return nil
	}
	linesLen := int(decodeUint32(data[offset : offset+4]))
	offset += 4
	d.lines = make([]string, 0, linesLen)
	for i := 0; i < linesLen; i++ {
		if len(data) < offset+4 {
			break
		}
		lineLen := int(decodeUint32(data[offset : offset+4]))
		offset += 4
		if len(data) < offset+lineLen {
			break
		}
		d.lines = append(d.lines, string(data[offset:offset+lineLen]))
		offset += lineLen
	}

	// Ensure blockTypes matches lines length
	for len(d.blockTypes) < len(d.lines) {
		d.blockTypes = append(d.blockTypes, BlockParagraph)
	}
	if len(d.blockTypes) > len(d.lines) {
		d.blockTypes = d.blockTypes[:len(d.lines)]
	}
	return nil
}

// rebuildLinesFromRGARaw rebuilds lines from an RGA's character sequence.
func rebuildLinesFromRGARaw(rga *crdt.RGA) []string {
	if rga.Length() == 0 {
		return []string{}
	}
	var full string
	for i := 0; i < rga.Length(); i++ {
		ch, err := rga.Get(i)
		if err != nil {
			break
		}
		full += ch
	}
	return splitLines(full)
}

// Slice helpers.

func insertSlice[T any](s []T, pos int, val T) []T {
	if pos >= len(s) {
		return append(s, val)
	}
	s = append(s[:pos+1], s[pos:]...)
	s[pos] = val
	return s
}

func insertSliceBlock(s []BlockType, pos int, defaultVal BlockType) []BlockType {
	if pos >= len(s) {
		return append(s, BlockParagraph)
	}
	s = append(s[:pos+1], s[pos:]...)
	s[pos] = defaultVal
	return s
}

func splitLines(s string) []string {
	if s == "" {
		return []string{}
	}
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	lines = append(lines, s[start:])
	return lines
}

func joinLines(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	result := lines[0]
	for i := 1; i < len(lines); i++ {
		result += "\n" + lines[i]
	}
	return result
}

// stringToID converts a string to a [32]byte peer ID.
func stringToID(s string) [32]byte {
	var id [32]byte
	for i := 0; i < len(s) && i < 32; i++ {
		id[i] = s[i]
	}
	return id
}

// _blockTypeStrings is used for validation during Unmarshal.
var _blockTypeStrings = []string{
	"paragraph", "heading1", "heading2", "heading3",
	"bullet_list", "numbered_list", "code_block", "quote",
}
