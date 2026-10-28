package lab

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Defense against accidentally wiring the contract to production invocation.
// This source check complements behavior tests; it is not a security proof.
func TestProductionHasNoMutationInvocationBoundary(t *testing.T) {
	files, e := filepath.Glob("*.go")
	if e != nil {
		t.Fatal(e)
	}
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, e := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if e != nil {
			t.Fatal(e)
		}
		for _, imp := range file.Imports {
			path, _ := strconv.Unquote(imp.Path.Value)
			if path == "os/exec" && name != "docker.go" && name != "process_other.go" && name != "process_unix.go" {
				t.Fatal("new subprocess boundary", name)
			}
		}
		ast.Inspect(file, func(n ast.Node) bool {
			c, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			var called string
			switch f := c.Fun.(type) {
			case *ast.Ident:
				called = f.Name
			case *ast.SelectorExpr:
				called = f.Sel.Name
			}
			switch called {
			case "activateAttempt", "dispatchPermit", "certifyResult":
				t.Errorf("production activation/response path in %s: %s", name, called)
			}
			return true
		})
	}
}
