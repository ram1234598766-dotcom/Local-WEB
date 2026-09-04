package docs

import (
	"strings"
	"testing"
)

func TestExportToMarkdown(t *testing.T) {
	doc := NewDocument(DocumentConfig{ID: "doc-md", Title: "My Doc", NodeID: "node-1"})
	doc.Insert(0, "First paragraph")
	doc.Insert(1, "Second paragraph")
	doc.FormatBlock(0, BlockHeading1)

	e := NewExport(doc)
	md := e.ToMarkdown()

	if !strings.Contains(md, "# My Doc") {
		t.Fatalf("expected title 'My Doc' in markdown, got:\n%s", md)
	}
	if !strings.Contains(md, "# First paragraph") {
		t.Fatalf("expected heading in markdown, got:\n%s", md)
	}
	if !strings.Contains(md, "Second paragraph") {
		t.Fatalf("expected 'Second paragraph' in markdown, got:\n%s", md)
	}
}

func TestExportToHTML(t *testing.T) {
	doc := NewDocument(DocumentConfig{ID: "doc-html", Title: "HTML Doc", NodeID: "node-1"})
	doc.Insert(0, "hello world")
	doc.FormatBlock(0, BlockHeading1)

	e := NewExport(doc)
	html := e.ToHTML()

	if !strings.Contains(html, "<!DOCTYPE html>") {
		t.Fatalf("expected DOCTYPE in HTML output")
	}
	if !strings.Contains(html, "<h1>hello world</h1>") {
		t.Fatalf("expected h1 tag in HTML, got:\n%s", html)
	}
	if !strings.Contains(html, "HTML Doc") {
		t.Fatalf("expected title in HTML, got:\n%s", html)
	}
	if !strings.Contains(html, "<meta charset=\"UTF-8\">") {
		t.Fatalf("expected charset meta tag")
	}
}

func TestExportToFormat(t *testing.T) {
	doc := NewDocument(DocumentConfig{ID: "doc-fmt", NodeID: "node-1"})
	doc.Insert(0, "test content")

	e := NewExport(doc)
	md := e.ToFormat(ExportMarkdown)
	if !strings.Contains(md, "test content") {
		t.Fatalf("expected 'test content' in markdown")
	}

	html := e.ToFormat(ExportHTML)
	if !strings.Contains(html, "<!DOCTYPE html>") {
		t.Fatalf("expected DOCTYPE in HTML")
	}
}

func TestExportEmptyDocument(t *testing.T) {
	doc := NewDocument(DocumentConfig{ID: "doc-empty", NodeID: "node-1"})
	e := NewExport(doc)
	md := e.ToMarkdown()
	if md != "" {
		t.Fatalf("expected empty markdown, got %q", md)
	}
	html := e.ToHTML()
	if !strings.Contains(html, "<!DOCTYPE html>") {
		t.Fatalf("expected DOCTYPE even in empty document")
	}
}

func TestExportFormatBlockTypes(t *testing.T) {
	doc := NewDocument(DocumentConfig{ID: "doc-blocks", NodeID: "node-1"})
	doc.Insert(0, "h1 text")
	doc.Insert(1, "h2 text")
	doc.Insert(2, "bullet item")
	doc.Insert(3, "code line")
	doc.Insert(4, "quote text")
	doc.Insert(5, "plain text")

	doc.FormatBlock(0, BlockHeading1)
	doc.FormatBlock(1, BlockHeading2)
	doc.FormatBlock(2, BlockBulletList)
	doc.FormatBlock(3, BlockCodeBlock)
	doc.FormatBlock(4, BlockQuote)

	e := NewExport(doc)
	md := e.ToMarkdown()

	if !strings.Contains(md, "# h1 text") {
		t.Fatalf("expected H1, got:\n%s", md)
	}
	if !strings.Contains(md, "## h2 text") {
		t.Fatalf("expected H2, got:\n%s", md)
	}
	if !strings.Contains(md, "- bullet item") {
		t.Fatalf("expected bullet, got:\n%s", md)
	}
	if !strings.Contains(md, "```\ncode line\n```") {
		t.Fatalf("expected code block, got:\n%s", md)
	}
	if !strings.Contains(md, "> quote text") {
		t.Fatalf("expected quote, got:\n%s", md)
	}
}

func TestExportHTMLEscaping(t *testing.T) {
	doc := NewDocument(DocumentConfig{ID: "doc-esc", NodeID: "node-1"})
	doc.Insert(0, "<script>alert('xss')</script>")
	doc.FormatBlock(0, BlockParagraph)

	e := NewExport(doc)
	html := e.ToHTML()

	if strings.Contains(html, "<script>") {
		t.Fatalf("HTML should escape <script>, got:\n%s", html)
	}
	if !strings.Contains(html, "&lt;script&gt;") {
		t.Fatalf("expected escaped script tag")
	}
}

func TestExportToBytes(t *testing.T) {
	doc := NewDocument(DocumentConfig{ID: "doc-bytes", NodeID: "node-1"})
	doc.Insert(0, "bytes test")

	mdBytes := ExportToBytes(doc, ExportMarkdown)
	if len(mdBytes) == 0 {
		t.Fatal("expected non-empty markdown bytes")
	}
	if string(mdBytes) != NewExport(doc).ToMarkdown() {
		t.Fatal("ExportToBytes markdown mismatch")
	}

	htmlBytes := ExportToBytes(doc, ExportHTML)
	if len(htmlBytes) == 0 {
		t.Fatal("expected non-empty HTML bytes")
	}
}

func TestPlainText(t *testing.T) {
	doc := NewDocument(DocumentConfig{ID: "doc-plain", NodeID: "node-1"})
	doc.Insert(0, "line 1")
	doc.Insert(1, "line 2")
	pt := PlainText(doc)
	if pt != "line 1\nline 2" {
		t.Fatalf("expected 'line 1\\nline 2', got %q", pt)
	}
}

func TestWordCount(t *testing.T) {
	doc := NewDocument(DocumentConfig{ID: "doc-wc", NodeID: "node-1"})
	doc.Insert(0, "hello world foo bar")
	wc := WordCount(doc)
	if wc != 4 {
		t.Fatalf("expected 4 words, got %d", wc)
	}

	doc2 := NewDocument(DocumentConfig{ID: "doc-wc2", NodeID: "node-1"})
	if WordCount(doc2) != 0 {
		t.Fatalf("expected 0 words for empty doc")
	}
}

func TestCharCount(t *testing.T) {
	doc := NewDocument(DocumentConfig{ID: "doc-cc", NodeID: "node-1"})
	doc.Insert(0, "abc")
	cc := CharCount(doc)
	if cc != 3 {
		t.Fatalf("expected 3 chars, got %d", cc)
	}
}

func TestLineCount(t *testing.T) {
	doc := NewDocument(DocumentConfig{ID: "doc-lc", NodeID: "node-1"})
	doc.Insert(0, "line1")
	doc.Insert(1, "line2")
	doc.Insert(2, "line3")
	lc := LineCount(doc)
	if lc != 3 {
		t.Fatalf("expected 3 lines, got %d", lc)
	}
}

func TestDiff(t *testing.T) {
	oldState := DocumentState{Content: []string{"hello", "world", "foo"}}
	newState := DocumentState{Content: []string{"hello", "bar", "foo"}}
	diff := Diff(oldState, newState)
	foundAdd, foundDel := false, false
	for _, d := range diff {
		if d == "+ bar" {
			foundAdd = true
		}
		if d == "- world" {
			foundDel = true
		}
	}
	if !foundAdd {
		t.Fatalf("expected '+ bar' in diff, got: %v", diff)
	}
	if !foundDel {
		t.Fatalf("expected '- world' in diff, got: %v", diff)
	}
}

func TestDiffEmpty(t *testing.T) {
	a := DocumentState{Content: []string{"a"}}
	b := DocumentState{Content: []string{"a"}}
	diff := Diff(a, b)
	if len(diff) != 0 {
		t.Fatalf("expected no diff, got: %v", diff)
	}
}

func TestMarkdownToPlainText(t *testing.T) {
	md := `# Heading
This is a paragraph.
> A quote
- bullet item
`+ "`" + `code` + "`" + `
`
	pt := MarkdownToPlainText(md)
	if !strings.Contains(pt, "Heading") {
		t.Fatalf("expected 'Heading' in plain text, got %q", pt)
	}
	if !strings.Contains(pt, "This is a paragraph") {
		t.Fatalf("expected paragraph in plain text")
	}
	if strings.Contains(pt, "#") {
		t.Fatalf("plain text should not contain '#'")
	}
}

func TestMergeMarkdown(t *testing.T) {
	a := DocumentState{Title: "Doc A", Content: []string{"from A", "from A2"}}
	b := DocumentState{Title: "Doc B", Content: []string{"from B", "from B2"}}
	result := MergeMarkdown(a, b)
	if !strings.Contains(result, "from A") {
		t.Fatalf("expected 'from A' in merged markdown")
	}
	if !strings.Contains(result, "from B") {
		t.Fatalf("expected 'from B' in merged markdown")
	}
	if !strings.Contains(result, "Doc A") {
		t.Fatalf("expected 'Doc A' in merged markdown")
	}
}

func TestExportBlockTypeString(t *testing.T) {
	if BlockHeading1.String() != "heading1" {
		t.Fatalf("expected 'heading1', got %q", BlockHeading1.String())
	}
	if BlockParagraph.String() != "paragraph" {
		t.Fatalf("expected 'paragraph', got %q", BlockParagraph.String())
	}
	if ParseBlockType("heading1") != BlockHeading1 {
		t.Fatal("parse heading1 failed")
	}
	if ParseBlockType("unknown") != BlockParagraph {
		t.Fatal("parse unknown should default to paragraph")
	}
}

func TestExportFormatString(t *testing.T) {
	if ExportMarkdown.String() != "markdown" {
		t.Fatalf("expected 'markdown', got %q", ExportMarkdown.String())
	}
	if ExportHTML.String() != "html" {
		t.Fatalf("expected 'html', got %q", ExportHTML.String())
	}
}

