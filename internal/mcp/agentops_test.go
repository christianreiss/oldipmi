package mcp

import (
	"encoding/json"
	"maps"
	"oldipmi/internal/testkit"
	"os"
	"slices"
	"strings"
	"testing"
)

// testdata/agent/names.json is the one list of the agent socket's names. Both
// directions are checked: every op* constant is in the file with its value,
// and every entry of the file is one; desktop/ipc_test.cpp does the same for
// agentops.h. exchanges.json, which both sides replay, records every op, and
// each error in it names one of the file's codes, which only the desktop
// spells (agentops::Err*): oldipmi-mcp passes on the message.
func TestAgentNames(t *testing.T) {
	var n struct {
		Ops   map[string]string
		API   int
		Codes map[string]string
	}
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
	if agentAPI != n.API {
		t.Errorf("agentAPI is %d, names.json has api %d", agentAPI, n.API)
	}
	codes := slices.Collect(maps.Values(n.Codes))
	if len(codes) == 0 {
		t.Error("names.json has no codes")
	}
	seen := map[string]bool{}
	for _, x := range allExchanges(t) {
		seen[x.Op] = true
		if x.Op != "" && (x.OK && x.Code != "" || !x.OK && !slices.Contains(codes, x.Code)) {
			t.Errorf("recorded %s %v: ok is %v and code %q, which is not one of names.json's codes", x.Op, x.Args, x.OK, x.Code)
		}
		// The app's hello lists every op and the API version.
		if x.Op == opHello && x.OK {
			h, err := readReply[helloReply](opHello, x.Result)
			if err != nil || h.API != n.API || !slices.Equal(slices.Sorted(slices.Values(h.Ops)), slices.Sorted(maps.Values(n.Ops))) {
				t.Errorf("the recorded hello reply %s does not list api %d and the ops of names.json (%v)", x.Result, n.API, err)
			}
		}
	}
	for _, op := range slices.Sorted(maps.Values(n.Ops)) {
		if !seen[op] {
			t.Errorf("exchanges.json records no %s", op)
		}
	}
}

// A reply the tools cannot read fails with what is wrong with it, instead of
// reading as zero values.
func TestUnreadableRepliesFail(t *testing.T) {
	full := `{"state":"connected","message":"","mounted":false,"width":0,"height":0,"changeSeq":0,` +
		`"msSinceChange":0,"passwordRequired":false,"consoleInfo":{}}`
	for raw, want := range map[string]string{
		`{"state":"connected"}`:                `OldIPMI's reply to status has no "message": oldipmi-mcp and OldIPMI may be from different versions`,
		`{"state":"connected","message":null}`: `has no "message"`,
		`[]`:                                   `OldIPMI's reply to status is not a JSON object`,
		`null`:                                 `is not a JSON object`,
		strings.Replace(full, `"width":0`, `"width":"wide"`, 1): `OldIPMI's reply to status has a string as "width"`,
	} {
		if _, err := readReply[statusReply](opStatus, json.RawMessage(raw)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v, want %q", raw, err, want)
		}
	}
	// Keys the tools do not read may come and go.
	if r, err := readReply[statusReply](opStatus, json.RawMessage(strings.Replace(full, "{", `{"later":[1],`, 1))); err != nil || r.State != "connected" {
		t.Errorf("status with a new key: %+v, %v", r, err)
	}
	// The tools pass the error on: connect no longer falls back to the name it
	// was given, and disconnect no longer reads what it cannot read as nothing.
	f := &fake{result: map[string]string{"connect": `{"state":"logging_in"}`}}
	if res := callTool(t, f, "connect", `{"profile":"A"}`); res["isError"] != true || !strings.Contains(content(res), `reply to connect has no "profile"`) {
		t.Errorf("connect: %v", res)
	}
	f = &fake{result: map[string]string{"disconnect": `{"state":"disconnected","stillOpen":"A"}`}}
	if res := callTool(t, f, "disconnect", `{}`); res["isError"] != true || !strings.Contains(content(res), `reply to disconnect has a string as "stillOpen"`) {
		t.Errorf("disconnect: %v", res)
	}
	f = &fake{result: map[string]string{"screenshot": `{"png":"AA==","width":8,"height":6}`}}
	if res := callTool(t, f, "screenshot", `{}`); res["isError"] != true || !strings.Contains(content(res), `reply to screenshot has no "fullWidth"`) {
		t.Errorf("screenshot: %v", res)
	}
}
