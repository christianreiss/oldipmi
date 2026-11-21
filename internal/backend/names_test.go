package backend

import (
	"maps"
	"oldipmi/internal/protocol"
	"oldipmi/internal/testkit"
	"oldipmi/internal/video/raster"
	"reflect"
	"slices"
	"testing"
)

// testdata/ipc/names.json is the one list of the IPC's names. Both directions
// are checked: every constant is in the file with its value, and every entry
// of the file is a constant; desktop/ipc_test.cpp does the same for ipc.h.

func TestIPCNames(t *testing.T) {
	var n struct {
		Ops, Events, States, Stages, PowerActions, Codes, Features map[string]string
		MaxFrame                                                   map[string]int
		Fields                                                     map[string][]string
	}
	fixture(t, "names.json", &n)
	for _, c := range []struct {
		dir, prefix string
		want        map[string]string
	}{
		{".", "Op", n.Ops},
		{".", "Event", n.Events},
		{".", "State", n.States},
		{"../protocol", "Stage", n.Stages},
		{"../protocol", "Power", n.PowerActions},
		{"../protocol", "Code", n.Codes},
		{"../protocol", "Feature", n.Features},
	} {
		if got := testkit.StringConsts(t, c.dir, c.prefix); len(c.want) == 0 || !maps.Equal(got, c.want) {
			t.Errorf("%s* constants are %v, names.json has %v", c.prefix, got, c.want)
		}
	}
	if want := map[string]int{"Width": raster.MaxWidth, "Height": raster.MaxHeight}; !maps.Equal(n.MaxFrame, want) {
		t.Errorf("raster.MaxWidth and MaxHeight are %v, names.json maxFrame has %v", want, n.MaxFrame)
	}
	for _, stage := range n.Stages {
		if stageLabel[stage] == "" {
			t.Errorf("stage %q has no label for its error message", stage)
		}
	}
	// A key cannot appear in a message without being in the file.
	parts := map[string]reflect.Type{
		"command":     reflect.TypeOf(Command{}),
		"event":       reflect.TypeOf(Event{}),
		"consoleInfo": reflect.TypeOf(protocol.ConsoleInfo{}),
		"endpoint":    reflect.TypeOf(protocol.Endpoint{}),
		"input":       reflect.TypeOf(protocol.Input{}),
	}
	if !slices.Equal(slices.Sorted(maps.Keys(parts)), slices.Sorted(maps.Keys(n.Fields))) {
		t.Errorf("names.json describes %v", slices.Sorted(maps.Keys(n.Fields)))
	}
	for part, typ := range parts {
		var keys []string
		for i := range typ.NumField() {
			keys = append(keys, jsonKey(typ.Field(i)))
		}
		if want := n.Fields[part]; !slices.Equal(slices.Sorted(slices.Values(keys)), slices.Sorted(slices.Values(want))) {
			t.Errorf("%s has the keys %v, names.json %v", part, keys, want)
		}
	}
}
