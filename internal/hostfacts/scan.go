package hostfacts

import (
	"bufio"
	"bytes"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// The patterns scan looks for, and the repository's test runs over every
// file in testdata.
var (
	EmailPattern = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9\-]+(?:\.[A-Za-z0-9\-]+)+`)
	HomePattern  = regexp.MustCompile(`(?:/Users/|/home/)[^/\s"'\\]+`)
	TokenPattern = regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----|\b(?:sk-[A-Za-z0-9_\-]{16,}|gh[pousr]_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,}|glpat-[A-Za-z0-9_\-]{20,}|xox[abposr]-[A-Za-z0-9\-]{10,}|AKIA[0-9A-Z]{16}|eyJ[A-Za-z0-9_\-]{10,}\.[A-Za-z0-9_\-]{10,}|[A-Za-z0-9+/_\-]{32,}={0,2})`)
)

// The values a host or the recorder makes, which scan counts by kind and
// does not show.
var knownShapes = []struct {
	kind string
	re   *regexp.Regexp
}{
	{"uuid", regexp.MustCompile(`\b[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}\b`)},
	{"host id", regexp.MustCompile(`\b(?:toolu|call|msg|resp)_[A-Za-z0-9]{8,}\b`)},
	{"hash of 64 hex digits", regexp.MustCompile(`\b[0-9a-f]{64}\b`)},
	{"commit hash", regexp.MustCompile(`\b[0-9a-f]{40}\b`)},
	{"lookup token", regexp.MustCompile(`\br:[0-9a-f]{8}\b`)},
}

// madeUpEmail is an address no one has: on a domain kept for examples or
// one that cannot exist.
var madeUpEmail = regexp.MustCompile(`(?i)@(?:[a-z0-9\-]+\.)*(?:invalid|example|example\.com|example\.org|test)$`)

// Hit is one place scan shows: a file, a line, what kind of thing it is,
// and the text, for the owner to judge and scrub.
type Hit struct {
	File string
	Line int
	Kind string
	Text string
}

// ScanResult is what scan found: the known kinds counted, and the hits to
// judge.
type ScanResult struct {
	Counts map[string]int
	Hits   []Hit
}

// Known are values the recorder made itself, counted and not shown: the
// public halves and fingerprints of the keys hostrec made.
type Known struct {
	Keys []string
}

// ScanWord is a word to look for, and the kind a hit on it is shown as.
type ScanWord struct {
	Text, Kind string
}

// Scan searches every file under dir for the words, matched as the
// recording's names are, for every email address, home path and anything
// shaped like a key or a token, and counts the values of a known shape.
func Scan(dir string, words []ScanWord, known Known) (ScanResult, error) {
	res := ScanResult{Counts: map[string]int{}}
	type finder struct {
		re   *regexp.Regexp
		kind string
	}
	var finders []finder
	for _, w := range words {
		if p := namePattern(w.Text); p != "" {
			finders = append(finders, finder{regexp.MustCompile("(?i)" + p), w.Kind})
		}
	}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		s := bufio.NewScanner(bytes.NewReader(data))
		s.Buffer(make([]byte, 64<<10), 256<<20)
		line := 0
		for s.Scan() {
			line++
			text := s.Text()
			hit := func(kind, t string) { res.Hits = append(res.Hits, Hit{rel, line, kind, t}) }
			for _, f := range finders {
				for _, m := range f.re.FindAllString(text, -1) {
					hit(f.kind, m)
				}
			}
			rest := text
			for _, k := range known.Keys {
				if k != "" && strings.Contains(rest, k) {
					res.Counts["key hostrec made"] += strings.Count(rest, k)
					rest = strings.ReplaceAll(rest, k, " ")
				}
			}
			for _, ks := range knownShapes {
				if n := len(ks.re.FindAllStringIndex(rest, -1)); n > 0 {
					res.Counts[ks.kind] += n
					rest = ks.re.ReplaceAllString(rest, " ")
				}
			}
			for _, m := range EmailPattern.FindAllString(rest, -1) {
				if madeUpEmail.MatchString(m) {
					res.Counts["made-up email address"]++
				} else {
					hit("email address", m)
				}
			}
			for _, m := range HomePattern.FindAllString(rest, -1) {
				if strings.HasSuffix(m, "/Shared") {
					res.Counts["shared folder"]++
				} else {
					hit("home path", m)
				}
			}
			for _, m := range TokenPattern.FindAllString(rest, -1) {
				if tokenShaped(m) {
					hit("key or token", m)
				}
			}
		}
		return s.Err()
	})
	slices.SortStableFunc(res.Hits, func(a, b Hit) int { return strings.Compare(a.File, b.File) })
	return res, err
}

// tokenShaped reports a run shaped like a key or a token: one with a known
// prefix, or one with a part of 24 characters or more, between slashes,
// hyphens and underscores, that mixes upper- and lower-case letters and
// digits. A path or a file name, whose parts are short, is not.
func tokenShaped(s string) bool {
	for _, p := range []string{"-----BEGIN", "sk-", "ghp_", "gho_", "ghu_", "ghs_", "ghr_", "github_pat_", "glpat-", "xox", "AKIA", "eyJ"} {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	for _, part := range strings.FieldsFunc(s, func(r rune) bool { return r == '/' || r == '-' || r == '_' }) {
		var lower, upper, digit bool
		for _, r := range part {
			switch {
			case r >= 'a' && r <= 'z':
				lower = true
			case r >= 'A' && r <= 'Z':
				upper = true
			case r >= '0' && r <= '9':
				digit = true
			}
		}
		if len(part) >= 24 && lower && upper && digit {
			return true
		}
	}
	return false
}

// namePattern is the pattern a name is matched by: its words, in any case,
// with any separators between them.
func namePattern(name string) string {
	words := splitName(name)
	if len(words) == 0 {
		return ""
	}
	quoted := make([]string, len(words))
	for i, w := range words {
		quoted[i] = regexp.QuoteMeta(w)
	}
	return strings.Join(quoted, separators)
}

// Scrub replaces every occurrence of text, in any case, in every file under
// dir with one made-up value of the same shape, and gives how many it
// replaced.
func Scrub(dir, text string) (int, error) {
	p := namePattern(text)
	if p == "" {
		return 0, nil
	}
	re := regexp.MustCompile("(?i)" + p)
	shape, _ := shapeOf(text)
	made := draw(randomBytes(32), shape, 0, nil)
	if strings.EqualFold(made, text) {
		made = draw(randomBytes(32), shape, 1, nil)
	}
	total := 0
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		n := len(re.FindAllIndex(data, -1))
		if n == 0 {
			return nil
		}
		total += n
		info, err := d.Info()
		if err != nil {
			return err
		}
		return os.WriteFile(path, re.ReplaceAllLiteral(data, []byte(made)), info.Mode().Perm())
	})
	return total, err
}

// ScanTestdata runs the email, home-path and token patterns over every file
// under dir, as the repository's test does over testdata, and gives each
// hit. Made-up addresses are passed over.
func ScanTestdata(dir string) ([]Hit, error) {
	res, err := Scan(dir, nil, Known{})
	if err != nil {
		return nil, err
	}
	return res.Hits, nil
}

// KindsCounted lists the kinds of res.Counts in order, for printing.
func (r ScanResult) KindsCounted() []string { return slices.Sorted(maps.Keys(r.Counts)) }
