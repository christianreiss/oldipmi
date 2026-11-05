package mcp

import (
	_ "embed"
	"path/filepath"
	"strings"
)

//go:embed instructions.md
var instructionsTemplate string

// Instructions returns the AI instructions with {{MCP_PATH}} replaced by the binary's absolute path.
func Instructions(binary string) string {
	if abs, err := filepath.Abs(binary); err == nil {
		binary = abs
	}
	return strings.ReplaceAll(instructionsTemplate, "{{MCP_PATH}}", binary)
}
