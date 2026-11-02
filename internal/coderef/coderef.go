// Package coderef ties system pages to the code they describe. A page cites
// code as <repo>@<commit>[:<path>]; coderef reads such a source, measures how
// far the code at its paths has moved since that commit with bounded,
// read-only git calls in a local clone, which it finds through the identity
// cache, and keeps what a pass over the pages measured in
// <state>/code-status.json, which the prompt hook and the doctor read. It
// never fetches, never writes into a clone and never changes the wiki
// (docs/design.md "System page").
package coderef

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/heyNag/eva-md/internal/identity"
	"github.com/heyNag/eva-md/internal/redact"
)

// MaxRefs is how many code sources a page may cite: past it, it should cite
// the folders it describes rather than every file.
const MaxRefs = 16

// MaxCount bounds a count of commits: past it, a count reads 1000+.
const MaxCount = 1000

var (
	// commitRef is <repo>@<commit> or <repo>@<commit>:<path>.
	commitRef = regexp.MustCompile(`^(.+)@([0-9a-f]{7,40})(?::(.+))?$`)
	// hashKey and localKey are the project keys that come from no remote
	// (see identity.Project.Key).
	hashKey  = regexp.MustCompile(`^(?:repo|local)-[0-9a-f]{12}$`)
	localKey = regexp.MustCompile(`^local-[a-z0-9._-]{1,40}-[0-9a-f]{6}$`)
)

// Ref is a page's code source: the project key of its repo, the commit it
// cites, 7 to 40 hex digits, and a path in the repo, a file or a folder, or
// "" for the whole repo.
type Ref struct {
	Key, Commit, Path string
}

// Parse reads a code source, <repo>@<commit>[:<path>]: the repo a git remote
// in any form git takes, a normalized remote such as github.com/acme/orders
// or a project key, and never with a password; the commit 7 to 40 hex
// digits; the path relative, without .., spaces, control characters or
// "](", a folder's trailing / dropped.
func Parse(source string) (Ref, bool) {
	m := commitRef.FindStringSubmatch(source)
	if m == nil || withPassword(m[1]) {
		return Ref{}, false
	}
	key, ok := ProjectKey(m[1])
	path := strings.TrimSuffix(m[3], "/")
	if !ok || m[3] != "" && !filePath(path) {
		return Ref{}, false
	}
	return Ref{Key: key, Commit: m[2], Path: path}, true
}

// FromMap reads a ref as the system map writes it, <key>@<commit>[:<path>],
// for the brief and the prompt hook, which read every line of the map. Code
// wrote the key, so only its form is checked: a key from no remote, or one
// of a remote, host and path joined by "__". Parse checks a source's repo in
// full, with the redaction rules that decide when a key is a hash, which
// cost microseconds a ref.
func FromMap(s string) (Ref, bool) {
	i := strings.LastIndexByte(s, '@')
	for ; i > 0; i = strings.LastIndexByte(s[:i], '@') {
		commit, path, _ := strings.Cut(s[i+1:], ":")
		if !hexCommit(commit) {
			continue
		}
		key := s[:i]
		keyed := hashKey.MatchString(key) || localKey.MatchString(key) || strings.Contains(key, "__") && !strings.ContainsFunc(key, unicode.IsSpace) && !strings.ContainsAny(key, ",/@")
		if path = strings.TrimSuffix(path, "/"); !keyed || strings.Contains(s[i+1:], ":") && !filePath(path) {
			return Ref{}, false
		}
		return Ref{Key: key, Commit: commit, Path: path}, true
	}
	return Ref{}, false
}

// hexCommit reports a commit as a source cites one: 7 to 40 lower-case hex
// digits.
func hexCommit(s string) bool {
	if len(s) < 7 || len(s) > 40 {
		return false
	}
	for _, c := range []byte(s) {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// String is the ref as the system map writes it: <key>@<commit>[:<path>].
func (r Ref) String() string {
	if r.Path == "" {
		return r.Key + "@" + r.Commit
	}
	return r.Key + "@" + r.Commit + ":" + r.Path
}

// Readable is a project key as a person reads it: the normalized remote it
// was made from, or the key itself when it comes from no remote.
func Readable(key string) string {
	if hashKey.MatchString(key) || localKey.MatchString(key) {
		return key
	}
	return strings.ReplaceAll(strings.ReplaceAll(key, "__", "/"), "_u", "_")
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

// filePath reports a path inside a repo: relative, without .. parts,
// without . parts, which git lists a path without, so it would never
// match, without spaces, control or format characters, which a printed
// line would show raw, and without "](", which would start a link on the
// system map's line.
func filePath(s string) bool {
	if strings.HasPrefix(s, "/") || strings.ContainsRune(s, '\\') || strings.Contains(s, "](") || !utf8.ValidString(s) ||
		strings.ContainsFunc(s, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) || unicode.Is(unicode.Cf, r) }) {
		return false
	}
	for _, part := range strings.Split(s, "/") {
		if part == ".." || part == "." || part == "" {
			return false
		}
	}
	return true
}

// ProjectKey returns the key the inbox and identity use for the project a
// repo names: a git remote in any form git takes, a normalized remote such
// as github.com/acme/orders, or a project key itself, as a page read back
// shows it.
func ProjectKey(s string) (string, bool) {
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

// Movement is how many commits since a ref's commit changed its paths, at
// most MaxCount, with Capped set when there were more.
type Movement struct {
	Count  int
	Capped bool
}

// Words says the movement as a pointer does without its day: "code moved 14
// commits", "code moved 1 commit", "code moved 1000+ commits" or "code
// unchanged".
func (m Movement) Words() string {
	switch {
	case m.Capped:
		return fmt.Sprintf("code moved %d+ commits", MaxCount)
	case m.Count == 0:
		return "code unchanged"
	case m.Count == 1:
		return "code moved 1 commit"
	}
	return fmt.Sprintf("code moved %d commits", m.Count)
}

// Note says the movement since a page's as_of day: "code moved 14 commits
// since 2026-10-30" or "code unchanged since 2026-10-30". It is "" when asOf
// is not a day, and a pointer then shows what it showed without a note.
func Note(asOf string, m Movement) string {
	if _, err := time.Parse(time.DateOnly, asOf); err != nil {
		return ""
	}
	return m.Words() + " since " + asOf
}

// Tally combines the movements of a page's refs into the one its pointer
// shows: the largest when any moved, unchanged when every ref was measured
// unchanged, and none when some couldn't be measured and none moved.
type Tally struct {
	most                   Movement
	counted, moved, missed bool
}

// Add counts one ref's movement, or a ref that couldn't be measured.
func (t *Tally) Add(m Movement, measured bool) {
	t.counted = true
	switch {
	case !measured:
		t.missed = true
	case m.Count > 0 || m.Capped:
		if !t.moved || m.Capped || !t.most.Capped && m.Count > t.most.Count {
			t.most = m
		}
		t.moved = true
	}
}

// Result is the movement the pointer shows, and whether it shows one.
func (t Tally) Result() (Movement, bool) {
	switch {
	case t.moved:
		return t.most, true
	case t.counted && !t.missed:
		return Movement{}, true
	}
	return Movement{}, false
}
