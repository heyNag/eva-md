package hostfacts

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

func randomBytes(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return b
}

// Problem is one value a check fails on, with the file and line it is in.
type Problem struct {
	File string
	Line int
	What string
}

func (p Problem) String() string { return fmt.Sprintf("%s:%d: %s", p.File, p.Line, p.What) }

// CheckFixtureDir rebuilds every value of the fixtures in dir, keys
// included, from the generator's seed and each value's shape, and gives a
// problem for each value the generator would not have made: a string that
// is not on the vocabulary and not drawn for its shape, a time that is not
// in UTC with Z from the made-up epoch on, a number not drawn for its form.
// It needs no recording.
func CheckFixtureDir(dir string) ([]Problem, error) {
	data, err := os.ReadFile(filepath.Join(dir, "fixture.json"))
	if err != nil {
		return nil, err
	}
	var meta FixtureMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, err
	}
	seed, err := hex.DecodeString(meta.Seed)
	if err != nil || len(seed) == 0 {
		return nil, fmt.Errorf("fixture.json: a seed that is not hex")
	}
	if meta.Epoch != Epoch.Format(time.RFC3339) || meta.MonoBase != MonoBase {
		return []Problem{{"fixture.json", 1, "the epoch or the monotonic base is not the generator's"}}, nil
	}
	type seen struct {
		file  string
		line  int
		value string
	}
	var problems []Problem
	byShape := map[string][]seen{}
	numbers := map[string][]seen{}
	type wrapper struct {
		file string
		line int
		t    voiceText
	}
	var wrappers []wrapper
	partLens := map[string]int{}
	for _, name := range meta.Files {
		if name != filepath.Base(name) {
			return nil, fmt.Errorf("fixture.json names %q", name)
		}
		lines, err := readLines(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		for i, l := range lines {
			at := func(what string) { problems = append(problems, Problem{name, i + 1, what}) }
			str := func(s string) string {
				// A voice hand-over is rebuilt from its skeleton, and its
				// parts are checked below. One out of form was made up by
				// HF-00's rule, as any string, and is checked as one.
				if t, ok := parseVoice(s); ok {
					wrappers = append(wrappers, wrapper{name, i + 1, t})
					return s
				}
				// The writer makes up an id inside a longer string as it
				// makes up the id alone, and the text around it piece by
				// piece: each piece is checked by itself.
				for _, piece := range splitIDs(s) {
					shape, times := shapeOf(piece)
					for _, t := range times {
						p, ok := parseTime(t, 0)
						switch {
						case !ok || !strings.HasSuffix(t, "Z"):
							at("a time is not in UTC with Z")
						case p.Before(Epoch) || !p.Before(EpochEnd):
							at("a time is not on the made-up epoch")
						}
					}
					if hasClass(shape) {
						byShape[shape] = append(byShape[shape], seen{name, i + 1, piece})
					}
				}
				return s
			}
			path := func(s string) string {
				for _, seg := range strings.Split(s, ".") {
					switch {
					case kept(seg) || isIndex(seg) && len(seg) < 4:
					case strings.HasPrefix(seg, "#"):
						str(seg[1:])
					default:
						str(seg)
					}
				}
				return s
			}
			num := func(n json.Number, key string) json.Number {
				switch {
				case key == "mono_ns":
					if v, err := n.Int64(); err != nil || v < MonoBase {
						at("a monotonic reading is below the made-up base")
					}
				case keptNumber(n, key):
				default:
					if unit, v, ok := epochRange(n); ok {
						t := time.Unix(0, v*int64(unit))
						if !t.Before(Epoch) && t.Before(EpochEnd) {
							return n
						}
					}
					form, ok := numberForm(string(n))
					if !ok {
						at("a number has an exponent")
						return n
					}
					numbers[form] = append(numbers[form], seen{name, i + 1, string(n)})
				}
				return n
			}
			xform{str: str, path: path, num: num}.value(l, "", "")
			for _, what := range checkSkeletons(l, partLens) {
				at(what)
			}
		}
	}
	// Each part of a hand-over is rebuilt from the seed: a line's rest or a
	// request, between its cut marks, or an unlisted source is a draw for
	// its byte length, a part equal to an earlier one is a repeat, a
	// transcript is its lines, and a request may be a line or a transcript.
	// A line off the two heads is all text, which no draw holds.
	parts := map[int]int{}
	for _, w := range wrappers {
		if s := w.t.source; s != nil && *s != TailFlush {
			parts[len(*s)]++
		}
		if d := w.t.delta; d != nil {
			_, rests := voiceLines(*d)
			for _, r := range rests {
				_, _, body := cutMarks(r)
				parts[len(body)]++
			}
		}
		_, _, body := cutMarks(w.t.input)
		parts[len(body)]++
	}
	for _, vals := range byShape {
		for _, v := range vals {
			bodies, _ := madeParts(v.value)
			for _, b := range bodies {
				parts[len(b)]++
			}
		}
	}
	madeVoices := map[int]map[string]bool{}
	isMade := func(s string) bool {
		if madeVoices[len(s)] == nil {
			madeVoices[len(s)] = madeSet(parts[len(s)], func(k int) string { return madeVoice(seed, len(s), k) })
		}
		return madeVoices[len(s)][s]
	}
	accepted := map[string]bool{}
	for _, w := range wrappers {
		if d := w.t.delta; d != nil {
			_, rests := voiceLines(*d)
			for _, r := range rests {
				if _, _, body := cutMarks(r); !accepted[r] && !isMade(body) {
					problems = append(problems, Problem{w.file, w.line, fmt.Sprintf("a line of a voice hand-over, of %d bytes, is not one the generator makes", len(r))})
				}
				accepted[r] = true
			}
			accepted[*d] = true
		}
	}
	for _, w := range wrappers {
		if s := w.t.source; s != nil && *s != TailFlush && !isMade(*s) {
			problems = append(problems, Problem{w.file, w.line, fmt.Sprintf("the source of a voice hand-over, of %d bytes, is not one the generator makes", len(*s))})
		}
		if in := w.t.input; !accepted[in] {
			if _, _, body := cutMarks(in); !isMade(body) {
				problems = append(problems, Problem{w.file, w.line, fmt.Sprintf("the request of a voice hand-over, of %d bytes, is not one the generator makes", len(in))})
			}
			accepted[in] = true
		}
	}
	// A string with a part's hash was given that part's text: a part of a
	// hand-over above, or of one only a row held, which reads as made
	// parts.
	madeText := func(s string) bool {
		bodies, ok := madeParts(s)
		for _, b := range bodies {
			ok = ok && isMade(b)
		}
		return ok
	}
	for shape, vals := range byShape {
		made := madeSet(len(vals), func(k int) string {
			_, times := shapeOf(vals[0].value)
			return maskTimes(draw(seed, shape, k, timesMask(times)))
		})
		for _, v := range vals {
			if !made[maskTimes(v.value)] && !accepted[v.value] && !madeText(v.value) {
				problems = append(problems, Problem{v.file, v.line, fmt.Sprintf("a string of %d bytes is not one the generator makes", len(v.value))})
			}
		}
	}
	for form, vals := range numbers {
		made := madeSet(len(vals), func(k int) string { return drawNumber(seed, form, k) })
		for _, v := range vals {
			if !made[v.value] {
				problems = append(problems, Problem{v.file, v.line, fmt.Sprintf("a number of %d characters is not one the generator makes", len(v.value))})
			}
		}
	}
	slices.SortFunc(problems, func(a, b Problem) int {
		if a.File != b.File {
			return strings.Compare(a.File, b.File)
		}
		return a.Line - b.Line
	})
	return problems, nil
}

// CheckTestdata checks every fixture under dir, at any depth: each folder
// that holds a fixture.json is checked by CheckFixtureDir, and a JSON or
// JSON-lines file that no fixture.json beside it lists is a problem of its
// own, since no check would rebuild its values.
func CheckTestdata(dir string) ([]Problem, error) {
	var problems []Problem
	listed := map[string]bool{}
	var folders []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() != "fixture.json" {
			return err
		}
		folders = append(folders, filepath.Dir(p))
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		var meta FixtureMeta
		if err := json.Unmarshal(data, &meta); err != nil {
			problems = append(problems, Problem{relTo(dir, p), 1, "fixture.json does not read"})
			return nil
		}
		for _, f := range meta.Files {
			listed[filepath.Join(filepath.Dir(p), f)] = true
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	err = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		ext := filepath.Ext(p)
		if (ext == ".json" || ext == ".jsonl") && d.Name() != "fixture.json" && !listed[p] {
			problems = append(problems, Problem{relTo(dir, p), 1, "a file no fixture.json beside it lists, whose values no check rebuilds"})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	for _, f := range folders {
		ps, err := CheckFixtureDir(f)
		if err != nil {
			problems = append(problems, Problem{relTo(dir, filepath.Join(f, "fixture.json")), 1, err.Error()})
			continue
		}
		for _, p := range ps {
			p.File = relTo(dir, filepath.Join(f, p.File))
			problems = append(problems, p)
		}
	}
	return problems, nil
}

func relTo(dir, p string) string {
	if rel, err := filepath.Rel(dir, p); err == nil {
		return filepath.ToSlash(rel)
	}
	return p
}

// checkSkeletons checks the skeletons of a line of fixtures, whose numbers
// the writer keeps as recorded: each is one a hand-over could have; each
// of the event's own is that of a hand-over its payload holds, but for its
// hashes; and one hash has one byte length across the fixture, which lens
// keeps.
func checkSkeletons(l any, lens map[string]int) []string {
	ev, ok := l.(map[string]any)
	if !ok {
		return nil
	}
	var out []string
	held := map[string]int{}
	walk(ev["payload"], nil, func(_ []string, x any) {
		if s, ok := x.(string); ok {
			if v, ok := VoiceOf(nil, s); ok {
				held[v.shape()]++
			}
		}
	})
	var skels []*Voice
	if m, ok := ev["voice"].(map[string]any); ok {
		for _, p := range slices.Sorted(maps.Keys(m)) {
			v, ok := decodeVoice(m[p])
			if !ok {
				out = append(out, "a voice skeleton does not read")
				continue
			}
			skels = append(skels, v)
			if held[v.shape()] == 0 {
				out = append(out, "a voice skeleton of the payload is not that of a hand-over the payload holds")
				continue
			}
			held[v.shape()]--
		}
	}
	walk(ev["record"], nil, func(path []string, x any) {
		if n := len(path); n >= 3 && path[n-1] == "voice" && path[n-3] == "fields" {
			v, ok := decodeVoice(x)
			if !ok {
				out = append(out, "a voice skeleton does not read")
				return
			}
			skels = append(skels, v)
		}
	})
	for _, v := range skels {
		if what := v.flaw(); what != "" {
			out = append(out, what)
		}
		for _, p := range v.parts() {
			if n, ok := lens[p.hash]; ok && n != p.len {
				out = append(out, fmt.Sprintf("a voice skeleton gives a part of one hash %d bytes, and another %d", n, p.len))
			}
			lens[p.hash] = p.len
		}
	}
	return out
}

// skeletonPath reports a path, from a line's top, inside a skeleton or an
// event's map of them.
func skeletonPath(path []string) bool {
	if len(path) > 0 && path[0] == "voice" {
		return true
	}
	return len(path) > 5 && path[0] == "record" && path[1] == "rows" && path[3] == "fields" && path[5] == "voice"
}

// voiceShaped is the shape of a hand-over part's made-up text.
var voiceShaped = regexp.MustCompile(`^[a-z]+(?: [a-z]+)*$`)

// madeParts reads s as the writer makes up a hand-over's part: after a
// leading cut mark, lines, each of an optional listed head and text of the
// made-up shape between optional cut marks. It gives each line's text, and
// false for any other string.
func madeParts(s string) ([]string, bool) {
	_, rests := voiceLines(s)
	bodies := make([]string, len(rests))
	for i, r := range rests {
		_, _, bodies[i] = cutMarks(r)
		if bodies[i] != "" && !voiceShaped.MatchString(bodies[i]) {
			return nil, false
		}
	}
	return bodies, true
}

// madeSet draws enough values of one shape to hold every one the writer
// could have taken for n recorded values: it skips a draw that is used,
// kept or holds a recorded value, which happens rarely.
func madeSet(n int, draw func(k int) string) map[string]bool {
	made := map[string]bool{}
	for k := 0; k < 2*n+maxDraws; k++ {
		made[draw(k)] = true
	}
	return made
}

// timesMask stands for a value's times while the rest of it is compared.
func timesMask(times []string) []string {
	out := make([]string, len(times))
	for i := range out {
		out[i] = "\x00"
	}
	return out
}

func maskTimes(s string) string {
	var b strings.Builder
	last := 0
	for _, m := range pieces().FindAllStringSubmatchIndex(s, -1) {
		if m[2] >= 0 {
			b.WriteString(s[last:m[2]])
			b.WriteString("\x00")
			last = m[3]
		}
	}
	b.WriteString(s[last:])
	return b.String()
}

// CheckSurvival compares fixtures with the recording they were made from,
// and gives a problem for anything of the recording, in a key or a value,
// that is not on the vocabulary and that a fixture holds: a string of four
// characters or more, compared whole and as a substring, or a number of four
// digits or more.
func CheckSurvival(rec, fixtures string) ([]Problem, error) {
	fb, err := recordedValues(rec)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(fixtures)
	if err != nil {
		return nil, err
	}
	var problems []Problem
	for _, e := range entries {
		name := e.Name()
		if !e.Type().IsRegular() || !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		lines, err := readLines(filepath.Join(fixtures, name))
		if err != nil {
			return nil, err
		}
		for i, l := range lines {
			at := func(what string) { problems = append(problems, Problem{name, i + 1, what}) }
			look := func(s string) {
				if kept(s) || keptPath(s) {
					return
				}
				// The recorded value is not shown: a check's output may be
				// pasted where the recording must not go.
				for _, r := range fb.within(s) {
					at(fmt.Sprintf("holds a recorded string of %d characters", len(r)))
				}
				for _, run := range digitRuns(s) {
					if fb.nums[run] {
						at(fmt.Sprintf("holds a recorded number of %d digits", len(run)))
					}
				}
			}
			walk(l, []string{""}, func(path []string, x any) {
				look(path[len(path)-1])
				switch t := x.(type) {
				case string:
					look(t)
				case json.Number:
					// A skeleton's byte lengths are kept as recorded, as
					// the hand-over rebuilt beside it shows them.
					whole, _, _ := strings.Cut(strings.TrimLeft(string(t), "-"), ".")
					if fb.nums[whole] && !skeletonPath(path[1:]) {
						at(fmt.Sprintf("holds a recorded number of %d digits", len(whole)))
					}
				}
			})
		}
	}
	return problems, nil
}
