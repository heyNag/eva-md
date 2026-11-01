package mcp

import (
	"path/filepath"
	"strings"
)

// installedBinary is where install puts the binary, under the home folder.
const installedBinary = "/.local/share/eva-md/bin/eva-md"

// HomeFromBinary returns the home folder the installed binary's path names,
// <home>/.local/share/eva-md/bin/eva-md, for a host that starts the server
// without HOME in its environment. Any other path names none.
func HomeFromBinary(exe string) (string, bool) {
	exe = filepath.Clean(exe)
	home, ok := strings.CutSuffix(exe, installedBinary)
	if !ok || !filepath.IsAbs(exe) || home == "" || home != filepath.Clean(home) {
		return "", false
	}
	return home, true
}
