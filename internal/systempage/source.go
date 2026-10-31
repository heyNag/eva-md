package systempage

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/heyNag/eva-md/internal/identity"
	"github.com/heyNag/eva-md/internal/redact"
)

var (
	// commitRef is <repo>@<commit> or <repo>@<commit>:<path>.
	commitRef = regexp.MustCompile(`^(.+)@([0-9a-f]{7,40})(?::(.+))?$`)
	// versionAfter is a URL followed by a space and the version it cites.
	versionAfter = regexp.MustCompile(`^(\S+) v([0-9]+)$`)
	// hashKey and localKey are the project keys that come from no remote
	// (see identity.Project.Key).
	hashKey  = regexp.MustCompile(`^(?:repo|local)-[0-9a-f]{12}$`)
	localKey = regexp.MustCompile(`^local-[a-z0-9._-]{1,40}-[0-9a-f]{6}$`)
)

// source reports whether a page may cite s: code as <repo>@<commit>, with
// :<path> for a file, where the repo is a git remote or a project key and
// the commit 7 to 40 hex digits; or a document as its https:// URL, without
// a user name, a password or a query parameter that carries a credential,
// perhaps with the version it cites as a query parameter or written after it
// as a space and v<n>, so a later digest can tell what changed.
func source(s string) bool {
	if m := commitRef.FindStringSubmatch(s); m != nil {
		if _, ok := projectKey(m[1]); ok && !withPassword(m[1]) && (m[3] == "" || filePath(m[3])) {
			return true
		}
	}
	if m := versionAfter.FindStringSubmatch(s); m != nil {
		s = m[1]
	}
	u, err := url.Parse(s)
	return err == nil && u.Scheme == "https" && u.Host != "" && u.User == nil && !strings.ContainsAny(s, " \t") && !withCredential(u)
}

// credentialWords name a query parameter that carries a credential, as its
// whole name or a part of it between '-', '_' and '.': sig, access_token or
// X-Amz-Signature.
var credentialWords = []string{"sig", "signature", "token", "key", "apikey", "secret", "password", "pwd", "auth"}

// withCredential reports a URL with a query parameter named as one that
// carries a credential.
func withCredential(u *url.URL) bool {
	for name := range u.Query() {
		for _, part := range strings.FieldsFunc(strings.ToLower(name), func(r rune) bool { return r == '-' || r == '_' || r == '.' }) {
			if slices.Contains(credentialWords, part) {
				return true
			}
		}
	}
	return false
}

// withPassword reports a remote URL that holds a password, which a source
// would keep.
func withPassword(remote string) bool {
	u, err := url.Parse(remote)
	if err != nil || u.User == nil {
		return false
	}
	_, has := u.User.Password()
	return has
}

// filePath reports a path inside a repo: relative, without .. or spaces.
func filePath(s string) bool {
	if strings.HasPrefix(s, "/") || strings.ContainsAny(s, " \\") {
		return false
	}
	for _, part := range strings.Split(s, "/") {
		if part == ".." || part == "" {
			return false
		}
	}
	return true
}

// projectKey returns the key the inbox and identity use for the project a
// repo names: a git remote in any form git takes, a normalized remote such
// as github.com/acme/orders, or a project key itself, as a page read back
// shows it.
func projectKey(s string) (string, bool) {
	if remote, ok := identity.NormalizeRemote(s); ok {
		return remoteKey(remote), true
	}
	if hashKey.MatchString(s) || localKey.MatchString(s) {
		return s, true
	}
	if host, _, ok := strings.Cut(s, "/"); ok {
		if remote, ok := identity.NormalizeRemote("https://" + s); ok && strings.Contains(host, ".") {
			return remoteKey(remote), true
		}
		return "", false
	}
	remote := strings.ReplaceAll(strings.ReplaceAll(s, "__", "/"), "_u", "_")
	if r, ok := identity.NormalizeRemote("https://" + remote); ok && r == remote && remoteKey(r) == s {
		return s, true
	}
	return "", false
}

// remoteKey is the project key of a normalized remote, made as
// identity.Resolve makes it: '_' escaped as "_u" and '/' as "__", or
// repo-<hash12> when that key reads as a provider token, which the inbox
// refuses.
func remoteKey(remote string) string {
	key := strings.ReplaceAll(strings.ReplaceAll(remote, "_", "_u"), "/", "__")
	if clean, n := redact.RedactKnown(key); n != 0 || clean != key {
		sum := sha256.Sum256([]byte(remote))
		return "repo-" + hex.EncodeToString(sum[:])[:12]
	}
	return key
}
