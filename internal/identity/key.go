package identity

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"
)

// repoKey returns the key of a project with a remote: the normalized remote
// with each '_' written as "_u" and each '/' as "__", or repo-<hash12> when
// the current redaction rules would rewrite that (see plainKey).
func repoKey(remote string) string {
	// Escape literal underscores before joining path components. Otherwise
	// team__sub/repo and team/sub__repo would share the same inbox key.
	key := strings.ReplaceAll(strings.ReplaceAll(remote, "_", "_u"), "/", "__")
	if !plainKey(key) {
		sum := sha256.Sum256([]byte(remote))
		key = "repo-" + hex.EncodeToString(sum[:])[:12]
	}
	return key
}

// localKey returns the key and slug of a project without a remote whose
// worktree top level is root: local-<folder>-<hash6> and <folder>-<hash6>,
// or local-<hash12> as the key when the current redaction rules would
// rewrite the readable one (see plainKey).
func localKey(root string) (key, slug string) {
	sum := sha256.Sum256([]byte(root))
	slug = folderName(filepath.Base(root)) + "-" + hex.EncodeToString(sum[:])[:6]
	key = "local-" + slug
	if !plainKey(key) {
		key = "local-" + hex.EncodeToString(sum[:])[:12]
	}
	return key, slug
}

// keyCurrent reports whether p's key is the one the current redaction rules
// give its remote or folder, as Resolve would give it now. A key an older
// eva-md stored can be one the inbox now refuses, which would stop the
// project's capture, or a hash where the readable key now passes.
func keyCurrent(p Project) bool {
	switch p.Kind {
	case KindRepo:
		return p.Key == repoKey(p.Remote)
	case KindLocal:
		key, _ := localKey(p.Root)
		return p.Key == key
	}
	return p.Key == GeneralKey
}
