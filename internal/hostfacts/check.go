package hostfacts

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
				shape, times := shapeOf(s)
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
					byShape[shape] = append(byShape[shape], seen{name, i + 1, s})
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
					if unit, v, ok := epochUnit(n); ok {
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
		}
	}
	for shape, vals := range byShape {
		made := madeSet(len(vals), func(k int) string {
			_, times := shapeOf(vals[0].value)
			return maskTimes(draw(seed, shape, k, timesMask(times)))
		})
		for _, v := range vals {
			if !made[maskTimes(v.value)] {
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
				if kept(s) {
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
					whole, _, _ := strings.Cut(strings.TrimLeft(string(t), "-"), ".")
					if fb.nums[whole] {
						at(fmt.Sprintf("holds a recorded number of %d digits", len(whole)))
					}
				}
			})
		}
	}
	return problems, nil
}
