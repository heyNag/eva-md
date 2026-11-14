package adapter

import (
	"slices"
	"strconv"
	"strings"
)

// Row is what eva-md knows of one host across a range of its versions: the
// events v1 declares there and the quirks seen there. Versions are N.N.N,
// and both ends of the range are in it.
type Row struct {
	Host    ID
	Lowest  string
	Highest string
	// Events are the hook events v1's plugin declares on the host, in the
	// order v1's doctor and status list them.
	Events []string
	Quirks []Quirk
	// Source says where the repository records each value.
	Source string
}

// Quirk is a way a host behaves that eva-md matches by its exact form. The
// fixture, under testdata/quirks/, shows it, in the form v1's tests fake;
// Shows is the text in it that does.
type Quirk struct {
	ID      string
	Host    ID
	Lowest  string
	Highest string
	What    string
	Fixture string
	Shows   string
}

var (
	codexFolderMissing = Quirk{
		ID: "codex.marketplace-folder-missing", Host: Codex, Lowest: "0.166.0", Highest: "0.166.0",
		What:    "While its eva-md marketplace's folder has no manifest, Codex fails its lists with a line naming that folder, symlinks resolved (MarketplaceFolderMissing).",
		Fixture: "codex-marketplace-folder-missing.txt",
		Shows:   ": marketplace root does not contain a supported manifest",
	}
	claudeCodeMarketplaceRoot = Quirk{
		ID: "claude-code.marketplace-root-keys", Host: ClaudeCode, Lowest: "2.1.311", Highest: "2.1.316",
		What:    "A marketplace's folder is read from the first of root, path, installLocation, marketplaceSource.source, source.path and source.source its listing gives (MarketplaceRoot).",
		Fixture: "claude-code-marketplace-list.json",
		Shows:   `"installLocation"`,
	}
	codexMarketplaceRoot = Quirk{
		ID: "codex.marketplace-root-keys", Host: Codex, Lowest: "0.164.0", Highest: "0.166.0",
		What:    "A marketplace's folder is read from the first of root, path, installLocation, marketplaceSource.source, source.path and source.source its listing gives (MarketplaceRoot).",
		Fixture: "codex-marketplace-list.json",
		Shows:   `"installLocation"`,
	}
	codexHooksFeature = Quirk{
		ID: "codex.features-list-hooks-row", Host: Codex, Lowest: "0.164.0", Highest: "0.166.0",
		What:    "Whether hooks are on is the row of codex features list whose first column is hooks, read by its last column only, since its middle column changes by version: experimental here, stable from 0.167 (ReadHooksRows, and internal/health/hosts.go).",
		Fixture: "codex-features-list.txt",
		Shows:   "\nhooks experimental true\n",
	}
)

// table is every host's rows, oldest first. The newest row of a host ends at
// its tested maximum: the newest version eva-md was checked against.
var table = []Row{
	{
		Host: ClaudeCode, Lowest: "2.1.311", Highest: "2.1.316",
		Events: []string{"SessionStart", "UserPromptSubmit", "PostToolUse", "Stop", "PreCompact", "SessionEnd", "PostToolUseFailure", "StopFailure", "PostCompact", "PostModelSwitch"},
		Quirks: []Quirk{claudeCodeMarketplaceRoot},
		Source: "Lowest: the lowest version the repository records eva-md being checked on, 2.1.311, where docs/design.md records --json-schema working with --tools \"\" and strict validation requiring the marketplace description. " +
			"Highest: docs/verification.md, Live checks, Claude Code marketplace removal. " +
			"Events: render in internal/install/templates.go.",
	},
	{
		Host: Codex, Lowest: "0.164.0", Highest: "0.166.0",
		Events: []string{"SessionStart", "UserPromptSubmit", "PostToolUse", "Stop", "PreCompact", "SessionEnd", "Interrupt"},
		Quirks: []Quirk{codexMarketplaceRoot, codexFolderMissing, codexHooksFeature},
		Source: "Lowest: the lowest version the repository records eva-md being checked on, 0.164.0, where docs/design.md records hook trust over the plugin's contents and the unified shell's exit status. " +
			"Highest: docs/verification.md, Live checks, Codex tool features and Codex plugin inventory. " +
			"Events: render in internal/install/templates.go.",
	},
}

// Rows gives a host's rows, oldest first. They are copies: a change to one
// leaves the table alone.
func Rows(id ID) []Row {
	var rows []Row
	for _, r := range table {
		if r.Host == id {
			r.Events = slices.Clone(r.Events)
			r.Quirks = slices.Clone(r.Quirks)
			rows = append(rows, r)
		}
	}
	return rows
}

// Events gives the hook events eva-md's plugin declares on the host, as its
// newest row lists them, in the order v1's doctor and status list them; none
// for a host eva-md doesn't know. It is a copy: a change to it leaves the
// table alone.
func (h Host) Events() []string {
	rows := Rows(h.ID)
	if len(rows) == 0 {
		return nil
	}
	return rows[len(rows)-1].Events
}

// Quirks gives every quirk of every row.
func Quirks() []Quirk {
	var quirks []Quirk
	for _, r := range table {
		quirks = append(quirks, r.Quirks...)
	}
	return quirks
}

// match finds the row of a host for a version. known says a row's range
// holds it. untested says it is above the host's tested maximum, and the
// newest row is given. Otherwise, as for a version that can't be read, below
// the lowest row or between two rows, no row is given.
func match(id ID, version string) (row *Row, known, untested bool) {
	v, ok := parseVersion(version)
	if !ok {
		return nil, false, false
	}
	rows := Rows(id)
	for i := range rows {
		lo, _ := parseVersion(rows[i].Lowest)
		hi, _ := parseVersion(rows[i].Highest)
		if compare(v, lo) >= 0 && compare(v, hi) <= 0 {
			return &rows[i], true, false
		}
	}
	if len(rows) == 0 {
		return nil, false, false
	}
	newest := &rows[len(rows)-1]
	if hi, _ := parseVersion(newest.Highest); compare(v, hi) > 0 {
		return newest, false, true
	}
	return nil, false, false
}

// findVersion is the first whitespace-separated token of out that is a
// version, N.N.N, or "".
func findVersion(out []byte) string {
	for _, field := range strings.Fields(string(out)) {
		if _, ok := parseVersion(field); ok {
			return field
		}
	}
	return ""
}

// parseVersion reads N.N.N, three runs of digits and nothing else.
func parseVersion(s string) ([3]int, bool) {
	var v [3]int
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return v, false
	}
	for i, p := range parts {
		if p == "" || strings.Trim(p, "0123456789") != "" {
			return v, false
		}
		n, err := strconv.Atoi(p)
		if err != nil {
			return v, false
		}
		v[i] = n
	}
	return v, true
}

func compare(a, b [3]int) int {
	for i := range a {
		if a[i] != b[i] {
			if a[i] < b[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}
