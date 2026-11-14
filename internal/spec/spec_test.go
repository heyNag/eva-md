package spec

import (
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// repoRoot is the repository's root folder.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// The repository's specs, records and indexes pass every check.
func TestTheSplitHolds(t *testing.T) {
	root := repoRoot(t)
	for _, p := range check(root) {
		t.Error(p)
	}
	tr, _ := load(root)
	blocks, _ := bySource(tr)
	for source := range sums {
		if len(blocks[source]) < 2 {
			t.Errorf("%s is in %d blocks", source, len(blocks[source]))
		}
	}
	if len(tr.records) < 18 || len(tr.specs) < 10 {
		t.Errorf("read %d records and %d specs", len(tr.records), len(tr.specs))
	}
	// The check that a bullet sits with its packages reads most of them.
	named := 0
	for _, b := range blocks[verificationIndex] {
		outsideFences(b.text, func(_ int, l string) {
			if packages.MatchString(l) {
				named++
			}
		})
	}
	if named < 30 {
		t.Errorf("only %d verification bullets name their packages as the check reads them", named)
	}
}

// copyFile copies a file of the repository into dst, under the same name.
func copyFile(t *testing.T, root, dst, name string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
	if err != nil {
		t.Fatal(err)
	}
	write(t, dst, name, string(data))
}

func write(t *testing.T, dst, name, text string) {
	t.Helper()
	file := filepath.Join(dst, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, dst, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dst, filepath.FromSlash(name)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// place puts what a repository path is into dst: a file's bytes, or an
// empty folder for a folder.
func place(t *testing.T, root, dst, name string) {
	t.Helper()
	info, err := os.Stat(filepath.Join(root, filepath.FromSlash(name)))
	if err != nil {
		t.Fatal(err)
	}
	if info.IsDir() {
		if err := os.MkdirAll(filepath.Join(dst, filepath.FromSlash(name)), 0o755); err != nil {
			t.Fatal(err)
		}
		return
	}
	copyFile(t, root, dst, name)
}

// copyTree copies what the checks read into a folder of the test's own:
// the specs, the records and the indexes, each file a link of theirs
// names and each path INDEX.md names, so the copy passes as the
// repository does.
func copyTree(t *testing.T) string {
	t.Helper()
	root, dst := repoRoot(t), t.TempDir()
	tr, problems := load(root)
	if len(problems) > 0 {
		t.Fatalf("reading the repository: %q", problems)
	}
	copied := map[string]bool{}
	for _, d := range tr.all() {
		write(t, dst, d.name, d.text)
		copied[d.name] = true
	}
	for _, name := range v2Specs {
		if !copied[name] {
			copyFile(t, root, dst, name)
			copied[name] = true
		}
	}
	for _, d := range tr.all() {
		for _, r := range refs(d) {
			if !copied[r.name] && !r.leavesTheRepo {
				place(t, root, dst, r.name)
				copied[r.name] = true
			}
		}
	}
	index, _ := tr.find(specIndex)
	for _, l := range strings.Split(index.text, "\n") {
		if c := cells(l); strings.HasPrefix(l, "| [") && len(c) == 4 {
			for _, m := range codePath.FindAllStringSubmatch(c[2], -1) {
				if !copied[m[1]] {
					place(t, root, dst, m[1])
				}
			}
		}
	}
	if problems := check(dst); len(problems) > 0 {
		t.Fatalf("the copy fails before any fault is seeded: %q", problems)
	}
	return dst
}

// edit changes the first old in a file of dir to new, failing the test
// when the file lacks old.
func edit(t *testing.T, dir, name, old, new string) {
	t.Helper()
	text := read(t, dir, name)
	if !strings.Contains(text, old) {
		t.Fatalf("%s doesn't hold %q", name, old)
	}
	write(t, dir, name, strings.Replace(text, old, new, 1))
}

// blockOf returns the first block of source in a file of dir, for a seed
// that changes it.
func blockOf(t *testing.T, dir, name, source string) block {
	t.Helper()
	d, _ := parse(name, read(t, dir, name))
	for _, b := range d.blocks {
		if b.source == source {
			return b
		}
	}
	t.Fatalf("%s holds no block of %s", name, source)
	return block{}
}

// editBlock applies f to the first block of source in a file of dir.
func editBlock(t *testing.T, dir, name, source string, f func(string) string) {
	t.Helper()
	b := blockOf(t, dir, name, source)
	text := read(t, dir, name)
	changed := f(b.text)
	if changed == b.text {
		t.Fatalf("the seed left %s's block as it was", name)
	}
	write(t, dir, name, text[:b.start]+changed+text[b.end:])
}

// A seeded fault in a copy fails the check that guards against it, with a
// problem that names it, and nothing else.
func TestEachCheckCatchesAFault(t *testing.T) {
	const overview, redaction = "docs/spec/overview.md", "docs/spec/redaction.md"
	record := "docs/decisions/2026-10-30-goal.md"
	cases := []struct {
		name string
		seed func(t *testing.T, dir string)
		want string
	}{
		{"a word changed", func(t *testing.T, dir string) {
			editBlock(t, dir, overview, designIndex, func(s string) string { return strings.Replace(s, "Capture", "Capturing", 1) })
		}, "docs/design.md: its 39 blocks' words hash to"},
		{"a word dropped", func(t *testing.T, dir string) {
			editBlock(t, dir, redaction, verificationIndex, func(s string) string { return strings.Replace(s, " the ", " ", 1) })
		}, `^docs/verification.md: its [0-9]+ blocks. words hash to [0-9a-f]{64}; at the split.s base, 3319eef, they hash to ad1d3ca7`},
		{"a word added", func(t *testing.T, dir string) {
			editBlock(t, dir, record, decisionsIndex, func(s string) string { return strings.Replace(s, "Memory you", "Memory that you", 1) })
		}, `^docs/decisions.md: its 22 blocks. words hash to`},
		{"a link's text changed", func(t *testing.T, dir string) {
			editBlock(t, dir, overview, designIndex, func(s string) string { return strings.Replace(s, "[verification.md]", "[the checks]", 1) })
		}, `^docs/design.md: its 39 blocks. words hash to`},
		{"two words swapped", func(t *testing.T, dir string) {
			editBlock(t, dir, record, decisionsIndex, func(s string) string { return strings.Replace(s, "Memory you can", "Memory can you", 1) })
		}, `^docs/decisions.md: its 22 blocks. words hash to`},
		{"a block dropped", func(t *testing.T, dir string) {
			b := blockOf(t, dir, record, decisionsIndex)
			edit(t, dir, record, "<!-- v1 docs/decisions.md 001 -->"+b.text, "")
		}, "block 002 of docs/decisions.md is out of the order 001 to 021"},
		{"a block numbered twice", func(t *testing.T, dir string) {
			edit(t, dir, redaction, "<!-- v1 docs/verification.md 002 -->", "<!-- v1 docs/verification.md 003 -->")
		}, "block 003 of docs/verification.md is out of the order"},
		{"a block never closed", func(t *testing.T, dir string) {
			edit(t, dir, redaction, "<!-- /v1 -->", "")
		}, "docs/spec/redaction.md: block 002 of docs/verification.md is never closed"},
		{"a block of no source", func(t *testing.T, dir string) {
			edit(t, dir, redaction, "<!-- v1 docs/verification.md 002 -->", "<!-- v1 docs/known-limitations.md 002 -->")
		}, "a block of docs/known-limitations.md numbered 002"},
		{"a block in an index", func(t *testing.T, dir string) {
			text := read(t, dir, designIndex)
			write(t, dir, designIndex, text+"\n<!-- v1 docs/design.md 040 -->\nmore\n<!-- /v1 -->\n")
		}, "docs/design.md: an index holds block 040"},
		{"a file over the size limit", func(t *testing.T, dir string) {
			text := read(t, dir, redaction)
			write(t, dir, redaction, text+strings.Repeat("Words that make the spec too long.\n", 700))
		}, `^docs/spec/redaction.md: [0-9]+ bytes, over the limit of 24576$`},
		{"a record over the size limit", func(t *testing.T, dir string) {
			text := read(t, dir, record)
			write(t, dir, record, text+strings.Repeat("Words that make the record too long.\n", 700))
		}, `^docs/decisions/2026-10-30-goal.md: [0-9]+ bytes, over the limit of 24576$`},
		{"an index over the size limit", func(t *testing.T, dir string) {
			text := read(t, dir, verificationIndex)
			write(t, dir, verificationIndex, text+strings.Repeat("Words that make the index too long.\n", 700))
		}, `^docs/verification.md: [0-9]+ bytes, over the limit of 24576$`},
		{"a line over 400 characters", func(t *testing.T, dir string) {
			text := read(t, dir, redaction)
			write(t, dir, redaction, text+strings.Repeat("word ", 81)+"\n")
		}, `^docs/spec/redaction.md:[0-9]+: a line of 405 characters, over the limit of 400 for a line that is not a table row$`},
		{"a table row over 2,000 characters", func(t *testing.T, dir string) {
			text := read(t, dir, redaction)
			write(t, dir, redaction, text+"| "+strings.Repeat("cell ", 400)+"|\n")
		}, "a line of 2003 characters, over the limit of 2000"},
		{"a heading missing from the design's index", func(t *testing.T, dir string) {
			edit(t, dir, designIndex, "| Summary | [spec/overview.md](spec/overview.md) |\n", "")
		}, `docs/design.md names no file for the heading "Summary", which docs/spec/overview.md holds`},
		{"a heading against a file that lacks it", func(t *testing.T, dir string) {
			edit(t, dir, designIndex, "| Summary | [spec/overview.md](spec/overview.md) |", "| Summary | [spec/hooks.md](spec/hooks.md) |")
		}, `docs/design.md names docs/spec/hooks.md for the heading "Summary", which no block there holds`},
		{"a bullet missing from verification's index", func(t *testing.T, dir string) {
			edit(t, dir, verificationIndex, "| Redaction | [spec/redaction.md](spec/redaction.md) |\n", "")
		}, `docs/verification.md names no file for the bullet "Redaction"`},
		{"a heading missing from the decisions' index", func(t *testing.T, dir string) {
			edit(t, dir, decisionsIndex, "| Ruled out | [decisions/2026-10-30-ruled-out.md](decisions/2026-10-30-ruled-out.md) |\n", "")
		}, `docs/decisions.md names no file for the heading "Ruled out"`},
		{"a spec INDEX.md lacks", func(t *testing.T, dir string) {
			write(t, dir, "docs/spec/extra.md", "# Extra\n")
		}, "docs/spec/INDEX.md doesn't list docs/spec/extra.md"},
		{"a spec INDEX.md lists that is gone", func(t *testing.T, dir string) {
			text := read(t, dir, specIndex)
			write(t, dir, specIndex, text+"| [gone.md](gone.md) | Gone | `internal` | v1 as built, frozen on 2026-11-14 |\n")
		}, "docs/spec/INDEX.md lists docs/spec/gone.md, which is not a spec there"},
		{"INDEX.md and a header disagree", func(t *testing.T, dir string) {
			edit(t, dir, specIndex, "| Redactor |", "| The redactor |")
		}, "docs/spec/INDEX.md gives docs/spec/redaction.md as"},
		{"a path that does not exist", func(t *testing.T, dir string) {
			edit(t, dir, specIndex, "| `internal/redact` |", "| `internal/redaction` |")
			edit(t, dir, redaction, "- **Paths:** `internal/redact`", "- **Paths:** `internal/redaction`")
		}, "names internal/redaction, which does not exist"},
		{"a v2 spec INDEX.md lacks", func(t *testing.T, dir string) {
			text := read(t, dir, specIndex)
			var kept []string
			for _, l := range strings.Split(text, "\n") {
				if !strings.Contains(l, "internal/ledger/SPEC.md") {
					kept = append(kept, l)
				}
			}
			write(t, dir, specIndex, strings.Join(kept, "\n"))
		}, "docs/spec/INDEX.md doesn't list internal/ledger/SPEC.md"},
		{"a spec without its last section", func(t *testing.T, dir string) {
			edit(t, dir, redaction, "## Since the split", "## Later")
		}, "docs/spec/redaction.md: it doesn't end with ## Since the split"},
		{"a record named off its date", func(t *testing.T, dir string) {
			edit(t, dir, record, "- **Date:** 2026-10-30", "- **Date:** 2026-10-31")
		}, "docs/decisions/2026-10-30-goal.md: its date \"2026-10-31\" is not its name's, 2026-10-30"},
		{"a record named off its title", func(t *testing.T, dir string) {
			edit(t, dir, record, "# Goal\n", "# The goal\n")
		}, `its title "The goal" gives the name 2026-10-30-the-goal.md`},
		{"a record's name of no form", func(t *testing.T, dir string) {
			text := read(t, dir, record)
			write(t, dir, "docs/decisions/goal.md", text)
		}, "docs/decisions/goal.md: a record is named <YYYY-MM-DD>-<slug>.md"},
		{"a record of no day", func(t *testing.T, dir string) {
			text := strings.Replace(read(t, dir, record), "2026-10-30", "2026-13-30", 1)
			write(t, dir, "docs/decisions/2026-13-30-goal.md", text)
		}, "its name's date 2026-13-30 is not a day"},
		{"a record the index lacks", func(t *testing.T, dir string) {
			edit(t, dir, decisionsIndex, "| [Goal](decisions/2026-10-30-goal.md) | 2026-10-30 | Accepted |\n", "")
		}, "docs/decisions.md doesn't list docs/decisions/2026-10-30-goal.md"},
		{"a record the index gives another status", func(t *testing.T, dir string) {
			edit(t, dir, decisionsIndex, "| [Goal](decisions/2026-10-30-goal.md) | 2026-10-30 | Accepted |", "| [Goal](decisions/2026-10-30-goal.md) | 2026-10-30 | Reopened |")
		}, "docs/decisions.md gives docs/decisions/2026-10-30-goal.md as"},
		{"a record missing a section", func(t *testing.T, dir string) {
			edit(t, dir, record, "## Consequences\n", "")
		}, "docs/decisions/2026-10-30-goal.md: its sections are"},
		{"a record with an empty section", func(t *testing.T, dir string) {
			text := read(t, dir, record)
			start := strings.Index(text, "## Reopens\n") + len("## Reopens\n")
			end := strings.Index(text, "## Reopened by")
			write(t, dir, record, text[:start]+"\n"+text[end:])
		}, "docs/decisions/2026-10-30-goal.md: its section Reopens is empty"},
		{"a record reopened but Accepted", func(t *testing.T, dir string) {
			name := "docs/decisions/2026-10-30-commits.md"
			edit(t, dir, name, "- **Status:** Reopened", "- **Status:** Accepted")
			edit(t, dir, decisionsIndex, "| [Commits](decisions/2026-10-30-commits.md) | 2026-10-30 | Reopened |", "| [Commits](decisions/2026-10-30-commits.md) | 2026-10-30 | Accepted |")
		}, "docs/decisions/2026-10-30-commits.md: its status \"Accepted\" and its Reopened by"},
		{"a record of another status", func(t *testing.T, dir string) {
			edit(t, dir, record, "- **Status:** Accepted", "- **Status:** Superseded")
		}, `its status "Superseded" is not Accepted or Reopened`},
		{"a broken link", func(t *testing.T, dir string) {
			edit(t, dir, record, "](../spec/overview.md)", "](../spec/overview-gone.md)")
		}, `^docs/decisions/2026-10-30-goal.md:[0-9]+: the link ../spec/overview-gone.md leads nowhere$`},
		{"a link to a heading that is gone", func(t *testing.T, dir string) {
			edit(t, dir, overview, "](hosts.md#open-host-questions)", "](hosts.md#open-questions)")
		}, "the link hosts.md#open-questions names no heading of docs/spec/hosts.md"},
		{"a link out of the repository", func(t *testing.T, dir string) {
			text := read(t, dir, redaction)
			write(t, dir, redaction, text+"See [the parent](../../../elsewhere.md).\n")
		}, "the link ../../../elsewhere.md leaves the repository"},
		{"a broken link in an index", func(t *testing.T, dir string) {
			edit(t, dir, designIndex, "(spec/INDEX.md)", "(spec/INDEXES.md)")
		}, `^docs/design.md:[0-9]+: the link spec/INDEXES.md leads nowhere$`},
		{"a spec without its title", func(t *testing.T, dir string) {
			edit(t, dir, redaction, "# Redaction\n", "Redaction\n")
		}, `^docs/spec/redaction.md: a spec opens with its title$`},
		{"a spec that names no component", func(t *testing.T, dir string) {
			edit(t, dir, redaction, "- **Component:** Redactor\n", "")
		}, `^docs/spec/redaction.md: its header names no component$`},
		{"a spec not frozen", func(t *testing.T, dir string) {
			edit(t, dir, redaction, "- **Status:** v1 as built, frozen on 2026-11-14", "- **Status:** v1 as built")
			edit(t, dir, specIndex, "| `internal/redact` | v1 as built, frozen on 2026-11-14 |", "| `internal/redact` | v1 as built |")
		}, `^docs/spec/redaction.md: its status "v1 as built" is not "v1 as built, frozen on <date>"$`},
		{"INDEX.md and a header disagree on the paths alone", func(t *testing.T, dir string) {
			edit(t, dir, specIndex, "| Redactor | `internal/redact` |", "| Redactor | `internal/redact/baseline` |")
		}, "^docs/spec/INDEX.md gives docs/spec/redaction.md as"},
		{"INDEX.md and a header disagree on the status alone", func(t *testing.T, dir string) {
			edit(t, dir, specIndex, "| `internal/redact` | v1 as built, frozen on 2026-11-14 |", "| `internal/redact` | v1 as built, frozen on 2026-11-13 |")
		}, "^docs/spec/INDEX.md gives docs/spec/redaction.md as"},
		{"a spec INDEX.md lists twice", func(t *testing.T, dir string) {
			text := read(t, dir, specIndex)
			r := "| [redaction.md](redaction.md) | Redactor | `internal/redact` | v1 as built, frozen on 2026-11-14 |\n"
			if !strings.Contains(text, r) {
				t.Fatalf("%s doesn't hold %q", specIndex, r)
			}
			write(t, dir, specIndex, text+r)
		}, "^docs/spec/INDEX.md lists docs/spec/redaction.md twice$"},
		{"a file in docs/spec that is not Markdown", func(t *testing.T, dir string) {
			write(t, dir, "docs/spec/notes.txt", "notes\n")
		}, "^docs/spec/notes.txt: docs/spec holds only .md files$"},
		{"a block numbered 000", func(t *testing.T, dir string) {
			edit(t, dir, redaction, "<!-- v1 docs/verification.md 002 -->", "<!-- v1 docs/verification.md 000 -->")
		}, "^docs/spec/redaction.md: a block of docs/verification.md numbered 000"},
		{"a spec's last section before its last block", func(t *testing.T, dir string) {
			edit(t, dir, redaction, "## Since the split", "## Later")
			edit(t, dir, redaction, "<!-- v1 docs/verification.md 002 -->", "## Since the split\n\n<!-- v1 docs/verification.md 002 -->")
		}, "^docs/spec/redaction.md: it doesn't end with ## Since the split, after its blocks$"},
		{"a verification bullet away from its packages", func(t *testing.T, dir string) {
			// Block 038 back in setup-2.md, where the split first put it.
			const from, to = "docs/spec/install.md", "docs/spec/setup-2.md"
			d, _ := parse(from, read(t, dir, from))
			b := d.blocks[len(d.blocks)-1]
			if b.source != verificationIndex || b.ordinal != 38 {
				t.Fatalf("%s's last block is %03d of %s, not 038 of %s", from, b.ordinal, b.source, verificationIndex)
			}
			moved := "<!-- v1 docs/verification.md 038 -->" + b.text
			edit(t, dir, from, moved, "")
			text := read(t, dir, to)
			i := strings.LastIndex(text, "<!-- /v1 -->")
			write(t, dir, to, text[:i]+moved+text[i:])
			edit(t, dir, verificationIndex, "and the wiki's mode | [spec/install.md](spec/install.md) |", "and the wiki's mode | [spec/setup-2.md](spec/setup-2.md) |")
		}, `^docs/spec/setup-2.md: the bullet "Setup's install checks, [^"]*" names internal/install, internal/update, internal/wikigit, internal/health, cmd/eva-md, and none is among the spec's paths`},
		{"a record row outside docs/decisions", func(t *testing.T, dir string) {
			text := read(t, dir, decisionsIndex)
			write(t, dir, decisionsIndex, text+"\n| [Overview](spec/overview.md) | 2026-10-30 | Accepted |\n")
		}, "^docs/decisions.md lists docs/spec/overview.md, which is not a record there$"},
		{"a record row of no file", func(t *testing.T, dir string) {
			text := read(t, dir, decisionsIndex)
			write(t, dir, decisionsIndex, text+"\n| [Gone](decisions/2026-10-30-gone.md) | 2026-10-30 | Accepted |\n")
		}, "^docs/decisions.md lists docs/decisions/2026-10-30-gone.md, which is not a record there$"},
		{"a record the index gives another date", func(t *testing.T, dir string) {
			edit(t, dir, decisionsIndex, "| [Goal](decisions/2026-10-30-goal.md) | 2026-10-30 | Accepted |", "| [Goal](decisions/2026-10-30-goal.md) | 2026-10-31 | Accepted |")
		}, "^docs/decisions.md gives docs/decisions/2026-10-30-goal.md as \"Goal\", 2026-10-31, Accepted"},
		{"a record the index gives another title", func(t *testing.T, dir string) {
			edit(t, dir, decisionsIndex, "| [Goal](decisions/2026-10-30-goal.md) | 2026-10-30 | Accepted |", "| [The goal](decisions/2026-10-30-goal.md) | 2026-10-30 | Accepted |")
		}, "^docs/decisions.md gives docs/decisions/2026-10-30-goal.md as \"The goal\""},
		{"a record's sections out of their order", func(t *testing.T, dir string) {
			text := read(t, dir, record)
			c := strings.Index(text, "## Consequences\n")
			r := strings.Index(text, "## Reopens\n")
			e := strings.Index(text, "## Reopened by")
			if c < 0 || r < c || e < r {
				t.Fatalf("%s's sections are not where the seed looks", record)
			}
			write(t, dir, record, text[:c]+text[r:e]+text[c:r]+text[e:])
		}, `^docs/decisions/2026-10-30-goal.md: its sections are \["Context" "Decision" "Reopens" "Consequences" "Reopened by"\]`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			dir := copyTree(t)
			c.seed(t, dir)
			problems := check(dir)
			found := false
			for _, p := range problems {
				if regexp.MustCompile(c.want).MatchString(p) {
					found = true
				}
			}
			if !found {
				t.Fatalf("no problem says %q: %q", c.want, problems)
			}
		})
	}
}

// What a block may change, its line breaks, its heading levels and its
// link targets, passes; so does a link in code that leads nowhere.
func TestWhatABlockMayChangePasses(t *testing.T) {
	const overview = "docs/spec/overview.md"
	dir := copyTree(t)
	editBlock(t, dir, overview, designIndex, func(s string) string {
		s = strings.Replace(s, "## Summary\n", "#### Summary\n", 1)
		s = strings.Replace(s, "](../verification.md)", "](../../docs/verification.md)", 1)
		s = strings.Replace(s, ". ", ".\n", 3)
		return strings.Replace(s, " the ", "   the\t", 2)
	})
	text := read(t, dir, overview)
	write(t, dir, overview, text+"\nIn code, `[x](nowhere.md)` and\n\n```\n[y](nowhere.md)\n```\n\n~~~\n[z](nowhere.md)\n~~~\n\n<!-- [w](nowhere.md) -->\n")
	if problems := check(dir); len(problems) > 0 {
		t.Fatalf("%q", problems)
	}
}

// The limits hold at their edges: 24,576 bytes pass and one more fails; a
// line of 400 characters passes, counted as characters, not bytes, and 401
// fail; a table row of 2,000 passes and 2,001 fail.
func TestLimitsAtTheirEdges(t *testing.T) {
	line := func(n int) string { return strings.Repeat("é", n) }
	row := func(n int) string { return "|" + strings.Repeat(" ", n-2) + "|" }
	for _, c := range []struct {
		text string
		want int
	}{
		{line(400) + "\n", 0},
		{line(401) + "\n", 1},
		{"  " + row(1998) + "\n", 0},
		{row(2000) + "\n" + row(2001) + "\n", 1},
		{strings.Repeat("a\n", maxBytes/2), 0},
		{strings.Repeat("a\n", maxBytes/2) + "b", 1},
	} {
		if got := checkLimits(doc{name: "x.md", text: c.text}); len(got) != c.want {
			t.Errorf("%d bytes: %q", len(c.text), got)
		}
	}
}

// The words compared drop comments, link targets and tokens of '#' only,
// and collapse white space; the rest is kept, a lone '#' inside a word
// included.
func TestWords(t *testing.T) {
	got := words("## Title <!-- a\nnote -->\n\nSee [the doc](a/b.md#c) and C#  ## or\t#x ##.\n")
	if want := "Title See [the doc] and C# or #x ##."; got != want {
		t.Fatalf("%q, want %q", got, want)
	}
}

// A heading's anchor is the one a host gives it, a repeat numbered.
func TestAnchors(t *testing.T) {
	got := anchors("# 7. Decisions for the owner\n## Project `now.md`\n## Gaps (watch okf)\n```\n# Not a heading\n```\n~~~\n# Not a heading\n~~~\n## Gaps (watch okf)\n")
	for _, a := range []string{"7-decisions-for-the-owner", "project-nowmd", "gaps-watch-okf", "gaps-watch-okf-1"} {
		if !got[a] {
			t.Errorf("no anchor %q in %v", a, got)
		}
	}
	if got["not-a-heading"] || len(got) != 4 {
		t.Errorf("%v", got)
	}
}

// A link inside a code span, a fence of either kind or a comment is not
// read; one beside it is.
func TestBlankCode(t *testing.T) {
	d := doc{name: path.Join(specDir, "x.md"), text: "a `[x](one.md)` b [y](two.md) ``c `[z](three.md)` d``\n```\n[w](four.md)\n```\n~~~\n[u](six.md)\n~~~\n<!-- [t](seven.md)\n[s](eight.md) -->\n[v](five.md)\n"}
	var got []string
	for _, r := range refs(d) {
		got = append(got, r.dest)
	}
	if strings.Join(got, " ") != "two.md five.md" {
		t.Fatalf("%q", got)
	}
}
