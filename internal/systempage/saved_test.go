package systempage

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/coderef"
)

// ReadFront reads a saved page's frontmatter as okf writes it and as a
// person may edit it, leaving out what fails a check rather than refusing:
// quoted and plain values, flow and block lists, the keys okf saves under a
// source, URLs, a code source cited twice, a source that is no source and a
// key a system page doesn't have. It keeps at most maxAliases aliases.
func TestReadFrontIsLenient(t *testing.T) {
	front := []string{
		`type: system`,
		`title: "Orders: intake"`,
		`description: Takes orders`,
		`generated: { by: claude-code/claude-opus-5-5, at: "2026-10-28T09:00:00Z" }`,
		`status: stable`,
		`sources:`,
		`  - resource: "https://docs.example.com/eng/orders v4"`,
		`  - resource: "github.com/team/resume@4e2bf4c:internal/orders"`,
		`    id: "first"`,
		`  - resource: 'git@github.com:team/resume.git@4e2bf4c:internal/orders'`,
		`  -   resource:   "github.com/team/resume@4e2bf4c"`,
		`  - resource: "not a source"`,
		`aliases: ["OMS", 'order api', "", plain]`,
		`as_of: "2026-10-28"`,
		`locked: true`,
		`state: active`,
	}
	f := ReadFront(front)
	want := []Cited{
		{Source: "github.com/team/resume@4e2bf4c:internal/orders", Ref: coderef.Ref{Key: "github.com__team__resume", Commit: "4e2bf4c", Path: "internal/orders"}},
		{Source: "github.com/team/resume@4e2bf4c", Ref: coderef.Ref{Key: "github.com__team__resume", Commit: "4e2bf4c"}},
	}
	if f.Title != "Orders: intake" || f.Description != "Takes orders" || f.AsOf != "2026-10-28" || !slices.Equal(f.Aliases, []string{"OMS", "order api", "plain"}) || !slices.Equal(f.Code, want) {
		t.Fatalf("read %+v", f)
	}
	if f.Line["title"] != 2 || f.Line["sources"] != 6 || f.Line["as_of"] != 14 {
		t.Fatalf("lines %v", f.Line)
	}
	// Block lists read as flow lists do; a key with a block has no value of
	// its own.
	f = ReadFront([]string{"title:", "  - Orders", "aliases:", "  - OMS", "  - 'order api'"})
	if f.Title != "" || !slices.Equal(f.Aliases, []string{"OMS", "order api"}) {
		t.Fatalf("block lists: %+v", f)
	}
	var many []string
	for i := range maxAliases + 2 {
		many = append(many, fmt.Sprintf("name-%02d", i))
	}
	if f = ReadFront([]string{"aliases: [" + strings.Join(many, ", ") + "]"}); !slices.Equal(f.Aliases, many[:maxAliases]) {
		t.Fatalf("aliases past the cap: %q", f.Aliases)
	}
}
