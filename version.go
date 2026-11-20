// Package oldipmi holds what the whole project shares: its version.
package oldipmi

import (
	_ "embed"
	"strings"
)

//go:embed VERSION
var version string

// Version is OldIPMI's version. CMake reads the same VERSION file for the
// desktop app, and packaging/fedora/build-rpm.sh for the RPM.
var Version = strings.TrimSpace(version)
