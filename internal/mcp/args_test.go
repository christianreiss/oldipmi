package mcp

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// testdata/tools.json is the tool list as agents see it (tools/list): every
// change of a name, a description or an argument shows in its diff.
// UPDATE_GOLDEN=1 writes it anew.
func TestToolListMatchesRecording(t *testing.T) {
	rs := rpc(t, &fake{}, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	got, err := json.MarshalIndent(rs[0]["result"].(map[string]any)["tools"], "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	const path = "testdata/tools.json"
	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("the tool list differs from %s; run UPDATE_GOLDEN=1 go test ./internal/mcp and review the diff", path)
	}
}

// What each tool makes of its arguments: the exact error of a call it refuses,
// or the requests it sends to the app for one it carries out.
func TestArgumentChecks(t *testing.T) {
	for _, c := range []struct {
		tool, args string
		err        string // the error, exactly; empty when the call is carried out
		sent       string // then the requests it sends: op and args of each, space-separated
	}{
		{tool: "key", args: `{"name":"a","profile":""}`, err: "profile must be a non-empty string"},
		{tool: "key", args: `{"name":"a","profile":5}`, err: "profile must be a non-empty string"},

		{tool: "connect", args: `{}`, err: "profile is required"},
		{tool: "connect", args: `{"profile":""}`, err: "profile must be a non-empty string"},
		{tool: "connect", args: `{"profile":5}`, err: "profile must be a non-empty string"},
		{tool: "connect", args: `{"profile":"x","timeout_ms":999}`, err: "timeout_ms must be an integer in 1000..300000"},
		{tool: "connect", args: `{"profile":"x","timeout_ms":1500.5}`, err: "timeout_ms must be an integer in 1000..300000"},
		{tool: "connect", args: `{"profile":"x","password_wait_ms":-1}`, err: "password_wait_ms must be an integer in 0..300000"},

		{tool: "disconnect", args: `{"all":true,"profile":"A"}`, err: "give either profile or all, not both"},
		{tool: "disconnect", args: `{"all":"yes"}`, sent: `disconnect{}`},
		{tool: "disconnect", args: `{"all":true}`, sent: `disconnect{"all":true}`},

		{tool: "screenshot", args: `{"x":-1}`, err: "x must be an integer in 0..4096"},
		{tool: "screenshot", args: `{"height":4097}`, err: "height must be an integer in 0..4096"},
		{tool: "screenshot", args: `{"x":4096}`, sent: `screenshot{"maxSize":1568,"x":4096}`},
		{tool: "screenshot", args: `{"width":0}`, sent: `screenshot{"maxSize":1568,"width":0}`},
		{tool: "screenshot", args: `{"x":null}`, sent: `screenshot{"maxSize":1568,"x":0}`},
		{tool: "screenshot", args: `{"scale":0.05}`, err: "scale must be a number in 0.1..1"},
		{tool: "screenshot", args: `{"scale":"1"}`, err: "scale must be a number in 0.1..1"},
		{tool: "screenshot", args: `{"max_size":32}`, err: "max_size must be 0 or 64..4096"},
		{tool: "screenshot", args: `{"max_size":5000}`, err: "max_size must be an integer in 0..4096"},
		{tool: "screenshot", args: `{"x":10,"y":20,"width":30,"height":40,"scale":0.5,"max_size":0}`,
			sent: `screenshot{"height":40,"maxSize":0,"scale":0.5,"width":30,"x":10,"y":20}`},

		{tool: "wait_for_change", args: `{"timeout_ms":-5}`, err: "timeout_ms must be an integer in 0..300000"},
		{tool: "wait_for_stable", args: `{"quiet_ms":"500"}`, err: "quiet_ms must be an integer in 0..300000"},
		{tool: "wait_for_stable", args: `{"timeout_ms":300001}`, err: "timeout_ms must be an integer in 0..300000"},
		{tool: "wait", args: `{}`, err: "ms is required"},
		{tool: "wait", args: `{"ms":null}`, err: "ms is required"},
		{tool: "wait", args: `{"ms":300001}`, err: "ms must be an integer in 0..300000"},

		{tool: "type_text", args: `{}`, err: "text is required"},
		{tool: "type_text", args: `{"text":""}`, err: "text must be a non-empty string"},
		{tool: "type_text", args: `{"text":null}`, err: "text must be a non-empty string"},
		{tool: "type_text", args: `{"text":"a","delay_ms":2001}`, err: "delay_ms must be an integer in 0..2000"},
		{tool: "type_text", args: `{"text":"é"}`, err: "character 'é' at position 0 cannot be typed on a US layout (only printable ASCII, newline and tab)"},

		{tool: "key", args: `{}`, err: "name is required"},
		{tool: "key", args: `{"name":"nosuchkey"}`, err: `unknown key "nosuchkey" (list_keys shows all key names)`},
		{tool: "key", args: `{"name":"nosuchkey","repeat":0}`, err: `unknown key "nosuchkey" (list_keys shows all key names)`},
		{tool: "key", args: `{"name":"a+b+c+d+e+f+g"}`, err: "at most 6 keys at once"},
		{tool: "key", args: `{"name":"a","repeat":0}`, err: "repeat must be an integer in 1..100"},
		{tool: "key", args: `{"name":"a","hold_ms":10001}`, err: "hold_ms must be an integer in 0..10000"},
		{tool: "key", args: `{"name":"ctrl+a","hold_ms":0}`, sent: `key{"down":true,"usage":224} key{"down":true,"usage":4} key{"down":false,"usage":4} key{"down":false,"usage":224}`},

		{tool: "key_combo", args: `{"keys":[]}`, err: "keys must list 1..6 key names"},
		{tool: "key_combo", args: `{"keys":"ctrl"}`, err: "keys must list 1..6 key names"},
		{tool: "key_combo", args: `{"keys":["a","b","c","d","e","f","g"]}`, err: "keys must list 1..6 key names"},
		{tool: "key_combo", args: `{"keys":[1]}`, err: `unknown key "" (list_keys shows all key names)`},
		{tool: "key_combo", args: `{"keys":["a"],"hold_ms":-1}`, err: "hold_ms must be an integer in 0..10000"},
		{tool: "key_down", args: `{"keys":["nosuch"]}`, err: `unknown key "nosuch" (list_keys shows all key names)`},
		{tool: "key_up", args: `{}`, err: "keys must list 1..6 key names"},

		{tool: "mouse_move", args: `{"x":1}`, err: "y is required"},
		{tool: "mouse_move", args: `{"x":4096,"y":0}`, err: "x must be an integer in 0..4095"},
		{tool: "mouse_click", args: `{"x":1,"y":1,"button":"side"}`, err: "button must be left, right or middle"},
		{tool: "mouse_click", args: `{"x":1,"y":1,"button":"LEFT"}`, sent: clicks(1, 1)},
		{tool: "mouse_click", args: `{"x":1,"y":1,"button":7}`, sent: clicks(1, 1)},
		{tool: "mouse_click", args: `{"x":1,"y":1,"button":""}`, sent: clicks(1, 1)},
		{tool: "mouse_click", args: `{"x":1,"y":1,"modifiers":"shift"}`, sent: clicks(1, 1)},
		{tool: "mouse_click", args: `{"x":1,"y":1,"modifiers":[]}`, sent: clicks(1, 1)},
		{tool: "mouse_click", args: `{"x":1,"y":1,"modifiers":["a","b","c","d","e"]}`, err: "modifiers must list 1..4 key names"},
		{tool: "mouse_double_click", args: `{"x":1,"y":1,"button":"right"}`, sent: clicks(2, 2)},
		{tool: "mouse_drag", args: `{"x":1,"y":1,"to_x":2}`, err: "to_y is required"},
		{tool: "mouse_drag", args: `{"x":1,"y":1,"to_x":2,"to_y":2,"button":"middle"}`, err: ""},
		{tool: "mouse_scroll", args: `{"x":1,"y":1}`, err: "amount must be a non-zero integer in -20..20"},
		{tool: "mouse_scroll", args: `{"x":1,"y":1,"amount":0}`, err: "amount must be a non-zero integer in -20..20"},
		{tool: "mouse_scroll", args: `{"x":1,"y":1,"amount":21}`, err: "amount must be a non-zero integer in -20..20"},

		{tool: "power", args: `{}`, err: "action is required"},
		{tool: "power", args: `{"action":5}`, err: "action must be a non-empty string"},
		{tool: "power", args: `{"action":"explode"}`, err: "action must be on, off, reset or soft"},
		{tool: "power", args: `{"action":"ON"}`, err: "action must be on, off, reset or soft"},
		{tool: "power", args: `{"action":"soft"}`, sent: `power{"action":"soft"}`},

		{tool: "mount_iso", args: `{}`, sent: `mount{}`},
		{tool: "mount_iso", args: `{"path":5}`, sent: `mount{"path":5}`},
		{tool: "mount_iso", args: `{"path":"/srv/x.iso"}`, sent: `mount{"path":"/srv/x.iso"}`},
	} {
		f := &fake{result: map[string]string{"screenshot": `{"png":"iVBO","width":1,"height":1,"fullWidth":1,"fullHeight":1,"x":0,"y":0,"regionW":1,"regionH":1}`}}
		res := callTool(t, f, c.tool, c.args)
		blocks, _ := res["content"].([]any)
		msg := ""
		if len(blocks) > 0 {
			msg, _ = blocks[len(blocks)-1].(map[string]any)["text"].(string)
		}
		var sent []string
		for _, x := range f.calls {
			b, _ := json.Marshal(x.args)
			sent = append(sent, x.op+string(b))
		}
		switch {
		case c.err != "" && (res["isError"] != true || msg != c.err):
			t.Errorf("%s %s: %q (error %v), want the error %q", c.tool, c.args, msg, res["isError"], c.err)
		case c.err != "" && len(f.calls) != 0:
			t.Errorf("%s %s: refused, but sent %s", c.tool, c.args, strings.Join(sent, " "))
		case c.err == "" && res["isError"] == true:
			t.Errorf("%s %s: %q, want it carried out", c.tool, c.args, msg)
		case c.err == "" && c.sent != "" && strings.Join(sent, " ") != c.sent:
			t.Errorf("%s %s: sent\n%s\nwant\n%s", c.tool, c.args, strings.Join(sent, " "), c.sent)
		}
	}
}

// clicks are the pointer requests of n clicks at 1,1 with the given buttons:
// the move there, then a press and a release for each click.
func clicks(buttons, n int) string {
	p := func(b int) string {
		return `pointer{"buttons":` + string(rune('0'+b)) + `,"wheel":0,"x":1,"y":1}`
	}
	out := p(0)
	for range n {
		out += " " + p(buttons) + " " + p(0)
	}
	return out
}
