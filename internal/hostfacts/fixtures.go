package hostfacts

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// xform walks one line of a recording and gives each key and value to the
// callback its role names: a string, a field path, or a number. Words on the
// vocabulary and the recorder's own words are passed through.
type xform struct {
	str  func(s string) string
	path func(s string) string
	num  func(n json.Number, key string) json.Number
}

func (x xform) value(v any, key, parent string) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for _, k := range slices.Sorted(maps.Keys(t)) {
			e := t[k]
			nk := k
			switch {
			case key == "payload_hashes":
				nk = x.path(k)
			case !kept(k):
				nk = x.str(k)
			}
			out[nk] = x.value(e, k, key)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = x.value(e, key, parent)
		}
		return out
	case string:
		switch {
		case key == "path" && parent == "fields":
			return x.path(t)
		case kept(t) && !hashedPath([]string{key}):
			// A model, an effort level, a plan or tier, or a version is
			// replaced even when it reads as a host word, such as "default".
			return t
		}
		return x.str(t)
	case json.Number:
		return x.num(t, key)
	}
	return v
}

// kept reports a key or value the fixtures keep as it is.
func kept(s string) bool { return Vocab().Has(s) || FormatWord(s) }

// keptNumber reports a number the fixtures keep: a count the recorder
// writes about a shape, under 1,000, or an exit code under 256.
func keptNumber(n json.Number, key string) bool {
	i, err := strconv.ParseInt(string(n), 10, 64)
	if err != nil || i < 0 {
		return false
	}
	switch key {
	case "v", "digits", "items", "index":
		return i < 1000
	}
	return Vocab().ExitField(key) && i < 256
}

// epochUnit gives the unit of a number that reads as a Unix time between
// 2015 and 2040: seconds or milliseconds.
func epochUnit(n json.Number) (time.Duration, int64, bool) {
	i, err := strconv.ParseInt(string(n), 10, 64)
	if err != nil {
		return 0, 0, false
	}
	lo, hi := time.Date(2015, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2040, 1, 1, 0, 0, 0, 0, time.UTC)
	switch {
	case len(n) == 10 && i >= lo.Unix() && i < hi.Unix():
		return time.Second, i, true
	case len(n) == 13 && i >= lo.UnixMilli() && i < hi.UnixMilli():
		return time.Millisecond, i, true
	}
	return 0, 0, false
}

// readLines decodes every line of a JSON-lines file.
func readLines(path string) ([]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []any
	s := bufio.NewScanner(bytes.NewReader(data))
	s.Buffer(make([]byte, 64<<10), 256<<20)
	n := 0
	for s.Scan() {
		n++
		if len(bytes.TrimSpace(s.Bytes())) == 0 {
			continue
		}
		v, err := Decode(s.Bytes())
		if err != nil {
			return nil, fmt.Errorf("%s:%d: %w", filepath.Base(path), n, err)
		}
		out = append(out, v)
	}
	return out, s.Err()
}

// FixtureOptions say what WriteFixtures makes.
type FixtureOptions struct {
	Rec, Out string
	// Host keeps only that host's events when it is not "".
	Host string
	// Seed is the generator's seed; a random one when nil.
	Seed []byte
}

// fixtureFiles are the files of a recording that become fixtures.
var fixtureFiles = []string{EventsFile}

// WriteFixtures turns a recording into fixtures: every field name on the
// vocabulary, the nesting, each JSON type and each array's length are kept,
// and every other key and value is replaced by a made-up one of the same
// shape, the same recorded value always by the same made-up one.
func WriteFixtures(opt FixtureOptions) (FixtureMeta, error) {
	meta := FixtureMeta{V: 1, Epoch: Epoch.Format(time.RFC3339), MonoBase: MonoBase}
	if opt.Seed == nil {
		opt.Seed = randomBytes(32)
	}
	meta.Seed = hex.EncodeToString(opt.Seed)
	lines := map[string][]any{}
	for _, name := range fixtureFiles {
		all, err := readLines(filepath.Join(opt.Rec, name))
		if err != nil {
			return meta, err
		}
		for _, l := range all {
			if opt.Host == "" || field(l, "host") == opt.Host {
				lines[name] = append(lines[name], l)
			}
		}
	}
	offset := recordedOffset(opt.Rec)
	f, err := newFaker(opt.Seed, offset, opt.Rec)
	if err != nil {
		return meta, err
	}
	for _, name := range fixtureFiles {
		for _, l := range lines[name] {
			f.scanTimes(l)
		}
	}
	if err := os.MkdirAll(opt.Out, 0o755); err != nil {
		return meta, err
	}
	x := xform{str: f.str, path: f.path, num: f.num}
	for _, name := range fixtureFiles {
		var b bytes.Buffer
		for _, l := range lines[name] {
			data, err := Marshal(x.value(l, "", ""))
			if err != nil {
				return meta, err
			}
			b.Write(data)
			b.WriteByte('\n')
		}
		if err := os.WriteFile(filepath.Join(opt.Out, name), b.Bytes(), 0o644); err != nil {
			return meta, err
		}
		meta.Files = append(meta.Files, name)
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return meta, err
	}
	return meta, os.WriteFile(filepath.Join(opt.Out, "fixture.json"), append(data, '\n'), 0o644)
}

// recordedOffset is the recording's offset from UTC, which its marks keep,
// for times written with no zone.
func recordedOffset(rec string) int {
	lines, err := readLines(filepath.Join(rec, MarksFile))
	if err != nil {
		return 0
	}
	offset := 0
	for _, l := range lines {
		if m, ok := l.(map[string]any); ok {
			if n, ok := m["utc_offset_s"].(json.Number); ok {
				if i, err := n.Int64(); err == nil {
					offset = int(i)
				}
			}
		}
	}
	return offset
}

// faker makes the made-up values: one per recorded value, drawn in turn
// for its shape, never one that holds a recorded value.
type faker struct {
	seed   []byte
	offset int
	memo   map[string]string
	next   map[string]int
	used   map[string]bool
	t0     time.Time
	mono0  int64
	monoOK bool
	forbid forbidden
}

func newFaker(seed []byte, offset int, rec string) (*faker, error) {
	fb, err := recordedValues(rec)
	if err != nil {
		return nil, err
	}
	return &faker{seed: seed, offset: offset, memo: map[string]string{}, next: map[string]int{}, used: map[string]bool{}, forbid: fb}, nil
}

// scanTimes finds the earliest time and monotonic reading of a line.
func (f *faker) scanTimes(v any) {
	see := func(t time.Time) {
		if f.t0.IsZero() || t.Before(f.t0) {
			f.t0 = t
		}
	}
	walk(v, nil, func(path []string, x any) {
		switch t := x.(type) {
		case string:
			_, times := shapeOf(t)
			for _, s := range times {
				if p, ok := parseTime(s, f.offset); ok {
					see(p)
				}
			}
		case json.Number:
			key := path[len(path)-1]
			if key == "mono_ns" {
				if i, err := t.Int64(); err == nil && (!f.monoOK || i < f.mono0) {
					f.mono0, f.monoOK = i, true
				}
			} else if unit, i, ok := epochUnit(t); ok {
				see(time.Unix(0, i*int64(unit)))
			}
		}
		if len(path) > 0 {
			key := path[len(path)-1]
			if _, times := shapeOf(key); len(times) > 0 {
				for _, s := range times {
					if p, ok := parseTime(s, f.offset); ok {
						see(p)
					}
				}
			}
		}
	})
}

func (f *faker) mapTime(t time.Time) time.Time { return Epoch.Add(t.Sub(f.t0)) }

func (f *faker) str(s string) string {
	if out, ok := f.memo[s]; ok {
		return out
	}
	shape, times := shapeOf(s)
	mapped := make([]string, len(times))
	for i, t := range times {
		p, ok := parseTime(t, f.offset)
		if !ok {
			p = f.t0
		}
		mapped[i] = formatTime(f.mapTime(p), timeForm(t))
	}
	out := s
	switch {
	case hasClass(shape):
		for try := 0; ; try++ {
			k := f.next[shape]
			f.next[shape]++
			cand := draw(f.seed, shape, k, mapped)
			if try >= 1<<16 {
				out = cand
				break
			}
			if got, _ := shapeOf(cand); got != shape || kept(cand) || f.forbid.holds(cand) {
				continue
			}
			// A shape with few values may have none left unused: then two
			// recorded values share one made-up value.
			if !f.used[cand] || try >= maxDraws {
				out = cand
				break
			}
		}
	case len(times) > 0:
		out = draw(f.seed, shape, 0, mapped)
	}
	f.memo[s] = out
	f.used[out] = true
	return out
}

func (f *faker) path(s string) string {
	segs := strings.Split(s, ".")
	for i, seg := range segs {
		switch {
		case kept(seg) || isIndex(seg) && len(seg) < 4:
		case strings.HasPrefix(seg, "#"):
			segs[i] = "#" + f.str(seg[1:])
		default:
			segs[i] = f.str(seg)
		}
	}
	return strings.Join(segs, ".")
}

func (f *faker) num(n json.Number, key string) json.Number {
	if key == "mono_ns" && f.monoOK {
		if i, err := n.Int64(); err == nil {
			return json.Number(strconv.FormatInt(MonoBase+i-f.mono0, 10))
		}
	}
	if keptNumber(n, key) {
		return n
	}
	if unit, i, ok := epochUnit(n); ok {
		t := f.mapTime(time.Unix(0, i*int64(unit)))
		return json.Number(strconv.FormatInt(t.UnixNano()/int64(unit), 10))
	}
	form, ok := numberForm(string(n))
	if !ok {
		return json.Number("0")
	}
	memoKey := "\x00n" + string(n)
	if out, ok := f.memo[memoKey]; ok {
		return json.Number(out)
	}
	var out string
	for try := 0; ; try++ {
		k := f.next["\x00n"+form]
		f.next["\x00n"+form]++
		out = drawNumber(f.seed, form, k)
		whole, _, _ := strings.Cut(strings.TrimLeft(out, "-"), ".")
		if got, _ := numberForm(out); got != form || f.forbid.nums[whole] {
			continue
		}
		if !f.used["\x00n"+out] || try >= maxDraws {
			break
		}
	}
	f.memo[memoKey] = out
	f.used["\x00n"+out] = true
	return json.Number(out)
}

// forbidden are a recording's values that no fixture may hold: its strings
// of four characters or more with a letter or digit that is not kept, and
// its numbers of four digits or more.
type forbidden struct {
	// byLen holds the strings of 4 to prefixLen bytes, by length; long
	// holds each longer one under its first prefixLen bytes.
	byLen map[int]map[string]bool
	long  map[string][]string
	nums  map[string]bool
}

// prefixLen is the length past which a recorded string is found by its
// prefix: a value that holds the string holds its prefix too.
const prefixLen = 64

// chanceLen is the longest recorded string a drawn value could hold by
// chance, which the writer looks for in each draw.
const chanceLen = 12

func recordedValues(rec string) (forbidden, error) {
	fb := forbidden{byLen: map[int]map[string]bool{}, long: map[string][]string{}, nums: map[string]bool{}}
	entries, err := os.ReadDir(rec)
	if err != nil {
		return fb, err
	}
	addStr := func(s string) {
		if len(s) < 4 || kept(s) || keptPath(s) {
			return
		}
		if shape, _ := shapeOf(s); !hasClass(shape) {
			return
		}
		if len(s) > prefixLen {
			if !slices.Contains(fb.long[s[:prefixLen]], s) {
				fb.long[s[:prefixLen]] = append(fb.long[s[:prefixLen]], s)
			}
			return
		}
		if fb.byLen[len(s)] == nil {
			fb.byLen[len(s)] = map[string]bool{}
		}
		fb.byLen[len(s)][s] = true
	}
	for _, e := range entries {
		name := e.Name()
		if !e.Type().IsRegular() || !strings.HasSuffix(name, ".jsonl") && !strings.HasSuffix(name, ".json") {
			continue
		}
		var lines []any
		if strings.HasSuffix(name, ".json") {
			data, err := os.ReadFile(filepath.Join(rec, name))
			if err != nil {
				return fb, err
			}
			v, err := Decode(data)
			if err != nil {
				continue
			}
			lines = []any{v}
		} else if lines, err = readLines(filepath.Join(rec, name)); err != nil {
			return fb, err
		}
		for _, l := range lines {
			walk(l, nil, func(path []string, x any) {
				addStr(path[len(path)-1])
				switch t := x.(type) {
				case string:
					addStr(t)
				case json.Number:
					if digits := strings.TrimLeft(string(t), "-"); len(strings.SplitN(digits, ".", 2)[0]) >= 4 {
						fb.nums[strings.SplitN(digits, ".", 2)[0]] = true
					}
				}
			})
		}
	}
	return fb, nil
}

func hasClass(shape string) bool {
	for i := 0; i+1 < len(shape); i++ {
		if shape[i] == '%' {
			switch shape[i+1] {
			case 'a', 'A', '9', 'h', 'H', 'u':
				return true
			case '%':
				i++
			}
		}
	}
	return false
}

// holds reports a drawn value that holds a short recorded string, or a run
// of digits equal to a recorded number.
func (fb forbidden) holds(s string) bool {
	for n, set := range fb.byLen {
		if n > chanceLen {
			continue
		}
		for i := 0; i+n <= len(s); i++ {
			if set[s[i:i+n]] {
				return true
			}
		}
	}
	for _, run := range digitRuns(s) {
		if fb.nums[run] {
			return true
		}
	}
	return false
}

// within gives every recorded string that s holds, whole or as a part.
func (fb forbidden) within(s string) []string {
	found := map[string]bool{}
	for n, set := range fb.byLen {
		for i := 0; i+n <= len(s); i++ {
			if set[s[i:i+n]] {
				found[s[i:i+n]] = true
			}
		}
	}
	for i := 0; i+prefixLen <= len(s); i++ {
		for _, r := range fb.long[s[i:i+prefixLen]] {
			if strings.HasPrefix(s[i:], r) {
				found[r] = true
			}
		}
	}
	return slices.Sorted(maps.Keys(found))
}

func digitRuns(s string) []string {
	var out []string
	start := -1
	for i := 0; i <= len(s); i++ {
		if i < len(s) && s[i] >= '0' && s[i] <= '9' {
			if start < 0 {
				start = i
			}
			continue
		}
		if start >= 0 && i-start >= 4 {
			out = append(out, s[start:i])
		}
		start = -1
	}
	return out
}

// InsideCheckout reports whether path is inside a git checkout: a folder of
// its own or one above holds .git.
func InsideCheckout(path string) (bool, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return false, err
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		abs = real
	}
	for dir := abs; ; dir = filepath.Dir(dir) {
		if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
			return true, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return false, err
		}
		if filepath.Dir(dir) == dir {
			return false, nil
		}
	}
}

// maxDraws bounds the draws for one value before a used one is taken.
const maxDraws = 64

// keptPath reports a field path whose every part the fixtures keep: a host
// word, the recorder's own word or a short index, as message.content.0.type.
func keptPath(s string) bool {
	if !strings.Contains(s, ".") {
		return false
	}
	for _, seg := range strings.Split(s, ".") {
		if !kept(seg) && !(isIndex(seg) && len(seg) < 4) {
			return false
		}
	}
	return true
}
