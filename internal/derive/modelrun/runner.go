// Package modelrun runs a host's CLI headless, sealed, for one model call,
// and says what happened in fixed words: a typed Outcome with the CLI's
// version and every model the host named, never the host's own text. A
// backend per host, registered from its own file, knows the host's command
// line and reads its output; this file and process.go hold what every
// backend keeps. SPEC.md beside it is the contract.
package modelrun

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/heyNag/eva-md/internal/host/adapter"
)

// Runner runs one host's CLI. Probe calls no model. Run makes one call: a
// failed run is an Outcome with a nil error; an error means the call could
// not run, or ctx ended.
type Runner interface {
	Probe(ctx context.Context) (Caps, error)
	Run(ctx context.Context, c Call) (Outcome, error)
}

// Options are a runner's: the CLI install recorded, and what probes run
// through.
type Options struct {
	// CLI is the CLI's absolute path as install recorded it. While it is an
	// executable file of the CLI's name it is used; otherwise PATH's is.
	CLI string
	// Env is what a probe reaches the machine through. Its Run, when set,
	// also runs the backend's probe step; otherwise that step is a sealed run.
	Env adapter.Env
}

// The tasks a call may be for.
const (
	TaskExtract   = "extract"
	TaskRepair    = "repair"
	TaskReview    = "review"
	TaskLivecheck = "livecheck"
)

var tasks = []string{TaskExtract, TaskRepair, TaskReview, TaskLivecheck}

// defaultDeadline is a call's deadline when it gives none.
const defaultDeadline = 600 * time.Second

// Call is one model call.
type Call struct {
	Task   string // extract, repair, review or livecheck
	System string // the fixed instructions
	Input  []byte // on stdin, never in an argument
	Schema []byte // a JSON object
	Model  string // empty: no model flag, the host's default
	Effort string // empty: no effort flag; else one of Caps.Efforts
	// Deadline is for all of the call's attempts together; 0 is 600 s.
	Deadline time.Duration
	// RunDir is an existing, empty folder, by absolute path: the child's
	// working folder.
	RunDir           string
	IgnoreUserConfig bool
}

// Class is what happened, in derive 8.1's eight words.
type Class string

const (
	ClassOK            Class = "ok"
	ClassAuth          Class = "auth"
	ClassRateLimit     Class = "rate_limit"
	ClassHost          Class = "host"
	ClassTimeout       Class = "timeout"
	ClassAnswerInvalid Class = "answer_invalid"
	ClassToolUsed      Class = "tool_used"
	ClassCLIMissing    Class = "cli_missing"
)

// What a host refused, in Outcome.Refused.
const (
	RefusedModel  = "model"
	RefusedEffort = "effort"
)

// BillingPlan is a call on the host's own sign-in, the only billing so far.
const BillingPlan = "plan"

// Signal names a structured field or event type a run saw, or a fact of the
// run. Each is a constant of this package, never the host's text.
type Signal string

// SignalModelUnreported says a run started a CLI and the host named no model
// that answered.
const SignalModelUnreported Signal = "model_unreported"

// coreSignals are the signals Run alone gives; no backend may.
var coreSignals = []Signal{SignalModelUnreported}

// Usage is what a call used, over its attempts. In is the uncached input,
// CachedIn the cached input, Out the output.
type Usage struct {
	In, CachedIn, Out int64
}

// Quota is what the host says is left of its plan. It is unknown for now.
type Quota struct {
	Known        bool
	RemainingPct int
	ResetAt      time.Time
}

// Outcome is what a call came to. Nothing of the host's output reaches it
// but an answer that is one JSON object, model names a model flag could
// take, token counts and the package's own words.
type Outcome struct {
	Class  Class
	Answer []byte // one JSON object; nil unless Class is ok
	// ModelAsked is the call's Model. ModelAnswered is the model the host
	// says answered, and ModelsSeen every model it named, sorted; neither is
	// ever filled from ModelAsked.
	ModelAsked    string
	ModelAnswered string
	ModelsSeen    []string
	Effort        string // the call's Effort
	Refused       string // RefusedModel, RefusedEffort or ""; set only on host
	CLI           string // the CLI's path; "" on cli_missing
	CLIVersion    string // the probe's; "" on cli_missing
	Billing       string
	Usage         Usage
	UsageSeen     bool // the host reported any usage
	Quota         Quota
	Signals       []Signal
	Attempts      int // CLI processes started
}

// backend is what differs between hosts: its effort values, the signals its
// attempts may give, its probe step and one attempt. A backend registers
// from its own file with register.
type backend struct {
	// efforts are the values the CLI's effort flag takes, lowest first.
	efforts []string
	// signals are the signals its results may carry; any other is dropped.
	// Each is a constant of the package: lower-case letters, digits and _,
	// and none of the core's.
	signals []Signal
	// probe is the probe step, run once per entry of the probe cache with
	// run, which starts the CLI with args under the step's own limit. It
	// gives the features it found, and ok when they may be cached. Nil: none.
	probe func(ctx context.Context, run probeRun) (features []string, ok bool)
	// attempt runs the CLI once, through a.run, and reads what it gave.
	attempt func(ctx context.Context, a *attempt) (result, error)
}

// probeRun starts the CLI with args and gives what it printed on stdout, at
// most maxList bytes, or an error that holds no text of its.
type probeRun func(ctx context.Context, args ...string) ([]byte, error)

// attempt is what one attempt is given.
type attempt struct {
	call Call
	caps Caps
	// n is 1 for the first attempt and 2 for the retry a first result asked
	// for with again.
	n int
	// run starts the CLI, caps.CLI, in the call's run folder with args, the
	// input on stdin and stdout to out, under what is left of the call's
	// deadline.
	run func(ctx context.Context, args []string, stdin []byte, out io.Writer) (exit, error)
}

// result is what a backend read from one attempt. Run takes from it only
// what an Outcome allows (SPEC.md, section 4).
type result struct {
	class     Class
	answer    []byte
	refused   string
	models    []string // every model the host named
	answered  string   // the one the host says answered
	usage     Usage
	usageSeen bool
	toolSign  bool // any sign of a tool, wherever the run stood
	signals   []Signal
	// again asks for one more attempt, under the same deadline, as when a
	// flag the backend can go without was refused.
	again bool
}

// maxAttempts is the most attempts one call makes.
const maxAttempts = 2

var backends = map[adapter.ID]backend{}

// register adds a host's backend. A host the adapter lacks, a second
// backend for a host or an invalid backend panics.
func register(id adapter.ID, b backend) {
	if _, ok := adapter.ByID(string(id)); !ok {
		panic(fmt.Sprintf("modelrun: no host %q", id))
	}
	if _, dup := backends[id]; dup {
		panic(fmt.Sprintf("modelrun: a second backend for %q", id))
	}
	if err := b.check(); err != nil {
		panic(err)
	}
	backends[id] = b
}

var signalName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

func (b backend) check() error {
	if b.attempt == nil {
		return errors.New("modelrun: a backend has no attempt")
	}
	for _, s := range b.signals {
		if !signalName.MatchString(string(s)) || slices.Contains(coreSignals, s) {
			return fmt.Errorf("modelrun: a backend's signal %q is not a name of its own", s)
		}
	}
	for _, e := range b.efforts {
		if !modelName.MatchString(e) {
			return fmt.Errorf("modelrun: a backend's effort %q is not a flag value", e)
		}
	}
	return nil
}

// New gives the runner of a host. A host with no backend is an error.
func New(id adapter.ID, o Options) (Runner, error) {
	b, ok := backends[id]
	if !ok {
		return nil, fmt.Errorf("modelrun: no backend for host %q", id)
	}
	return newRunner(id, b, o)
}

// runner is a Runner on one backend.
type runner struct {
	host adapter.Host
	b    backend
	opts Options
	// waitDelay, when set, is this runner's wait for output once a CLI has
	// exited, in place of the package's.
	waitDelay time.Duration
}

// newRunner is New for a given backend; tests reach their own backend
// through it.
func newRunner(id adapter.ID, b backend, o Options) (*runner, error) {
	h, ok := adapter.ByID(string(id))
	if !ok {
		return nil, fmt.Errorf("modelrun: no host %q", id)
	}
	if err := b.check(); err != nil {
		return nil, err
	}
	return &runner{host: h, b: b, opts: o}, nil
}

// Run makes the call. It refuses, before any process starts, a call that
// can't run; then it probes, and runs at most two attempts under the call's
// one deadline.
func (r *runner) Run(ctx context.Context, c Call) (Outcome, error) {
	if err := r.check(c); err != nil {
		return Outcome{}, err
	}
	caps, err := r.Probe(ctx)
	if err != nil {
		return Outcome{}, err
	}
	base := Outcome{ModelAsked: c.Model, Effort: c.Effort, Billing: BillingPlan, CLI: caps.CLI, CLIVersion: caps.Version}
	if caps.CLI == "" {
		return settle(base, result{}, tally{missing: true}, r.allowed()), nil
	}
	deadline := c.Deadline
	if deadline == 0 {
		deadline = defaultDeadline
	}
	end := time.Now().Add(deadline)
	var (
		t   tally
		res result
	)
	for n := 1; ; n++ {
		// The retry gets what the first attempt left, and none is a
		// timeout, as v1's ladder had it.
		if n > 1 && time.Until(end) <= 0 {
			t.timedOut = true
			break
		}
		a := &attempt{call: c, caps: caps, n: n, run: r.starter(&t, caps.CLI, c.RunDir, deadline, end)}
		res, err = r.b.attempt(ctx, a)
		if ctx.Err() != nil {
			return Outcome{}, fmt.Errorf("modelrun: run canceled: %w", ctx.Err())
		}
		if err != nil {
			// A backend's error may hold a path, an argument or the input,
			// whatever its care: only the core's own words are passed on.
			var own runError
			if errors.As(err, &own) {
				return Outcome{}, own
			}
			return Outcome{}, errCouldNotRun
		}
		t.add(res)
		if !res.again || n >= maxAttempts || t.toolSign || t.missing || t.timedOut {
			break
		}
	}
	return settle(base, res, t, r.allowed()), nil
}

// errCouldNotRun is what Run gives for an error of a backend's own.
const errCouldNotRun = runError("modelrun: the call could not run")

// starter is an attempt's run: the CLI in the run folder, sealed, each
// process it ends counted in t. The call's first run gets the whole
// deadline; every later one, in the same attempt or the next, gets what is
// left of it until end, and none is a timeout with no process started.
func (r *runner) starter(t *tally, cli, dir string, deadline time.Duration, end time.Time) func(context.Context, []string, []byte, io.Writer) (exit, error) {
	return func(ctx context.Context, args []string, stdin []byte, out io.Writer) (exit, error) {
		timeout := deadline
		if t.runs > 0 {
			if timeout = time.Until(end); timeout <= 0 {
				t.timedOut = true
				return exit{failed: true, timedOut: true}, nil
			}
		}
		t.runs++
		e, err := spawn(ctx, proc{
			cli: cli, args: args, dir: dir, stdin: stdin, stdout: out,
			timeout: timeout, waitDelay: r.wait(), pathDir: r.pathDir(cli),
		})
		if e.started {
			t.attempts++
		}
		t.missing = t.missing || e.missing
		t.timedOut = t.timedOut || e.timedOut
		return e, err
	}
}

func (r *runner) wait() time.Duration {
	if r.waitDelay > 0 {
		return r.waitDelay
	}
	return waitDelay
}

// pathDir is the folder added to the child's PATH: the CLI's, when the CLI
// install recorded is the one used.
func (r *runner) pathDir(cli string) string {
	if recordedCLI(r.host.Names.CLI, r.opts.CLI) == "" {
		return ""
	}
	return filepath.Dir(cli)
}

// allowed are the signals the backend declared.
func (r *runner) allowed() map[Signal]bool {
	allowed := map[Signal]bool{}
	for _, s := range r.b.signals {
		allowed[s] = true
	}
	return allowed
}

// tally is what Run learned over the attempts, apart from the last result.
type tally struct {
	runs      int // calls of an attempt's run
	attempts  int
	missing   bool // a CLI was not there
	timedOut  bool // the call's time ended
	toolSign  bool
	usage     Usage
	usageSeen bool
	models    []string
	signals   []Signal
}

func (t *tally) add(res result) {
	t.toolSign = t.toolSign || res.toolSign || res.class == ClassToolUsed
	t.usage.In += tokens(res.usage.In)
	t.usage.CachedIn += tokens(res.usage.CachedIn)
	t.usage.Out += tokens(res.usage.Out)
	t.usageSeen = t.usageSeen || res.usageSeen
	t.models = append(t.models, res.models...)
	if res.answered != "" {
		t.models = append(t.models, res.answered)
	}
	t.signals = append(t.signals, res.signals...)
}

// maxTokens bounds one attempt's count, so no sum can overflow.
const maxTokens = 1 << 40

func tokens(n int64) int64 {
	return min(max(n, 0), maxTokens)
}

// settle applies what every backend keeps (SPEC.md, section 4) to the last
// result and the tally: the class, in this order, cli_missing, tool_used,
// timeout, then the backend's; an answer only on ok; only model names a flag
// could take, never the model asked for; only allowed signals; the version
// only where a CLI was found.
func settle(o Outcome, res result, t tally, allowed map[Signal]bool) Outcome {
	o.Attempts = t.attempts
	o.Usage, o.UsageSeen = t.usage, t.usageSeen
	switch {
	case t.missing:
		o.Class = ClassCLIMissing
	case t.toolSign:
		o.Class = ClassToolUsed
	case t.timedOut:
		o.Class = ClassTimeout
	default:
		switch res.class {
		case ClassOK, ClassAuth, ClassRateLimit, ClassHost, ClassAnswerInvalid:
			o.Class = res.class
		default:
			// cli_missing and timeout come from the process alone; any other
			// word is no class.
			o.Class = ClassHost
		}
	}
	if o.Class == ClassOK {
		if answer, ok := object(res.answer); ok {
			o.Answer = bytes.Clone(answer)
		} else {
			o.Class = ClassAnswerInvalid
		}
	}
	if o.Class == ClassHost && (res.refused == RefusedModel || res.refused == RefusedEffort) {
		o.Refused = res.refused
	}
	if o.Class == ClassCLIMissing {
		o.CLI, o.CLIVersion = "", ""
	}
	for _, m := range t.models {
		if modelName.MatchString(m) && !slices.Contains(o.ModelsSeen, m) {
			o.ModelsSeen = append(o.ModelsSeen, m)
		}
	}
	slices.Sort(o.ModelsSeen)
	if modelName.MatchString(res.answered) {
		o.ModelAnswered = res.answered
	}
	for _, s := range t.signals {
		if allowed[s] && !slices.Contains(o.Signals, s) {
			o.Signals = append(o.Signals, s)
		}
	}
	if o.Attempts > 0 && o.ModelAnswered == "" {
		o.Signals = append(o.Signals, SignalModelUnreported)
	}
	return o
}

// A model name never starts with a dash, so it can't be read as a flag.
// The rest allows aliases, dated ids, provider prefixes and "[1m]".
var modelName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/@\[\]-]{0,199}$`)

// check refuses a call that can't run, before any process starts: a task it
// doesn't know, nothing to run, a value a CLI could read as another flag, an
// effort the backend doesn't take, a negative deadline, or a run folder that
// is not an existing, empty folder. Blank instructions would replace a
// host's system prompt with nothing.
func (r *runner) check(c Call) error {
	switch {
	case !slices.Contains(tasks, c.Task):
		return errors.New("modelrun: the task is not one of " + strings.Join(tasks, ", "))
	case len(bytes.TrimSpace(c.Input)) == 0:
		return errors.New("modelrun: the input is empty")
	case strings.TrimSpace(c.System) == "":
		return errors.New("modelrun: the instructions are empty")
	case strings.HasPrefix(c.System, "-") || strings.ContainsRune(c.System, 0):
		return errors.New("modelrun: the instructions must not start with a dash or hold a NUL byte")
	case c.Model != "" && !modelName.MatchString(c.Model):
		return errors.New("modelrun: the model name has characters a CLI flag can't take")
	case c.Effort != "" && !slices.Contains(r.b.efforts, c.Effort):
		return errors.New("modelrun: the effort is not one the host takes")
	case !isObject(c.Schema):
		return errors.New("modelrun: the schema is not a JSON object")
	case c.Deadline < 0:
		return errors.New("modelrun: the deadline is negative")
	}
	return checkRunDir(c.RunDir)
}

// checkRunDir requires an existing, empty folder: the child must find nothing
// there to read, and no answer left by an earlier run. Its errors name no
// path.
func checkRunDir(dir string) error {
	if !filepath.IsAbs(dir) {
		return errors.New("modelrun: the run folder must be an absolute path")
	}
	f, err := os.Open(dir)
	if err != nil {
		return errors.New("modelrun: the run folder can't be opened")
	}
	defer f.Close()
	if info, err := f.Stat(); err != nil || !info.IsDir() {
		return errors.New("modelrun: the run folder is not a folder")
	}
	names, err := f.Readdirnames(1)
	if len(names) > 0 {
		return errors.New("modelrun: the run folder is not empty")
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return errors.New("modelrun: the run folder can't be read")
	}
	return nil
}

// object returns data without surrounding space when it is one JSON object,
// in UTF-8 throughout, in which no object at any depth gives a key twice:
// json.Valid takes invalid UTF-8 in a string, which a decoder later replaces
// without a word, and a key given twice, of which one reader keeps the first
// value and another the last, so two readers would take two answers from
// one.
func object(data []byte) ([]byte, bool) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || data[0] != '{' || !utf8.Valid(data) || !json.Valid(data) {
		return nil, false
	}
	if ok, _ := keysOnce(data); !ok {
		return nil, false
	}
	return data, true
}

// maxAnswerDepth is encoding/json's own limit of nesting, to which
// json.Valid holds an answer before keysOnce reads it, and keysOnce holds
// itself.
const maxAnswerDepth = 10000

// keyWork is what keysOnce did: the tokens it read and the bytes its reader
// took. Tests read it to show the work grows with the answer's size alone.
type keyWork struct{ tokens, bytes int }

// keysOnce reports whether no object in data, one JSON value, gives a key
// twice. It reads data once, with encoding/json's token reader, and keeps a
// set of keys for each object open on a stack of its own, at most
// maxAnswerDepth deep: there is no recursion, so no depth of the answer can
// exhaust the stack, and each key's check is one lookup in its object's
// set, so the work and the memory are bounded by data's size. Keys are
// compared as the reader decodes them, escapes undone, in their letter case.
func keysOnce(data []byte) (bool, keyWork) {
	src := &countingReader{r: bytes.NewReader(data)}
	dec := json.NewDecoder(src)
	// A number is kept as its text: no number, however large, is an error.
	dec.UseNumber()
	type open struct {
		object bool
		keys   map[string]struct{} // made at the object's first key
		key    bool                // the object's next token is a key
	}
	var (
		stack []open
		w     keyWork
	)
	for {
		tok, err := dec.Token()
		w.bytes = src.n
		if err == io.EOF {
			return len(stack) == 0, w
		}
		if err != nil {
			return false, w
		}
		w.tokens++
		if tok == json.Delim('}') || tok == json.Delim(']') {
			stack = stack[:len(stack)-1]
			continue
		}
		if n := len(stack); n > 0 && stack[n-1].object {
			top := &stack[n-1]
			if top.key {
				k, ok := tok.(string)
				if !ok {
					return false, w
				}
				if _, twice := top.keys[k]; twice {
					return false, w
				}
				if top.keys == nil {
					top.keys = map[string]struct{}{}
				}
				top.keys[k] = struct{}{}
				top.key = false
				continue
			}
			// This token is the pair's value, or opens it: the object's
			// next token, once the value is read, is a key.
			top.key = true
		}
		if tok == json.Delim('{') || tok == json.Delim('[') {
			if len(stack) >= maxAnswerDepth {
				return false, w
			}
			stack = append(stack, open{object: tok == json.Delim('{'), key: true})
		}
	}
}

// countingReader counts the bytes read through it.
type countingReader struct {
	r io.Reader
	n int
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += n
	return n, err
}

func isObject(data []byte) bool {
	_, ok := object(data)
	return ok
}
