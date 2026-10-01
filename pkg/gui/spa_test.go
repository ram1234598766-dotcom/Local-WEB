package gui

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// The shipped SPA is embedded into the binary and served verbatim, so a
// JavaScript syntax error means the GUI silently fails to load while every Go
// test still passes. Two such errors shipped: a stray template terminator
// opened a literal that swallowed the rest of the file, and a method was missing
// its closing brace. Neither was caught by any Go test.
//
// These checks are heuristic by necessity - Go cannot parse JavaScript - but
// they catch the specific defect that occurred, and they are cheap.

// standaloneTerminator matches a line containing nothing but a template
// literal terminator, e.g. "`;" possibly indented.
var standaloneTerminator = regexp.MustCompile(`^\s*` + "`" + `\s*;?\s*$`)

func TestEmbeddedSPANoStrayTemplateTerminators(t *testing.T) {
	src := string(appJS)

	lines := strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n")

	var closers []int
	for i, line := range lines {
		if standaloneTerminator.MatchString(line) {
			closers = append(closers, i)
		}
	}

	// Two terminators in a row with nothing between them means the second one
	// opened a template literal that is never closed. That single character
	// swallowed every following line, so the whole file stopped parsing.
	for k := 1; k < len(closers); k++ {
		prev, cur := closers[k-1], closers[k]
		between := lines[prev+1 : cur]
		blank := true
		for _, l := range between {
			if strings.TrimSpace(l) != "" {
				blank = false
				break
			}
		}
		if blank {
			t.Errorf(
				"lines %d and %d are consecutive template terminators with nothing between them; "+
					"the second opens an unterminated template literal and the rest of the file will not parse",
				prev+1, cur+1,
			)
		}
	}
}

// A class method at two-space indentation must be preceded by balanced braces.
// This catches a method that is missing its closing brace, which otherwise makes
// every following method appear nested and illegal.
func TestEmbeddedSPAClassMethodBracesBalanced(t *testing.T) {
	src := string(appJS)
	lines := strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n")

	depth := 0
	inTemplate := false
	methodDecl := regexp.MustCompile(`^  (static )?(async )?[A-Za-z_$][\w$]*\s*\(`)
	for i, line := range lines {
		// Depth before this line's own braces. A method declared at
		// class-body indentation must sit directly inside a class body, i.e.
		// depth 1. Checking before the line matters because the method's own
		// opening brace is counted while scanning it.
		depthBefore := depth

		// Strip line comments and string/template contents so braces inside them
		// are not counted.
		inBlockComment := false
		for j := 0; j < len(line); j++ {
			c := line[j]
			if inBlockComment {
				if c == '*' && j+1 < len(line) && line[j+1] == '/' {
					inBlockComment = false
					j++
				}
				continue
			}
			if inTemplate {
				if c == '\\' {
					j++
					continue
				}
				if c == '`' {
					inTemplate = false
				}
				continue
			}
			switch {
			case c == '/' && j+1 < len(line) && line[j+1] == '/':
				j = len(line)
			case c == '/' && j+1 < len(line) && line[j+1] == '*':
				inBlockComment = true
				j++
			case c == '"' || c == '\'':
				q := c
				j++
				for j < len(line) {
					if line[j] == '\\' {
						j++
					} else if line[j] == q {
						break
					}
					j++
				}
			case c == '`':
				inTemplate = true
			case c == '{':
				depth++
			case c == '}':
				depth--
			}
		}

		if depthBefore != 1 && methodDecl.MatchString(line) {
			t.Errorf("line %d declares a class method while brace depth is %d, not 1: %q",
				i+1, depthBefore, strings.TrimSpace(line))
		}
	}

	if depth != 0 {
		t.Errorf("brace depth ends at %d, not 0: the file has an unbalanced brace", depth)
	}
}

func TestEmbeddedSPAPresent(t *testing.T) {
	if len(appJS) == 0 {
		t.Fatal("app.js is not embedded; the GUI would serve an empty script")
	}
	if _, err := os.Stat("static/app.js"); err != nil {
		t.Errorf("static/app.js unreadable: %v", err)
	}
}
