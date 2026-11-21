package mcp

import (
	"encoding/json"
	"maps"
	"oldipmi/internal/testkit"
	"os"
	"slices"
	"testing"
)

// testdata/agent/names.json is the one list of the agent socket's names. Both
// directions are checked: every op* constant is in the file with its value,
// and every entry of the file is one; desktop/ipc_test.cpp does the same for
// agentops.h. exchanges.json, which both sides replay, records every op.
func TestAgentNames(t *testing.T) {
	var n struct{ Ops map[string]string }
	b, err := os.ReadFile("../../testdata/agent/names.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &n); err != nil {
		t.Fatal(err)
	}
	if got := testkit.StringConsts(t, ".", "op"); len(n.Ops) == 0 || !maps.Equal(got, n.Ops) {
		t.Errorf("op* constants are %v, names.json has %v", got, n.Ops)
	}
	seen := map[string]bool{}
	for _, x := range allExchanges(t) {
		seen[x.Op] = true
	}
	for _, op := range slices.Sorted(maps.Values(n.Ops)) {
		if !seen[op] {
			t.Errorf("exchanges.json records no %s", op)
		}
	}
}
