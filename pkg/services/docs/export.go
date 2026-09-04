package docs

import (
	"bytes"
	"fmt"
	"html"
	"strings"
)

// ExportFormat specifies the output format for document export.
type ExportFormat uint8

const (
	ExportMarkdown ExportFormat = iota
	ExportHTML
)

func (f ExportFormat) String() string {
	switch f {
	case ExportHTML:
		return "html"
	default:
		return "markdown"
	}
}

// Export converts a document to the specified format.
type Export struct {
	doc *Document
}

// NewExport creates an exporter for a document.
func NewExport(doc *Document) *Export {
	return &Export{doc: doc}
}

// ToMarkdown exports the document as Markdown text.
func (e *Export) ToMarkdown() string {
	lines := e.doc.Lines()
	blockTypes := e.doc.BlockTypes()
	title := e.doc.Title()

	var sb strings.Builder
	if title != "" {
		sb.WriteString("# ")
		sb.WriteString(title)
		sb.WriteString("\n\n")
	}

	for i, line := range lines {
		if i < len(blockTypes) {
			sb.WriteString(formatMarkdownLine(line, blockTypes[i], i))
		} else {
			sb.WriteString(line)
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

// ToHTML exports the document as HTML.
func (e *Export) ToHTML() string {
	lines := e.doc.Lines()
	blockTypes := e.doc.BlockTypes()
	title := e.doc.Title()

	var sb strings.Builder
	sb.WriteString("<!DOCTYPE html>\n<html lang=\"en\">\n<head>\n")
	sb.WriteString("  <meta charset=\"UTF-8\">\n")
	sb.WriteString("  <meta name=\"viewport\" content=\"width=device-width, initial-scale=1.0\">\n")
	if title != "" {
		sb.WriteString("  <title>")
		sb.WriteString(html.EscapeString(title))
		sb.WriteString("</title>\n")
	}
	sb.WriteString("  <style>\n")
	sb.WriteString("    body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; max-width: 800px; margin: 0 auto; padding: 2rem; line-height: 1.6; }\n")
	sb.WriteString("    h1 { border-bottom: 1px solid #eaecef; padding-bottom: 0.3em; }\n")
	sb.WriteString("    h2 { border-bottom: 1px solid #eaecef; padding-bottom: 0.3em; }\n")
	sb.WriteString("    pre { background: #f6f8fa; padding: 1rem; border-radius: 6px; overflow-x: auto; }\n")
	sb.WriteString("    blockquote { border-left: 4px solid #dfe2e5; padding-left: 1rem; color: #6a737d; }\n")
	sb.WriteString("  </style>\n")
	sb.WriteString("</head>\n<body>\n")

	if title != "" {
		sb.WriteString("<h1>")
		sb.WriteString(html.EscapeString(title))
		sb.WriteString("</h1>\n")
	}

	for i, line := range lines {
		bt := BlockParagraph
		if i < len(blockTypes) {
			bt = blockTypes[i]
		}
		sb.WriteString(formatHTMLLine(line, bt))
		sb.WriteString("\n")
	}

	sb.WriteString("</body>\n</html>\n")
	return sb.String()
}

// ToFormat exports the document to the specified format.
func (e *Export) ToFormat(format ExportFormat) string {
	switch format {
	case ExportHTML:
		return e.ToHTML()
	default:
		return e.ToMarkdown()
	}
}

// formatMarkdownLine formats a single line in Markdown.
func formatMarkdownLine(line string, bt BlockType, index int) string {
	switch bt {
	case BlockHeading1:
		return "# " + line
	case BlockHeading2:
		return "## " + line
	case BlockHeading3:
		return "### " + line
	case BlockBulletList:
		return "- " + line
	case BlockNumberedList:
		return fmt.Sprintf("%d. %s", index+1, line)
	case BlockCodeBlock:
		return "```\n" + line + "\n```"
	case BlockQuote:
		return "> " + line
	default:
		return line
	}
}

// formatHTMLLine formats a single line as an HTML element.
func formatHTMLLine(line string, bt BlockType) string {
	escaped := html.EscapeString(line)
	switch bt {
	case BlockHeading1:
		return fmt.Sprintf("<h1>%s</h1>", escaped)
	case BlockHeading2:
		return fmt.Sprintf("<h2>%s</h2>", escaped)
	case BlockHeading3:
		return fmt.Sprintf("<h3>%s</h3>", escaped)
	case BlockBulletList:
		return fmt.Sprintf("<ul><li>%s</li></ul>", escaped)
	case BlockNumberedList:
		return fmt.Sprintf("<ol><li>%s</li></ol>", escaped)
	case BlockCodeBlock:
		return fmt.Sprintf("<pre><code>%s</code></pre>", escaped)
	case BlockQuote:
		return fmt.Sprintf("<blockquote>%s</blockquote>", escaped)
	default:
		return fmt.Sprintf("<p>%s</p>", escaped)
	}
}

// ExportToBytes exports the document to bytes in the specified format.
func ExportToBytes(doc *Document, format ExportFormat) []byte {
	e := NewExport(doc)
	return []byte(e.ToFormat(format))
}

// PlainText extracts plain text from a document without formatting.
func PlainText(doc *Document) string {
	return doc.Text()
}

// WordCount returns the word count of a document.
func WordCount(doc *Document) int {
	text := doc.Text()
	if text == "" {
		return 0
	}
	words := strings.Fields(text)
	return len(words)
}

// CharCount returns the character count of a document.
func CharCount(doc *Document) int {
	return doc.Length()
}

// LineCount returns the number of lines in a document.
func LineCount(doc *Document) int {
	return len(doc.Lines())
}

// Diff computes a simple line-level diff between two document states.
func Diff(oldState, newState DocumentState) []string {
	oldLines := oldState.Content
	newLines := newState.Content
	var result []string

	oldSet := make(map[string]bool)
	for _, l := range oldLines {
		oldSet[l] = true
	}
	for _, l := range newLines {
		if !oldSet[l] {
			result = append(result, "+ "+l)
		}
	}
	newSet := make(map[string]bool)
	for _, l := range newLines {
		newSet[l] = true
	}
	for _, l := range oldLines {
		if !newSet[l] {
			result = append(result, "- "+l)
		}
	}
	if result == nil {
		result = []string{}
	}
	return result
}

// MarkdownToPlainText converts a Markdown string to plain text.
func MarkdownToPlainText(md string) string {
	lines := strings.Split(md, "\n")
	var out []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			line = strings.TrimLeft(line, "# ")
			line = strings.TrimSpace(line)
		}
		if strings.HasPrefix(line, ">") {
			line = strings.TrimLeft(line, "> ")
			line = strings.TrimSpace(line)
		}
		line = strings.TrimRight(line, "*")
		if strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") {
			line = line[2:]
		}
		if idx := strings.Index(line, ". "); idx >= 0 && idx < 4 {
			line = line[idx+2:]
		}
		line = strings.Trim(line, "`")
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

// MergeMarkdown exports two document states as a combined Markdown diff.
func MergeMarkdown(a, b DocumentState) string {
	var sb bytes.Buffer
	sb.WriteString(fmt.Sprintf("# %s (merged)\n\n", a.Title))
	sb.WriteString("## From Document A\n\n")
	for _, line := range a.Content {
		sb.WriteString(line)
		sb.WriteString("\n")
	}
	sb.WriteString("\n## From Document B\n\n")
	for _, line := range b.Content {
		sb.WriteString(line)
		sb.WriteString("\n")
	}
	return sb.String()
}
