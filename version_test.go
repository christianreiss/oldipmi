package oldipmi

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// VERSION is the one place the version is kept (E-05, B-11): the desktop
// app, both Go programs and the RPM take it from there, and the newest
// release in the CHANGELOG is not ahead of it.
func TestVersionHasOneSource(t *testing.T) {
	semver := regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+)$`)
	if !semver.MatchString(Version) {
		t.Fatalf("VERSION holds %q, want MAJOR.MINOR.PATCH", Version)
	}
	read := func(name string) string {
		t.Helper()
		b, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	for name, use := range map[string]string{
		"CMakeLists.txt":                "project(OldIPMI VERSION ${OLDIPMI_VERSION}",
		"packaging/fedora/build-rpm.sh": "< VERSION",
		"cmd/oldipmi-mcp/main.go":       "oldipmi.Version",
		"internal/protocol/web.go":      `"OldIPMI/"+oldipmi.Version`,
	} {
		if !strings.Contains(read(name), use) {
			t.Errorf("%s does not take the version from VERSION (no %q)", name, use)
		}
	}
	released := regexp.MustCompile(`(?m)^## (\d+\.\d+\.\d+)`).FindStringSubmatch(read("CHANGELOG.md"))
	if released == nil {
		t.Fatal("CHANGELOG.md names no release")
	}
	key := func(v string) (k [3]int) {
		for i, s := range semver.FindStringSubmatch(v)[1:] {
			k[i], _ = strconv.Atoi(s)
		}
		return k
	}
	a, b := key(released[1]), key(Version)
	if a[0] > b[0] || a[0] == b[0] && (a[1] > b[1] || a[1] == b[1] && a[2] > b[2]) {
		t.Errorf("CHANGELOG.md has release %s, newer than VERSION %s", released[1], Version)
	}
}
