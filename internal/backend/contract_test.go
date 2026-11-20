package backend

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"oldipmi/internal/protocol"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// testdata/ipc holds the messages of the desktop-core IPC as each side sends
// them; the desktop's tests use the same files (desktop/session_test.cpp,
// desktop/controller_test.cpp). Here: the core reads every key the desktop
// sends, and what it sends looks exactly so.

func fixture(t *testing.T, name string, v any) []byte {
	t.Helper()
	b, err := os.ReadFile("../../testdata/ipc/" + name)
	if err != nil {
		t.Fatal(err)
	}
	if v != nil {
		if err := json.Unmarshal(b, v); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	return b
}

// read decodes a command as the core does, from its length-prefixed frame;
// strict also refuses keys the core has no field for.
func read(t *testing.T, msg []byte, strict bool) Command {
	t.Helper()
	var compact bytes.Buffer
	if err := json.Compact(&compact, msg); err != nil {
		t.Fatal(err)
	}
	frame := binary.BigEndian.AppendUint32(nil, uint32(compact.Len()))
	c, err := ReadCommand(bytes.NewReader(append(frame, compact.Bytes()...)))
	if err != nil {
		t.Fatalf("%s: %v", compact.Bytes(), err)
	}
	if strict {
		var again Command
		dec := json.NewDecoder(bytes.NewReader(compact.Bytes()))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&again); err != nil {
			t.Errorf("%s: %v", compact.Bytes(), err)
		}
	}
	return c
}

func jsonKey(f reflect.StructField) string { return strings.Split(f.Tag.Get("json"), ",")[0] }

func TestConnectCommandContract(t *testing.T) {
	msg := fixture(t, "connect.json", nil)
	c := read(t, msg, false)
	if c.Version != 1 || c.ID != 1 || c.Op != "connect" || c.Password != "pw-1" {
		t.Fatalf("%+v", c)
	}
	// Every profile field the core reads arrives, none at its default value.
	profile := reflect.ValueOf(c.Profile)
	known := map[string]bool{}
	for i := range profile.NumField() {
		f := profile.Type().Field(i)
		known[jsonKey(f)] = true
		if profile.Field(i).IsZero() {
			t.Errorf("connect.json does not set the profile's %q", jsonKey(f))
		}
	}
	// What the desktop keeps for itself and the core ignores: exactly these.
	var raw struct {
		Profile map[string]any `json:"profile"`
	}
	fixture(t, "connect.json", &raw)
	var desktopOnly []string
	for k := range raw.Profile {
		if !known[k] {
			desktopOnly = append(desktopOnly, k)
		}
	}
	slices.Sort(desktopOnly)
	if want := []string{"id", "lastConnected", "remember"}; !slices.Equal(desktopOnly, want) {
		t.Errorf("profile keys the core does not read: %v, want %v", desktopOnly, want)
	}
}

func TestCommandsContract(t *testing.T) {
	var msgs []json.RawMessage
	fixture(t, "commands.json", &msgs)
	want := map[string]Command{
		"key":        {Input: protocol.Input{Usage: 4, Down: true}},
		"pointer":    {Input: protocol.Input{X: 640, Y: 360, Buttons: 1, Wheel: -1}},
		"release":    {},
		"mount":      {Path: "/srv/iso/install.iso"},
		"eject":      {},
		"power":      {Action: "soft"},
		"disconnect": {},
	}
	var ops []string
	for _, m := range msgs {
		c := read(t, m, true)
		ops = append(ops, c.Op)
		w, ok := want[c.Op]
		w.Version, w.ID, w.Op = 1, c.ID, c.Op
		if !ok || c.ID == 0 || !reflect.DeepEqual(c, w) {
			t.Errorf("%s: read as %+v", m, c)
		}
	}
	// The ops the core's command loop knows, besides connect.
	if want := []string{"key", "pointer", "release", "mount", "eject", "power", "disconnect"}; !slices.Equal(ops, want) {
		t.Errorf("commands.json has %v, want %v", ops, want)
	}
}

// Every kind of event the core sends, from the Event type itself: the field
// names and which ones are left out when empty.
func TestEventsContract(t *testing.T) {
	var msgs []json.RawMessage
	fixture(t, "events.json", &msgs)
	types := map[string]bool{}
	for _, m := range msgs {
		var e Event
		dec := json.NewDecoder(bytes.NewReader(m))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&e); err != nil {
			t.Fatalf("%s: %v", m, err)
		}
		types[e.Type] = true
		b, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		var got, want any
		json.Unmarshal(b, &got)
		json.Unmarshal(m, &want)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("the core sends\n %s\nfor\n %s", b, m)
		}
	}
	if len(types) != 4 || !types["state"] || !types["ok"] || !types["error"] || !types["media"] {
		t.Errorf("events.json covers %v, want state, ok, error and media", types)
	}
}
