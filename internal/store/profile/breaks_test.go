package profile

import (
	"slices"
	"strings"
	"testing"
)

// moreBreaks breaks the checks inside a rule that its one case in breaks
// leaves alone, each in a copy of the fixture, so that no check can be
// dropped or loosened unseen.
var moreBreaks = []struct {
	rule, name string
	brk        func(t *testing.T, root string)
}{
	{"name", "a key", func(t *testing.T, r string) {
		edit(t, r, sys, "type: system\n", "type: system\nokf_version: \"0.2\"\n")
	}},
	{"name", "a writer record", func(t *testing.T, r string) { edit(t, r, wrA, `label = "mac-k3f9"`, `label = "okf-mac"`) }},
	{"name", "the index", func(t *testing.T, r string) { appendTo(t, r, "index.md", "* [Pages](pages/) - kept as OKF pages\n") }},
	{"name", "the README", func(t *testing.T, r string) {
		edit(t, r, "README.md", `"What this folder is and who writes it"`, `"An okf store: what it is and who writes it"`)
	}},
	{"name", "a camel-case key", func(t *testing.T, r string) { edit(t, r, sys, "type: system\n", "type: system\nokfVersion: \"0.2\"\n") }},
	{"frontmatter", "a repeated key", func(t *testing.T, r string) { edit(t, r, fact, "scope: project\n", "scope: project\nscope: project\n") }},
	{"month", "a session's started", func(t *testing.T, r string) {
		edit(t, r, ses, `started: "2026-11-09T13:40:12+01:00"`, `started: "2026-10-09T13:40:12+01:00"`)
	}},
	{"month", "a disagreement", func(t *testing.T, r string) { move(t, r, dis, strings.Replace(dis, "2026/11", "2026/12", 1)) }},
	{"month", "a session's started in UTC", func(t *testing.T, r string) {
		edit(t, r, ses, `started: "2026-11-09T13:40:12+01:00"`, `started: "2026-11-01T00:30:00+01:00"`)
	}},
	{"hand-edits", "a writer with no record", func(t *testing.T, r string) { move(t, r, edits, ".eva-md/hand-edits/w-zzzzzzzz.tsv") }},
	{"hand-edits", "a state", func(t *testing.T, r string) {
		appendTo(t, r, edits, hand+"\t2026-11-09T19:00:00+01:00\tseen\n")
	}},
	{"hand-edits", "a datetime", func(t *testing.T, r string) {
		appendTo(t, r, edits, hand+"\t2026-11-09\tacknowledged\n")
	}},
	{"hand-edits", "a static file", func(t *testing.T, r string) {
		appendTo(t, r, edits, ".gitignore\t2026-11-09T19:00:00+01:00\tunacknowledged\n")
	}},
	{"project-record", "a project folder with no record", func(t *testing.T, r string) {
		remove(t, r, areaB+"/project.md")
		edit(t, r, projA, "related: [\"p_6pbe1q7fxb5h\"]\nshares_with: [\"p_6pbe1q7fxb5h\"]\n", "")
	}},
	{"project-record", "a second folder for a project", func(t *testing.T, r string) {
		other := "projects/p_00gb3367a19r-other-name/items/2026/11/i01qa78gk3m9pb-orders-port.md"
		put(t, r, other, strings.Replace(readFile(t, r, fact), "id: i01qa78gk3m9pa", "id: i01qa78gk3m9pb", 1))
	}},
	{"id", "a second folder for a project, with its record", func(t *testing.T, r string) {
		put(t, r, "projects/p_00gb3367a19r-other-name/project.md", readFile(t, r, projA))
	}},
	{"workstream", "a workstream of another project", func(t *testing.T, r string) {
		put(t, r, areaB+"/workstreams/w01j3400b7c2d5-elsewhere.md", strings.ReplaceAll(strings.Replace(readFile(t, r, ws3), "id: w01j3400b7c2d4", "id: w01j3400b7c2d5", 1), "p_00gb3367a19r", "p_6pbe1q7fxb5h"))
		edit(t, r, fact, "scope: project\n", "scope: project\nworkstream: w01j3400b7c2d5\n")
	}},
	{"writer", "a session writer with no record", func(t *testing.T, r string) {
		edit(t, r, ses, "writer: w-k3f9a2bq", "writer: w-zzzzzzzz")
		move(t, r, ses, strings.Replace(ses, "w-k3f9a2bq.md", "w-zzzzzzzz.md", 1))
	}},
	{"min_reader", "above schema", func(t *testing.T, r string) { edit(t, r, man, "min_reader = 2", "min_reader = 3") }},
	{"store_id", "a number", func(t *testing.T, r string) { edit(t, r, man, `store_id = "s01mj780q8r2t6"`, "store_id = 5") }},
	{"reads", "a generation below 1", func(t *testing.T, r string) { edit(t, r, wrA, "reads = [2, 2]", "reads = [0, 2]") }},
	{"item", "an item in another area", func(t *testing.T, r string) { edit(t, r, dis, "item: i01qcsw8g7h8j9", "item: i01qqqp0w9x0y1") }},
	{"words", "241 characters", func(t *testing.T, r string) {
		edit(t, r, rule, `words: "always answer me in British English"`, `words: "`+strings.Repeat("w", 241)+`"`)
	}},
	{"words", "on a fact", func(t *testing.T, r string) { edit(t, r, fact, "tag: observed\n", "tag: observed\nwords: \"8443\"\n") }},
	{"words", "missing on a rule", func(t *testing.T, r string) { edit(t, r, rule, "words: \"always answer me in British English\"\n", "") }},
	{"cues", "nine cues", func(t *testing.T, r string) {
		edit(t, r, dec2, `cues: ["commit signing prompt", "machine key"]`, `cues: ["a", "b", "c", "d", "e", "f", "g", "h", "i"]`)
	}},
	{"cues", "a cue of 61 characters", func(t *testing.T, r string) {
		edit(t, r, dec2, `"machine key"]`, `"`+strings.Repeat("k", 61)+`"]`)
	}},
	{"anchors", "seventeen anchors", func(t *testing.T, r string) {
		a := `"github.com/acme/orders@4e2bf4c:deploy/orders.yaml"`
		edit(t, r, fact, "anchors: ["+a+"]", "anchors: ["+strings.Repeat(a+", ", 16)+a+"]")
	}},
	{"title", "201 characters", func(t *testing.T, r string) {
		edit(t, r, fact, `title: "The orders service listens on port 8443"`, `title: "`+strings.Repeat("t", 201)+`"`)
	}},
	{"title", "missing", func(t *testing.T, r string) {
		edit(t, r, fact, "title: \"The orders service listens on port 8443\"\n", "")
	}},
	{"citations", "a source not cited", func(t *testing.T, r string) {
		edit(t, r, ses, "---\n## Log", `  - {"id": "e4", "resource": "claude-code:5f0c9a1e-2b7d-4c1a-9e3f-0a1b2c3d4e5f#185", "pv": "agent", "supports": "suggested", "hash": "sha256:`+strings.Repeat("ab", 32)+"\"}\n---\n## Log")
	}},
	{"citations", "an item's source with no definition", func(t *testing.T, r string) {
		edit(t, r, fact, "\n## Evidence\n[^e1]: claude-code:7a8b9c0d-1e2f-4a3b-8c4d-5e6f7a8b9c0d#31\n", "")
	}},
	{"citations", "a footnote naming no source", func(t *testing.T, r string) {
		edit(t, r, fact, "port 8443.[^e1]", "port 8443.[^e1][^e2]")
	}},
	{"project", "a global item's missing project", func(t *testing.T, r string) {
		edit(t, r, rule, "project: p_00gb3367a19r", "project: p_0000000000zz")
	}},
	{"sources", "an extra key", func(t *testing.T, r string) { edit(t, r, fact, `bbb767"}`, `bbb767", "x": 1}`) }},
	{"sources", "a missing key", func(t *testing.T, r string) {
		edit(t, r, fact, `, "hash": "sha256:f6b28acd05c2aa9ab36778da1c8ebe7175c52eb54b4da50d0fabcd5f11bbb767"`, "")
	}},
	{"sources", "four on an item", func(t *testing.T, r string) {
		line := `  - {"id": "e1", "resource": "claude-code:7a8b9c0d-1e2f-4a3b-8c4d-5e6f7a8b9c0d#31", "pv": "tool", "supports": "observed", "hash": "sha256:f6b28acd05c2aa9ab36778da1c8ebe7175c52eb54b4da50d0fabcd5f11bbb767"}`
		var more, defs string
		for _, n := range []string{"2", "3", "4"} {
			more += "\n" + strings.Replace(strings.Replace(line, `"e1"`, `"e`+n+`"`, 1), "#31", "#3"+n, 1)
			defs += "\n[^e" + n + "]: claude-code:7a8b9c0d-1e2f-4a3b-8c4d-5e6f7a8b9c0d#3" + n
		}
		edit(t, r, fact, line, line+more)
		edit(t, r, fact, "port 8443.[^e1]", "port 8443.[^e1][^e2][^e3][^e4]")
		appendTo(t, r, fact, strings.TrimPrefix(defs, "\n")+"\n")
	}},
	{"id", "a workstream's id not derived from its branch", func(t *testing.T, r string) {
		edit(t, r, ws1, `branch: "feature/signing"`, `branch: "feature/signing-2"`)
	}},
	{"statement", "a status line with no citation", func(t *testing.T, r string) {
		edit(t, r, ws3, "until the invoice work starts.[^e1]", "until the invoice work starts.\n\n## Open questions\n- Which invoice run comes first? [^e1] <!-- id:q01j3400b7c2d4 -->")
	}},
	{"log", "a log line with no citation", func(t *testing.T, r string) {
		edit(t, r, ses, "- did: chose a machine key for store commits [^e1]", "- did: chose a machine key for store commits\n- did: kept it [^e1]")
	}},
	{"log", "no log", func(t *testing.T, r string) { edit(t, r, ses, "## Log\n", "## Notes\n") }},
	{"state_at", "before learned", func(t *testing.T, r string) {
		edit(t, r, fact, `state_at: "2026-11-03T10:00:00+01:00"`, `state_at: "2026-11-03T09:00:00+01:00"`)
	}},
	{"covers", "touching ranges", func(t *testing.T, r string) { edit(t, r, ses, `{"from": 185, "to": 190`, `{"from": 184, "to": 190`) }},
	{"covers", "a done range with a reason", func(t *testing.T, r string) {
		edit(t, r, ses, `"outcome": "done", "contract": 2}`, `"outcome": "done", "reason": "empty", "contract": 2}`)
	}},
	{"covers", "a skip for a refusal's reason", func(t *testing.T, r string) {
		edit(t, r, ses, `"outcome": "skipped", "reason": "no_user_turn"`, `"outcome": "skipped", "reason": "timeout"`)
	}},
	{"decisions-in-force", "a superseded decision", func(t *testing.T, r string) {
		edit(t, r, ws1, "/items/2026/11/i01qtk73d4e5f6-sign-with-machine-key.md", "/items/2026/11/i01q7krma1b2c3-sign-with-own-key.md")
	}},
	{"working-items", "a question's id on a next step", func(t *testing.T, r string) {
		edit(t, r, ws1, "<!-- id:n01qtke8j1k2m3 -->", "<!-- id:q01qtke8j1k2m3 -->")
	}},
	{"state_reason", "missing when retired", func(t *testing.T, r string) { edit(t, r, moved, "state_reason: user\n", "") }},
	{"state_reason", "forgotten when retired", func(t *testing.T, r string) {
		edit(t, r, moved, "state_reason: user", "state_reason: forgotten")
	}},
	{"scope", "here or project in global", func(t *testing.T, r string) { edit(t, r, rule, "scope: all", "scope: project") }},
	{"tag", "an accepted item with one span", func(t *testing.T, r string) {
		edit(t, r, dec2, `  - {"id": "e1", "resource": "claude-code:5f0c9a1e-2b7d-4c1a-9e3f-0a1b2c3d4e5f#142", "pv": "agent", "supports": "suggested", "hash": "sha256:35b555288e03bad67cd145efc0725544c1d70562fa8822aa9b409e1a64779108"}`+"\n", "")
		edit(t, r, dec2, "[^e1][^e2]", "[^e2]")
		edit(t, r, dec2, "[^e1]: claude-code:5f0c9a1e-2b7d-4c1a-9e3f-0a1b2c3d4e5f#142\n", "")
	}},
	{"locked", "on a session, in a quoted key", func(t *testing.T, r string) {
		appendTo(t, r, edits, ses+"\t2026-11-10T10:00:00+01:00\tunacknowledged\n")
		edit(t, r, ses, "type: session\n", "type: session\n\"locked\": true\n")
	}},
	{"locked", "on a session, as an alias", func(t *testing.T, r string) {
		appendTo(t, r, edits, ses+"\t2026-11-10T10:00:00+01:00\tunacknowledged\n")
		edit(t, r, ses, "type: session\n", "type: session\n*k : true\n")
	}},
	// A mark on a line the codec reads as no key still protects, and is
	// still carried where none may be.
	{"locked", "on a session, with no space after the colon", func(t *testing.T, r string) {
		appendTo(t, r, edits, ses+"\t2026-11-10T10:00:00+01:00\tunacknowledged\n")
		edit(t, r, ses, "type: session\n", "type: session\nlocked:false\n")
	}},
	{"verified", "on a project, with a tab before the key", func(t *testing.T, r string) {
		appendTo(t, r, edits, projA+"\t2026-11-10T10:00:00+01:00\tacknowledged\n")
		edit(t, r, projA, "type: project\n", "type: project\n\tverified: []\n")
	}},
	{"verified", "on a project", func(t *testing.T, r string) {
		edit(t, r, projA, "type: project\n", "type: project\nverified: [{\"by\": \"human:user\", \"at\": \"2026-11-06T09:00:00Z\"}]\n")
	}},
}

func TestBreakEachCheck(t *testing.T) {
	for _, c := range moreBreaks {
		t.Run(c.rule+"/"+c.name, func(t *testing.T) {
			checkBreak(t, c.rule, c.brk)
		})
	}
	for _, c := range moreBreaks {
		if !slices.Contains(Rules, c.rule) {
			t.Errorf("a case breaks %s, which is no rule", c.rule)
		}
	}
}
