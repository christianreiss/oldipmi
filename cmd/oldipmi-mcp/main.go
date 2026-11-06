// oldipmi-mcp is a stdio MCP server that controls the running OldIPMI desktop app.
package main

import (
	"fmt"
	"os"
	"strings"

	"oldipmi/internal/mcp"
)

const version = "0.2.0" // keep in step with CMakeLists.txt project(VERSION)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--print-instructions" {
		fmt.Print(mcp.Instructions(os.Args[0]))
		return
	}
	s := mcp.NewServer(&mcp.Client{Path: mcp.SocketPath()}, version, mcp.Instructions(os.Args[0]))
	if err := s.Serve(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, strings.TrimSpace(err.Error()))
		os.Exit(1)
	}
}
