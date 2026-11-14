package spec

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// The benchmark and its targets (CAP-K18, EVL-K09, EVL-K10, and the gate's
// half of EVL-K08): scripts/benchmark.py holds the hooks to the four
// targets the specs state, checks what each hook wrote as well as how long
// it took, and runs in a home of its own.

const (
	benchmarkScript = "scripts/benchmark.py"
	benchmarkCore   = "scripts/bench/core.py"
)

// hookTarget is the target the specs state for a figure of the
// benchmark's, by the hook it times: capture and a prompt under 50 ms, the
// startup brief under 300 ms, and SessionEnd under 200 ms. ok is false for
// a name that times no hook the specs give a target.
func hookTarget(name string) (ms int, ok bool) {
	switch {
	case name == "session_end":
		return 200, true
	case strings.HasPrefix(name, "startup"), name == "code_brief":
		return 300, true
	case strings.HasPrefix(name, "capture"), name == "transcript_capture":
		return 50, true
	case strings.HasPrefix(name, "prompt_"), name == "code_prompt":
		return 50, true
	}
	return 0, false
}

var (
	// gated is a figure of the gate's list: its name, its samples and its
	// target in ms.
	gated = regexp.MustCompile(`\("([a-z_]+)",\s*[A-Za-z_]+(?:\["[a-z]+"\])?,\s*([0-9]+)\)`)
	// reported is a figure the report judges against its target itself.
	reported = regexp.MustCompile(`"([a-z_]+)_target_met":\s*p95\([^()]*(?:\([^()]*\))?[^()]*\)\s*<\s*([0-9]+)`)
	// raised is a check that ends a run with an error.
	raised = regexp.MustCompile(`raise RuntimeError\("([^"%]+)`)
)

// outputChecks are checks of what hooks wrote, by the start of each
// one's message, which the benchmark keeps beside its timings: a brief
// that resumes the other host's chat, pointers and recall in a prompt's
// context, the jobs the hooks start, and Codex's Stop answer.
var outputChecks = map[string][]string{
	benchmarkScript: {
		"cross-host startup failed",
		"a prompt naming three systems did not get three pointers",
		"a prompt about three facts did not get them fenced",
		"the hooks started the stand-in",
	},
	benchmarkCore: {"invalid Stop output"},
}

// code is a Python file's text without its comments, which hold no check:
// each line cut at a # outside a string.
func code(text string) string {
	var out []string
	for _, l := range strings.Split(text, "\n") {
		out = append(out, l[:commentAt(l)])
	}
	return strings.Join(out, "\n")
}

// commentAt is where a line's comment starts, or its length for none: the
// first # outside a string, a backslash escaping the character after it.
func commentAt(l string) int {
	quote, escaped := byte(0), false
	for i := 0; i < len(l); i++ {
		switch c := l[i]; {
		case escaped:
			escaped = false
		case c == '\\':
			escaped = true
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '#':
			return i
		}
	}
	return len(l)
}

// checkBenchmark holds the benchmark's script and its core to the specs:
// every hook figure it gates or reports has the target the specs state,
// each of the four targets is gated, the output checks are there, and its
// home is a temporary folder of its own.
func checkBenchmark(script, core string) []string {
	var problems []string
	script, core = code(script), code(core)
	seen := map[int]bool{}
	figures := 0
	for _, re := range []*regexp.Regexp{gated, reported} {
		for _, m := range re.FindAllStringSubmatch(script, -1) {
			name, ms := m[1], m[2]
			if strings.HasPrefix(name, "mcp_") {
				continue // the MCP server's figures time no hook
			}
			want, ok := hookTarget(name)
			got, _ := strconv.Atoi(ms)
			switch {
			case !ok:
				problems = append(problems, fmt.Sprintf("%s: the figure %s has the target %d ms, but no spec gives it one", benchmarkScript, name, got))
			case got != want:
				problems = append(problems, fmt.Sprintf("%s: the figure %s has the target %d ms, not the %d ms the specs state", benchmarkScript, name, got, want))
			}
			if re == gated && ok && got == want {
				seen[want] = true
				figures++
			}
		}
	}
	for _, c := range []struct {
		name string
		ms   int
	}{{"capture", 50}, {"startup", 300}, {"session_end", 200}, {"prompt_after_brief", 50}} {
		if !regexp.MustCompile(`\("` + c.name + `",\s*[A-Za-z_]+,\s*` + strconv.Itoa(c.ms) + `\)`).MatchString(script) {
			problems = append(problems, fmt.Sprintf("%s: the gate does not hold %s to %d ms", benchmarkScript, c.name, c.ms))
		}
	}
	for file, checks := range map[string]string{benchmarkScript: script, benchmarkCore: core} {
		var have []string
		for _, m := range raised.FindAllStringSubmatch(checks, -1) {
			have = append(have, m[1])
		}
		for _, want := range outputChecks[file] {
			if !slices.ContainsFunc(have, func(h string) bool { return strings.HasPrefix(h, want) }) {
				problems = append(problems, fmt.Sprintf("%s no longer checks what the hooks wrote: it raises no %q", file, want))
			}
		}
	}
	if !regexp.MustCompile(`tempfile\.TemporaryDirectory\(`).MatchString(core) || !regexp.MustCompile(`env\.update\(HOME=str\(self\.path\)`).MatchString(core) {
		problems = append(problems, fmt.Sprintf("%s no longer runs each scenario with HOME in a temporary folder of its own", benchmarkCore))
	}
	if figures < 10 {
		problems = append(problems, fmt.Sprintf("%s: read %d gated hook figures; the check read the wrong text", benchmarkScript, figures))
	}
	slices.Sort(problems)
	return problems
}

// The benchmark holds every hook to the target its spec states, gates the
// four, checks what the hooks wrote, and runs in a home of its own.
func TestTheBenchmarkHoldsTheSpecsTargets(t *testing.T) {
	tempHome(t)
	root := repoRoot(t)
	for _, p := range checkBenchmark(read(t, root, benchmarkScript), read(t, root, benchmarkCore)) {
		t.Error(p)
	}
}

// The check fails a target changed to 60, in the gate's list or in the
// report, a hook figure no spec gives a target, a target the gate no
// longer holds, an output check removed or only left in a comment, and a
// scenario run in the real home.
func TestABenchmarkOffItsTargetsIsFound(t *testing.T) {
	tempHome(t)
	root := repoRoot(t)
	script, core := read(t, root, benchmarkScript), read(t, root, benchmarkCore)
	for _, c := range []struct {
		name     string
		file     string
		old, new string
		want     []string
	}{
		{"capture's gate at 60", benchmarkScript, `("capture",capture,50)`, `("capture",capture,60)`,
			[]string{`the figure capture has the target 60 ms, not the 50 ms the specs state$`, `the gate does not hold capture to 50 ms$`}},
		{"SessionEnd's report at 250", benchmarkScript, `"session_end_target_met":p95(ends)<200`, `"session_end_target_met":p95(ends)<250`,
			[]string{`the figure session_end has the target 250 ms, not the 200 ms`}},
		{"the brief's gate at 400", benchmarkScript, `("startup",startup,300)`, `("startup",startup,400)`,
			[]string{`the figure startup has the target 400 ms`, `the gate does not hold startup to 300 ms$`}},
		{"a hook figure with no spec", benchmarkScript, `("code_prompt",code["prompts"],50))`, `("code_prompt",code["prompts"],50),("stop_alone",stops,80))`,
			[]string{`the figure stop_alone has the target 80 ms, but no spec gives it one$`}},
		{"a prompt's gate gone", benchmarkScript, `("prompt_after_brief",prompts,50),`, ``,
			[]string{`the gate does not hold prompt_after_brief to 50 ms$`}},
		{"an output check gone", benchmarkScript, `raise RuntimeError("a prompt about three facts did not get them fenced")`, `pass`,
			[]string{`no longer checks what the hooks wrote: it raises no "a prompt about three facts did not get them fenced"$`}},
		{"an output check left in a comment", benchmarkCore, `raise RuntimeError("invalid Stop output")`, `pass  # raise RuntimeError("invalid Stop output")`,
			[]string{`no longer checks what the hooks wrote: it raises no "invalid Stop output"$`}},
		{"the real home", benchmarkCore, `env.update(HOME=str(self.path)`, `env.update(HOME=os.environ["HOME"]`,
			[]string{`no longer runs each scenario with HOME in a temporary folder of its own$`}},
	} {
		t.Run(c.name, func(t *testing.T) {
			s, k := script, core
			if c.file == benchmarkScript {
				if !strings.Contains(s, c.old) {
					t.Fatalf("%s doesn't hold %q", c.file, c.old)
				}
				s = strings.Replace(s, c.old, c.new, 1)
			} else {
				if !strings.Contains(k, c.old) {
					t.Fatalf("%s doesn't hold %q", c.file, c.old)
				}
				k = strings.Replace(k, c.old, c.new, 1)
			}
			problems := checkBenchmark(s, k)
			for _, p := range problems {
				if !slices.ContainsFunc(c.want, func(w string) bool { return regexp.MustCompile(w).MatchString(p) }) {
					t.Errorf("a problem the seed does not explain: %q", p)
				}
			}
			for _, w := range c.want {
				if !slices.ContainsFunc(problems, regexp.MustCompile(w).MatchString) {
					t.Errorf("no problem says %q: %q", w, problems)
				}
			}
		})
	}
}
