package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/wikigit"
	"github.com/heyNag/eva-md/internal/wikiinit"
)

// eva-md system write of a page that lists no repo and cites no code writes
// it, exits 0 and says, after the line naming what it wrote, that no
// project's brief or eva_status points at the page.
func TestSystemWriteNotesAPageNoRepoLists(t *testing.T) {
	dirs, home := gitHomeFixture(t, "")
	remote, wiki := filepath.Join(home, "remote.git"), filepath.Join(home, "wiki")
	if _, err := wikigit.Run(context.Background(), time.Minute, home, "init", "--bare", "--quiet", remote); err != nil {
		t.Fatal(err)
	}
	if _, err := wikiinit.Run(context.Background(), wikiinit.Options{Remote: remote, Dir: wiki, Dirs: dirs, Version: "test"}); err != nil {
		t.Fatal(err)
	}
	page := strings.Replace(systemPage("Handbook"), "repos: [git@github.com:acme/fixture.git]\n", "", 1)
	var out, errOut bytes.Buffer
	code := run(context.Background(), []string{"system", "write", "handbook"}, strings.NewReader(page), &out, &errOut)
	info, err := os.Stat(filepath.Join(wiki, "systems", "handbook.md"))
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("wrote systems/handbook.md (%d bytes); the map has 1 system\n", info.Size()) +
		"no repos: no project's brief or eva_status points at this page, which lists no repo and cites no code; to have them point at it, list the repos that build or use the system under repos and write it again with --replace\n"
	if code != 0 || errOut.String() != "" || out.String() != want {
		t.Fatalf("write: %d %q %q", code, out.String(), errOut.String())
	}
	// The index the brief and eva_status point at lists the page, a line of
	// its title and description.
	out.Reset()
	errOut.Reset()
	code = run(context.Background(), []string{"show", "systems/index"}, strings.NewReader(""), &out, &errOut)
	if code != 0 || errOut.String() != "" || !strings.HasPrefix(out.String(), "# Systems\n\n* [Handbook](handbook.md) - Handbook for the fixture\n") {
		t.Fatalf("show the index: %d %q %q", code, out.String(), errOut.String())
	}
}
