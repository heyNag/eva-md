package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/install"
)

// goList lists a package's dependencies, as go list -deps does.
func goList(t *testing.T, pkg string) []string {
	t.Helper()
	cmd := exec.Command("go", "list", "-deps", pkg)
	cmd.Dir = filepath.Join("..", "..")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list -deps %s: %v", pkg, err)
	}
	return strings.Fields(string(out))
}

// No release holds the recorder: eva-md imports neither it nor its
// package, no script or workflow builds or names it, and it imports
// nothing outside the standard library, as its card asks.
func TestNoReleaseContainsTheRecorder(t *testing.T) {
	product := goList(t, "./cmd/eva-md")
	if len(product) < 50 {
		t.Fatalf("eva-md's dependencies: %v", product)
	}
	for _, p := range product {
		if strings.Contains(p, "hostfacts") || strings.Contains(p, "hostrec") {
			t.Errorf("eva-md imports %s", p)
		}
	}
	for _, p := range goList(t, "./cmd/hostrec") {
		if first, _, _ := strings.Cut(p, "/"); strings.Contains(first, ".") && !strings.HasPrefix(p, "github.com/heyNag/eva-md/") {
			t.Errorf("the recorder imports %s, which is not in the standard library", p)
		}
	}
	root := filepath.Join("..", "..")
	files, _ := filepath.Glob(filepath.Join(root, ".github", "workflows", "*.y*ml"))
	scripts, _ := filepath.Glob(filepath.Join(root, "scripts", "*"))
	if len(files) < 3 || len(scripts) < 3 {
		t.Fatalf("found %v and %v; the check read the wrong folders", files, scripts)
	}
	name := regexp.MustCompile(`hostrec|hostfacts`)
	for _, path := range append(files, scripts...) {
		if info, err := os.Stat(path); err != nil || info.IsDir() {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if m := name.Find(data); m != nil {
			t.Errorf("%s names %s", path, m)
		}
	}
	if installRecord != install.RecordFile {
		t.Errorf("the recorder looks for %s; eva-md's install record is %s", installRecord, install.RecordFile)
	}
}
