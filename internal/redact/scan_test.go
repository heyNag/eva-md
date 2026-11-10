package redact

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// Tests for the driver: Scan and Apply give what Redact gave before them
// (redactV1), on every text the other tests of this package redact and on
// generated texts; a scan stopped by its context keeps a safe prefix.
// Fixtures that look like secrets are joined from fragments at run time.

// TestMain checks every text the package's tests redact against redactV1, as
// they run (every tenth under the race detector), and fails the run if one
// differs.
func TestMain(m *testing.M) {
	redacted = oracle.check
	code := m.Run()
	redacted = nil
	if bad := oracle.report(); bad != "" {
		fmt.Fprint(os.Stderr, bad)
		code = 1
	}
	os.Exit(code)
}

// oracle checks each of Redact's calls against redactV1.
var oracle = &v1Oracle{}

type v1Oracle struct {
	mu      sync.Mutex
	calls   int // calls of Redact
	checked int // calls checked
	bad     []string
}

// check is set as redacted: it compares one call of Redact with redactV1,
// and checks the findings and that redacting the result again changes
// nothing (see differs). Under the race detector, which slows the rules
// many times over, it checks every tenth call.
func (o *v1Oracle) check(text, out string, n int) {
	o.mu.Lock()
	o.calls++
	call := o.calls
	o.mu.Unlock()
	if raceOn && call%10 != 1 {
		return
	}
	problem := differs(text, out, n, ruleMemo{})
	o.mu.Lock()
	defer o.mu.Unlock()
	o.checked++
	if problem != "" && len(o.bad) < 20 {
		o.bad = append(o.bad, problem)
	}
}

// report says what check found wrong, or nothing.
func (o *v1Oracle) report() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	if len(o.bad) == 0 {
		return ""
	}
	return fmt.Sprintf("Redact differs from redactV1 on texts the tests redact (%d checked):\n%s", o.checked, strings.Join(o.bad, "\n"))
}

// ruleMemo keeps what the rule sets returned for each text, so redactV1, the
// driver and the driver again on the result, which run them on the same
// texts, run them once. The rules are a function of the text alone, so what
// is kept is what they would return again.
type ruleMemo map[string]ruleRun

type ruleRun struct {
	floor, current string
	nf             int
}

func (m ruleMemo) rules(text string, stop func() bool) (string, int, string, bool) {
	if r, ok := m[text]; ok {
		return r.floor, r.nf, r.current, true
	}
	floor, nf, current, ok := v1Rules(text, stop)
	m[text] = ruleRun{floor: floor, current: current, nf: nf}
	return floor, nf, current, ok
}

// differs checks out and n, redacted from text: they are redactV1's, Scan's
// findings give them and are well formed, and redacting out again changes
// nothing. It says what is wrong, or returns "". Scan runs on the rules'
// output kept in memo.
func differs(text, out string, n int, memo ruleMemo) string {
	want, wantN := redactV1With(text, memo.rules)
	if out != want || n != wantN {
		return fmt.Sprintf("  Redact(%q) = %q, %d; redactV1 %q, %d", text, out, n, want, wantN)
	}
	d := driver{ctx: context.Background(), rules: memo.rules}
	r := d.scan(text, firstPrefix)
	if Apply(r) != out || r.count != n {
		return fmt.Sprintf("  Scan(%q) gives %q, %d; Redact %q, %d", text, Apply(r), r.count, out, n)
	}
	if bad := badFindings(r); bad != "" {
		return fmt.Sprintf("  Scan(%q): %s", text, bad)
	}
	again := d.scan(out, firstPrefix)
	if twice := Apply(again); twice != out || again.count != 0 || len(again.Findings) != 0 {
		return fmt.Sprintf("  not idempotent on %q: %q then %q, %d", text, out, twice, again.count)
	}
	return ""
}

// badFindings says what is wrong with r's findings, or returns "". Each must
// be a range of Clean up to Through, in order, not overlapping, that a
// marker of a known kind replaces, with a rule set and a tier. Where they
// could not follow the rules' output, the one finding must hold all that
// changed.
func badFindings(r Result) string {
	if r.Through > len(r.Clean) {
		return fmt.Sprintf("through %d of %d", r.Through, len(r.Clean))
	}
	at := 0
	for _, f := range r.Findings {
		switch {
		case f.Start < at || f.End <= f.Start || f.End > r.Through:
			return fmt.Sprintf("finding %+v out of order or outside [%d, %d)", f, at, r.Through)
		case !isMarker(marker + f.Kind + "]"):
			return fmt.Sprintf("finding %+v has no marker kind", f)
		case f.Rule != RuleCurrent && f.Rule != RuleBaseline:
			return fmt.Sprintf("finding %+v has no rule set", f)
		case f.Tier < Certain || f.Tier > FloorOnly:
			return fmt.Sprintf("finding %+v has no tier", f)
		}
		at = f.End
	}
	if r.untraced {
		out := Apply(r)
		if len(r.Findings) != 1 || !strings.HasPrefix(out, r.Clean[:r.Findings[0].Start]) || !strings.HasSuffix(out, r.Clean[r.Findings[0].End:r.Through]) {
			return fmt.Sprintf("untraced, and the findings %+v don't hold all that changed in %q", r.Findings, out)
		}
	}
	return ""
}

// Redact tells the oracle TestMain sets of each of its calls, so every text
// the package's tests redact is checked against redactV1.
func TestOracleSeesRedactCalls(t *testing.T) {
	isolate(t)
	before := oracleCalls()
	Redact("token=" + j("Ab3d", "Ef6hIj9l"))
	if oracleCalls() != before+1 {
		t.Fatal("Redact did not tell the oracle of its call")
	}
	// And the oracle can tell a wrong answer.
	if differs("token="+j("Ab3d", "Ef6hIj9l"), "token=Ab3dEf6hIj9l", 0, ruleMemo{}) == "" {
		t.Fatal("the oracle took the text unredacted")
	}
}

func oracleCalls() int {
	oracle.mu.Lock()
	defer oracle.mu.Unlock()
	return oracle.calls
}

// textSource is a source of texts: secret shapes from the other tests of
// this package, between ordinary words, code, URLs, commands, key: value
// lines and long lines, from a fixed seed.
type textSource struct{ seed uint32 }

func (g *textSource) next(n int) int {
	g.seed = g.seed*1664525 + 1013904223
	return int(g.seed>>8) % n
}

func (g *textSource) pick(from []string) string { return from[g.next(len(from))] }

var (
	genSecrets = []string{
		ghTok, patTok, glTok, slack, hook, tgBot, discord, oai, ant, goog, stripe, ops, jwt, akid, awsSec, pemKey, pemOpen,
		random, sha40, uuid, j("S3cr3t", "Pw9"), j("Tr0ub4", "dor3x"), j("hunter2", "Pass9"), j("s3cret", "Value!"),
		j("a&bcdefg", "hijklmnop"), j("Xy7(kLm9", "qR2zT5"), j("correct", " horse battery"), j("1234", "5678"),
		j("change", "me"), j("aZbYcXdWeVfUgT", "hSiRjQkPlOmN"), j("dXNlcjpw", "YXNzd29yZA=="),
		"[REDACTED:secret_value]", "[REDACTED:password]", "[REDACTED:", "[REDACTED:not a marker]",
	}
	genKeys = []string{
		"password", "token", "api_key", "DB_PASS", "MYSQL_PWD", "client_secret", "accessToken", "Authorization",
		"aws_secret_access_key", "secret", "PGPASSWORD", "pass", "pwd", "private_key", "GITHUB_TOKEN", "apiKey",
		"tokenizer", "input_tokens", "passphrase", "x-api-key", "SMTP__PASS", "export DB_PASSWORD",
	}
	genSeps  = []string{"=", ": ", " = ", ":", `="`, "='", " is ", `": "`, ":\n  ", " => ", "==", " := "}
	genWords = []string{
		"the", "deploy", "rotated", "commit", "session", "we", "agreed", "to", "keep", "it", "and", "a", "secret",
		"bearer", "Bearer", "key", "password", "was", "is", "token", "notes", "for", "release", "branch", "main",
		"op://Vault/db/password", "<your-token>", "${DB_PASSWORD}", "$TOKEN", "nil", "None", "true", "42", "3599",
	}
	genCode = []string{
		"getToken(user)", "os.environ['OPENAI_API_KEY']", "$(gh auth token)", "func f() {", "if err != nil {",
		"return err", "}", "tokenizer = NewTokenizer(cfg)", "cfg.Token = nil // reset", "const DB_PASS = '",
		"api_key: str = ", "token: string | null", `[]byte("x")`, "map[string]int{}", "&Block{", "await new ",
		"%r{", `/^v(\d+)$/i`, "do |x|", "1 << iota", "x = y; ", `print(f"{token}")`,
	}
	genURLs = []string{
		"https://alice:" + j("hunter2", "Pass9") + "@git.acme.dev/r.git", "https://app.example.com/cb?access_token=",
		"&refresh_token=", "&expires_in=3599", "https://github.com/heyNag/eva-md.git", "ssh://git@gitlab.acme.dev:2222/team/repo.git",
		"https://x.example/login?user=bob&pass=", "Server=db;Uid=sa;Pwd=", "postgres://app:", "@db.internal:5432/app",
		"https://auth.example.com/oauth/token", "token=op://Vault/x/credential", "gh://",
	}
	genCommands = []string{
		"curl -u admin:", "curl -H 'Authorization: Bearer ", "mysql -uroot -p", "sshpass -p ", "docker login -p ",
		"redis-cli -a ", "openssl enc -pass pass:", "pymysql ", "mariadb-dump --password=", "git clone ", "go test ./... ",
		"echo ", "sh -c '", "export ",
	}
	genGlue = []string{" ", " ", " ", "\n", "\n", "\r\n", "\t", `"`, "'", "`", "&", ";", ",", "(", ")", "{", "}", "\\\n", "# ", "// ", ".", "​", "\x1b[31m"}
)

// text returns the next generated text, of up to 3n pieces and the glue
// after each.
func (g *textSource) text(n int) string {
	var b strings.Builder
	for k := g.next(3*n + 1); k > 0; k-- {
		switch g.next(9) {
		case 0, 1:
			b.WriteString(g.pick(genWords))
		case 2:
			b.WriteString(g.pick(genSecrets))
		case 3:
			// A key: value line.
			b.WriteString(g.pick(genKeys) + g.pick(genSeps))
			switch g.next(3) {
			case 0:
				b.WriteString(g.pick(genSecrets))
			case 1:
				b.WriteString(g.pick(genWords))
			default:
				b.WriteString(g.pick(genCode))
			}
			b.WriteString("\n")
		case 4:
			b.WriteString(g.pick(genCode))
		case 5:
			b.WriteString(g.pick(genURLs))
		case 6:
			b.WriteString(g.pick(genCommands))
		case 7:
			// A long line of words.
			for n := g.next(60); n > 0; n-- {
				b.WriteString(g.pick(genWords) + " ")
			}
		}
		b.WriteString(g.pick(genGlue))
	}
	return b.String()
}

// Apply(Scan(x)) is redactV1(x), with the same count, on 150,000 generated
// texts, every 250th a long one, and redacting the result again changes
// nothing (see differs). Both run on the rules' output kept for the text,
// so the rules run once for each text; for each long one, Redact runs
// the rules itself, and a scan whose context can end, which redacts
// prefixes first, ends with the same findings. Under the race detector,
// which slows the rules many times over, it checks 5,000.
func TestScanMatchesRedactV1(t *testing.T) {
	isolate(t)
	total := 150000
	if raceOn {
		total = 5000
	}
	type text struct {
		in   string
		long bool
	}
	texts := make(chan text, 256)
	go func() {
		defer close(texts)
		g := &textSource{seed: 20261110}
		for i := 0; i < total; i++ {
			if i%250 == 0 {
				texts <- text{g.text(20), true}
			} else {
				texts <- text{g.text(2), false}
			}
		}
	}()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var mu sync.Mutex
	done, untraced, failures := 0, 0, 0
	var wg sync.WaitGroup
	for w := 0; w < runtime.GOMAXPROCS(0); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for x := range texts {
				in := x.in
				memo := ruleMemo{}
				out, n := redactV1With(in, memo.rules)
				problem := differs(in, out, n, memo)
				r := driver{ctx: context.Background(), rules: memo.rules}.scan(in, firstPrefix)
				if problem == "" && x.long {
					if got, k := Redact(in); got != out || k != n {
						problem = fmt.Sprintf("Redact(%q) = %q, %d; want %q, %d", in, got, k, out, n)
					} else if p := (driver{ctx: ctx}).scan(in, 64); fmt.Sprint(p) != fmt.Sprint(r) {
						problem = fmt.Sprintf("a scan that could stop differs on %q: %+v; want %+v", in, p, r)
					}
				}
				mu.Lock()
				done++
				if r.untraced {
					untraced++
				}
				if problem != "" && failures < 5 {
					failures++
					t.Error(problem)
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if done != total {
		t.Fatalf("checked %d texts, want %d", done, total)
	}
	t.Logf("%d texts checked, %d with part of a marker in the rules' output", done, untraced)
}

// A scan stopped at any point it checks its context returns at that point,
// and keeps a prefix that ends a line, reaches no further than the text it
// was reading, is the rules' redaction of that prefix, and keeps nothing a
// scan of the whole text removes from it. The scan blocks at the chosen
// point until the test has cancelled its context and released it. Under the
// race detector it stops scans of 20 texts, not 200.
func TestCancelledScanKeepsSafePrefix(t *testing.T) {
	isolate(t)
	g := &textSource{seed: 7}
	texts := 200
	if raceOn {
		texts = 20
	}
	stops := 0
	for i := 0; i < texts; i++ {
		in := g.text(30)
		full := Scan(context.Background(), in)
		// The points a scan that runs to the end checks its context at.
		var points []int
		ctx, cancel := context.WithCancel(context.Background())
		driver{ctx: ctx, step: func(through int) { points = append(points, through) }}.scan(in, 64)
		cancel()
		for _, k := range []int{0, len(points) / 3, len(points) / 2, len(points) - 1, (i * 7) % len(points)} {
			r, point, calls := stopAt(t, in, k)
			stops++
			if calls != k+1 {
				t.Fatalf("scan of %q went on past the point it was stopped at: %d checks, want %d", in, calls, k+1)
			}
			if point != points[k] {
				t.Fatalf("stopped at %d, want %d", point, points[k])
			}
			if r.Through > point || r.Through > 0 && r.Clean[r.Through-1] != '\n' || r.Clean != full.Clean {
				t.Fatalf("stopped at %d of %q, kept through %d", point, in, r.Through)
			}
			if bad := badFindings(r); bad != "" {
				t.Fatalf("stopped scan of %q: %s", in, bad)
			}
			kept := Apply(r)
			if want, _ := redactV1(r.Clean[:r.Through]); kept != want {
				t.Fatalf("kept %q; want the prefix redacted, %q", kept, want)
			}
			if leak := uncovered(full, r); leak != "" {
				t.Fatalf("stopped at %d of %q, the prefix keeps %q, which the whole scan removes:\nprefix %q\nwhole  %q", point, in, leak, kept, Apply(full))
			}
		}
	}
	if stops != 5*texts {
		t.Fatalf("%d scans stopped, want %d", stops, 5*texts)
	}
}

// stopAt runs a scan of in, with small prefixes, that blocks at its kth
// check of its context until the test has cancelled the context. It returns
// the result, how far the text being read reached at that check, and how
// many checks the scan made in all.
func stopAt(t *testing.T, in string, k int) (Result, int, int) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reached := make(chan int)
	release := make(chan struct{})
	calls := 0
	step := func(through int) {
		calls++
		if calls == k+1 {
			reached <- through
			<-release
		}
	}
	done := make(chan Result)
	go func() { done <- driver{ctx: ctx, step: step}.scan(in, 64) }()
	var point int
	select {
	case point = <-reached:
	case <-done:
		t.Fatalf("scan of %q ended at %d checks, before check %d", in, calls, k+1)
	case <-time.After(time.Minute):
		t.Fatal("scan never reached its check: hang guard")
	}
	cancel()
	close(release)
	select {
	case r := <-done:
		return r, point, calls
	case <-time.After(time.Minute):
		t.Fatal("scan did not return: hang guard")
	}
	return Result{}, 0, 0
}

// uncovered returns text of the prefix r kept that full replaces, or "".
func uncovered(full, r Result) string {
	removed := make([]bool, r.Through)
	for _, f := range r.Findings {
		for i := f.Start; i < f.End; i++ {
			removed[i] = true
		}
	}
	for _, f := range full.Findings {
		for i := f.Start; i < min(f.End, r.Through); i++ {
			if !removed[i] {
				return r.Clean[i:min(f.End, r.Through)]
			}
		}
	}
	return ""
}

// uncovered finds what a prefix keeps that the whole scan removes.
func TestUncoveredFindsLeaks(t *testing.T) {
	c := "abc secret def\nmore\n"
	full := Result{Clean: c, Through: len(c), Findings: []Finding{{Start: 4, End: 10}}}
	if got := uncovered(full, Result{Clean: c, Through: 15}); got != "secret" {
		t.Fatalf("uncovered = %q; want the secret", got)
	}
	if got := uncovered(full, Result{Clean: c, Through: 15, Findings: []Finding{{Start: 3, End: 10}}}); got != "" {
		t.Fatalf("uncovered = %q; want nothing", got)
	}
}

// The findings say which rule set found a secret and how sure it is: a
// provider token is certain, a key's value likely, and a value only the
// baseline rules remove is the floor's alone.
func TestFindingLabels(t *testing.T) {
	isolate(t)
	pw := j("S3cr3t", "Pw9")
	for _, c := range []struct {
		in   string
		want []Finding
	}{
		{"id " + akid + " here", []Finding{{3, 3 + len(akid), "aws_key", RuleCurrent, Certain}}},
		{"password: " + j("s3cret", "Value!"), []Finding{{10, 22, "secret_value", RuleCurrent, Likely}}},
		{"x\n" + pemKey, []Finding{{2, 2 + len(pemKey), "private_key", RuleCurrent, Certain}}},
		// The current rules keep this as code; the baseline rules take the
		// name after the separator.
		{"\tcliSecret := cli2.writeSecret[QUICEncryptionLevelEarly]", []Finding{{14, 30, "secret_value", RuleBaseline, FloorOnly}}},
		{"the token " + ghTok + " and DB_PASS=" + pw, []Finding{
			{10, 10 + len(ghTok), "github_token", RuleCurrent, Certain},
			{10 + len(ghTok) + 13, 10 + len(ghTok) + 13 + len(pw), "secret_value", RuleCurrent, Likely},
		}},
	} {
		r := Scan(context.Background(), c.in)
		if fmt.Sprint(r.Findings) != fmt.Sprint(c.want) {
			t.Errorf("Scan(%q) found %+v; want %+v (redacted %q)", c.in, r.Findings, c.want, Apply(r))
		}
	}
}

// A marker that stands for a finding a later round takes in is part of
// the later finding, and its tier, if surer, stays.
func TestRemapTakesInMarkers(t *testing.T) {
	ps := []piece{{start: 0, end: 4}, {start: 4, end: 10, marker: true, kind: "aws_key", rule: RuleCurrent, tier: Certain}, {start: 10, end: 20}}
	// In the text: "abcd" [0,4), the marker [4,22), the rest [22,32).
	got, bad := remap(append([]piece(nil), ps...), []removal{{start: 2, end: 24, kind: "secret_value"}})
	want := []piece{{start: 0, end: 2}, {start: 2, end: 12, marker: true, kind: "secret_value", rule: RuleCurrent, tier: Certain}, {start: 12, end: 20}}
	if bad || fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("remap = %+v, %v; want %+v", got, bad, want)
	}
	// A range that starts or ends inside the marker can't be followed.
	for _, r := range []removal{{start: 5, end: 24, kind: "x"}, {start: 0, end: 7, kind: "x"}} {
		if _, bad := remap(append([]piece(nil), ps...), []removal{r}); !bad {
			t.Errorf("remap of %+v cut the marker and said nothing", r)
		}
	}
}

// Where the rules' output holds part of a marker, which the baseline rules
// write when they replace a range that starts inside a marker of their own,
// Apply returns the rules' own text, and the one finding covers all that
// changed.
func TestUntracedKeepsRulesText(t *testing.T) {
	isolate(t)
	in := "gh://" + oai + "]" + random[:20] + "@x"
	r := Scan(context.Background(), in)
	out := Apply(r)
	if want, n := redactV1(in); out != want || r.count != n {
		t.Fatalf("Apply(Scan(%q)) = %q, %d; want %q, %d", in, out, r.count, want, n)
	}
	if !r.untraced || !strings.HasPrefix(out, "gh://[REDACTED:[REDACTED:") {
		t.Fatalf("Scan(%q) = %+v; want the rules' text, with part of a marker", in, r)
	}
	if want := []Finding{{5, len(in) - 2, "secret_value", RuleCurrent, Likely}}; fmt.Sprint(r.Findings) != fmt.Sprint(want) {
		t.Fatalf("findings %+v; want %+v", r.Findings, want)
	}
	// A trace whose pieces don't give its text is untraced too.
	tr := newTrace("abc")
	tr.text = "abd"
	if r := tr.result(); !r.untraced || Apply(r) != "abd" || fmt.Sprint(r.Findings) != fmt.Sprint([]Finding{{2, 3, "secret_value", RuleCurrent, Likely}}) {
		t.Fatalf("pieces that don't give the text: %+v", r)
	}
}

// A scan whose context has ended before it starts keeps nothing.
func TestScanEndedContext(t *testing.T) {
	isolate(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	in := strings.Repeat("line of text\n", 10000) + "token " + ghTok
	r := Scan(ctx, in)
	if r.Through != 0 || len(r.Findings) != 0 || Apply(r) != "" || r.Clean != in {
		t.Fatalf("Scan with an ended context: through %d, %d findings", r.Through, len(r.Findings))
	}
}
