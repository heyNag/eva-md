package modelrun

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/host/adapter"
)

const featureList = `alpha    stable  true
beta     under development  false
a notice that is not a feature
`

// A second probe of an unchanged CLI runs no command: the adapter keeps the
// version and the probe keeps the step's features, by the CLI's path, size,
// modification time and version. A new modification time runs both again,
// though the version is the same; a new version reaches the next call.
func TestASecondProbeOfAnUnchangedCLIRunsNoCommand(t *testing.T) {
	isolate(t)
	dump := puppetHost(t)
	put(t, dump, "features", featureList)
	cli := ownCLI(t, adapter.Codex)
	r, calls := puppetRunner(t, adapter.Codex, Options{CLI: cli})
	caps, err := r.Probe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := Caps{Host: adapter.Codex, CLI: cli, Version: "0.1.0", Efforts: []string{"low", "medium", "high"}, Features: []string{"alpha", "beta"}}
	if !sameCaps(caps, want) {
		t.Fatalf("caps %+v, want %+v", caps, want)
	}
	if calls.count("--version") != 1 || calls.count("features list") != 1 || calls.total() != 2 {
		t.Fatalf("the first probe ran %q", calls.calls)
	}
	if got := read(t, dump, "features.args"); got != "features\x00list\x00" {
		t.Fatalf("the probe step's arguments %q", got)
	}
	// The caps given are copies.
	caps.Features[0], caps.Efforts[0] = "changed", "changed"
	for i := range 2 {
		again, err := r.Probe(context.Background())
		if err != nil || !sameCaps(again, want) {
			t.Fatalf("probe %d: %+v %v", i+2, again, err)
		}
	}
	if out, err := r.Run(context.Background(), call(t, adapter.Codex)); err != nil || out.CLIVersion != "0.1.0" {
		t.Fatalf("a run: %+v %v", out, err)
	}
	if n := calls.total(); n != 2 {
		t.Fatalf("the probes of an unchanged CLI ran %d commands, want the first probe's 2: %q", n, calls.calls)
	}
	// A runner of its own shares the cache.
	r2, calls2 := puppetRunner(t, adapter.Codex, Options{CLI: cli})
	if caps, err := r2.Probe(context.Background()); err != nil || !sameCaps(caps, want) || calls2.total() != 0 {
		t.Fatalf("another runner's probe: %+v %v, %d commands", caps, err, calls2.total())
	}
	// A new modification time, the same version: both run again.
	info, err := os.Stat(cli)
	if err != nil {
		t.Fatal(err)
	}
	later := info.ModTime().Add(time.Hour)
	if err := os.Chtimes(cli, later, later); err != nil {
		t.Fatal(err)
	}
	if caps, err := r.Probe(context.Background()); err != nil || !sameCaps(caps, want) {
		t.Fatalf("after a new time: %+v %v", caps, err)
	}
	if calls.count("--version") != 2 || calls.count("features list") != 2 {
		t.Fatalf("after a new time the probe ran %q", calls.calls)
	}
	// A new version and time: the next call's version is the new one, and
	// the step runs again.
	put(t, dump, "version", "fake-cli 7.8.9 (built for the test)\n")
	put(t, dump, "features", "gamma stable true\n")
	later = later.Add(time.Hour)
	if err := os.Chtimes(cli, later, later); err != nil {
		t.Fatal(err)
	}
	out, err := r.Run(context.Background(), call(t, adapter.Codex))
	if err != nil || out.CLIVersion != "7.8.9" || out.Class != ClassOK {
		t.Fatalf("a run after a new version: %+v %v", out, err)
	}
	if calls.count("--version") != 3 || calls.count("features list") != 3 {
		t.Fatalf("after a new version the probe ran %q", calls.calls)
	}
	if caps, err := r.Probe(context.Background()); err != nil || caps.Version != "7.8.9" || !slices.Equal(caps.Features, []string{"gamma"}) || calls.total() != 6 {
		t.Fatalf("after a new version: %+v %v, %d commands", caps, err, calls.total())
	}
}

func sameCaps(a, b Caps) bool {
	return a.Host == b.Host && a.CLI == b.CLI && a.Version == b.Version && a.Known == b.Known && a.Untested == b.Untested &&
		slices.Equal(a.Efforts, b.Efforts) && slices.Equal(a.Features, b.Features)
}

// The probe reads Known and Untested from the adapter's table.
func TestProbeReadsTheVersionsRow(t *testing.T) {
	rows := adapter.Rows(adapter.Codex)
	if len(rows) == 0 {
		t.Fatal("the adapter has no row for the host")
	}
	newest := rows[len(rows)-1].Highest
	parts := strings.Split(newest, ".")
	above := parts[0] + "." + parts[1] + ".9999"
	for _, tc := range []struct {
		version         string
		known, untested bool
	}{
		{newest, true, false},
		{above, false, true},
		{"0.0.1", false, false},
	} {
		t.Run(tc.version, func(t *testing.T) {
			isolate(t)
			dump := puppetHost(t)
			put(t, dump, "version", "fake "+tc.version+"\n")
			r, _ := puppetRunner(t, adapter.Codex, Options{CLI: ownCLI(t, adapter.Codex)})
			caps, err := r.Probe(context.Background())
			if err != nil || caps.Version != tc.version || caps.Known != tc.known || caps.Untested != tc.untested {
				t.Fatalf("caps %+v %v", caps, err)
			}
		})
	}
}

// A probe step that gives no usable list is not kept: the next probe runs
// it again, and once it gives one, that is kept.
func TestAProbeStepThatFailsIsNotKept(t *testing.T) {
	isolate(t)
	dump := puppetHost(t)
	cli := ownCLI(t, adapter.Codex)
	r, calls := puppetRunner(t, adapter.Codex, Options{CLI: cli})
	for i := 1; i <= 2; i++ {
		caps, err := r.Probe(context.Background())
		if err != nil || !slices.Equal(caps.Features, puppetFallback) || calls.count("features list") != i {
			t.Fatalf("probe %d with no list: %+v %v, %q", i, caps, err, calls.calls)
		}
	}
	put(t, dump, "features", "nothing listed\n")
	if caps, err := r.Probe(context.Background()); err != nil || !slices.Equal(caps.Features, puppetFallback) || calls.count("features list") != 3 {
		t.Fatalf("a list with no feature: %+v %v", caps, err)
	}
	put(t, dump, "features", featureList)
	for range 2 {
		if caps, err := r.Probe(context.Background()); err != nil || !slices.Equal(caps.Features, []string{"alpha", "beta"}) || calls.count("features list") != 4 {
			t.Fatalf("a list: %+v %v, %q", caps, err, calls.calls)
		}
	}
	// What the step prints past its cap is no list.
	cli2 := ownCLI(t, adapter.Codex)
	put(t, dump, "features", strings.Repeat("alpha stable true\n", maxList/18+1))
	r2, calls2 := puppetRunner(t, adapter.Codex, Options{CLI: cli2})
	if caps, err := r2.Probe(context.Background()); err != nil || !slices.Equal(caps.Features, puppetFallback) || calls2.count("features list") != 1 {
		t.Fatalf("a list past the cap: %+v %v", caps, err)
	}
}

// The probe step ends at a limit of its own, and the probe goes on with
// what the backend gives for no list, which is not kept. The test ends that
// limit once the fake lists, so the machine's speed decides nothing.
func TestTheProbeStepEndsAtItsOwnLimit(t *testing.T) {
	isolate(t)
	dump := puppetHost(t)
	put(t, dump, "features", featureList)
	put(t, dump, "features.sh", `: > "$dump/listing"; exec sleep 300`)
	r, calls := puppetRunner(t, adapter.Codex, Options{CLI: ownCLI(t, adapter.Codex)})
	ctx, step := holdProbeStep(context.Background())
	done := launch(t, ctx, r, call(t, adapter.Codex))
	if d := step.next(t); d != probeStepTimeout {
		t.Fatalf("the step's limit is %s, want %s", d, probeStepTimeout)
	}
	if !waitFile(dump, "listing") {
		t.Fatalf("the fake didn't list in %s", hangGuard)
	}
	step.expire()
	out, err := await(t, done, "a run whose probe step hung")
	if err != nil || out.Class != ClassOK {
		t.Fatalf("outcome %+v %v", out, err)
	}
	if err := os.Remove(filepath.Join(dump, "features.sh")); err != nil {
		t.Fatal(err)
	}
	if caps, err := r.Probe(context.Background()); err != nil || !slices.Equal(caps.Features, []string{"alpha", "beta"}) || calls.count("features list") != 2 {
		t.Fatalf("after a step that hung: %+v %v, %q", caps, err, calls.calls)
	}
}

// Without Env.Run the probe step is a sealed run: the cleared environment,
// in a private folder that is gone afterwards. The version is read that way
// too, but under the adapter's own limit, which can't be raised here, so
// this test doesn't look at it.
func TestTheProbeStepWithNoEnvRunIsSealed(t *testing.T) {
	isolate(t)
	dump := puppetHost(t)
	put(t, dump, "features", featureList)
	put(t, dump, "features.sh", `env > "$dump/features.env"; pwd -P > "$dump/features.pwd"`)
	t.Setenv("OPENAI_API_KEY", "fake-openai-value")
	r, err := newRunner(adapter.Codex, puppet(), Options{CLI: ownCLI(t, adapter.Codex)})
	if err != nil {
		t.Fatal(err)
	}
	caps, err := r.Probe(context.Background())
	if err != nil || !slices.Equal(caps.Features, []string{"alpha", "beta"}) {
		t.Fatalf("caps %+v %v", caps, err)
	}
	env := read(t, dump, "features.env")
	if !strings.Contains(env, "EVA_MD_INNER=1\n") || strings.Contains(env, "OPENAI_API_KEY") {
		t.Fatalf("the step's environment was not cleared:\n%s", env)
	}
	pwd := strings.TrimSpace(read(t, dump, "features.pwd"))
	if wd, _ := os.Getwd(); pwd == "/" || pwd == wd {
		t.Fatalf("the step ran in %s", pwd)
	}
	if _, err := os.Stat(pwd); !os.IsNotExist(err) {
		t.Fatalf("the step's folder is left: %v", err)
	}
}

// Probes of one entry that overlap run its step once: the second waits for
// the first's.
func TestOverlappingProbesRunTheStepOnce(t *testing.T) {
	isolate(t)
	dump := puppetHost(t)
	put(t, dump, "features", featureList)
	put(t, dump, "features.sh", `: > "$dump/listing"; while [ ! -f "$dump/go" ]; do sleep 0.05; done`)
	cli := ownCLI(t, adapter.Codex)
	r, calls := puppetRunner(t, adapter.Codex, Options{CLI: cli})
	type probed struct {
		caps Caps
		err  error
	}
	first, second := make(chan probed, 1), make(chan probed, 1)
	// The test learns that the second probe waits for the first's step, and
	// only then lets the step end, so no ordering of the two can hide a
	// probe that doesn't wait.
	waiting := make(chan struct{})
	var once sync.Once
	saved := stepWaits
	t.Cleanup(func() { stepWaits = saved })
	stepWaits = func() { once.Do(func() { close(waiting) }) }
	go func() { c, err := r.Probe(context.Background()); first <- probed{c, err} }()
	if !waitFile(dump, "listing") {
		t.Fatalf("the fake didn't list in %s", hangGuard)
	}
	r2, _ := puppetRunner(t, adapter.Codex, Options{CLI: cli})
	r2.opts.Env.Run = r.opts.Env.Run
	go func() { c, err := r2.Probe(context.Background()); second <- probed{c, err} }()
	select {
	case <-waiting:
	case <-time.After(hangGuard):
		put(t, dump, "go", "")
		t.Fatalf("the second probe didn't wait for the first's step in %s", hangGuard)
	}
	put(t, dump, "go", "")
	for _, ch := range []chan probed{first, second} {
		select {
		case p := <-ch:
			if p.err != nil || !slices.Equal(p.caps.Features, []string{"alpha", "beta"}) {
				t.Fatalf("a probe gave %+v %v", p.caps, p.err)
			}
		case <-time.After(hangGuard):
			t.Fatalf("a probe still runs after %s", hangGuard)
		}
	}
	if n := calls.count("features list"); n != 1 {
		t.Fatalf("the step ran %d times", n)
	}
}

// The probe cache's key holds the CLI's size and the host too: a file
// rewritten to another size, with the same modification time and version,
// runs the step again, and so does another host's probe of the same file.
func TestTheProbeCacheKeysTheSizeAndTheHost(t *testing.T) {
	isolate(t)
	dump := puppetHost(t)
	put(t, dump, "features", featureList)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// A file of its own, not a link to the rig's script, which it changes.
	cli := filepath.Join(dir, cliName(t, adapter.Codex))
	if err := os.WriteFile(cli, []byte(fakeScript), 0o700); err != nil {
		t.Fatal(err)
	}
	r, calls := puppetRunner(t, adapter.Codex, Options{CLI: cli})
	for range 2 {
		if caps, err := r.Probe(context.Background()); err != nil || caps.Version != "0.1.0" || calls.count("features list") != 1 {
			t.Fatalf("an unchanged CLI: %+v %v, %q", caps, err, calls.calls)
		}
	}
	info, err := os.Stat(cli)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cli, []byte(fakeScript+"# a longer file\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(cli, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if again, err := os.Stat(cli); err != nil || !again.ModTime().Equal(info.ModTime()) || again.Size() == info.Size() {
		t.Fatalf("the rewritten file: %v", err)
	}
	if caps, err := r.Probe(context.Background()); err != nil || caps.Version != "0.1.0" || calls.count("features list") != 2 {
		t.Fatalf("a new size: %+v %v, %q", caps, err, calls.calls)
	}
	// Another host's probe of the same file runs the step for its own entry.
	r2, calls2 := puppetRunner(t, adapter.ClaudeCode, Options{Env: adapter.Env{LookPath: func(string) (string, error) { return cli, nil }}})
	for range 2 {
		if caps, err := r2.Probe(context.Background()); err != nil || caps.CLI != cli || calls2.count("features list") != 1 {
			t.Fatalf("another host: %+v %v, %q", caps, err, calls2.calls)
		}
	}
	if _, err := r.Probe(context.Background()); err != nil || calls.count("features list") != 2 {
		t.Fatalf("the first host after another's: %v, %q", err, calls.calls)
	}
}

// Of what a probe step found, only names of a feature's shape reach Caps:
// the host's other text is dropped.
func TestOnlyFeatureNamesReachCaps(t *testing.T) {
	isolate(t)
	dump := puppetHost(t)
	put(t, dump, "features", `planted.marker@example.invalid stable true
Users/planted/client-project stable false
alpha stable true
BETA stable true
-flag stable true
beta.two_3-x stable false
`)
	r, _ := puppetRunner(t, adapter.Codex, Options{CLI: ownCLI(t, adapter.Codex)})
	if caps, err := r.Probe(context.Background()); err != nil || !slices.Equal(caps.Features, []string{"alpha", "beta.two_3-x"}) {
		t.Fatalf("caps %+v %v", caps, err)
	}
}

// Without Env.Run the adapter reads the version through a sealed run too:
// the cleared environment, in a private folder that is gone afterwards. The
// test takes the Env the probe gives the adapter and runs --version through
// it itself, with no limit, since the adapter's own can't be raised here.
func TestTheVersionWithNoEnvRunIsSealed(t *testing.T) {
	isolate(t)
	dump := puppetHost(t)
	put(t, dump, "version.sh", `env > "$dump/version.env"; pwd -P > "$dump/version.pwd"`)
	t.Setenv("OPENAI_API_KEY", "fake-openai-value")
	var given adapter.Env
	saved := probeCLI
	t.Cleanup(func() { probeCLI = saved })
	probeCLI = func(ctx context.Context, id adapter.ID, env adapter.Env) adapter.ProbeResult {
		given = env
		return adapter.ProbeResult{Host: id}
	}
	cli := ownCLI(t, adapter.Codex)
	r, err := newRunner(adapter.Codex, puppet(), Options{CLI: cli})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Probe(context.Background()); err != nil {
		t.Fatal(err)
	}
	if given.Run == nil || given.LookPath == nil {
		t.Fatal("the adapter was given no run or no lookup: it would start the CLI with the whole environment")
	}
	if found, err := given.LookPath(cliName(t, adapter.Codex)); err != nil || found != cli {
		t.Fatalf("the adapter's lookup gave %q %v, want the recorded %q", found, err, cli)
	}
	out, err := given.Run(context.Background(), cli, "--version")
	if err != nil || string(out) != "0.1.0 (fake)\n" {
		t.Fatalf("--version gave %q %v", out, err)
	}
	env := read(t, dump, "version.env")
	if !strings.Contains(env, "EVA_MD_INNER=1\n") || strings.Contains(env, "OPENAI_API_KEY") {
		t.Fatalf("the version's environment was not cleared:\n%s", env)
	}
	pwd := strings.TrimSpace(read(t, dump, "version.pwd"))
	if wd, _ := os.Getwd(); pwd == "/" || pwd == wd {
		t.Fatalf("--version ran in %s", pwd)
	}
	if _, err := os.Stat(pwd); !os.IsNotExist(err) {
		t.Fatalf("the version's folder is left: %v", err)
	}
}

// A probe whose context has ended runs nothing and says so.
func TestAProbeWhoseContextEndedIsAnError(t *testing.T) {
	isolate(t)
	puppetHost(t)
	r, calls := puppetRunner(t, adapter.Codex, Options{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := r.Probe(ctx); err == nil || calls.total() != 0 {
		t.Fatalf("probe: %v, %d commands", err, calls.total())
	}
}
