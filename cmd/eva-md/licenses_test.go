package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// The architectures the release workflow builds a binary for.
var releaseArches = []string{"arm64", "amd64"}

// The headings of LICENSES.txt's sections after the modules'.
const (
	goLicenseTitle = "The Go runtime and standard library"
	goNoticesTitle = "Notices in parts of the Go runtime and standard library"
)

// Each release publishes scripts/LICENSES.txt beside its binaries, since
// the licenses of the code built into them ask for their notices to go with
// every copy. It starts with eva-md's license. Then comes a section for each
// module the binaries are built with, and no other, holding the license
// file the module ships, then Go's license, whose runtime and standard
// library every binary contains, then the notices in Go's own code that ask
// to go with a copy of the program. The modules and Go's files are those go
// list gives for the release builds; Go's parts are read from the Go that
// runs the test, so the file holds those of each Go the tests run with, the
// release's included.
func TestReleaseLicenses(t *testing.T) {
	root := filepath.Join(buildDir, "..", "..")
	workflow := readText(t, filepath.Join(root, ".github", "workflows", "release.yml"))
	if m := regexp.MustCompile(`(?m)^\s+for arch in (.+); do$`).FindAllStringSubmatch(workflow, -1); len(m) != 1 || !slices.Equal(strings.Fields(m[0][1]), releaseArches) {
		t.Fatalf("the release workflow's build loops %q don't build for %q", m, releaseArches)
	}
	build := listRelease(t, buildDir)
	if len(build.modules) == 0 || len(build.files) < 100 {
		t.Fatalf("go list gave %d modules and %d files of Go's; the check read the wrong build", len(build.modules), len(build.files))
	}
	file := readText(t, filepath.Join(root, "scripts", "LICENSES.txt"))
	lacks := func(what, text string) {
		t.Helper()
		t.Errorf("scripts/LICENSES.txt lacks %s. Add:\n%s", what, text)
	}
	if head := "eva-md\n======\n\n" + readText(t, filepath.Join(root, "LICENSE")) + "\nThe eva-md binaries also contain the code below, each under its own license.\n"; !strings.HasPrefix(file, head) {
		lacks("eva-md's license at its start", head)
	}
	var headings []string
	for _, m := range build.modules {
		if s := section(m.title, readText(t, licenseFile(t, m.dir))); !strings.Contains(file, s) {
			lacks(m.title+"'s license", s)
		}
		headings = append(headings, m.title)
	}
	goroot := strings.TrimSpace(goCommand(t, buildDir, nil, "env", "GOROOT"))
	if s := section(goLicenseTitle, readText(t, goLicense(t, goroot))); !strings.Contains(file, s) {
		lacks("Go's license", s)
	}
	_, notices, _ := strings.Cut(file, "\n"+goNoticesTitle+"\n")
	for _, n := range goNotices(t, goroot, build.files) {
		if !strings.Contains(notices, "\n"+n.text+"\n") {
			lacks("the notice in Go's "+n.file+", under "+goNoticesTitle, n.text)
		}
	}
	if got, want := fileHeadings(file), append(headings, goLicenseTitle, goNoticesTitle); !slices.Equal(got, want) {
		t.Errorf("scripts/LICENSES.txt has the sections %q, not %q", got, want)
	}
}

// releaseBuild is what a release is built from, as go list gives it.
type releaseBuild struct {
	modules []listedModule // by heading
	files   []string       // of Go's standard library, sorted
}

// listedModule is a module other than eva-md: its heading in LICENSES.txt,
// which gives its path and version and what replaces it, if anything does,
// and the folder of its files, which for a replaced module is the
// replacement's.
type listedModule struct{ title, dir string }

// listRelease lists the main module in dir as the release workflow builds
// it, for darwin on each of its architectures with cgo off and no build
// tags, both builds together.
func listRelease(t *testing.T, dir string) releaseBuild {
	t.Helper()
	const format = `{{if .Standard}}{{range .GoFiles}}file{{"\t"}}{{$.Dir}}/{{.}}{{"\n"}}{{end}}{{range .SFiles}}file{{"\t"}}{{$.Dir}}/{{.}}{{"\n"}}{{end}}` +
		`{{else}}{{with .Module}}{{if not .Main}}module{{"\t"}}{{.Path}} {{.Version}}{{with .Replace}} => {{.Path}}{{with .Version}} {{.}}{{end}}{{end}}{{"\t"}}{{.Dir}}{{"\n"}}{{end}}{{end}}{{end}}`
	var b releaseBuild
	seen := map[string]bool{}
	for _, arch := range releaseArches {
		out := goCommand(t, dir, []string{"GOOS=darwin", "GOARCH=" + arch, "CGO_ENABLED=0"}, "list", "-deps", "-f", format, ".")
		for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
			kind, rest, _ := strings.Cut(line, "\t")
			switch title, folder, _ := strings.Cut(rest, "\t"); {
			case kind == "file":
				b.files = append(b.files, rest)
			case kind == "module" && folder != "":
				if !seen[title] {
					seen[title] = true
					b.modules = append(b.modules, listedModule{title, folder})
				}
			case line != "":
				t.Fatalf("go list for %s printed %q", arch, line)
			}
		}
	}
	slices.Sort(b.files)
	b.files = slices.Compact(b.files)
	slices.SortFunc(b.modules, func(x, y listedModule) int { return strings.Compare(x.title, y.title) })
	return b
}

// A replaced module goes under a heading that names what replaces it, and
// its license is read from there: here a folder, as the temporary replace
// for a security fix that design.md allows is.
func TestListReleaseReadsAReplacedModulesFolder(t *testing.T) {
	// go list gives the folder with its symlinks resolved, as macOS's
	// temporary folder has them.
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"go.mod":       "module example.com/app\n\ngo 1.26.0\n\nrequire example.com/lib v1.0.0\n\nreplace example.com/lib => ./fork\n",
		"main.go":      "package main\n\nimport \"example.com/lib\"\n\nfunc main() { lib.F() }\n",
		"fork/go.mod":  "module example.com/lib\n\ngo 1.26.0\n",
		"fork/lib.go":  "package lib\n\nfunc F() {}\n",
		"fork/LICENSE": "A made-up license.\n",
	} {
		if err := os.MkdirAll(filepath.Join(dir, filepath.Dir(name)), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	build := listRelease(t, dir)
	want := []listedModule{{"example.com/lib v1.0.0 => ./fork", filepath.Join(dir, "fork")}}
	if !slices.Equal(build.modules, want) {
		t.Fatalf("modules %q, want %q", build.modules, want)
	}
	if got := licenseFile(t, build.modules[0].dir); got != filepath.Join(dir, "fork", "LICENSE") {
		t.Fatalf("license file %s", got)
	}
}

// licenseFile is the license file at the root of a module's folder.
func licenseFile(t *testing.T, dir string) string {
	t.Helper()
	for _, name := range []string{"LICENSE", "LICENSE.md", "LICENSE.txt", "COPYING"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return filepath.Join(dir, name)
		}
	}
	t.Fatalf("found no license file in %s", dir)
	return ""
}

// goLicense is the license file of the Go at goroot: in it, or beside it
// for Homebrew's Go, whose GOROOT is a libexec folder.
func goLicense(t *testing.T, goroot string) string {
	t.Helper()
	files := []string{filepath.Join(goroot, "LICENSE")}
	if filepath.Base(goroot) == "libexec" {
		files = append(files, filepath.Join(filepath.Dir(goroot), "LICENSE"))
	}
	for _, f := range files {
		if _, err := os.Stat(f); err == nil {
			return f
		}
	}
	t.Fatalf("found no license file for Go: %s", strings.Join(files, " or "))
	return ""
}

// goNotice is a notice in Go's own code, from the first file it is in.
type goNotice struct{ file, text string }

// goNotices are the notices in files of Go's, the code the release binaries
// are built from, that ask to go with a copy of the program beside Go's
// license, each distinct one once.
func goNotices(t *testing.T, goroot string, files []string) []goNotice {
	t.Helper()
	var notices []goNotice
	seen := map[string]bool{}
	for _, f := range files {
		rel, err := filepath.Rel(filepath.Join(goroot, "src"), f)
		if err != nil {
			t.Fatal(err)
		}
		found, err := fileNotices(readText(t, f))
		if err != nil {
			t.Errorf("Go's %s: %v", rel, err)
		}
		for _, text := range found {
			if !seen[text] {
				seen[text] = true
				notices = append(notices, goNotice{filepath.ToSlash(rel), text})
			}
		}
	}
	if len(notices) == 0 {
		t.Fatal("found no notices in Go's code; the check read the wrong files")
	}
	return notices
}

// copyrightLine finds a line that names a copyright.
var copyrightLine = regexp.MustCompile(`(?i)\bcopyright\b`)

// freeNotices are the notices in Go's own code that ask nothing of a copy
// of the program, by their first line: the Cephes library may be used
// freely, and fiat-crypto's license binds only copies of its source.
var freeNotices = map[string]bool{
	"Copyright 1984, 1987, 1989, 1992, 2000 by Stephen L. Moshier":           true,
	"Copyright (c) 2015-2020 the fiat-crypto authors (see the AUTHORS file)": true,
	"Copyright (c) 2015-2020 The fiat-crypto Authors. All rights reserved.":  true,
}

// fileNotices are the notices in a Go or assembly file that ask to go with
// a copy of the program. A notice starts at a copyright line of someone
// other than the Go Authors and runs to the end of its comment, or to a
// line of = signs, which sets off the notices of the fdlibm code in package
// math. A copyright line outside a // comment is an error, since its notice
// can't be read this way.
func fileNotices(text string) ([]string, error) {
	var notices []string
	lines := strings.Split(text, "\n")
	for i := 0; i < len(lines); i++ {
		if !copyrightLine.MatchString(lines[i]) || strings.Contains(lines[i], "The Go Authors") {
			continue
		}
		if _, ok := commentText(lines[i]); !ok {
			return notices, fmt.Errorf("a copyright line outside a // comment, whose notice the check can't read: %s", strings.TrimSpace(lines[i]))
		}
		var notice []string
		for ; i < len(lines); i++ {
			s, ok := commentText(lines[i])
			if !ok || s != "" && strings.Trim(s, "=") == "" {
				break
			}
			notice = append(notice, s)
		}
		if !freeNotices[notice[0]] {
			notices = append(notices, strings.TrimRight(strings.Join(notice, "\n"), "\n"))
		}
	}
	return notices, nil
}

// A notice runs from its copyright line to the end of its comment or to a
// line of = signs; Go's own copyright lines and the notices that ask
// nothing of a copy start none, and a copyright line the check can't read
// is an error.
func TestFileNotices(t *testing.T) {
	for _, c := range []struct {
		name, text string
		want       []string
		err        bool
	}{
		{"Go's own", "// Copyright 2009 The Go Authors. All rights reserved.\n// Use of this source code is governed by a BSD-style\n// license that can be found in the LICENSE file.\n\npackage math\n", nil, false},
		{"set off by = signs", "// The original C code came with this notice.\n//\n// ====\n// Copyright (C) 1993 by Someone, Inc.\n//\n// Use it, provided that this notice\n//   is preserved.\n// ====\n//\n// How it works.\nfunc f() {}\n",
			[]string{"Copyright (C) 1993 by Someone, Inc.\n\nUse it, provided that this notice\nis preserved."}, false},
		{"to the end of the comment", "// Derived from elsewhere.\n//\n//         Copyright © 1994 Someone Inc.\n//         Portions Copyright 2009 The Go Authors.\n//\n// Keep this notice in all copies.\n//\n\n#include \"textflag.h\"\n",
			[]string{"Copyright © 1994 Someone Inc.\nPortions Copyright 2009 The Go Authors.\n\nKeep this notice in all copies."}, false},
		{"two in a file", "// Copyright 2001 One\n// Keep it.\n\n// Copyright 2002 Two\n// Keep this too.\n",
			[]string{"Copyright 2001 One\nKeep it.", "Copyright 2002 Two\nKeep this too."}, false},
		{"one that asks nothing", "// Copyright 1984, 1987, 1989, 1992, 2000 by Stephen L. Moshier\n//\n// What you see here may be used freely.\n", nil, false},
		{"in a block comment", "/*\n   Copyright 1993 Someone\n   Keep this notice.\n*/\n", nil, true},
	} {
		got, err := fileNotices(c.text)
		if !slices.Equal(got, c.want) || (err != nil) != c.err {
			t.Errorf("%s: %q, %v", c.name, got, err)
		}
	}
}

// commentText is a line's comment, without its // and the space around
// it, and false for a line that isn't a comment.
func commentText(line string) (string, bool) {
	s, ok := strings.CutPrefix(strings.TrimSpace(line), "//")
	return strings.TrimSpace(s), ok
}

// section is a section of LICENSES.txt: a blank line, the heading
// underlined with dashes, a blank line and the text.
func section(title, text string) string {
	return "\n" + title + "\n" + strings.Repeat("-", len(title)) + "\n\n" + text
}

// fileHeadings are the headings of a file's sections, in order: each line
// underlined with as many dashes.
func fileHeadings(file string) []string {
	var headings []string
	lines := strings.Split(file, "\n")
	for i := 1; i < len(lines); i++ {
		if lines[i] != "" && strings.Trim(lines[i], "-") == "" && len(lines[i]) == len(lines[i-1]) {
			headings = append(headings, lines[i-1])
		}
	}
	return headings
}

// goCommand runs the go command in dir as the stand-in is built, in a home
// of its own, with env added, and returns what it printed.
func goCommand(t *testing.T, dir string, env []string, args ...string) string {
	t.Helper()
	base, err := standInEnv(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), scriptHangGuard)
	defer cancel()
	cmd := exec.CommandContext(ctx, goTool, args...)
	cmd.Dir, cmd.Env = dir, append(base, env...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go %s: %v\n%s", strings.Join(args, " "), err, stderr.String())
	}
	return string(out)
}

func readText(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
