package task

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"

	"github.com/heyNag/eva-md/internal/redact"
)

// workplacePattern reads the pattern TestThePublicRepoUsesNeutralWording
// holds, from its own file, so the two cannot drift apart.
func workplacePattern(t *testing.T) *regexp.Regexp {
	t.Helper()
	path := filepath.Join(moduleRoot(t), "cmd", "eva-md", "wording_test.go")
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var pattern string
	ast.Inspect(file, func(n ast.Node) bool {
		spec, ok := n.(*ast.ValueSpec)
		if !ok || len(spec.Names) != 1 || spec.Names[0].Name != "workplaceWording" || len(spec.Values) != 1 {
			return true
		}
		if call, ok := spec.Values[0].(*ast.CallExpr); ok && len(call.Args) == 1 {
			if lit, ok := call.Args[0].(*ast.BasicLit); ok {
				pattern, _ = strconv.Unquote(lit.Value)
			}
		}
		return false
	})
	if pattern == "" {
		t.Fatalf("found no workplaceWording pattern in %s", path)
	}
	return regexp.MustCompile(pattern)
}

// Nothing under eval/pilot looks like a secret to the redaction rules, or
// speaks of a workplace as TestThePublicRepoUsesNeutralWording forbids,
// which reads nothing under eval/.
func TestEvalPilotHoldsNoSecretAndNoWorkplace(t *testing.T) {
	workplace := workplacePattern(t)
	if !workplace.MatchString("a work laptop") {
		t.Fatal("the workplace pattern read is not the one meant")
	}
	if len(redact.Scan(context.Background(), "password=Xk29fLq0Zr8vTw3m").Findings) == 0 {
		t.Fatal("the redaction rules find nothing in a made-up secret")
	}
	root := filepath.Join(moduleRoot(t), "eval", "pilot")
	n := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		n++
		rel, _ := filepath.Rel(root, path)
		if r := redact.Scan(context.Background(), string(data)); len(r.Findings) > 0 {
			t.Errorf("%s holds %d ranges the redaction rules would replace, the first a %s", rel, len(r.Findings), r.Findings[0].Kind)
		}
		if m := workplace.FindString(string(data)); m != "" {
			t.Errorf("%s says %q", rel, m)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n < 50 {
		t.Fatalf("read %d files under eval/pilot; the walk read the wrong folder", n)
	}
}
