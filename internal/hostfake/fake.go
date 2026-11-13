package hostfake

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// The CLIs the fake answers for.
const (
	Claude = "claude"
	Codex  = "codex"
)

// The versions a new fake reports.
const (
	DefaultCodexVersion  = "0.167.0"
	DefaultClaudeVersion = "2.1.321"
)

// contentBoundSince is the first Codex version that binds a plugin hook's
// approval to the plugin's contents as well as its declaration.
const contentBoundSince = "0.164.0"

// Fake is the fake Claude Code and Codex CLIs. Its state is a file under its
// root, so the CLIs WriteCLIs writes, which run in processes of their own,
// share it with the test that made it.
type Fake struct {
	root string
	// onStall, when set, is called once a command has started to stall.
	// Only this package's tests set it.
	onStall func()
}

// Policy is what an administrator has turned off. Nothing the fake answers
// lifts it. Each flag's one effect is in the package documentation.
type Policy struct {
	// CodexHooksOff is Codex's [features].hooks = false in requirements.toml.
	CodexHooksOff bool
	// CodexPluginsOff is Codex's features.plugins = false.
	CodexPluginsOff bool
	// CodexManagedHooksOnly is Codex's allow_managed_hooks_only = true.
	CodexManagedHooksOnly bool
	// ClaudeManagedHooksOnly is Claude Code's allowManagedHooksOnly.
	ClaudeManagedHooksOnly bool
	// BlockedMarketplace, when not "", is the name of a marketplace policy
	// blocks on this CLI: Claude Code's blockedMarketplaces, or a Codex
	// marketplace outside its allowed sources.
	BlockedMarketplace string
}

// When says when a command made to fail fails.
type When int

const (
	// Before fails the command before it takes effect.
	Before When = iota + 1
	// After fails the command once it has taken effect.
	After
	// Stall holds the command until its context ends. A written CLI is held
	// until it is killed.
	Stall
)

// Hook is one handler a hooks file, or an administrator, declares.
type Hook struct {
	Event   string
	Matcher string
	// Command is a shell command, or with Args, a program run with them,
	// as Claude Code's exec form.
	Command       string
	Args          []string
	Timeout       int
	Async         bool
	StatusMessage string
	// ContextLimit is additionalContextLimit; 0 is the default.
	ContextLimit int
}

// Call is one command a fake CLI was given.
type Call struct {
	CLI  string
	Args []string
	// Refused says the fake refused it, as it refuses
	// --dangerously-bypass-hook-trust.
	Refused bool
}

func (c Call) String() string { return strings.Join(append([]string{c.CLI}, c.Args...), " ") }

type state struct {
	Versions map[string]string      `json:"versions,omitempty"`
	Policies map[string]Policy      `json:"policies,omitempty"`
	Markets  map[string][]market    `json:"markets,omitempty"`
	Plugins  map[string][]plugin    `json:"plugins,omitempty"`
	Trust    map[string]trustRecord `json:"trust,omitempty"`
	Managed  map[string][]Hook      `json:"managed,omitempty"`
	Faults   []fault                `json:"faults,omitempty"`
	Calls    []Call                 `json:"calls,omitempty"`
}

// market is a registered marketplace: its name, its folder as it was given,
// and that folder with its symlinks resolved when it was added.
type market struct {
	Name, Path, Real string
}

// plugin is an installed plugin: Source is the folder it was installed
// from, in its marketplace.
type plugin struct {
	Name, Market, Source string
}

func (p plugin) id() string { return p.Name + "@" + p.Market }

// trustRecord is what an approval stored for one hook key.
type trustRecord struct {
	Decl, Content string
}

type fault struct {
	Prefix string
	When   When
}

// New is a fake whose state is kept under root, which it makes when it
// first needs it. A relative root is made absolute.
func New(root string) *Fake {
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	return &Fake{root: root}
}

// Root is the folder the fake keeps its state under.
func (f *Fake) Root() string { return f.root }

// CachePath is the folder Codex loads an installed plugin from: its copy,
// <root>/codex/plugins/cache/<marketplace>/<plugin>/local.
func (f *Fake) CachePath(marketplace, plugin string) string {
	return filepath.Join(f.root, "codex", "plugins", "cache", marketplace, plugin, "local")
}

// dataPath is a plugin's data folder, outside its root.
func (f *Fake) dataPath(cli string, p plugin) string {
	return filepath.Join(f.root, cli, "plugins", "data", p.Name+"-"+p.Market)
}

// withState runs fn on the state under the fake's lock, and saves the state
// when save is true and fn succeeds.
func (f *Fake) withState(save bool, fn func(*state) error) error {
	if err := os.MkdirAll(f.root, 0o700); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(f.root, "lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	st := &state{}
	data, err := os.ReadFile(filepath.Join(f.root, "state.json"))
	if err == nil {
		if err := json.Unmarshal(data, st); err != nil {
			return fmt.Errorf("hostfake: read state: %w", err)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	st.init()
	if err := fn(st); err != nil {
		return err
	}
	if !save {
		return nil
	}
	data, err = json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(f.root, "state.json.tmp")
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(f.root, "state.json"))
}

func (st *state) init() {
	if st.Versions == nil {
		st.Versions = map[string]string{}
	}
	if st.Policies == nil {
		st.Policies = map[string]Policy{}
	}
	if st.Markets == nil {
		st.Markets = map[string][]market{}
	}
	if st.Plugins == nil {
		st.Plugins = map[string][]plugin{}
	}
	if st.Trust == nil {
		st.Trust = map[string]trustRecord{}
	}
	if st.Managed == nil {
		st.Managed = map[string][]Hook{}
	}
}

func (st *state) version(cli string) string {
	if v := st.Versions[cli]; v != "" {
		return v
	}
	if cli == Codex {
		return DefaultCodexVersion
	}
	return DefaultClaudeVersion
}

func (st *state) market(cli, name string) (market, bool) {
	for _, m := range st.Markets[cli] {
		if m.Name == name {
			return m, true
		}
	}
	return market{}, false
}

func (st *state) plugin(cli, id string) (plugin, bool) {
	for _, p := range st.Plugins[cli] {
		if p.id() == id {
			return p, true
		}
	}
	return plugin{}, false
}

// checkCLI refuses a CLI the fake doesn't answer for.
func checkCLI(cli string) error {
	if cli != Claude && cli != Codex {
		return fmt.Errorf("hostfake: no CLI %q; the fake answers for %s and %s", cli, Claude, Codex)
	}
	return nil
}

// SetVersion sets the version cli reports, N.N.N. For Codex it also sets
// the trust rule: before 0.164.0 a plugin hook's approval is bound to its
// declaration only.
func (f *Fake) SetVersion(cli, version string) error {
	if err := checkCLI(cli); err != nil {
		return err
	}
	if _, ok := parseVersion(version); !ok {
		return fmt.Errorf("hostfake: version %q is not N.N.N", version)
	}
	return f.withState(true, func(st *state) error {
		st.Versions[cli] = version
		return nil
	})
}

// SetPolicy sets what an administrator has turned off on cli, in place of
// what was set before. A flag of the other host's is refused.
func (f *Fake) SetPolicy(cli string, p Policy) error {
	if err := checkCLI(cli); err != nil {
		return err
	}
	if cli == Claude && (p.CodexHooksOff || p.CodexPluginsOff || p.CodexManagedHooksOnly) {
		return errors.New("hostfake: a Codex policy flag was set on claude")
	}
	if cli == Codex && p.ClaudeManagedHooksOnly {
		return errors.New("hostfake: a Claude Code policy flag was set on codex")
	}
	return f.withState(true, func(st *state) error {
		st.Policies[cli] = p
		return nil
	})
}

// DeclareManaged adds a hook an administrator declares on cli. Managed hooks
// run without review, and hooks/list shows them as managed.
func (f *Fake) DeclareManaged(cli string, h Hook) error {
	if err := checkCLI(cli); err != nil {
		return err
	}
	if h.Event == "" || h.Command == "" {
		return errors.New("hostfake: a managed hook needs an event and a command")
	}
	return f.withState(true, func(st *state) error {
		st.Managed[cli] = append(st.Managed[cli], h)
		return nil
	})
}

// FailNext makes the next command that starts with command, as the CLI's
// name and its arguments joined by spaces ("codex plugin add"), fail, once,
// at when.
func (f *Fake) FailNext(command string, when When) error {
	if when != Before && when != After && when != Stall {
		return errors.New("hostfake: FailNext needs Before, After or Stall")
	}
	return f.withState(true, func(st *state) error {
		st.Faults = append(st.Faults, fault{Prefix: command, When: when})
		return nil
	})
}

// Calls gives every command the fake CLIs were given, in order, those Run
// answered and those the written CLIs answered alike.
func (f *Fake) Calls() ([]Call, error) {
	var calls []Call
	err := f.withState(false, func(st *state) error {
		calls = st.Calls
		return nil
	})
	return calls, err
}
