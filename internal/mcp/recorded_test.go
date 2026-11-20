package mcp

import (
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
)

// testdata/agent/exchanges.json holds requests as this package sends them and
// the replies the desktop's AgentApi gave, recorded against its demo console;
// desktop/agentapi_test.cpp replays them there. Here the tools read those
// replies as they are, instead of hand-written ones.

type exchange struct {
	Op     string          `json:"op"`
	Args   map[string]any  `json:"args"`
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result"`
	Error  string          `json:"error"`
}

// recorded is the n-th recorded exchange of op with exactly these args.
func recorded(t *testing.T, op, args string, n int) exchange {
	t.Helper()
	b, err := os.ReadFile("../../testdata/agent/exchanges.json")
	if err != nil {
		t.Fatal(err)
	}
	var f struct{ Exchanges []exchange }
	var want map[string]any
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(args), &want); err != nil {
		t.Fatal(err)
	}
	for _, x := range f.Exchanges {
		if x.Op == op && reflect.DeepEqual(x.Args, want) {
			if n == 0 {
				return x
			}
			n--
		}
	}
	t.Fatalf("exchanges.json has no such %s %s", op, args)
	return exchange{}
}

// The status type reads only keys the app sends.
func TestStatusReadsRecordedKeys(t *testing.T) {
	connected := recorded(t, "status", `{}`, 1)
	var sent map[string]any
	if err := json.Unmarshal(connected.Result, &sent); err != nil || sent["state"] != "connected" {
		t.Fatalf("%s: %v", connected.Result, err)
	}
	st := reflect.TypeOf(status{})
	for i := range st.NumField() {
		if k := jsonName(st.Field(i)); sent[k] == nil {
			t.Errorf("status reads %q, which the app does not send", k)
		}
	}
}

func jsonName(f reflect.StructField) string { return strings.Split(f.Tag.Get("json"), ",")[0] }

func TestToolsReadRecordedReplies(t *testing.T) {
	ok := func(x exchange) string {
		t.Helper()
		if !x.OK {
			t.Fatalf("%s %v failed: %s", x.Op, x.Args, x.Error)
		}
		return string(x.Result)
	}
	// connect: the profile it opened, then the status that shows the picture.
	f := &fake{result: map[string]string{"connect": ok(recorded(t, "connect", `{"profile":"demo"}`, 0))},
		statuses: []string{ok(recorded(t, "status", `{}`, 1))}}
	if c := content(callTool(t, f, "connect", `{"profile":"demo"}`)); !strings.Contains(c, "Connected to Demo (OldIPMI demo (synthetic, no network)). Framebuffer 1024x768.") {
		t.Errorf("connect: %s", c)
	}
	if f.calls[1].args["profile"] != "demo-1" {
		t.Errorf("connect polls %v, want the id it opened", f.calls[1].args)
	}
	f = &fake{result: map[string]string{"connect": ok(recorded(t, "connect", `{"profile":"Fake"}`, 0))},
		statuses: []string{ok(recorded(t, "status", `{"profile":"Fake"}`, 0))}}
	res := callTool(t, f, "connect", `{"profile":"Fake","password_wait_ms":0}`)
	if res["isError"] != true || !strings.Contains(content(res), `asking for the password of "Fake"`) {
		t.Errorf("connect without a password: %v", res)
	}
	// screenshot: the image and where it lies in the framebuffer.
	shot := recorded(t, "screenshot", `{"x":0,"y":0,"width":8,"height":6}`, 0)
	f = &fake{result: map[string]string{"screenshot": ok(shot)}}
	res = callTool(t, f, "screenshot", `{"x":0,"y":0,"width":8,"height":6}`)
	var png struct{ PNG string }
	json.Unmarshal(shot.Result, &png)
	if img := res["content"].([]any)[0].(map[string]any); img["data"] != png.PNG || png.PNG == "" {
		t.Errorf("screenshot image: %v", img)
	}
	if c := content(res); !strings.Contains(c, "Framebuffer 1024x768; image 8x6. Image shows framebuffer region x=0 y=0 8x6 at scale 1:") {
		t.Errorf("screenshot: %s", c)
	}
	refused := recorded(t, "screenshot", `{"x":1020,"width":10}`, 0)
	f = &fake{err: map[string]error{"screenshot": errors.New(refused.Error)}}
	if res := callTool(t, f, "screenshot", `{"x":1020,"width":10}`); res["isError"] != true || !strings.Contains(content(res), "Region outside the 1024x768 framebuffer.") {
		t.Errorf("screenshot outside: %v", res)
	}
	// disconnect: what closed and what stays open.
	for args, want := range map[string]string{
		`{"profile":"Wide"}`: "Disconnected from Wide. Still open: Demo.",
		`{}`:                 "Disconnected from Demo.",
		`{"all":true}`:       "Dismissed the password prompt; no console was open.",
	} {
		f = &fake{result: map[string]string{"disconnect": ok(recorded(t, "disconnect", args, 0))}}
		if c := content(callTool(t, f, "disconnect", args)); !strings.Contains(c, want) {
			t.Errorf("disconnect %s: %s, want %q", args, c, want)
		}
	}
	f = &fake{result: map[string]string{"power": ok(recorded(t, "power", `{"action":"soft"}`, 0))}}
	if c := content(callTool(t, f, "power", `{"action":"soft"}`)); !strings.Contains(c, "Power action 'soft' accepted by the BMC.") {
		t.Errorf("power: %s", c)
	}
	// mount and eject: the app answers {"message": ""}, not the {} for which
	// the tools have a sentence, so an agent reads the raw reply today. Phase 1
	// fixes that; until then this pins it.
	f = &fake{result: map[string]string{"mount": ok(recorded(t, "mount", `{"path":"$ISO"}`, 0))}}
	if c := content(callTool(t, f, "mount_iso", `{"path":"/srv/iso/install.iso"}`)); !strings.Contains(c, `"message": ""`) {
		t.Errorf("mount_iso: %s", c)
	}
	f = &fake{result: map[string]string{"eject": ok(recorded(t, "eject", `{}`, 0))},
		statuses: []string{ok(recorded(t, "status", `{}`, 2))}}
	if c := content(callTool(t, f, "eject_iso", `{}`)); !strings.Contains(c, `"message": ""`) {
		t.Errorf("eject_iso: %s", c)
	}
}
