package shapes

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Format is the format of eva-md audit shapes. Raise it whenever a key is
// added, removed or changes its meaning.
const Format = 1

// The states a part of the report can be in.
const (
	PartCounted    = "counted"       // the part counts what was found
	PartNone       = "none_recorded" // nothing it counts was there: its figures are zeros
	PartIncomplete = "incomplete"    // time ran out, or something it counts could not be read: its figures are a floor
	PartUnreadable = "unreadable"    // its records could not be read: its figures are zeros
)

// under3 is how a count of 1 or 2 is printed.
const under3 = "under 3"

// Count is a count as the report prints it: a count of 1 or 2 as "under
// 3", so that no figure names one chat, row or file.
type Count int

// MarshalJSON prints 1 and 2 as "under 3".
func (c Count) MarshalJSON() ([]byte, error) {
	if c == 1 || c == 2 {
		return []byte(`"` + under3 + `"`), nil
	}
	return []byte(strconv.Itoa(int(c))), nil
}

// Report is eva-md audit shapes: the shapes of the hosts' records and of
// eva-md's, in counts, buckets, days, versions and fixed names only.
// Parts says, for the inbox and each host, whether it counted what it
// found. Days gives, by host, the first and last day a row read is dated.
// Hosts holds what was listed, read and left out, by host; Facts the
// figures for each fact of the host fact sheet that shapes settle, under
// its number, then by host; Settled which facts those are, in full or in
// part, and which shapes cannot settle. A figure that is 0 is left out.
type Report struct {
	Format  int                  `json:"format"`
	Version string               `json:"version"`
	Day     string               `json:"day"`
	Parts   map[string]string    `json:"parts"`
	Days    map[string][2]string `json:"days"`
	Hosts   map[string]any       `json:"hosts"`
	Facts   map[string]any       `json:"facts"`
	Settled map[string][]string  `json:"settled"`
}

// tree turns counts keyed by a path of words joined by "/" into nested
// objects whose leaves are Counts. A count of 0 is left out.
func tree(counts map[string]int) map[string]any {
	root := map[string]any{}
	for path, n := range counts {
		if n == 0 {
			continue
		}
		parts := strings.Split(path, "/")
		node := root
		for _, p := range parts[:len(parts)-1] {
			next, ok := node[p].(map[string]any)
			if !ok {
				next = map[string]any{}
				node[p] = next
			}
			node = next
		}
		node[parts[len(parts)-1]] = Count(n)
	}
	return root
}

// versionForm is a version as the report prints one: three numbers of
// ASCII digits, at most four each, joined by dots.
var versionForm = regexp.MustCompile(`^[0-9]{1,4}\.[0-9]{1,4}\.[0-9]{1,4}$`)

// dayForm is a day as the report prints one.
var dayForm = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}$`)

// errUnlisted is Write's refusal of a report that would print a string
// none of its lists hold, or a byte that is not printable ASCII.
var errUnlisted = errors.New("shapes: the report holds a string no list holds")

// Write prints the report as indented JSON. It refuses, printing nothing,
// a report that holds a byte other than printable ASCII and a line end, or
// a string other than a word of Words, a day, a version of three numbers
// or eva-md's own version.
func (r Report) Write(w io.Writer) error {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := checkPrintable(data, r.Version); err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

// checkPrintable reports errUnlisted for data that holds a byte other
// than printable ASCII and a line end, or a string, key or value, other
// than a word of Words, a day, a version of three numbers or version.
func checkPrintable(data []byte, version string) error {
	for _, b := range data {
		if b != '\n' && (b < 0x20 || b > 0x7e) {
			return errUnlisted
		}
	}
	words := Words()
	allowed := func(s string) bool {
		_, found := slices.BinarySearch(words, s)
		return found || s == version || dayForm.MatchString(s) || versionForm.MatchString(s)
	}
	d := json.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := d.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if s, ok := tok.(string); ok && !allowed(s) {
			return errUnlisted
		}
	}
}
