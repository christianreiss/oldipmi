package mcp

import (
	"encoding/json"
	"maps"
	"os"
	"slices"
	"strings"
	"testing"
)

// The power tool takes the words of the app's agent API, which are the IPC's
// power actions (testdata/ipc/names.json): the same set both ways, and the
// tool's refusal names each of them.
func TestPowerActionsAreTheApps(t *testing.T) {
	b, err := os.ReadFile("../../testdata/ipc/names.json")
	if err != nil {
		t.Fatal(err)
	}
	var names struct{ PowerActions map[string]string }
	if err := json.Unmarshal(b, &names); err != nil {
		t.Fatal(err)
	}
	app := slices.Sorted(maps.Values(names.PowerActions))
	if mine := slices.Sorted(slices.Values(powerActions)); len(app) == 0 || !slices.Equal(mine, app) {
		t.Fatalf("the power tool takes %v, the app %v", powerActions, app)
	}
	var power *Tool
	for _, tool := range tools() {
		if tool.Name == "power" {
			power = &tool
		}
	}
	if power == nil {
		t.Fatal("no power tool")
	}
	_, err = power.Run(nil, nil, map[string]any{"action": "bogus"})
	if err == nil {
		t.Fatal("the power tool took an unknown action")
	}
	for _, a := range powerActions {
		if !strings.Contains(err.Error(), a) {
			t.Errorf("the refusal %q does not name %q", err, a)
		}
	}
}
