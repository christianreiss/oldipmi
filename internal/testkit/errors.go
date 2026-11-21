package testkit

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"oldipmi/internal/protocol"
)

// testdata/ipc/errors.json holds errors as the core words them, with the class
// it sends as their code, for the desktop's hint tests. The connection errors
// come from internal/protocol's tests; each driver's tests check the rows of
// its own errors with ErrorRow.

// ErrorExample is a row of testdata/ipc/errors.json; Code is "" for an error
// of no known class.
type ErrorExample struct {
	Name    string `json:"name"`
	Message string `json:"message"`
	Code    string `json:"code"`
}

// ErrorsFile is testdata/ipc/errors.json.
type ErrorsFile struct {
	About  string         `json:"about"`
	Errors []ErrorExample `json:"errors"`
}

// ReadErrors reads testdata/ipc/errors.json and returns its path too.
func ReadErrors(t testing.TB) (string, ErrorsFile) {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the test's directory")
		}
		dir = parent
	}
	path := filepath.Join(dir, "testdata", "ipc", "errors.json")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var f ErrorsFile
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return path, f
}

// WriteErrors writes f to path, a row to a line.
func WriteErrors(t testing.TB, path string, f ErrorsFile) {
	t.Helper()
	quote := func(s string) string {
		var b bytes.Buffer
		e := json.NewEncoder(&b)
		e.SetEscapeHTML(false)
		e.Encode(s)
		return strings.TrimSuffix(b.String(), "\n")
	}
	var b strings.Builder
	b.WriteString("{\n  \"about\": " + quote(f.About) + ",\n  \"errors\": [\n")
	for i, r := range f.Errors {
		b.WriteString(`    {"name": ` + quote(r.Name) + `, "message": ` + quote(r.Message) + `, "code": ` + quote(r.Code) + "}")
		if i < len(f.Errors)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	b.WriteString("  ]\n}\n")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

// ErrorRow checks err against the row name of testdata/ipc/errors.json: the
// core must word err as the row does and give it the row's code.
func ErrorRow(t testing.TB, name string, err error) {
	t.Helper()
	_, f := ReadErrors(t)
	for _, r := range f.Errors {
		if r.Name != name {
			continue
		}
		if err == nil || err.Error() != r.Message || protocol.ErrorCode(err) != r.Code {
			t.Errorf("errors.json row %q has %q with code %q; the core sends %v with code %q",
				name, r.Message, r.Code, err, protocol.ErrorCode(err))
		}
		return
	}
	t.Errorf("errors.json has no row %q", name)
}
