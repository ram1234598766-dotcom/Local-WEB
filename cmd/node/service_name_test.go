package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// wxsAttr returns the value of attr on the self-closing element whose tag is
// tag, failing if the file has no such element carrying that attribute.
//
// The regex stops at the first ">" so it cannot run past the element it is
// aimed at, which matters because localweb.wxs is mostly long explanatory
// comments containing quoted words that would otherwise match loosely.
func wxsAttr(t *testing.T, source, tag, attr string) string {
	t.Helper()
	re := regexp.MustCompile(`<` + tag + `\b[^>]*?\s` + attr + `="([^"]*)"`)
	m := re.FindStringSubmatch(source)
	if m == nil {
		t.Fatalf("localweb.wxs has no <%s ... %s=\"...\"> element", tag, attr)
	}
	return m[1]
}

func readRepoFile(t *testing.T, parts ...string) string {
	t.Helper()
	path := filepath.Join(append([]string{repoRoot(t)}, parts...)...)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(raw)
}

// TestServiceNameMatchesMSI pins serviceName to every place the MSI names the
// service.
//
// The three elements are deliberately redundant in WiX and have to agree:
// ServiceInstall creates the service, ServiceControl starts and stops it, and
// util:ServiceConfig writes the recovery actions to it. Two of the three
// agreeing is enough for the install to appear to work and then fail later,
// which is the harder version of this bug to diagnose.
func TestServiceNameMatchesMSI(t *testing.T) {
	wxs := readRepoFile(t, "installers", "windows", "localweb.wxs")

	checks := []struct {
		tag  string
		attr string
	}{
		{"ServiceInstall", "Name"},
		{"ServiceControl", "Name"},
		{"util:ServiceConfig", "ServiceName"},
	}
	for _, c := range checks {
		if got := wxsAttr(t, wxs, c.tag, c.attr); got != serviceName {
			t.Errorf("localweb.wxs <%s %s=%q> = %q", c.tag, c.attr, got, serviceName)
		}
	}
}

// TestServiceNameMatchesNSIS holds the second Windows installer to the same
// name.
//
// installers/windows/localweb.nsi is a separate package that also defines
// SERVICE_NAME and also stops and deletes a service by that name. It is not the
// artifact this release ships, so it is not fixed here, but a typo in it is the
// same bug with a different failure message.
func TestServiceNameMatchesNSIS(t *testing.T) {
	nsi := readRepoFile(t, "installers", "windows", "localweb.nsi")

	re := regexp.MustCompile(`(?m)^\s*!define\s+SERVICE_NAME\s+"([^"]*)"`)
	m := re.FindStringSubmatch(nsi)
	if m == nil {
		t.Fatal("localweb.nsi has no !define SERVICE_NAME")
	}
	if m[1] != serviceName {
		t.Errorf("localweb.nsi SERVICE_NAME = %q, want %q", m[1], serviceName)
	}
}

// TestMSIArgumentsSurviveSubcommandStrip checks the packaged command line
// against the real parsing path.
//
// ServiceInstall passes "node --data-dir C:\ProgramData\LocalWEB". The leading
// verb exists for compatibility with the documented invocation and
// stripLeadingSubcommand removes it; main_test.go already covers that function,
// so this asserts the packaging half. What matters is that the first token
// left over is a flag the node actually declares. If it is not, the node exits
// on flag.Parse and the service fails to start, which from the MSI's point of
// view is an indistinguishable Error 1920.
func TestMSIArgumentsSurviveSubcommandStrip(t *testing.T) {
	wxs := readRepoFile(t, "installers", "windows", "localweb.wxs")
	arguments := wxsAttr(t, wxs, "ServiceInstall", "Arguments")

	fields := strings.Fields(arguments)
	if len(fields) < 2 {
		t.Fatalf("ServiceInstall Arguments = %q, want a verb and at least one flag", arguments)
	}
	// The verb has to be a non-flag, because stripLeadingSubcommand only strips
	// a leading non-flag. A leading flag here would mean the verb was already
	// dropped and the rest of the string is what actually gets parsed.
	if strings.HasPrefix(fields[0], "-") {
		t.Errorf("ServiceInstall Arguments = %q starts with a flag, so there is no "+
			"compatibility verb to strip", arguments)
	}

	// Run the real strip rather than reimplementing it.
	restore := os.Args
	defer func() { os.Args = restore }()
	os.Args = append([]string{"localweb.exe"}, fields...)
	stripLeadingSubcommand()

	remaining := os.Args[1:]
	if len(remaining) != len(fields)-1 {
		t.Fatalf("strip left %v from %q, want the verb removed", remaining, fields)
	}
	if !strings.HasPrefix(remaining[0], "-") {
		t.Fatalf("after the strip the node would parse %q as a positional value", remaining[0])
	}

	// The flag the package passes has to be one main.go declares, or the service
	// dies on startup with "flag provided but not defined".
	known := realFlagNames(t)
	name := strings.TrimLeft(remaining[0], "-")
	if !known[name] {
		t.Errorf("MSI passes -%s, which main.go does not declare", name)
	}
}

// TestRunNodeTakesCallerContext guards the seam the Windows service relies on.
//
// The service hands runNode a context it cancels on Stop, and only that
// arrangement lets the handler wait for a clean shutdown instead of exiting
// mid-flush. A signature that went back to owning its own context would still
// compile and would silently restore the bug this split exists to fix.
func TestRunNodeTakesCallerContext(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	if !strings.Contains(string(src), "func runNode(ctx context.Context)") {
		t.Error("main.go no longer declares func runNode(ctx context.Context); " +
			"the Windows service needs it to accept a context it cancels")
	}
}
