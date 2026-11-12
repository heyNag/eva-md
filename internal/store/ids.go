package store

import (
	"crypto/sha256"
	"errors"
	"strconv"
	"strings"
	"time"
)

// idAlphabet is lower-case Crockford base32, in ascending order, so ids
// sort as their numbers do.
const idAlphabet = "0123456789abcdefghjkmnpqrstvwxyz"

// passAlphabet is upper-case Crockford base32, a ULID's alphabet.
const passAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// idEpoch is the zero of an id's time part.
var idEpoch = time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

// ErrID refuses an id that cannot be minted: a letter outside a to z, a
// pass id that is no ULID, a negative index, or a pass time outside the
// 35 bits of seconds from 2025-01-01.
var ErrID = errors.New("store: an id cannot be minted from that letter, pass and index")

// NewID mints the id of the format's section 3: the letter, then 13
// characters for 65 bits, high first: 35 bits of seconds from 2025-01-01
// UTC to the pass id's time, then the first 30 bits of SHA-256 of the pass
// id, a line feed and the operation's index in decimal. A retried pass
// mints the same ids, and ids sort by the pass's time.
func NewID(letter byte, pass string, op int) (string, error) {
	if letter < 'a' || letter > 'z' || op < 0 {
		return "", ErrID
	}
	t, ok := PassTime(pass)
	if !ok {
		return "", ErrID
	}
	secs := t.Unix() - idEpoch.Unix()
	if secs < 0 || secs >= 1<<35 {
		return "", ErrID
	}
	sum := sha256.Sum256([]byte(pass + "\n" + strconv.Itoa(op)))
	hash := uint64(sum[0])<<22 | uint64(sum[1])<<14 | uint64(sum[2])<<6 | uint64(sum[3])>>2
	out := make([]byte, 14)
	out[0] = letter
	// The 65 bits are secs<<30 | hash: the low 64 in low, the top one,
	// the time's highest bit, in top until the first shift brings it in.
	top := uint64(secs) >> 34
	low := uint64(secs)<<30 | hash
	for i := 13; i >= 1; i-- {
		out[i] = idAlphabet[low&31]
		low = low>>5 | top<<59
		top = 0
	}
	return string(out), nil
}

// PassTime is the time a pass id encodes: a ULID's first 10 characters are
// 48 bits of milliseconds since the Unix epoch. It is false for a string
// that is no ULID: 26 upper-case Crockford characters, the first at most 7.
func PassTime(pass string) (time.Time, bool) {
	if len(pass) != 26 || pass[0] > '7' {
		return time.Time{}, false
	}
	var ms int64
	for i := 0; i < 26; i++ {
		v := strings.IndexByte(passAlphabet, pass[i])
		if v < 0 {
			return time.Time{}, false
		}
		if i < 10 {
			ms = ms<<5 | int64(v)
		}
	}
	return time.UnixMilli(ms).UTC(), true
}
