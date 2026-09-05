package main

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ram1234598766-dotcom/Local-WEB/pkg/crypto"
)

func TestRunInitCreatesIdentity(t *testing.T) {
	dir := t.TempDir()

	input := strings.NewReader("\n\n")
	scanner := bufio.NewReader(input)
	err := runInit(scanner, dir)
	if err != nil {
		t.Fatalf("runInit failed: %v", err)
	}

	pub, _, err := crypto.LoadOrGenerateIdentity(dir)
	if err != nil {
		t.Fatalf("identity not created: %v", err)
	}
	if pub == ([32]byte{}) {
		t.Fatal("public key is zero")
	}

	data, err := os.ReadFile(filepath.Join(dir, "identity.json"))
	if err != nil {
		t.Fatalf("identity.json not found: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("identity.json is empty")
	}
}

func TestRunInitExistingIdentityNotOverwritten(t *testing.T) {
	dir := t.TempDir()

	pub, _, err := crypto.LoadOrGenerateIdentity(dir)
	if err != nil {
		t.Fatalf("setup identity: %v", err)
	}

	input := strings.NewReader("n\n")
	scanner := bufio.NewReader(input)
	err = runInit(scanner, dir)
	if err != nil {
		t.Fatalf("runInit should not fail: %v", err)
	}

	pub2, _, err := crypto.LoadOrGenerateIdentity(dir)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if pub != pub2 {
		t.Error("identity was overwritten")
	}
}

func TestRunInitCustomDataDir(t *testing.T) {
	dir := t.TempDir()

	input := strings.NewReader(filepath.Join(dir, "custom") + "\n\n")
	scanner := bufio.NewReader(input)
	err := runInit(scanner, "")
	if err != nil {
		t.Fatalf("runInit with custom dir: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "custom", "identity.json")); err != nil {
		t.Fatalf("custom identity.json not found: %v", err)
	}
}

func TestInitOutput(t *testing.T) {
	dir := t.TempDir()

	var buf strings.Builder
	input := strings.NewReader("\n\n")
	scanner := bufio.NewReader(input)

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := runInit(scanner, dir)

	w.Close()
	os.Stdout = oldStdout

	out, _ := bufio.NewReader(r).ReadString('\n')
	buf.WriteString(out)

	if err != nil {
		t.Fatalf("runInit failed: %v", err)
	}
	if !strings.Contains(buf.String(), "Local-WEB") {
		t.Error("expected output to mention Local-WEB")
	}
}

func TestRunInitHelpText(t *testing.T) {
	help := initHelpText()
	if !strings.Contains(help, "data directory") {
		t.Error("help text should mention data directory")
	}
	if !strings.Contains(help, "Ed25519") {
		t.Error("help text should mention Ed25519")
	}
	if !strings.Contains(help, "keypair") {
		t.Error("help text should mention keypair")
	}
}

func TestRunInitCustomName(t *testing.T) {
	dir := t.TempDir()

	input := strings.NewReader("\nmy-custom-node\n")
	scanner := bufio.NewReader(input)
	err := runInit(scanner, dir)
	if err != nil {
		t.Fatalf("runInit: %v", err)
	}

	configData, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatalf("config.json not found: %v", err)
	}
	if !strings.Contains(string(configData), "my-custom-node") {
		t.Error("config.json should contain custom node name")
	}
}

func TestInitConfigWritten(t *testing.T) {
	dir := t.TempDir()

	input := strings.NewReader("\nmy-node\n")
	scanner := bufio.NewReader(input)
	err := runInit(scanner, dir)
	if err != nil {
		t.Fatalf("runInit: %v", err)
	}

	configPath := filepath.Join(dir, "config.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("config.json not found: %v", err)
	}
	if !strings.Contains(string(data), "my-node") {
		t.Error("config.json should contain node name")
	}
}
