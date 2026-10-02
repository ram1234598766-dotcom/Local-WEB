package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"testing"
)

// flagNameRE finds a flag the docs tell a reader to pass to the node binary, e.g.
// "go run ./cmd/node --dashboard" or "./bin/node -addr 0.0.0.0:4444".
//
// It is anchored on the node invocation deliberately. A looser match swept up
// flags belonging to the cli subcommand (--help, --json) and to roadmap prose
// (--config), which are not node flags and are not bugs. Only text that actually
// invokes cmd/node or bin/node counts.
var flagNameRE = regexp.MustCompile(`(?:\./)?(?:cmd/node|bin/node)\s+--?([a-z][a-z0-9-]+)`)

// realFlagNames returns every flag name declared in main.go.
//
// The flags are declared inside main rather than at package level, so the source is
// parsed rather than flag.CommandLine being inspected. That keeps the check working
// in a test binary that never calls main.
func realFlagNames(t *testing.T) map[string]bool {
	t.Helper()
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "main.go", src, 0)
	if err != nil {
		t.Fatalf("parse main.go: %v", err)
	}

	names := map[string]bool{}
	declared := map[string]bool{"flag": true}
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok || !declared[pkg.Name] {
			return true
		}
		if len(call.Args) == 0 {
			return true
		}
		lit, ok := call.Args[0].(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		name, err := strconv.Unquote(lit.Value)
		if err != nil {
			return true
		}
		names[name] = true
		return true
	})

	if len(names) == 0 {
		t.Fatal("no flags found in main.go; the parser is broken, not the code")
	}
	return names
}

// repoRoot walks up from the test's working directory to the directory holding
// go.mod, so the docs are found regardless of where the test runs from.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatal("could not find go.mod above the test's working directory")
	return ""
}

// docFiles lists every markdown file the reader is told to trust.
func docFiles(root string) ([]string, error) {
	var out []string
	for _, pattern := range []string{
		"README.md",
		filepath.Join("docs", "*", "*.md"),
		filepath.Join("docs", "*.md"),
	} {
		matches, err := filepath.Glob(filepath.Join(root, pattern))
		if err != nil {
			return nil, err
		}
		out = append(out, matches...)
	}
	if len(out) == 0 {
		return nil, os.ErrNotExist
	}
	return out, nil
}

// TestDocumentedFlagsExist stops the docs inventing flags.
//
// This is not hypothetical. The README and two guides told readers to run
// "go run ./cmd/node --dashboard" and to change a busy port with "--listen"; the
// daemon had never defined either, so a reader following the guide got "flag
// provided but not defined" and no dashboard. A guide that is wrong about how to
// reach the dashboard is a security problem here: the dashboard is
// unauthenticated, can write files and restore backups, and its loopback default
// is the only thing keeping it off the network.
//
// The docs are read rather than trusted, because the failure mode is precisely
// that nobody remembers to update them.
func TestDocumentedFlagsExist(t *testing.T) {
	real := realFlagNames(t)

	docs, err := docFiles(repoRoot(t))
	if err != nil {
		t.Fatalf("find docs: %v", err)
	}
	if len(docs) == 0 {
		t.Fatal("no documentation files found; the glob is wrong, not the docs")
	}

	var missing []string
	seen := map[string]string{}
	for _, path := range docs {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for _, m := range flagNameRE.FindAllStringSubmatch(string(body), -1) {
			name := m[1]
			if real[name] {
				continue
			}
			// Only the first sighting per name, with its file, so the failure
			// message points somewhere actionable.
			if _, dup := seen[name]; !dup {
				seen[name] = path
				missing = append(missing, name)
			}
		}
	}

	sort.Strings(missing)
	for _, name := range missing {
		t.Errorf("%s documents --%s but the daemon defines no such flag", seen[name], name)
	}
}

// TestDashboardHasNoEnableFlag records why "-dashboard" must stay out of the docs.
//
// The dashboard is unauthenticated and can create documents, upload files and
// restore backups. Its loopback default is a safety property, so it starts
// whenever the node does rather than behind a switch someone could leave off in
// production while believing the surface was off.
func TestDashboardHasNoEnableFlag(t *testing.T) {
	real := realFlagNames(t)
	if real["dashboard"] {
		t.Error("a -dashboard flag exists; the docs say the dashboard always starts, " +
			"so either the docs are stale or the switch should go")
	}
	if !real["gui-addr"] {
		t.Error("expected a -gui-addr flag: it is the only way the dashboard's bind " +
			"address is controlled, and the guides tell readers to use it")
	}
}
