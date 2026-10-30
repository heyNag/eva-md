// Package config reads and writes eva-md's per-Mac config file.
package config

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"github.com/BurntSushi/toml"

	"github.com/heyNag/eva-md/internal/identity"
	"github.com/heyNag/eva-md/internal/paths"
)

// Notice modes.
const (
	NoticeStart    = "start"    // a one-line start notice on Claude, plus problem notices
	NoticeProblems = "problems" // notices only when something is wrong
)

// EffortHighest asks for the highest effort the chat's model supports.
const EffortHighest = "highest"

// DefaultDailyCap is the default number of background update runs per host
// per day. Background updates are planned; for now the cap is only stored.
const DefaultDailyCap = 40

// RemotePrefix marks an Exclude entry that matches the normalized remote.
const RemotePrefix = "remote:"

// foldCase makes path matching ignore case, as the default macOS file system does.
var foldCase = runtime.GOOS == "darwin"

// Config is the per-machine config at ~/.config/eva-md/config.toml. DailyCap,
// Effort and CodexIgnoreUserConfig configure the planned background update;
// they are validated and saved, but nothing uses them yet.
type Config struct {
	WikiPath       string `toml:"wiki_path"`
	ExpectedRemote string `toml:"expected_remote"`
	Instance       string `toml:"instance"`
	MachineLabel   string `toml:"machine_label"`
	// Exclude lists folders and remotes whose chats are never captured. An
	// entry starting with "remote:" is a glob (path.Match) against the
	// normalized remote, for example "remote:github.com/acme/*". Any other
	// entry is an absolute path glob (filepath.Match), where a leading "~/"
	// stands for the home folder. An entry matches when it matches the path or
	// remote or any of its parents, so an entry without glob characters covers
	// that folder and everything below it. Remotes are compared in lower case,
	// and on macOS paths are too.
	Exclude               []string          `toml:"exclude"`
	Notice                string            `toml:"notice"`                   // "start" (default) or "problems"
	DailyCap              int               `toml:"daily_cap"`                // default 40
	Effort                map[string]string `toml:"effort"`                   // keys "claude-code", "codex" or "<host>/<model>"; value an effort level or "highest"
	CodexIgnoreUserConfig bool              `toml:"codex_ignore_user_config"` // default false
	// Unknown lists keys in the file that eva-md does not know, such as a
	// misspelled one. They are reported, not saved.
	Unknown []string `toml:"-"`
}

// Default returns the config used when no file exists.
func Default() Config {
	return Config{Notice: NoticeStart, DailyCap: DefaultDailyCap}
}

// Load reads the config file. A missing file gives Default() and an error
// that satisfies errors.Is(err, fs.ErrNotExist). Any other error means the
// file exists but can't be used; Default() is returned with it, and callers
// that rely on Exclude must fail closed.
func Load(file string) (Config, error) {
	return LoadContext(context.Background(), file)
}

// LoadContext is Load with cancellation checks and bounded, regular-file reads.
func LoadContext(ctx context.Context, file string) (Config, error) {
	data, err := readTOML(ctx, file)
	if err != nil {
		return Default(), fmt.Errorf("config: %w", err)
	}
	// Decoding into Default() keeps the defaults of absent keys, so an
	// explicit daily_cap = 0 stays 0.
	c := Default()
	md, err := toml.Decode(string(data), &c)
	if err != nil {
		return Default(), errors.New("config: invalid TOML")
	}
	for _, key := range md.Undecoded() {
		c.Unknown = append(c.Unknown, key.String())
	}
	if c.Notice == "" {
		c.Notice = NoticeStart
	}
	if err := c.Validate(); err != nil {
		return Default(), fmt.Errorf("config: %s: %w", file, err)
	}
	return c, nil
}

// Save writes c to file atomically with mode 0600, creating its folder.
func Save(file string, c Config) error {
	if err := c.Validate(); err != nil {
		return fmt.Errorf("config: %w", err)
	}
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(c); err != nil {
		return fmt.Errorf("config: encode: %w", err)
	}
	if err := paths.EnsureDir(filepath.Dir(file)); err != nil {
		return err
	}
	return paths.WriteFileAtomic(file, buf.Bytes(), 0o600)
}

// Validate reports the first problem in c.
func (c Config) Validate() error {
	switch c.Notice {
	case NoticeStart, NoticeProblems:
	default:
		return fmt.Errorf("notice must be %q or %q", NoticeStart, NoticeProblems)
	}
	if c.DailyCap < 0 {
		return fmt.Errorf("daily_cap %d must not be negative", c.DailyCap)
	}
	for _, e := range c.Exclude {
		if err := checkExclude(e); err != nil {
			return err
		}
	}
	for k, v := range c.Effort {
		if strings.TrimSpace(k) == "" || strings.TrimSpace(v) == "" {
			return errors.New("effort entry must have a key and a value")
		}
	}
	return nil
}

func checkExclude(e string) error {
	e = strings.TrimSpace(e)
	if pat, ok := strings.CutPrefix(e, RemotePrefix); ok {
		pat = remotePattern(pat)
		if pat == "" {
			return errors.New("exclude entry has an empty remote pattern")
		}
		if strings.Contains(pat, "://") {
			return errors.New("exclude entry has a remote URL that can't be normalized; write it as host/owner/repo")
		}
		if _, err := path.Match(pat, ""); err != nil {
			return errors.New("exclude entry has an invalid remote glob")
		}
		return nil
	}
	if e != "~" && !strings.HasPrefix(e, "~/") && !filepath.IsAbs(e) {
		return fmt.Errorf("exclude entry must be an absolute path, start with ~/, or start with %q", RemotePrefix)
	}
	if _, err := filepath.Match(e, ""); err != nil {
		return errors.New("exclude entry has an invalid path glob")
	}
	return nil
}

// EffortFor returns the effort for a background update (planned): the
// "<host>/<model>" entry, then the "<host>" entry, then "highest".
func (c Config) EffortFor(host, model string) string {
	if model != "" {
		if v := strings.TrimSpace(c.Effort[host+"/"+model]); v != "" {
			return v
		}
	}
	if v := strings.TrimSpace(c.Effort[host]); v != "" {
		return v
	}
	return EffortHighest
}

// Excluded reports whether a chat in folder dir, in a repo with the given
// normalized remote (empty without one), must not be captured. See Exclude.
func (c Config) Excluded(dir, remote string) bool {
	if len(c.Exclude) == 0 {
		return false
	}
	home, _ := os.UserHomeDir()
	remote = strings.ToLower(strings.Trim(remote, "/"))
	for _, e := range c.Exclude {
		e = strings.TrimSpace(e)
		if pat, ok := strings.CutPrefix(e, RemotePrefix); ok {
			pat = remotePattern(pat)
			if remote != "" && matchUp(pat, remote, path.Match, path.Dir) {
				return true
			}
			continue
		}
		if dir == "" {
			continue
		}
		pat := e
		if pat == "~" || strings.HasPrefix(pat, "~/") {
			if home == "" {
				continue
			}
			pat = filepath.Join(home, strings.TrimPrefix(pat, "~"))
		}
		if !filepath.IsAbs(pat) {
			continue
		}
		patterns := []string{filepath.Clean(pat), canonicalPattern(pat)}
		targets := []string{filepath.Clean(dir), canonicalPattern(dir)}
		for _, pattern := range patterns {
			for _, target := range targets {
				if foldCase {
					pattern, target = strings.ToLower(pattern), strings.ToLower(target)
				}
				if matchUp(pattern, target, filepath.Match, filepath.Dir) {
					return true
				}
			}
		}
	}
	return false
}

// remotePattern normalizes a remote exclusion the way remotes are normalized,
// so "remote: git@github.com:acme/*.git" becomes "github.com/acme/*". A URL
// with "?" or "#" is left as it is, since a URL parser would drop the glob.
func remotePattern(pat string) string {
	pat = strings.TrimSpace(pat)
	if id, ok := identity.NormalizeRemote(pat); ok && !(strings.Contains(pat, "://") && strings.ContainsAny(pat, "?#")) {
		return id
	}
	pat = strings.Trim(strings.ToLower(pat), "/")
	return strings.Trim(strings.TrimSuffix(pat, ".git"), "/")
}

// canonicalPattern resolves an existing literal prefix, so entering an
// excluded directory through a symlink cannot bypass its exclusion.
func canonicalPattern(pattern string) string {
	pattern = filepath.Clean(pattern)
	prefix := pattern
	if i := strings.IndexAny(prefix, "*?["); i >= 0 {
		prefix = filepath.Dir(prefix[:i])
	}
	for {
		if real, err := filepath.EvalSymlinks(prefix); err == nil {
			suffix, err := filepath.Rel(prefix, pattern)
			if err == nil {
				return filepath.Join(real, suffix)
			}
		}
		parent := filepath.Dir(prefix)
		if parent == prefix || parent == "." {
			return pattern
		}
		prefix = parent
	}
}

// WikiSchema is the metadata stored in a wiki's .eva-md/schema.toml file.
type WikiSchema struct {
	SchemaVersion int    `toml:"schema_version"`
	OKFSpec       string `toml:"okf_spec"`
	OKFModule     string `toml:"okf_module"`
	Instance      string `toml:"instance"`
	Created       string `toml:"created"`
}

// LoadSchema reads wiki metadata; its caller validates supported versions.
func LoadSchema(file string) (WikiSchema, error) {
	data, err := readTOML(context.Background(), file)
	if err != nil {
		return WikiSchema{}, fmt.Errorf("config: %w", err)
	}
	var schema WikiSchema
	if _, err := toml.Decode(string(data), &schema); err != nil {
		return WikiSchema{}, errors.New("config: invalid wiki schema TOML")
	}
	return schema, nil
}

// SaveSchema writes wiki metadata atomically with mode 0600.
func SaveSchema(file string, schema WikiSchema) error {
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(schema); err != nil {
		return errors.New("config: encode wiki schema")
	}
	if err := paths.EnsureDir(filepath.Dir(file)); err != nil {
		return err
	}
	return paths.WriteFileAtomic(file, buf.Bytes(), 0o600)
}

func readTOML(ctx context.Context, file string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// The errors name the file once; callers add the "config: " prefix.
	f, err := os.OpenFile(file, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", file)
	}
	const maxBytes = 1 << 20
	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxBytes {
		return nil, fmt.Errorf("%s exceeds 1 MiB", file)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return data, nil
}

// matchUp reports whether pattern matches target or any of its parents.
func matchUp(pattern, target string, match func(string, string) (bool, error), dir func(string) string) bool {
	for {
		if ok, err := match(pattern, target); err == nil && ok {
			return true
		}
		parent := dir(target)
		if parent == target || parent == "." || parent == "" {
			return false
		}
		target = parent
	}
}
