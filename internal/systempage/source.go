package systempage

import (
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/heyNag/eva-md/internal/coderef"
)

// versionAfter is a URL followed by a space and the version it cites.
var versionAfter = regexp.MustCompile(`^(\S+) v([0-9]+)$`)

// source reports whether a page may cite s: code as <repo>@<commit>, with
// :<path> for a file or a folder, where the repo is a git remote or a
// project key and the commit 7 to 40 hex digits (see coderef.Parse); or a
// document as its https:// URL, without a user name, a password or a query
// parameter that carries a credential, perhaps with the version it cites as
// a query parameter or written after it as a space and v<n>, so a later
// digest can tell what changed.
func source(s string) bool {
	if _, ok := coderef.Parse(s); ok {
		return true
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

// codeRefs returns the code sources among sources, each once, in their
// order.
func codeRefs(sources []string) []coderef.Ref {
	var out []coderef.Ref
	for _, s := range sources {
		if r, ok := coderef.Parse(s); ok && !slices.Contains(out, r) {
			out = append(out, r)
		}
	}
	return out
}

// projectKey returns the key the inbox and identity use for the project a
// repo names (see coderef.ProjectKey).
func projectKey(s string) (string, bool) { return coderef.ProjectKey(s) }
