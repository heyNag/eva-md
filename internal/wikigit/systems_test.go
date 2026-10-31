package wikigit_test

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/wikigit"
)

// SecretLines names the lines the commit's scan would leave a page out for,
// and passes identifiers the scan reads as identifiers.
func TestSecretLines(t *testing.T) {
	secret := "ghp_" + strings.Repeat("ab12", 10)
	text := "## Interfaces\n" +
		"- Branch `local-token-refresh-worker-c9a8c7f1e2d3b4a5`, tracked in ENG-1234\n" +
		"- The CI token is " + secret + "\n" +
		"\n" +
		"api_key = " + secret
	if got := wikigit.SecretLines(text); !reflect.DeepEqual(got, []int{3, 5}) {
		t.Fatalf("lines %v", got)
	}
	if got := wikigit.SecretLines("## Summary\nPlain text.\n"); got != nil {
		t.Fatalf("clean text: %v", got)
	}
}

// The daily commit stages the system pages with the day's other pages.
func TestCommitsSystemPages(t *testing.T) {
	f := newFixture(t)
	f.write(t, "systems/orders.md", "---\ntype: system\ntitle: Orders\ndescription: Takes orders\nstatus: stable\n---\n\n## Summary\nTakes orders.\n\nPart of the [system map](map.md).\n")
	f.write(t, "systems/index.md", "# Systems\n\n* [Orders](orders.md) - Takes orders\n")
	if r := f.commit(t); r.Outcome != wikigit.Committed || len(r.Unstaged) != 0 {
		t.Fatalf("result %+v", r)
	}
	files := f.committedFiles(t)
	if !slices.Contains(files, "systems/orders.md") || !slices.Contains(files, "systems/index.md") {
		t.Fatalf("committed %v", files)
	}
}
