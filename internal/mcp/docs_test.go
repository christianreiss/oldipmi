package mcp

import (
	"os"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// The instructions an agent reads (instructions.md) and the README name every
// tool, and their rule about the profile argument names exactly the Global
// tools, the ones that do not take it as the console to act on (B-17).
func TestDocsNameEveryTool(t *testing.T) {
	readme, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatal(err)
	}
	var names, global []string
	for _, tool := range tools() {
		names = append(names, tool.Name)
		if tool.Global {
			global = append(global, tool.Name)
		}
		props := tool.Schema["properties"].(map[string]any)
		if selects := reflect.DeepEqual(props["profile"], profileArg.schema()); selects == tool.Global {
			t.Errorf("%s: Global is %v, but its profile argument is %v", tool.Name, tool.Global, props["profile"])
		}
	}
	// The part of a document from the first line that starts with from up to
	// the first line after it that starts with to.
	part := func(doc, from, to string) string {
		t.Helper()
		i := strings.Index(doc, "\n"+from)
		if i < 0 {
			t.Fatalf("no line starts with %q", from)
		}
		rest := doc[i+1:]
		if j := strings.Index(rest[len(from):], "\n"+to); j >= 0 {
			rest = rest[:len(from)+j]
		}
		return rest
	}
	quoted := regexp.MustCompile("`([a-z_]+)`")
	named := func(text string) []string {
		var out []string
		for _, m := range quoted.FindAllStringSubmatch(text, -1) {
			out = append(out, m[1])
		}
		return out
	}
	exception := regexp.MustCompile("Every tool except (.+?) takes an optional `profile`")
	for _, doc := range []struct{ name, tools, all string }{
		{"instructions.md", part(instructionsTemplate, "## Tools", "## "), instructionsTemplate},
		{"README.md", part(string(readme), "Tools: ", "\n"), string(readme)},
	} {
		listed := named(doc.tools)
		for _, n := range names {
			if !slices.Contains(listed, n) {
				t.Errorf("%s does not name the tool %s", doc.name, n)
			}
		}
		m := exception.FindStringSubmatch(strings.Join(strings.Fields(doc.all), " "))
		if m == nil {
			t.Errorf("%s has no sentence \"Every tool except … takes an optional `profile`\"", doc.name)
			continue
		}
		if got := named(m[1]); !slices.Equal(sorted(got), sorted(global)) {
			t.Errorf("%s says every tool except %v takes a profile; the tools without one are %v", doc.name, got, global)
		}
	}
}

func sorted(s []string) []string { return slices.Sorted(slices.Values(s)) }
