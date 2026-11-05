package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

type call_ struct {
	op   string
	args map[string]any
}
type fake struct {
	calls  []call_
	result map[string]string
	err    map[string]error
}

func (f *fake) Call(op string, a map[string]any) (json.RawMessage, error) {
	f.calls = append(f.calls, call_{op, a})
	if e := f.err[op]; e != nil {
		return nil, e
	}
	if r, ok := f.result[op]; ok {
		return json.RawMessage(r), nil
	}
	return json.RawMessage(`{}`), nil
}

func init() { sleep = func(time.Duration) {} }

func rpc(t *testing.T, f *fake, lines ...string) []map[string]any {
	t.Helper()
	var out bytes.Buffer
	s := NewServer(f, "test", "INSTR")
	if err := s.Serve(strings.NewReader(strings.Join(lines, "\n")+"\n"), &out); err != nil {
		t.Fatal(err)
	}
	var rs []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if l == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatal(err)
		}
		rs = append(rs, m)
	}
	return rs
}

func TestInitializeAndList(t *testing.T) {
	rs := rpc(t, &fake{},
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"nope"}`)
	if len(rs) != 3 {
		t.Fatalf("want 3 replies (no reply to notification), got %d", len(rs))
	}
	r := rs[0]["result"].(map[string]any)
	if r["protocolVersion"] != "2025-03-26" || r["instructions"] != "INSTR" {
		t.Fatalf("bad initialize: %v", r)
	}
	tl := rs[1]["result"].(map[string]any)["tools"].([]any)
	names := map[string]bool{}
	for _, x := range tl {
		names[x.(map[string]any)["name"].(string)] = true
	}
	for _, n := range []string{"screenshot", "type_text", "key_combo", "mouse_click", "power", "mount_iso", "connect", "wait_for_stable"} {
		if !names[n] {
			t.Errorf("missing tool %s", n)
		}
	}
	if rs[2]["error"] == nil {
		t.Error("unknown method must error")
	}
}

func callTool(t *testing.T, f *fake, name, args string) map[string]any {
	rs := rpc(t, f, fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":%q,"arguments":%s}}`, name, args))
	return rs[0]["result"].(map[string]any)
}

func TestTypeTextSendsShiftedKeys(t *testing.T) {
	f := &fake{}
	res := callTool(t, f, "type_text", `{"text":"A1!\n"}`)
	if res["isError"] == true {
		t.Fatalf("%v", res)
	}
	var seq []string
	for _, c := range f.calls {
		if c.op == "key" {
			seq = append(seq, fmt.Sprintf("%d:%v", int(c.args["usage"].(int)), c.args["down"]))
		}
	}
	want := "225:true 4:true 4:false 225:false 30:true 30:false 225:true 30:true 30:false 225:false 40:true 40:false"
	if strings.Join(seq, " ") != want {
		t.Fatalf("got %s", strings.Join(seq, " "))
	}
	if f.calls[len(f.calls)-1].op != "release" {
		t.Error("input must be released at the end")
	}
}

func TestTypeTextRejectsBeforeSending(t *testing.T) {
	f := &fake{}
	res := callTool(t, f, "type_text", `{"text":"ok é"}`)
	if res["isError"] != true || len(f.calls) != 0 {
		t.Fatalf("must reject non-ASCII without sending: %v %v", res, f.calls)
	}
	res = callTool(t, f, "type_text", `{"text":"`+strings.Repeat("a", maxTextLen+1)+`"}`)
	if res["isError"] != true || len(f.calls) != 0 {
		t.Fatal("must enforce length limit")
	}
}

func TestKeyNamesAndCombo(t *testing.T) {
	for n, want := range map[string]int{"Enter": 40, "F1": 58, "f12": 69, "a": 4, "7": 36, "Delete": 76, "ctrl": 224, "Up": 82} {
		if u, err := KeyUsage(n); err != nil || u != want {
			t.Errorf("%s = %d,%v want %d", n, u, err, want)
		}
	}
	for _, bad := range []string{"F13", "F0", "nonsense", "A!"} {
		if _, err := KeyUsage(bad); err == nil {
			t.Errorf("%q must fail", bad)
		}
	}
	f := &fake{}
	callTool(t, f, "key_combo", `{"keys":["ctrl","alt","delete"]}`)
	var seq []string
	for _, c := range f.calls {
		if c.op == "key" {
			seq = append(seq, fmt.Sprintf("%d:%v", c.args["usage"], c.args["down"]))
		}
	}
	if strings.Join(seq, " ") != "224:true 226:true 76:true 76:false 226:false 224:false" {
		t.Fatalf("got %v", seq)
	}
}

func TestEveryASCIIPrintableTypable(t *testing.T) {
	seen := map[[2]int]rune{}
	for r := rune(32); r < 127; r++ {
		u, sh, ok := CharUsage(r)
		if !ok {
			t.Fatalf("%q not typable", r)
		}
		k := [2]int{u, map[bool]int{false: 0, true: 1}[sh]}
		if o, dup := seen[k]; dup {
			t.Fatalf("%q and %q map to the same key", r, o)
		}
		seen[k] = r
	}
}

func TestPowerValidationAndPassThrough(t *testing.T) {
	f := &fake{}
	if res := callTool(t, f, "power", `{"action":"explode"}`); res["isError"] != true || len(f.calls) != 0 {
		t.Fatal("invalid action must not reach the app")
	}
	if res := callTool(t, f, "power", `{"action":"reset"}`); res["isError"] == true || f.calls[0].op != "power" {
		t.Fatalf("%v", res)
	}
	f = &fake{err: map[string]error{"power": fmt.Errorf("IPMI connection failed")}}
	if res := callTool(t, f, "power", `{"action":"on"}`); res["isError"] != true {
		t.Fatal("backend error must surface")
	}
}

func TestScreenshotReturnsImage(t *testing.T) {
	f := &fake{result: map[string]string{"screenshot": `{"png":"AAAA","width":10,"height":5,"fullWidth":10,"fullHeight":5,"x":0,"y":0,"regionW":10,"regionH":5}`}}
	res := callTool(t, f, "screenshot", `{}`)
	c := res["content"].([]any)[0].(map[string]any)
	if c["type"] != "image" || c["mimeType"] != "image/png" || c["data"] != "AAAA" {
		t.Fatalf("%v", c)
	}
}

func TestConnectWaitsAndReportsFailure(t *testing.T) {
	f := &fake{result: map[string]string{"status": `{"state":"connected","message":"ok","width":640,"height":480}`}}
	if res := callTool(t, f, "connect", `{"profile":"Lab-AMI"}`); res["isError"] == true {
		t.Fatalf("%v", res)
	}
	f = &fake{result: map[string]string{"status": `{"state":"disconnected","message":"Login failed: bad credentials"}`}}
	res := callTool(t, f, "connect", `{"profile":"Lab-AMI"}`)
	if res["isError"] != true || !strings.Contains(fmt.Sprint(res["content"]), "Login failed") {
		t.Fatalf("%v", res)
	}
}

func TestMouseClickReleases(t *testing.T) {
	f := &fake{}
	callTool(t, f, "mouse_click", `{"x":10,"y":20,"button":"right"}`)
	last := f.calls[len(f.calls)-1]
	if last.op != "release" {
		t.Fatal("must release")
	}
	if res := callTool(t, f, "mouse_click", `{"x":-1,"y":2}`); res["isError"] != true {
		t.Fatal("negative coords must fail")
	}
}

func TestInstructionsContainPath(t *testing.T) {
	s := Instructions("/opt/x/oldipmi-mcp")
	if !strings.Contains(s, "claude mcp add oldipmi -- /opt/x/oldipmi-mcp") || strings.Contains(s, "{{") {
		t.Fatal(s)
	}
}
