package profile

import (
	"crypto/sha256"
	"regexp"
	"strings"
	"time"
)

// alphabet is lower-case Crockford base32, in ascending order, so ids sort
// as their numbers do.
const alphabet = "0123456789abcdefghjkmnpqrstvwxyz"

// epoch is the zero of an id's time part.
var epoch = time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

var (
	reGrammar = regexp.MustCompile(`^[a-z][0-9a-hjkmnp-tv-z]{13}$`)
	reProject = regexp.MustCompile(`^p_[0-9a-hjkmnp-tv-z]{12}$`)
	reWriter  = regexp.MustCompile(`^w-[0-9a-hjkmnp-tv-z]{8}$`)
	rePass    = regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{26}$`)
	reSlug    = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	reSKey    = regexp.MustCompile(`^[A-Za-z0-9_-]{1,80}$`)
)

// validID reports an id of the grammar with the given letter.
func validID(id string, letter byte) bool {
	return reGrammar.MatchString(id) && id[0] == letter
}

// idTime is the time an id of the grammar encodes: its first 7 characters
// after the letter are 35 bits of seconds since the epoch.
func idTime(id string) time.Time {
	var s int64
	for _, c := range id[1:8] {
		s = s<<5 | int64(strings.IndexRune(alphabet, c))
	}
	return epoch.Add(time.Duration(s) * time.Second)
}

// bits encodes the first n*5 bits of b as n characters of the alphabet.
func bits(b []byte, n int) string {
	out := make([]byte, n)
	for i := range n {
		v := 0
		for j := range 5 {
			bit := i*5 + j
			v = v<<1 | int(b[bit/8]>>(7-bit%8)&1)
		}
		out[i] = alphabet[v]
	}
	return string(out)
}

// hashChars is the first n characters of the alphabet's encoding of
// SHA-256 of s.
func hashChars(s string, n int) string {
	sum := sha256.Sum256([]byte(s))
	return bits(sum[:], n)
}

// workstreamID is the id of a workstream bound to a branch: all 65 bits
// from SHA-256 of the project id, a line feed and the branch.
func workstreamID(pid, branch string) string {
	return "w" + hashChars(pid+"\n"+branch, 13)
}

// sessionKey is the part of a session file's name that stands for the
// session id.
func sessionKey(sid string) string {
	if reSKey.MatchString(sid) {
		return sid
	}
	return "x" + hashChars(sid, 13)
}
