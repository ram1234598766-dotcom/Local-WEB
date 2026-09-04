package docs

import (
	"sync"
	"testing"

	"github.com/mrityunjay/LocalWEB/pkg/crdt"
)

func TestDocumentCreate(t *testing.T) {
	doc := NewDocument(DocumentConfig{
		ID:      "doc-1",
		Title:   "Test Doc",
		NodeID:  "node-1",
	})
	if doc.ID() != "doc-1" {
		t.Fatalf("expected doc ID 'doc-1', got %q", doc.ID())
	}
	if doc.Title() != "Test Doc" {
		t.Fatalf("expected title 'Test Doc', got %q", doc.Title())
	}
	if doc.Length() != 0 {
		t.Fatalf("expected length 0, got %d", doc.Length())
	}
	if doc.Version() != 0 {
		t.Fatalf("expected version 0, got %d", doc.Version())
	}
}

func TestDocumentInsert(t *testing.T) {
	doc := NewDocument(DocumentConfig{ID: "doc-1", NodeID: "node-1"})
	result := doc.Insert(0, "hello")
	if !result.Applied {
		t.Fatalf("insert failed: %v", result.Error)
	}
	if doc.Text() != "hello" {
		t.Fatalf("expected 'hello', got %q", doc.Text())
	}
	if doc.Length() != 5 {
		t.Fatalf("expected length 5, got %d", doc.Length())
	}
	if doc.Version() != 1 {
		t.Fatalf("expected version 1, got %d", doc.Version())
	}
}

func TestDocumentInsertMultiple(t *testing.T) {
	doc := NewDocument(DocumentConfig{ID: "doc-1", NodeID: "node-1"})
	doc.Insert(0, "hello")
	doc.Insert(1, "world")
	lines := doc.Lines()
	if len(lines) != 2 || lines[0] != "hello" || lines[1] != "world" {
		t.Fatalf("expected 2 lines [hello, world], got %v", lines)
	}
}

func TestDocumentInsertOutOfRange(t *testing.T) {
	doc := NewDocument(DocumentConfig{ID: "doc-1", NodeID: "node-1"})
	doc.Insert(0, "hello")
	result := doc.Insert(100, "out")
	if result.Applied {
		t.Fatal("expected insert at out-of-range position to fail")
	}
}

func TestDocumentDelete(t *testing.T) {
	doc := NewDocument(DocumentConfig{ID: "doc-1", NodeID: "node-1"})
	doc.Insert(0, "hello")
	doc.Insert(0, "world")
	doc.Insert(0, "foo")
	// Lines are: "foo", "world", "hello"
	result := doc.Delete(1)
	if !result.Applied {
		t.Fatalf("delete failed: %v", result.Error)
	}
	lines := doc.Lines()
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
	if lines[0] != "foo" || lines[1] != "hello" {
		t.Fatalf("unexpected lines: %v", lines)
	}
}

func TestDocumentDeleteOutOfRange(t *testing.T) {
	doc := NewDocument(DocumentConfig{ID: "doc-1", NodeID: "node-1"})
	result := doc.Delete(0)
	if result.Applied {
		t.Fatal("expected delete at out-of-range position to fail")
	}
}

func TestDocumentFormatBlock(t *testing.T) {
	doc := NewDocument(DocumentConfig{ID: "doc-1", NodeID: "node-1"})
	doc.Insert(0, "heading")
	result := doc.FormatBlock(0, BlockHeading1)
	if !result.Applied {
		t.Fatalf("format block failed: %v", result.Error)
	}
	btypes := doc.BlockTypes()
	if btypes[0] != BlockHeading1 {
		t.Fatalf("expected block type heading1, got %v", btypes[0])
	}
}

func TestDocumentFormatBlockOutOfRange(t *testing.T) {
	doc := NewDocument(DocumentConfig{ID: "doc-1", NodeID: "node-1"})
	result := doc.FormatBlock(5, BlockHeading1)
	if result.Applied {
		t.Fatal("expected format at out-of-range position to fail")
	}
}

func TestDocumentUndoRedo(t *testing.T) {
	doc := NewDocument(DocumentConfig{ID: "doc-1", NodeID: "node-1"})
	doc.Insert(0, "hello")
	doc.Insert(0, "world")
	// Lines: "world", "hello"
	if !doc.CanUndo() {
		t.Fatal("expected undo to be available")
	}
	if doc.CanRedo() {
		t.Fatal("expected redo to be unavailable")
	}

	r := doc.Undo()
	if !r.Applied {
		t.Fatalf("undo failed: %v", r.Error)
	}
	lines := doc.Lines()
	if len(lines) != 1 || lines[0] != "hello" {
		t.Fatalf("after undo, expected ['hello'], got %v", lines)
	}

	if !doc.CanRedo() {
		t.Fatal("expected redo to be available after undo")
	}

	r2 := doc.Redo()
	if !r2.Applied {
		t.Fatalf("redo failed: %v", r2.Error)
	}
	lines = doc.Lines()
	if len(lines) != 2 || lines[0] != "world" || lines[1] != "hello" {
		t.Fatalf("after redo, expected ['world','hello'], got %v", lines)
	}
}

func TestDocumentUndoNothing(t *testing.T) {
	doc := NewDocument(DocumentConfig{ID: "doc-1", NodeID: "node-1"})
	r := doc.Undo()
	if r.Applied {
		t.Fatal("expected undo on empty document to fail")
	}
}

func TestDocumentRedoNothing(t *testing.T) {
	doc := NewDocument(DocumentConfig{ID: "doc-1", NodeID: "node-1"})
	doc.Insert(0, "hello")
	doc.Undo()
	r := doc.Redo()
	if !r.Applied {
		t.Fatalf("redo after undo should succeed: %v", r.Error)
	}
	// Redo again should fail
	r2 := doc.Redo()
	if r2.Applied {
		t.Fatal("expected second redo to fail")
	}
}

func TestDocumentMerge(t *testing.T) {
	docA := NewDocument(DocumentConfig{ID: "doc-1", NodeID: "node-a"})
	docB := NewDocument(DocumentConfig{ID: "doc-1", NodeID: "node-b"})

	docA.Insert(0, "hello")
	docB.Insert(0, "world")

	// Merge B into A
	opsB := []Op{
		{Type: OpInsert, DocID: "doc-1", AuthorID: stringToID("node-b"), Position: 0, Value: "world", Timestamp: 1},
	}
	if err := docA.Merge(opsB); err != nil {
		t.Fatalf("merge failed: %v", err)
	}
	lines := docA.Lines()
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines after merge, got %d: %v", len(lines), lines)
	}
}

func TestDocumentMarshalRoundTrip(t *testing.T) {
	doc := NewDocument(DocumentConfig{ID: "doc-1", Title: "Round Trip", NodeID: "node-1"})
	doc.Insert(0, "hello world")
	doc.FormatBlock(0, BlockHeading1)

	data := doc.Marshal()
	doc2 := NewDocument(DocumentConfig{NodeID: "node-2"})
	if err := doc2.Unmarshal(data); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if doc2.ID() != "doc-1" {
		t.Fatalf("expected ID 'doc-1', got %q", doc2.ID())
	}
	if doc2.Title() != "Round Trip" {
		t.Fatalf("expected title 'Round Trip', got %q", doc2.Title())
	}
	lines := doc2.Lines()
	if len(lines) != 1 || lines[0] != "hello world" {
		t.Fatalf("expected ['hello world'], got %v", lines)
	}
	btypes := doc2.BlockTypes()
	if len(btypes) != 1 || btypes[0] != BlockHeading1 {
		t.Fatalf("expected [heading1], got %v", btypes)
	}
}

func TestDocumentRGABacking(t *testing.T) {
	doc := NewDocument(DocumentConfig{ID: "doc-rga", NodeID: "node-1"})
	doc.Insert(0, "abc")
	if doc.rga.Length() != 3 {
		t.Fatalf("expected RGA length 3, got %d", doc.rga.Length())
	}
	// Verify characters are in RGA
	for i := 0; i < 3; i++ {
		ch, err := doc.rga.Get(i)
		if err != nil {
			t.Fatalf("RGA Get(%d): %v", i, err)
		}
		expected := "abc"[i]
		if ch != string(expected) {
			t.Fatalf("expected RGA char %q at %d, got %q", string(expected), i, ch)
		}
	}
}

func TestDocumentCRDTMerge(t *testing.T) {
	rgaA := crdt.NewRGA("node-a")
	rgaB := crdt.NewRGA("node-b")

	rgaA.Insert("head", "h")
	rgaA.Insert("h", "e")
	rgaB.Insert("head", "w")
	rgaB.Insert("w", "o")

	rgaA.Merge(rgaB)

	expected := "hewo"
	if rgaA.Length() != 4 {
		t.Fatalf("expected length 4, got %d", rgaA.Length())
	}
	var got string
	for i := 0; i < rgaA.Length(); i++ {
		ch, _ := rgaA.Get(i)
		got += ch
	}
	if got != expected {
		t.Fatalf("expected %q, got %q", expected, got)
	}
}

func TestDocumentEmptyText(t *testing.T) {
	doc := NewDocument(DocumentConfig{ID: "doc-empty", NodeID: "node-1"})
	if doc.Text() != "" {
		t.Fatalf("expected empty text, got %q", doc.Text())
	}
	if len(doc.Lines()) != 0 {
		t.Fatalf("expected 0 lines, got %d", len(doc.Lines()))
	}
}

func TestDocumentStateSnapshot(t *testing.T) {
	doc := NewDocument(DocumentConfig{ID: "doc-snap", Title: "Snapshot", NodeID: "node-1"})
	doc.Insert(0, "line1")
	doc.Insert(1, "line2")

	state := doc.State()
	if state.DocID != "doc-snap" {
		t.Fatalf("expected DocID 'doc-snap', got %q", state.DocID)
	}
	if state.Title != "Snapshot" {
		t.Fatalf("expected title 'Snapshot', got %q", state.Title)
	}
	if len(state.Content) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(state.Content))
	}
	if state.Version != 2 {
		t.Fatalf("expected version 2, got %d", state.Version)
	}
}

func TestDocumentConcurrentAccess(t *testing.T) {
	doc := NewDocument(DocumentConfig{ID: "doc-conc", NodeID: "node-1"})
	var wg sync.WaitGroup
	wg.Add(10)
	for i := 0; i < 10; i++ {
		go func(idx int) {
			defer wg.Done()
			doc.Insert(idx, "x")
		}(i)
	}
	wg.Wait()
	_ = doc.Lines()
	_ = doc.Text()
}

func TestDocumentVersionIncrements(t *testing.T) {
	doc := NewDocument(DocumentConfig{ID: "doc-v", NodeID: "node-1"})
	for i := 0; i < 5; i++ {
		if doc.Version() != int64(i) {
			t.Fatalf("expected version %d, got %d", i, doc.Version())
		}
		doc.Insert(0, "x")
	}
	if doc.Version() != 5 {
		t.Fatalf("expected version 5, got %d", doc.Version())
	}
}

func TestDocumentMarshalEmptyRoundTrip(t *testing.T) {
	doc := NewDocument(DocumentConfig{ID: "doc-e", NodeID: "node-1"})
	data := doc.Marshal()
	doc2 := NewDocument(DocumentConfig{NodeID: "node-2"})
	if err := doc2.Unmarshal(data); err != nil {
		t.Fatalf("unmarshal empty doc: %v", err)
	}
	if doc2.ID() != "doc-e" {
		t.Fatalf("expected 'doc-e', got %q", doc2.ID())
	}
}

