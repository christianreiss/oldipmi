package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

type call_ struct {
	op   string
	args map[string]any
}
type fake struct {
	mu       sync.Mutex
	calls    []call_
	result   map[string]string
	err      map[string]error
	statuses []string // successive status replies; the last one repeats
	failAt   int      // fail the n-th key call (1-based)
	keys     int
	hello    []string
}

func (f *fake) Call(op string, a map[string]any) (json.RawMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call_{op, a})
	if op == "key" {
		f.keys++
		if f.keys == f.failAt {
			return nil, fmt.Errorf("Not connected")
		}
	}
	if e := f.err[op]; e != nil {
		return nil, e
	}
	if op == "status" && len(f.statuses) > 0 {
		s := f.statuses[0]
		if len(f.statuses) > 1 {
			f.statuses = f.statuses[1:]
		}
		return json.RawMessage(s), nil
	}
	if r, ok := f.result[op]; ok {
		return json.RawMessage(r), nil
	}
	return json.RawMessage(`{}`), nil
}

type greeter struct{ fake }

func (g *greeter) Hello(name, version, protocol string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.hello = append(g.hello, name+"/"+version+"/"+protocol)
}

func (f *fake) ops(kind string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var seq []string
	for _, c := range f.calls {
		switch {
		case c.op == "key" && (kind == "" || kind == "key"):
			seq = append(seq, fmt.Sprintf("%v:%v", c.args["usage"], c.args["down"]))
		case c.op == "pointer" && (kind == "" || kind == "pointer"):
			seq = append(seq, fmt.Sprintf("p%v,%v,%v,%v", c.args["x"], c.args["y"], c.args["buttons"], c.args["wheel"]))
		}
	}
	return seq
}

func init() { sleep = func(ctx context.Context, _ time.Duration) error { return ctx.Err() } }

func realSleep(t *testing.T) {
	old := sleep
	sleep = func(ctx context.Context, d time.Duration) error {
		tm := time.NewTimer(d)
		defer tm.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tm.C:
			return nil
		}
	}
	t.Cleanup(func() { sleep = old })
}

func rpc(t *testing.T, b Backend, lines ...string) []map[string]any {
	t.Helper()
	var out bytes.Buffer
	s := NewServer(b, "test", "INSTR")
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
			t.Fatalf("%v: %s", err, l)
		}
		rs = append(rs, m)
	}
	return rs
}

func byID(rs []map[string]any) map[string]map[string]any {
	m := map[string]map[string]any{}
	for _, r := range rs {
		m[fmt.Sprint(r["id"])] = r
	}
	return m
}

func errCode(r map[string]any) int {
	e, _ := r["error"].(map[string]any)
	if e == nil {
		return 0
	}
	return int(e["code"].(float64))
}

func TestInitializeNegotiatesLegacyVersions(t *testing.T) {
	for req, want := range map[string]string{"2024-11-05": "2024-11-05", "2025-03-26": "2025-03-26", "2025-06-18": "2025-06-18", "2025-11-25": "2025-11-25", "1999-01-01": "2025-11-25", "2026-07-28": "2025-11-25"} {
		rs := rpc(t, &fake{}, fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":%q,"capabilities":{},"clientInfo":{"name":"c","version":"1"}}}`, req),
			`{"jsonrpc":"2.0","method":"notifications/initialized"}`)
		if len(rs) != 1 {
			t.Fatalf("want exactly one reply, got %v", rs)
		}
		r := rs[0]["result"].(map[string]any)
		if r["protocolVersion"] != want || r["instructions"] != "INSTR" || r["capabilities"].(map[string]any)["tools"] == nil {
			t.Fatalf("%s: %v", req, r)
		}
		if r["serverInfo"].(map[string]any)["name"] != "oldipmi" {
			t.Fatal(r)
		}
	}
}

func TestModernDiscoverAndMeta(t *testing.T) {
	meta := `"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{},"io.modelcontextprotocol/clientInfo":{"name":"modern","version":"9"}}`
	g := &greeter{}
	rs := byID(rpc(t, g,
		`{"jsonrpc":"2.0","id":"d","method":"server/discover","params":{`+meta+`}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{`+meta+`}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{`+meta+`,"name":"list_keys","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2030-01-01","io.modelcontextprotocol/clientCapabilities":{}}}}`,
		`{"jsonrpc":"2.0","id":5,"method":"tools/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28"}}}`))
	d := rs["d"]["result"].(map[string]any)
	if d["resultType"] != "complete" || fmt.Sprint(d["supportedVersions"]) != "[2026-07-28]" || d["instructions"] != "INSTR" ||
		d["_meta"].(map[string]any)["io.modelcontextprotocol/serverInfo"] == nil || d["ttlMs"] == nil || d["cacheScope"] == nil {
		t.Fatalf("discover: %v", d)
	}
	l := rs["2"]["result"].(map[string]any)
	if l["resultType"] != "complete" || l["ttlMs"] == nil || l["cacheScope"] != "public" {
		t.Fatalf("tools/list: %v", l)
	}
	if rs["3"]["result"].(map[string]any)["resultType"] != "complete" {
		t.Fatalf("tools/call: %v", rs["3"])
	}
	if errCode(rs["4"]) != -32022 || !strings.Contains(fmt.Sprint(rs["4"]["error"]), "2026-07-28") || !strings.Contains(fmt.Sprint(rs["4"]["error"]), "2030-01-01") {
		t.Fatalf("unsupported version: %v", rs["4"])
	}
	if errCode(rs["5"]) != -32602 {
		t.Fatalf("missing capabilities: %v", rs["5"])
	}
	waitFor(t, func() bool { g.mu.Lock(); defer g.mu.Unlock(); return len(g.hello) == 1 })
	if g.hello[0] != "modern/9/2026-07-28" {
		t.Fatal(g.hello)
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	for i := 0; i < 200 && !cond(); i++ {
		time.Sleep(5 * time.Millisecond)
	}
	if !cond() {
		t.Fatal("condition not reached")
	}
}

func TestHelloOnInitialize(t *testing.T) {
	g := &greeter{}
	rpc(t, g, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","clientInfo":{"name":"claude-code","version":"2.1"}}}`)
	waitFor(t, func() bool { g.mu.Lock(); defer g.mu.Unlock(); return len(g.hello) == 1 })
	if g.hello[0] != "claude-code/2.1/2025-06-18" {
		t.Fatal(g.hello)
	}
}

func TestProtocolErrors(t *testing.T) {
	rs := rpc(t, &fake{},
		`{not json`,
		`{"jsonrpc":"2.0","id":2,"method":"nope"}`,
		`{"id":3,"method":"ping"}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"no_such_tool"}}`,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"status","arguments":[1]}}`,
		`{"jsonrpc":"2.0","method":"whatever/notification"}`,
		`{"jsonrpc":"2.0","id":99,"result":{}}`,
		`{"jsonrpc":"2.0","id":6,"method":"ping"}`,
		`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"key","arguments":{"name":"F99"}}}`,
		`{"jsonrpc":"2.0","id":{},"method":"ping"}`)
	m := map[string]map[string]any{}
	var nulls []map[string]any
	for _, r := range rs {
		if r["id"] == nil && r["error"] != nil {
			nulls = append(nulls, r)
		} else {
			m[fmt.Sprint(r["id"])] = r
		}
	}
	if errCode(nulls[0]) != -32700 {
		t.Fatalf("parse error: %v", nulls)
	}
	if errCode(m["2"]) != -32601 || errCode(m["3"]) != -32600 || errCode(m["4"]) != -32602 || errCode(m["5"]) != -32602 {
		t.Fatalf("%v", m)
	}
	if m["6"]["result"] == nil || m["99"] != nil {
		t.Fatalf("ping/response handling: %v", m)
	}
	if r := m["7"]["result"].(map[string]any); r["isError"] != true || !strings.Contains(fmt.Sprint(r["content"]), "list_keys") {
		t.Fatalf("argument errors must be tool errors with a hint: %v", m["7"])
	}
	if len(nulls) != 2 || errCode(nulls[1]) != -32600 {
		t.Fatalf("object id must be rejected: %v", nulls)
	}
}

func TestBatch(t *testing.T) {
	var out bytes.Buffer
	s := NewServer(&fake{}, "t", "")
	s.Serve(strings.NewReader(`[{"jsonrpc":"2.0","id":8,"method":"ping"},{"jsonrpc":"2.0","method":"notifications/initialized"},{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"wait","arguments":{"ms":1}}}]`+"\n[]\n"), &out)
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("%q", out.String())
	}
	var arr []map[string]any
	if err := json.Unmarshal([]byte(lines[1]), &arr); err != nil {
		if err := json.Unmarshal([]byte(lines[0]), &arr); err != nil {
			t.Fatal(out.String())
		}
	}
	if len(arr) != 2 {
		t.Fatalf("batch must answer both requests and skip the notification: %v", arr)
	}
	if !strings.Contains(out.String(), `"code":-32600`) {
		t.Fatal("empty batch must be an invalid request")
	}
}

func TestToolsHaveSchemasAndAnnotations(t *testing.T) {
	rs := rpc(t, &fake{}, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	tl := rs[0]["result"].(map[string]any)["tools"].([]any)
	names := map[string]bool{}
	for _, x := range tl {
		tool := x.(map[string]any)
		name := tool["name"].(string)
		names[name] = true
		sch := tool["inputSchema"].(map[string]any)
		if sch["type"] != "object" || tool["description"] == "" || tool["title"] == "" || tool["annotations"] == nil {
			t.Errorf("%s: incomplete definition", name)
		}
		props := sch["properties"].(map[string]any)
		if req, ok := sch["required"].([]any); ok {
			for _, r := range req {
				if props[r.(string)] == nil {
					t.Errorf("%s: required %s not in properties", name, r)
				}
			}
		}
	}
	for _, n := range []string{"list_profiles", "status", "connect", "disconnect", "screenshot", "wait_for_change", "wait_for_stable", "wait", "list_keys",
		"type_text", "key", "key_combo", "key_down", "key_up", "release_input", "mouse_move", "mouse_click", "mouse_double_click", "mouse_drag", "mouse_scroll",
		"power", "mount_iso", "eject_iso"} {
		if !names[n] {
			t.Errorf("missing tool %s", n)
		}
	}
	if len(names) != 23 {
		t.Errorf("unexpected tool count %d", len(names))
	}
}

// pipeServer runs Serve on a pipe so requests can arrive while a tool is running.
func pipeServer(t *testing.T, b Backend) (send func(string), next func() map[string]any, closeIn func() error) {
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	s := NewServer(b, "t", "")
	done := make(chan error, 1)
	go func() { done <- s.Serve(inR, outW); outW.Close() }()
	sc := bufio.NewScanner(outR)
	sc.Buffer(nil, 1<<20)
	lines := make(chan map[string]any, 16)
	go func() {
		for sc.Scan() {
			var m map[string]any
			json.Unmarshal(sc.Bytes(), &m)
			lines <- m
		}
		close(lines)
	}()
	send = func(l string) { inW.Write([]byte(l + "\n")) }
	next = func() map[string]any {
		select {
		case m := <-lines:
			return m
		case <-time.After(3 * time.Second):
			t.Fatal("no reply")
			return nil
		}
	}
	closeIn = func() error {
		inW.Close()
		select {
		case err := <-done:
			return err
		case <-time.After(3 * time.Second):
			t.Fatal("Serve did not return after EOF")
			return nil
		}
	}
	return
}

func TestPingAndCancelDuringLongTool(t *testing.T) {
	realSleep(t)
	send, next, closeIn := pipeServer(t, &fake{})
	send(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"wait","arguments":{"ms":100000}}}`)
	send(`{"jsonrpc":"2.0","id":2,"method":"ping"}`)
	if r := next(); fmt.Sprint(r["id"]) != "2" {
		t.Fatalf("ping must be answered while a tool runs: %v", r)
	}
	start := time.Now()
	send(`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":1,"reason":"user"}}`)
	send(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"wait","arguments":{"ms":1}}}`)
	if r := next(); fmt.Sprint(r["id"]) != "3" {
		t.Fatalf("cancelled request must not be answered: %v", r)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("cancellation did not stop the tool")
	}
	if err := closeIn(); err != nil {
		t.Fatal(err)
	}
}

func TestEOFCancelsRunningTool(t *testing.T) {
	realSleep(t)
	old := ShutdownGrace
	ShutdownGrace = 100 * time.Millisecond
	defer func() { ShutdownGrace = old }()
	send, _, closeIn := pipeServer(t, &fake{})
	send(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"wait","arguments":{"ms":100000}}}`)
	time.Sleep(50 * time.Millisecond)
	closeIn()
}

func TestPipelinedToolsKeepOrder(t *testing.T) {
	f := &fake{}
	var lines []string
	for i := 0; i < 20; i++ {
		lines = append(lines, fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"tools/call","params":{"name":"key_down","arguments":{"keys":["%c"]}}}`, i+1, 'a'+i))
	}
	rs := rpc(t, f, lines...)
	if len(rs) != 20 {
		t.Fatal(len(rs))
	}
	for i, op := range f.ops("key") {
		if op != fmt.Sprintf("%d:true", 4+i) {
			t.Fatalf("tool %d ran out of order: %v", i, f.ops("key"))
		}
	}
}

func callTool(t *testing.T, b Backend, name, args string) map[string]any {
	t.Helper()
	rs := rpc(t, b, fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":%q,"arguments":%s}}`, name, args))
	return rs[0]["result"].(map[string]any)
}

func content(r map[string]any) string { return fmt.Sprint(r["content"]) }

func TestTypeTextSendsShiftedKeys(t *testing.T) {
	f := &fake{}
	res := callTool(t, f, "type_text", `{"text":"A1!\r\n"}`)
	if res["isError"] == true {
		t.Fatalf("%v", res)
	}
	want := "225:true 4:true 4:false 225:false 30:true 30:false 225:true 30:true 30:false 225:false 40:true 40:false"
	if got := strings.Join(f.ops("key"), " "); got != want {
		t.Fatalf("got %s", got)
	}
}

func TestTypeTextRejectsBeforeSending(t *testing.T) {
	f := &fake{}
	res := callTool(t, f, "type_text", `{"text":"ok é"}`)
	if res["isError"] != true || len(f.calls) != 0 || !strings.Contains(content(res), "position 3") {
		t.Fatalf("must reject non-ASCII without sending: %v %v", res, f.calls)
	}
	res = callTool(t, f, "type_text", `{"text":"`+strings.Repeat("a", maxTextLen+1)+`"}`)
	if res["isError"] != true || len(f.calls) != 0 {
		t.Fatal("must enforce length limit")
	}
}

func TestEveryPrintableASCIIIsTyped(t *testing.T) {
	var all strings.Builder
	for r := rune(32); r < 127; r++ {
		all.WriteRune(r)
	}
	b, _ := json.Marshal(map[string]any{"text": all.String()})
	f := &fake{}
	if res := callTool(t, f, "type_text", string(b)); res["isError"] == true {
		t.Fatal(res)
	}
	downs := 0
	for _, op := range f.ops("key") {
		if strings.HasSuffix(op, ":true") && !strings.HasPrefix(op, "225:") {
			downs++
		}
	}
	if downs != 95 {
		t.Fatalf("want 95 key presses, got %d", downs)
	}
}

func TestInputIsReleasedWhenAToolFails(t *testing.T) {
	f := &fake{failAt: 3} // shift down, A down, then A up fails
	res := callTool(t, f, "type_text", `{"text":"A"}`)
	if res["isError"] != true {
		t.Fatal(res)
	}
	seq := strings.Join(f.ops("key"), " ")
	if !strings.HasSuffix(seq, "4:false 225:false") {
		t.Fatalf("held keys must be let go after a failure: %s", seq)
	}
}

func TestKeyNamesCombosAndRepeat(t *testing.T) {
	cases := map[string]string{
		`{"name":"ctrl+alt+f1"}`:         "224:true 226:true 58:true 58:false 226:false 224:false",
		`{"name":"Alt+SysRq+b"}`:         "226:true 70:true 5:true 5:false 70:false 226:false",
		`{"name":"F24"}`:                 "115:true 115:false",
		`{"name":"KP_Enter"}`:            "88:true 88:false",
		`{"name":"Down","repeat":3}`:     "81:true 81:false 81:true 81:false 81:true 81:false",
		`{"name":"PrintScreen"}`:         "70:true 70:false",
		`{"name":"rwin"}`:                "231:true 231:false",
		`{"name":"Pause","hold_ms":200}`: "72:true 72:false",
	}
	for args, want := range cases {
		f := &fake{}
		if res := callTool(t, f, "key", args); res["isError"] == true {
			t.Fatalf("%s: %v", args, res)
		}
		if got := strings.Join(f.ops("key"), " "); got != want {
			t.Errorf("%s: got %s want %s", args, got, want)
		}
	}
	f := &fake{}
	callTool(t, f, "key_combo", `{"keys":["ctrl","alt","delete"]}`)
	if got := strings.Join(f.ops("key"), " "); got != "224:true 226:true 76:true 76:false 226:false 224:false" {
		t.Fatalf("got %v", got)
	}
	for _, bad := range []string{`{"name":"F25"}`, `{"name":"!"}`, `{"name":"ctrl++"}`, `{"name":"a","repeat":0}`} {
		f := &fake{}
		if res := callTool(t, f, "key", bad); res["isError"] != true || len(f.calls) != 0 {
			t.Errorf("%s must fail without sending: %v", bad, res)
		}
	}
}

func TestKeyDownStaysHeld(t *testing.T) {
	f := &fake{}
	callTool(t, f, "key_down", `{"keys":["shift"]}`)
	if got := strings.Join(f.ops("key"), " "); got != "225:true" {
		t.Fatalf("key_down must not release: %s", got)
	}
	f = &fake{}
	callTool(t, f, "key_up", `{"keys":["shift","ctrl"]}`)
	if got := strings.Join(f.ops("key"), " "); got != "225:false 224:false" {
		t.Fatal(got)
	}
}

func TestListKeys(t *testing.T) {
	c := content(callTool(t, &fake{}, "list_keys", `{}`))
	for _, want := range []string{"F24", "KP_Enter", "SysRq", "PageDown", "RightSuper", "CapsLock", "Menu"} {
		if !strings.Contains(c, want) {
			t.Errorf("list_keys lacks %s", want)
		}
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
	if res := callTool(t, f, "power", `{"action":"on"}`); res["isError"] != true || !strings.Contains(content(res), "IPMI") {
		t.Fatal("backend error must surface")
	}
}

func TestScreenshotReturnsImageAndMapping(t *testing.T) {
	f := &fake{result: map[string]string{"screenshot": `{"png":"iVBORw0KGgo=","width":10,"height":5,"fullWidth":10,"fullHeight":5,"x":0,"y":0,"regionW":10,"regionH":5}`}}
	res := callTool(t, f, "screenshot", `{}`)
	c := res["content"].([]any)[0].(map[string]any)
	if c["type"] != "image" || c["mimeType"] != "image/png" || c["data"] != "iVBORw0KGgo=" {
		t.Fatalf("%v", c)
	}
	if f.calls[0].args["maxSize"] != defaultMaxImage {
		t.Fatalf("default max size not sent: %v", f.calls[0].args)
	}
	if !strings.Contains(content(res), "equal framebuffer pixels") {
		t.Fatal(content(res))
	}
	f = &fake{result: map[string]string{"screenshot": `{"png":"AA==","width":784,"height":441,"fullWidth":1920,"fullHeight":1080,"x":0,"y":0,"regionW":1920,"regionH":1080}`}}
	res = callTool(t, f, "screenshot", `{"max_size":784}`)
	if !strings.Contains(content(res), "framebuffer_x = 0 + image_x / 0.4083") {
		t.Fatal(content(res))
	}
	for _, bad := range []string{`{"scale":2}`, `{"max_size":10}`, `{"x":-1}`} {
		if res := callTool(t, &fake{}, "screenshot", bad); res["isError"] != true {
			t.Errorf("%s must fail", bad)
		}
	}
}

func TestConnectWaitsAndReportsFailure(t *testing.T) {
	f := &fake{result: map[string]string{"connect": `{"state":"logging_in","profile":{"name":"Demo"}}`},
		statuses: []string{`{"state":"logging_in"}`, `{"state":"connecting"}`, `{"state":"connected","width":0}`, `{"state":"connected","width":640,"height":480,"consoleInfo":{"protocol":"X"}}`}}
	if res := callTool(t, f, "connect", `{"profile":"demo"}`); res["isError"] == true || !strings.Contains(content(res), "640x480") {
		t.Fatalf("%v", res)
	}
	f = &fake{statuses: []string{`{"state":"logging_in"}`, `{"state":"disconnected","message":"Login failed: bad credentials"}`}}
	res := callTool(t, f, "connect", `{"profile":"Lab-AMI"}`)
	if res["isError"] != true || !strings.Contains(content(res), "Login failed") {
		t.Fatalf("%v", res)
	}
	f = &fake{statuses: []string{`{"state":"connecting","message":"Connecting"}`}}
	res = callTool(t, f, "connect", `{"profile":"x","timeout_ms":1000}`)
	if res["isError"] != true || !strings.Contains(content(res), "timed out") {
		t.Fatalf("%v", res)
	}
}

func TestConnectRejectsAProfileSwitchedMeanwhile(t *testing.T) {
	f := &fake{result: map[string]string{"connect": `{"state":"logging_in","profile":{"id":"a","name":"A"}}`},
		statuses: []string{`{"state":"connected","width":800,"height":600,"profile":{"id":"b","name":"B"}}`}}
	if res := callTool(t, f, "connect", `{"profile":"A"}`); res["isError"] != true || !strings.Contains(content(res), "another profile (B)") {
		t.Fatalf("%v", res)
	}
}

func TestConnectWaitsForThePassword(t *testing.T) {
	pw := `{"state":"disconnected","passwordRequired":true,"message":"Enter this profile's password"}`
	f := &fake{result: map[string]string{"connect": `{"state":"disconnected","passwordRequired":true,"profile":{"name":"Lab-AMI"}}`},
		statuses: []string{pw, pw, `{"state":"logging_in"}`, `{"state":"connected","width":800,"height":600}`}}
	if res := callTool(t, f, "connect", `{"profile":"lab-ami"}`); res["isError"] == true {
		t.Fatalf("must succeed once the user typed the password: %v", res)
	}
	f = &fake{result: map[string]string{"connect": `{"state":"disconnected","passwordRequired":true,"profile":{"name":"Lab-AMI"}}`}, statuses: []string{pw}}
	res := callTool(t, f, "connect", `{"profile":"lab-ami","password_wait_ms":0}`)
	if res["isError"] != true || !strings.Contains(content(res), "Password required") || !strings.Contains(content(res), "Lab-AMI") {
		t.Fatalf("%v", res)
	}
}

func TestWaits(t *testing.T) {
	f := &fake{statuses: []string{`{"state":"connected","changeSeq":1}`, `{"state":"connected","changeSeq":1}`, `{"state":"connected","changeSeq":2}`}}
	if c := content(callTool(t, f, "wait_for_change", `{}`)); !strings.Contains(c, "changed") {
		t.Fatal(c)
	}
	f = &fake{statuses: []string{`{"state":"connected","msSinceChange":10}`, `{"state":"connected","msSinceChange":900}`}}
	if c := content(callTool(t, f, "wait_for_stable", `{}`)); !strings.Contains(c, "Stable") {
		t.Fatal(c)
	}
	f = &fake{statuses: []string{`{"state":"disconnected","message":"Disconnected."}`}}
	if res := callTool(t, f, "wait_for_stable", `{}`); res["isError"] != true {
		t.Fatal("waiting on a closed console must fail")
	}
}

func TestMouse(t *testing.T) {
	f := &fake{}
	callTool(t, f, "mouse_click", `{"x":10,"y":20,"button":"right","modifiers":["shift"]}`)
	if got := strings.Join(f.ops(""), " "); got != "p10,20,0,0 225:true p10,20,2,0 p10,20,0,0 225:false" {
		t.Fatal(got)
	}
	f = &fake{}
	callTool(t, f, "mouse_double_click", `{"x":1,"y":2}`)
	if got := strings.Join(f.ops(""), " "); got != "p1,2,0,0 p1,2,1,0 p1,2,0,0 p1,2,1,0 p1,2,0,0" {
		t.Fatal(got)
	}
	f = &fake{}
	callTool(t, f, "mouse_scroll", `{"x":5,"y":5,"amount":-2}`)
	if got := strings.Join(f.ops(""), " "); got != "p5,5,0,0 p5,5,0,-1 p5,5,0,-1" {
		t.Fatal(got)
	}
	f = &fake{}
	callTool(t, f, "mouse_drag", `{"x":0,"y":0,"to_x":100,"to_y":50}`)
	ops := f.ops("")
	if ops[1] != "p0,0,1,0" || ops[len(ops)-2] != "p100,50,1,0" || ops[len(ops)-1] != "p100,50,0,0" {
		t.Fatal(ops)
	}
	f = &fake{err: map[string]error{}}
	if res := callTool(t, f, "mouse_click", `{"x":-1,"y":2}`); res["isError"] != true || len(f.calls) != 0 {
		t.Fatal("negative coords must fail")
	}
}

func TestInstructionsContainPath(t *testing.T) {
	s := Instructions("/opt/x/oldipmi-mcp")
	if !strings.Contains(s, "claude mcp add oldipmi -- /opt/x/oldipmi-mcp") || strings.Contains(s, "{{") {
		t.Fatal(s)
	}
}
