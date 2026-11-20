package main

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"oldipmi"
)

// --version prints the version from VERSION.
func TestVersion(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "oldipmi-core")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	out, err := exec.Command(bin, "--version").Output()
	if err != nil || strings.TrimSpace(string(out)) != oldipmi.Version {
		t.Fatalf("--version: %q, %v; want %s", out, err, oldipmi.Version)
	}
}
