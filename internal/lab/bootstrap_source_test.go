package lab

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBootstrapDefinitionsAreSeparatePinnedPreparation(t *testing.T) {
	root := filepath.Join("..", "..")
	trusted, e := os.ReadFile(filepath.Join(root, "deploy/custom-lab/Dockerfile"))
	if e != nil {
		t.Fatal(e)
	}
	s := string(trusted)
	for _, required := range []string{"golang:1.26.8-bookworm@sha256:dc9ad6c05acc7a88e5b71bde60a5fe3bd4b9f0db209011711b464107438a8107", "CGO_ENABLED=0 GOOS=linux GOARCH=arm64", "FROM scratch", "./cmd/lab-worker", "./cmd/lab-fixture", "com.faultline.lab.contract=\"http-work-v1\"", "SOURCE_REVISION", "USER 65532:65532"} {
		if !strings.Contains(s, required) {
			t.Fatal(required)
		}
	}
	for _, bad := range []string{"./cmd/sample", "./cmd/faultline ", "VOLUME", "--privileged", "docker.sock"} {
		if strings.Contains(s, bad) {
			t.Fatal("unsafe bootstrap", bad)
		}
	}
	app := filepath.Join(root, "examples/custom-service/http-work/main.go")
	ast, e := parser.ParseFile(token.NewFileSet(), app, nil, parser.ImportsOnly)
	if e != nil {
		t.Fatal(e)
	}
	for _, imp := range ast.Imports {
		if strings.Contains(imp.Path.Value, "github.com") || strings.Contains(imp.Path.Value, "faultline") {
			t.Fatal("app is not independent")
		}
	}
}
