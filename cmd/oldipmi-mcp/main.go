// oldipmi-mcp is a stdio MCP server that controls the running OldIPMI desktop app.
package main

import (
	"fmt"
	"os"
	"strings"

	"oldipmi/internal/mcp"
)

const version = "0.3.0" // keep in step with CMakeLists.txt project(VERSION)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--print-instructions" {
		fmt.Print(mcp.Instructions(self()))
		return
	}
	s := mcp.NewServer(&mcp.Client{Path: mcp.SocketPath()}, version, mcp.Instructions(self()))
	if err := s.Serve(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, strings.TrimSpace(err.Error()))
		os.Exit(1)
	}
}

// self is where this binary is, for the instructions, also when it was
// started by bare name from PATH and argv[0] holds only that name.
func self() string {
	if p, err := os.Executable(); err == nil {
		return p
	}
	return os.Args[0]
}
