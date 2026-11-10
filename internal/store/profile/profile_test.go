package profile

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const (
	fixture = "../testdata/fixture-store"
	specMD  = "../../../docs/spec/store-format.md"

	areaA = "projects/p_00gb3367a19r-acme-orders"
	areaB = "projects/p_6pbe1q7fxb5h-scratch-notes"
	fact  = areaA + "/items/2026/11/i01qa78gk3m9pa-orders-port.md"
	dec1  = areaA + "/items/2026/11/i01q7krma1b2c3-sign-with-own-key.md"
	dec2  = areaA + "/items/2026/11/i01qtk73d4e5f6-sign-with-machine-key.md"
	cons  = areaA + "/items/2026/11/i01qcsw8g7h8j9-no-push-without-checks.md"
	proc  = areaA + "/items/2026/11/i01qgb8ck0m1n2-run-make-check.md"
	bad   = areaA + "/items/2026/11/i01qmw90s6t7v8-broken-merge.md"
	moved = areaA + "/items/2026/10/i01p54qrz2a3b4-export-moved.md"
	hand  = areaB + "/items/2026/11/i01qqqp0w9x0y1-sandbox-account.md"
	rule  = "global/items/2026/11/i01qjck0p3q4r5-british-english.md"
	dis   = areaA + "/disagreements/2026/11/d01qwjc0c5d6e7-keep-the-lock.md"
	ses   = areaA + "/sessions/2026/11/claude-code.5f0c9a1e-2b7d-4c1a-9e3f-0a1b2c3d4e5f.w-k3f9a2bq.md"
	ws1   = areaA + "/workstreams/w4w1gxsy1dkz19-commit-signing.md"
	ws3   = areaA + "/workstreams/w01j3400b7c2d4-billing-notes.md"
	projA = areaA + "/project.md"
	sys   = "systems/orders.md"
	edits = ".eva-md/hand-edits/w-k3f9a2bq.tsv"
	cover = ".eva-md/coverage-v1/w-k3f9a2bq.tsv"
	man   = ".eva-md/store.toml"
	wrA   = ".eva-md/writers/w-k3f9a2bq.toml"
	wrB   = ".eva-md/writers/w-7m2pq4zx.toml"

	bom = "\xef\xbb\xbf"
)

func TestFixturePasses(t *testing.T) {
	rep, err := Check(fixture)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range rep.Faults {
		t.Error(f)
	}
	want := map[string]Protection{cons: {Locked: true}, proc: {Verified: true}, bad: {Unreadable: true}}
	if fmt.Sprint(rep.Protected) != fmt.Sprint(want) {
		t.Errorf("protected: got %v, want %v", rep.Protected, want)
	}
	if !slices.Equal(rep.Unacknowledged, []string{hand}) {
		t.Errorf("unacknowledged: got %v, want [%s]", rep.Unacknowledged, hand)
	}
}

// The fixture holds what the card asks for, so that passing means something.
func TestFixtureHoldsEachCase(t *testing.T) {
	var all []string
	filepath.WalkDir(fixture, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			b, _ := os.ReadFile(p)
			all = append(all, string(b))
		}
		return nil
	})
	text := strings.Join(all, "\n")
	for _, s := range []string{"type: fact", "type: decision", "type: constraint", "type: procedure", "type: rule",
		"supersedes: [", "state: active", "state: merged", "state: archived", "covers:", "type: system", "type: disagreement",
		`signing_key = ""`, `signing_key = "ssh-ed25519`} {
		if !strings.Contains(text, s) {
			t.Errorf("the fixture holds no %q", s)
		}
	}
	projects := 0
	for _, s := range all {
		if strings.Contains(s, "type: project\n") {
			projects++
		}
	}
	if projects < 2 {
		t.Errorf("the fixture holds %d projects", projects)
	}
}

// Each mutation breaks exactly one rule of the fixture, and the checker
// must name that rule and no other.
var breaks = map[string]func(t *testing.T, root string){
	"path":     func(t *testing.T, r string) { put(t, r, areaA+"/index.md", "# Index\n") },
	"symlink":  func(t *testing.T, r string) { link(t, r, fact, areaA+"/items/2026/11/link.md") },
	"name":     func(t *testing.T, r string) { put(t, r, areaA+"/notes/okf-ideas.md", "notes\n") },
	"encoding": func(t *testing.T, r string) { edit(t, r, fact, "#31\n", "#31") },
	"size": func(t *testing.T, r string) {
		edit(t, r, fact, "\n## Evidence", "\n"+strings.Repeat("x", 17000)+"\n\n## Evidence")
	},
	"unreadable":  func(t *testing.T, r string) { edit(t, r, "README.md", "---\ntype", bom+"---\ntype") },
	"frontmatter": func(t *testing.T, r string) { edit(t, r, fact, "scope: project\n", "scope: project\nextra: \"x\"\n") },
	"written-form": func(t *testing.T, r string) {
		edit(t, r, fact, `title: "The orders service listens on port 8443"`, `title: 'The orders service listens on port 8443'`)
	},
	"month":         func(t *testing.T, r string) { move(t, r, fact, strings.Replace(fact, "2026/11", "2026/12", 1)) },
	"index":         func(t *testing.T, r string) { edit(t, r, "index.md", "# Memory\n", "# Memory\nplain text\n") },
	"readme":        func(t *testing.T, r string) { remove(t, r, "README.md") },
	"gitattributes": func(t *testing.T, r string) { put(t, r, ".gitattributes", "* text=auto\n") },
	"gitignore":     func(t *testing.T, r string) { put(t, r, ".gitignore", ".tmp-*\n*.log\n") },
	"manifest": func(t *testing.T, r string) {
		put(t, r, man, "min_reader = 2\nschema = 2\nstore_id = \"s01mj780q8r2t6\"\nepoch = 1\n")
	},
	"schema-marker": func(t *testing.T, r string) { put(t, r, ".eva-md/schema.toml", "schema_version=2\n") },
	"writer-record": func(t *testing.T, r string) { move(t, r, wrB, ".eva-md/writers/linux.toml") },
	"hand-edits": func(t *testing.T, r string) {
		appendTo(t, r, edits, areaA+"/items/2026/11/gone.md\t2026-11-09T18:30:00Z\tunacknowledged\n")
	},
	"coverage-v1": func(t *testing.T, r string) {
		appendTo(t, r, cover, "codex:019a2b3c-4d5e-7f60-8a9b-0c1d2e3f4a5b\t70\n")
	},
	"project-record": func(t *testing.T, r string) {
		put(t, r, "projects/p_bad-x/project.md", "---\ntype: project\n---\n")
	},
	"workstream-file":   func(t *testing.T, r string) { move(t, r, ws3, areaA+"/workstreams/billing-notes.md") },
	"item-file":         func(t *testing.T, r string) { move(t, r, fact, areaA+"/items/2026/11/orders-port.md") },
	"disagreement-file": func(t *testing.T, r string) { move(t, r, dis, areaA+"/disagreements/2026/11/keep.md") },
	"session-file": func(t *testing.T, r string) {
		edit(t, r, ses, `session: "claude-code:5f0c9a1e`, `session: "claude-code:6f0c9a1e`)
	},
	"notes":       func(t *testing.T, r string) { put(t, r, areaB+"/notes/bad name.md", "---\ntype: note\n---\n") },
	"legacy":      func(t *testing.T, r string) { put(t, r, "legacy/v1/Bad Name.md", "x\n") },
	"system-page": func(t *testing.T, r string) { appendTo(t, r, sys, "\nPart of the [system map](map.md).\n") },
	"type":        func(t *testing.T, r string) { edit(t, r, fact, "type: fact", "type: lesson") },
	"id":          func(t *testing.T, r string) { edit(t, r, fact, "id: i01qa78gk3m9pa", "id: i01qa78gk3m9pb") },
	"title": func(t *testing.T, r string) {
		edit(t, r, fact, `title: "The orders service listens on port 8443"`, `title: ""`)
	},
	"description": func(t *testing.T, r string) {
		edit(t, r, fact, "status: stable", "description: \"two\\nlines\"\nstatus: stable")
	},
	"status": func(t *testing.T, r string) { edit(t, r, fact, "status: stable", "status: deprecated") },
	"state":  func(t *testing.T, r string) { edit(t, r, fact, "state: active", "state: paused") },
	"state_at": func(t *testing.T, r string) {
		edit(t, r, fact, `state_at: "2026-11-03T10:00:00+01:00"`, `state_at: "2026-11-03"`)
	},
	"state_reason": func(t *testing.T, r string) {
		edit(t, r, fact, "state: active\n", "state: active\nstate_reason: user\n")
	},
	"scope":   func(t *testing.T, r string) { edit(t, r, fact, "scope: project", "scope: all") },
	"project": func(t *testing.T, r string) { edit(t, r, fact, "project: p_00gb3367a19r", "project: p_6pbe1q7fxb5h") },
	"workstream": func(t *testing.T, r string) {
		edit(t, r, fact, "scope: project\n", "scope: project\nworkstream: w01j3400b7c2d5\n")
	},
	"tag": func(t *testing.T, r string) { edit(t, r, fact, "tag: observed", "tag: stated") },
	"supports": func(t *testing.T, r string) {
		edit(t, r, fact, `"pv": "tool", "supports": "observed"`, `"pv": "tool", "supports": "stated"`)
	},
	"words": func(t *testing.T, r string) {
		edit(t, r, fact, "tag: observed\n", "tag: observed\nwords: \"port 8443\"\n")
	},
	"generated": func(t *testing.T, r string) { edit(t, r, fact, `"by": "claude-code/claude-haiku-5"`, `"by": "Claude"`) },
	"learned": func(t *testing.T, r string) {
		edit(t, r, fact, `learned: "2026-11-03T09:58:12+01:00"`, `learned: "today"`)
	},
	"asserted_at": func(t *testing.T, r string) {
		edit(t, r, fact, `asserted_at: "2026-11-03T09:58:12+01:00"`, `asserted_at: "2026-11-03T09:00:00+01:00"`)
	},
	"confirmed": func(t *testing.T, r string) {
		edit(t, r, fact, `confirmed: "2026-11-06T08:00:00+01:00"`, `confirmed: "2026-11-01T08:00:00+01:00"`)
	},
	"supersedes": func(t *testing.T, r string) {
		edit(t, r, dec2, `supersedes: ["i01q7krma1b2c3"]`, `supersedes: ["i01q7krma1b2c9"]`)
	},
	"branch": func(t *testing.T, r string) {
		edit(t, r, dec2, `branch: "feature/signing"`, `branch: "feature signing"`)
	},
	"anchors": func(t *testing.T, r string) {
		edit(t, r, fact, `"github.com/acme/orders@4e2bf4c:deploy/orders.yaml"`, `"deploy/orders.yaml"`)
	},
	"cues": func(t *testing.T, r string) {
		edit(t, r, dec2, `cues: ["commit signing prompt", "machine key"]`, `cues: [""]`)
	},
	"sources": func(t *testing.T, r string) { edit(t, r, fact, `"hash": "sha256:f6b2`, `"hash": "sha1:f6b2`) },
	"migrated_from": func(t *testing.T, r string) {
		edit(t, r, moved, `migrated_from: "knowledge/k3.md"`, `migrated_from: "k3"`)
	},
	"locked": func(t *testing.T, r string) {
		appendTo(t, r, edits, ses+"\t2026-11-10T10:00:00+01:00\tunacknowledged\n")
		edit(t, r, ses, "type: session\n", "type: session\nlocked: true\n")
	},
	"verified": func(t *testing.T, r string) { edit(t, r, proc, `"by": "human:user"`, `"by": "user"`) },
	"remotes": func(t *testing.T, r string) {
		edit(t, r, projA, `"git.example.internal/acme/orders"`, `"https://git.example.internal/acme/orders"`)
	},
	"root_commits": func(t *testing.T, r string) {
		edit(t, r, projA, `"4b825dc642cb6eb9a060e54bf8d69288fbee4904"`, `"4b825dc"`)
	},
	"legacy_keys": func(t *testing.T, r string) { edit(t, r, projA, `"github.com__acme__orders"`, `"has space"`) },
	"merged_into": func(t *testing.T, r string) {
		edit(t, r, projA, "shares_with:", "merged_into: p_00gb3367a19r\nshares_with:")
	},
	"related": func(t *testing.T, r string) {
		edit(t, r, projA, `related: ["p_6pbe1q7fxb5h"]`, `related: ["p_0000000000zz"]`)
	},
	"shares_with": func(t *testing.T, r string) {
		edit(t, r, projA, `shares_with: ["p_6pbe1q7fxb5h"]`, `shares_with: ["p_6pbe1q7fxb5h", "p_0000000000zz"]`)
	},
	"session": func(t *testing.T, r string) {
		edit(t, r, dis, `session: "claude-code:5f0c9a1e-2b7d-4c1a-9e3f-0a1b2c3d4e5f"`, `session: "claude-code"`)
	},
	"writer": func(t *testing.T, r string) { edit(t, r, ses, "writer: w-k3f9a2bq", "writer: w-7m2pq4zx") },
	"started": func(t *testing.T, r string) {
		edit(t, r, ses, `started: "2026-11-09T13:40:12+01:00"`, `started: "2026-11-09 13:40"`)
	},
	"workstreams": func(t *testing.T, r string) {
		edit(t, r, ses, `workstreams: ["w4w1gxsy1dkz19"]`, `workstreams: ["w01j3400b7c2d5"]`)
	},
	"covers": func(t *testing.T, r string) { edit(t, r, ses, `{"from": 185, "to": 190`, `{"from": 100, "to": 190`) },
	"item":   func(t *testing.T, r string) { edit(t, r, dis, "item: i01qcsw8g7h8j9", "item: i01qa78gk3m9pb") },
	"op":     func(t *testing.T, r string) { edit(t, r, dis, "op: supersede", "op: edit") },
	"reason": func(t *testing.T, r string) { edit(t, r, dis, "reason: locked", "reason: whim") },
	"at":     func(t *testing.T, r string) { edit(t, r, dis, `at: "2026-11-10T09:00:00+01:00"`, `at: "yesterday"`) },
	"stale_after": func(t *testing.T, r string) {
		edit(t, r, sys, `stale_after: "2027-01-31T00:00:00Z"`, `stale_after: 2027-01-31`)
	},
	"schema":     func(t *testing.T, r string) { edit(t, r, man, "schema = 2", "schema = 3") },
	"min_reader": func(t *testing.T, r string) { edit(t, r, man, "min_reader = 2", "min_reader = 1") },
	"store_id":   func(t *testing.T, r string) { edit(t, r, man, `"s01mj780q8r2t6"`, `"s01"`) },
	"epoch":      func(t *testing.T, r string) { edit(t, r, man, "epoch = 1", "epoch = 0") },
	"schema_version": func(t *testing.T, r string) {
		put(t, r, ".eva-md/schema.toml", "schema_version = 1\n")
	},
	"label":       func(t *testing.T, r string) { edit(t, r, wrA, `label = "mac-k3f9"`, `label = "Mac Book"`) },
	"signing_key": func(t *testing.T, r string) { edit(t, r, wrB, `signing_key = ""`, `signing_key = "ssh-rsa AAAAB3"`) },
	"reads":       func(t *testing.T, r string) { edit(t, r, wrA, "reads = [2, 2]", "reads = [2, 1]") },
	"statement": func(t *testing.T, r string) {
		edit(t, r, fact, "The orders service listens on port 8443.[^e1]\n", "[^e1]\n")
	},
	"citations": func(t *testing.T, r string) {
		edit(t, r, fact, "[^e1]: claude-code:7a8b9c0d-1e2f-4a3b-8c4d-5e6f7a8b9c0d#31", "[^e1]: claude-code:x#1")
	},
	"working-items": func(t *testing.T, r string) {
		edit(t, r, ws1, "- Pass the key on each git call <!-- id:n01qtke8j1k2m3 -->", "- Pass the key on each git call <!-- id:n01qtkccf8g9h0 -->")
	},
	"decisions-in-force": func(t *testing.T, r string) {
		edit(t, r, ws1, "/items/2026/11/i01qtk73d4e5f6-sign-with-machine-key.md", "/items/2026/11/i01qa78gk3m9pa-orders-port.md")
	},
	"log": func(t *testing.T, r string) { edit(t, r, ses, "- did: chose", "- thought: chose") },
}

func TestBreakEachRule(t *testing.T) {
	for _, rule := range Rules {
		t.Run(rule, func(t *testing.T) {
			brk, ok := breaks[rule]
			if !ok {
				t.Fatalf("no case breaks rule %s", rule)
			}
			root := copyFixture(t)
			brk(t, root)
			rep, err := Check(root)
			if err != nil {
				t.Fatal(err)
			}
			if len(rep.Faults) == 0 {
				t.Fatalf("broken %s, the checker found nothing", rule)
			}
			for _, f := range rep.Faults {
				if f.Rule != rule {
					t.Errorf("broken %s, the checker names another rule: %v", rule, f)
				}
				if f.Path == "" || !strings.Contains(f.String(), "(rule "+rule+")") {
					t.Errorf("the message does not name the file and the rule: %v", f)
				}
			}
		})
	}
	for rule := range breaks {
		if !slices.Contains(Rules, rule) {
			t.Errorf("a case breaks %s, which is no rule", rule)
		}
	}
}

// specTable reads the names of the specification's last table.
func specTable(t *testing.T) (names, sources []string) {
	b, err := os.ReadFile(specMD)
	if err != nil {
		t.Fatal(err)
	}
	_, table, ok := strings.Cut(string(b), "## 10. Fields and path classes")
	if !ok {
		t.Fatal("the specification has no section 10")
	}
	for _, line := range strings.Split(table, "\n") {
		cells := strings.Split(line, "|")
		if len(cells) != 8 || !strings.HasPrefix(strings.TrimSpace(cells[1]), "`") {
			continue
		}
		names = append(names, strings.Trim(strings.TrimSpace(cells[1]), "`"))
		sources = append(sources, strings.TrimSpace(cells[6]))
	}
	return names, sources
}

func TestSpecTableMatchesChecks(t *testing.T) {
	names, _ := specTable(t)
	for _, n := range names {
		if !slices.Contains(Rules, n) {
			t.Errorf("the specification has a row %s and the checker no check for it", n)
		}
	}
	for _, r := range Rules {
		if !slices.Contains(names, r) {
			t.Errorf("the checker has a check %s and the specification no row for it", r)
		}
	}
	seen := map[string]bool{}
	for _, n := range names {
		if seen[n] {
			t.Errorf("the row %s is repeated", n)
		}
		seen[n] = true
	}
}

func TestSpecNamesASourceForEachRow(t *testing.T) {
	names, sources := specTable(t)
	if len(names) < len(Rules) {
		t.Fatalf("read %d rows", len(names))
	}
	plan, store, derive := readDoc(t, "../../../docs/v2/plan.md"), readDoc(t, "../../../docs/v2/working-papers/design/store.md"), readDoc(t, "../../../docs/v2/working-papers/design/derive.md")
	for i, src := range sources {
		for _, s := range strings.Split(src, ", ") {
			var ok bool
			switch {
			case strings.HasPrefix(s, "P8 "):
				ok = strings.Contains(plan, "| "+strings.TrimPrefix(s, "P8 ")+" |")
			case strings.HasPrefix(s, "P"):
				sec, _, _ := strings.Cut(strings.TrimPrefix(s, "P"), "d")
				sec = strings.TrimSuffix(sec, "R")
				ok = strings.Contains(plan, "\n### "+sec+" ")
			case strings.HasPrefix(s, "D"):
				ok = strings.Contains(plan, "\n| "+strings.TrimPrefix(s, "D")+" | ")
			case strings.HasPrefix(s, "S"):
				ok = strings.Contains(store, "\n### "+strings.TrimPrefix(s, "S")+" ")
			case strings.HasPrefix(s, "V"):
				ok = strings.Contains(derive, "\n### "+strings.TrimPrefix(s, "V")+" ")
			case strings.HasPrefix(s, "OKF §"):
				ok = true
			}
			if !ok {
				t.Errorf("row %s names no source the documents hold: %q", names[i], s)
			}
		}
	}
}

func readDoc(t *testing.T, p string) string {
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestSpecSize(t *testing.T) {
	b, err := os.ReadFile(specMD)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) > 24<<10 {
		t.Errorf("the specification is %d bytes, over 24 KB", len(b))
	}
	for i, line := range strings.Split(string(b), "\n") {
		if n := len([]rune(line)); n > 400 {
			t.Errorf("line %d is %d characters, over 400", i+1, n)
		}
	}
}

// Check reads and never writes: the tree is the same after it.
func TestCheckWritesNothing(t *testing.T) {
	root := copyFixture(t)
	before := snapshot(t, root)
	if _, err := Check(root); err != nil {
		t.Fatal(err)
	}
	if after := snapshot(t, root); after != before {
		t.Error("Check changed the tree")
	}
}

func TestCheckRefusesAMissingFolder(t *testing.T) {
	if _, err := Check(filepath.Join(t.TempDir(), "none")); err == nil {
		t.Error("no error for a missing folder")
	}
}

func snapshot(t *testing.T, root string) string {
	h := sha256.New()
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			t.Fatal(err)
		}
		info, _ := d.Info()
		b, _ := os.ReadFile(p)
		fmt.Fprintf(h, "%s %v %v %x\n", p, info.Mode(), info.ModTime().UnixNano(), sha256.Sum256(b))
		return nil
	})
	return fmt.Sprintf("%x", h.Sum(nil))
}

func copyFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	err := filepath.WalkDir(fixture, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(fixture, p)
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(root, rel), 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(root, rel), b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func put(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func edit(t *testing.T, root, rel, old, new string) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(b), old) != 1 {
		t.Fatalf("%s holds %q %d times", rel, old, strings.Count(string(b), old))
	}
	put(t, root, rel, strings.Replace(string(b), old, new, 1))
}

func appendTo(t *testing.T, root, rel, s string) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		t.Fatal(err)
	}
	put(t, root, rel, string(b)+s)
}

func move(t *testing.T, root, from, to string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filepath.Join(root, to)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(root, from), filepath.Join(root, to)); err != nil {
		t.Fatal(err)
	}
}

func remove(t *testing.T, root, rel string) {
	t.Helper()
	if err := os.Remove(filepath.Join(root, rel)); err != nil {
		t.Fatal(err)
	}
}

func link(t *testing.T, root, target, rel string) {
	t.Helper()
	if err := os.Symlink(filepath.Join(root, target), filepath.Join(root, rel)); err != nil {
		t.Fatal(err)
	}
}
