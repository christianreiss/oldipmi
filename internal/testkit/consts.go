package testkit

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// StringConsts reads the string constants of the Go files in dir (tests left
// out) whose names start with prefix, by the rest of their name. The names
// tests check them against the testdata/*/names.json files.
func StringConsts(t testing.TB, dir, prefix string) map[string]string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no Go files in %s: %v", dir, err)
	}
	out := map[string]string{}
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range f.Decls {
			g, ok := d.(*ast.GenDecl)
			if !ok || g.Tok != token.CONST {
				continue
			}
			for _, spec := range g.Specs {
				v := spec.(*ast.ValueSpec)
				for i, id := range v.Names {
					if !strings.HasPrefix(id.Name, prefix) {
						continue
					}
					var lit *ast.BasicLit
					if i < len(v.Values) {
						lit, _ = v.Values[i].(*ast.BasicLit)
					}
					if lit == nil || lit.Kind != token.STRING {
						t.Errorf("%s: %s is not a string literal", name, id.Name)
						continue
					}
					out[strings.TrimPrefix(id.Name, prefix)], _ = strconv.Unquote(lit.Value)
				}
			}
		}
	}
	return out
}
