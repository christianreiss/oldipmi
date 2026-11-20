package main

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Started by bare name from PATH, in another directory, the instructions
// still name where the binary is, not a file in the working directory
// (E-09: they took the path from argv[0]).
func TestInstructionsNameThisBinary(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "oldipmi-mcp")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	t.Setenv("PATH", dir)
	cmd := exec.Command("oldipmi-mcp", "--print-instructions")
	cmd.Dir = t.TempDir()
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), bin) || strings.Contains(string(out), cmd.Dir) {
		t.Fatalf("instructions do not name %s:\n%s", bin, out)
	}
}
