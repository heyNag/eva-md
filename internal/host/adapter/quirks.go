package adapter

import "strings"

// The functions here read a host's output by the exact form a quirk of the
// table records, beside the fixture that shows it.

// missingManifest is how Codex ends the line naming a marketplace whose
// folder has no manifest (codex.marketplace-folder-missing).
const missingManifest = ": marketplace root does not contain a supported manifest"

// MarketplaceFolderMissing says whether a host command's diagnostics say
// only that eva-md's marketplace, registered at one of folders, has no
// manifest, as Codex 0.166.0 says it for the marketplace it was given as
// eva-md, at the folder it was added from, symlinks resolved: on a line of
// its own, "- `eva-md` at <folder>: marketplace root does not contain a
// supported manifest". A line that names any other marketplace or folder,
// or no such line, is not this. The caller gives the folder both as it is
// and resolved.
func MarketplaceFolderMissing(diagnostics string, folders ...string) bool {
	want := map[string]bool{}
	for _, folder := range folders {
		want["- `eva-md` at "+folder+missingManifest] = true
	}
	found := false
	for _, line := range strings.Split(diagnostics, "\n") {
		i := strings.Index(line, "- `")
		if i < 0 {
			continue
		}
		if !want[strings.TrimSpace(line[i:])] {
			return false
		}
		found = true
	}
	return found
}

// MarketplaceRoot is the folder an entry of a host's marketplace list names,
// read from the first of root, path, installLocation,
// marketplaceSource.source, source.path and source.source that gives one,
// or "" (claude-code.marketplace-root-keys, codex.marketplace-root-keys).
func MarketplaceRoot(entry map[string]any) string {
	if root := str(entry, "root", "path", "installLocation"); root != "" {
		return root
	}
	source, _ := entry["marketplaceSource"].(map[string]any)
	if root := str(source, "source"); root != "" {
		return root
	}
	source, _ = entry["source"].(map[string]any)
	return str(source, "path", "source")
}

// HooksRows is the last column of each row of a host's features list whose
// first column is hooks and which has another, in order
// (codex.features-list-hooks-row). The middle column changes by version, so
// it is not read.
type HooksRows []string

// ReadHooksRows reads the hooks rows of what a host's features list printed.
func ReadHooksRows(out []byte) HooksRows {
	var rows HooksRows
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "hooks" {
			rows = append(rows, fields[len(fields)-1])
		}
	}
	return rows
}

// Off is install's rule: a hooks row ends in false, so eva-md's hooks would
// never run. Any other last column, or no hooks row, is not off.
func (r HooksRows) Off() bool {
	for _, state := range r {
		if state == "false" {
			return true
		}
	}
	return false
}

// On is the doctor's rule: known says there is a hooks row, and on says the
// last of them ends in true. Any other last column is not on.
func (r HooksRows) On() (known, on bool) {
	if len(r) == 0 {
		return false, false
	}
	return true, r[len(r)-1] == "true"
}
