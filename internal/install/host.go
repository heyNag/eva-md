package install

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/heyNag/eva-md/internal/host/adapter"
	"github.com/heyNag/eva-md/internal/paths"
)

// Status is derived from native host inventories, never host settings files.
type Status struct {
	Host        string `json:"host"`
	Available   bool   `json:"available"`
	Installed   bool   `json:"installed"`
	Enabled     bool   `json:"enabled"`
	Valid       bool   `json:"valid"`
	Marketplace bool   `json:"marketplace"`
	// FolderMissing says Codex has eva-md's marketplace registered at
	// eva-md's own folder, which has no manifest, as after that folder was
	// deleted; Codex then fails its marketplace list and every plugin list
	// that takes eva-md's in, so whether it has the plugin is unknown.
	FolderMissing bool   `json:"folder_missing,omitempty"`
	Reason        string `json:"reason,omitempty"`
}

type runner func(context.Context, string, ...string) ([]byte, error)

// hostError is a failed host command. Its diagnostics, which can name paths
// or credentials, are only matched against a wording the host is known to
// print, and never shown.
type hostError struct {
	msg         string
	diagnostics string
}

func (e *hostError) Error() string { return e.msg }

// folderMissing says whether a host command failed only because the
// marketplace the host has registered as eva-md, at path, has no manifest,
// in the words the adapter reads (MarketplaceFolderMissing): Codex names the
// folder as it was when it was added, its symlinks resolved. A failure that
// names any other marketplace, or no marketplace, is not this. Only a host
// whose Plugin has MissingFolderFails is asked.
func folderMissing(err error, path string) bool {
	var e *hostError
	if !errors.As(err, &e) {
		return false
	}
	return adapter.MarketplaceFolderMissing(e.diagnostics, path, realPath(path))
}

// realPath is path with the symlinks of its deepest folder that exists
// resolved, since the rest of it may be gone.
func realPath(path string) string {
	dir, rest := filepath.Clean(path), ""
	for {
		if real, err := filepath.EvalSymlinks(dir); err == nil {
			return filepath.Join(real, rest)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return path
		}
		rest = filepath.Join(filepath.Base(dir), rest)
		dir = parent
	}
}

// ErrConflict marks Inspect's errors about another eva-md registration on
// the host, apart from a host command or its output that failed.
var ErrConflict = errors.New("install: conflicting eva-md registration")

// conflict is an error about another eva-md registration, worded for the
// person who reads it.
type conflict string

func (c conflict) Error() string        { return string(c) }
func (c conflict) Is(target error) bool { return target == ErrConflict }

// Inspect checks exactly eva-md's registration through read-only host commands.
func Inspect(ctx context.Context, dirs paths.Dirs, host string) (Status, error) {
	return inspect(ctx, dirs, host, native)
}

// HostTimeout bounds one host command, and HostWaitDelay the wait for its
// output once it has exited or run out of time. Codex gives a request to
// chatgpt.com 30 seconds before it goes on without the answer, so a host
// command gets twice that. Tests raise them before any test runs.
var (
	HostTimeout   = 60 * time.Second
	HostWaitDelay = time.Second
)

// native runs a host's CLI by its command, the adapter's Names.CLI.
func native(ctx context.Context, cli string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, HostTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, cli, args...)
	// From /, no project's own settings take part. Claude Code reads the
	// folder it runs in as a project, and from the home folder its project
	// settings are the user settings file, so a marketplace removed from
	// user settings stays listed there as another scope's.
	cmd.Dir = "/"
	cmd.WaitDelay = HostWaitDelay
	var stdout, stderr limitedBuffer
	cmd.Stdout = &stdout
	// Native commands can include paths or credentials in diagnostics. Keep
	// errors generic; the user can run the named command for host details.
	// The diagnostics are only matched (see folderMissing).
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, &hostError{msg: fmt.Sprintf("install: %s failed", commandDescription(cli, args)), diagnostics: stderr.String()}
	}
	if stdout.overflow {
		return nil, errors.New("install: host inventory exceeded limit")
	}
	return stdout.buffer.Bytes(), nil
}

// hostOutputLimit is how much of what a host command prints, on stdout and
// on stderr each, eva-md keeps.
const hostOutputLimit = 2 << 20

// limitedBuffer keeps the first hostOutputLimit bytes a host command prints
// and drops the rest, noting that it did. The buffer is a named field, not
// an embedded bytes.Buffer, whose ReadFrom os/exec would call in place of
// Write, so the limit would never hold.
type limitedBuffer struct {
	buffer   bytes.Buffer
	overflow bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if room := hostOutputLimit - b.buffer.Len(); n > room {
		p, b.overflow = p[:max(room, 0)], true
	}
	_, _ = b.buffer.Write(p)
	return n, nil
}

func (b *limitedBuffer) String() string { return b.buffer.String() }

// hostFor is the adapter's host of an install name its caller has checked,
// or the zero Host for a name eva-md doesn't know.
func hostFor(host string) adapter.Host {
	h, _ := adapter.ByInstallName(host)
	return h
}

// command is the command that runs the host of an install name.
func command(host string) string { return hostFor(host).Names.CLI }

func inspect(ctx context.Context, dirs paths.Dirs, host string, run runner) (Status, error) {
	s := Status{Host: host}
	h, ok := adapter.ByInstallName(host)
	if !ok {
		return s, errors.New("install: host must be claude or codex")
	}
	if h.FindCLI(nil) == "" {
		s.Reason = "host CLI not found"
		return s, nil
	}
	s.Available = true
	plugin := h.Plugin()
	data, err := run(ctx, h.Names.CLI, "plugin", "marketplace", "list", "--json")
	if err != nil {
		// Install stops there, and uninstall removes the registration.
		if plugin.MissingFolderFails && folderMissing(err, marketplacePath(dirs.Share, host)) {
			s.Marketplace, s.FolderMissing = true, true
			s.Reason = "eva-md's marketplace folder is missing"
			return s, nil
		}
		return s, err
	}
	markets, err := objects(data, "marketplaces")
	if err != nil {
		return s, err
	}
	list, err := installed(ctx, h, markets, run)
	if err != nil {
		return s, err
	}
	count := 0
	for _, p := range list {
		id := str(p, "pluginId", "id")
		name := str(p, "name")
		if name == "" {
			name = strings.Split(id, "@")[0]
		}
		if strings.Split(name, "@")[0] != "eva-md" {
			continue
		}
		count++
		if id == "" {
			id = name
		}
		market := str(p, "marketplaceName", "marketplace")
		if id != "eva-md@eva-md" && !(id == "eva-md" && market == "eva-md") {
			return s, conflict("install: another eva-md plugin copy is registered; remove that copy through its host first")
		}
		s.Installed = true
		s.Enabled, _ = p["enabled"].(bool)
		s.Valid = s.Enabled
		if errs, ok := p["errors"].([]any); ok && len(errs) > 0 {
			s.Valid = false
			s.Reason = "host reports plugin errors"
		}
		if !plugin.LoadsFrom(p, pluginPath(dirs.Share, host)) {
			s.Valid = false
			s.Reason = plugin.SourceDiffers
		}
	}
	if count > 1 {
		return s, conflict("install: multiple eva-md plugin copies are registered")
	}
	for _, m := range markets {
		if str(m, "name") != "eva-md" {
			continue
		}
		if s.Marketplace {
			return s, conflict("install: duplicate eva-md marketplaces")
		}
		s.Marketplace = true
		root := adapter.MarketplaceRoot(m)
		if filepath.Clean(root) != marketplacePath(dirs.Share, host) {
			return s, conflict("install: eva-md marketplace name belongs to another folder")
		}
	}
	if s.Installed && !s.Marketplace {
		s.Valid = false
		s.Reason = "eva-md marketplace registration is missing"
	}
	if s.Installed && !s.Enabled {
		s.Reason = "eva-md plugin is disabled"
	}
	if !s.Installed {
		s.Reason = "eva-md plugin is not installed"
	}
	return s, nil
}

// installed lists the plugins the host has installed. A host whose Plugin
// has ListPerMarketplace, as Codex, is asked one of its marketplaces at a
// time, by name, which it answers from this Mac: Codex's list of every
// plugin also asks chatgpt.com for the account's remote ones, and waits 30
// seconds on a network that holds the request. A plugin from Codex's remote
// catalog is not among them. Each marketplace is asked once.
func installed(ctx context.Context, h adapter.Host, markets []map[string]any, run runner) ([]map[string]any, error) {
	if !h.Plugin().ListPerMarketplace {
		data, err := run(ctx, h.Names.CLI, "plugin", "list", "--json")
		if err != nil {
			return nil, err
		}
		return objects(data, "installed")
	}
	var list []map[string]any
	asked := map[string]bool{}
	for _, m := range markets {
		name := str(m, "name")
		if name == "" || asked[name] {
			continue
		}
		asked[name] = true
		data, err := run(ctx, h.Names.CLI, "plugin", "list", "--json", "--marketplace="+name)
		if err != nil {
			return nil, err
		}
		plugins, err := objects(data, "installed")
		if err != nil {
			return nil, err
		}
		list = append(list, plugins...)
	}
	return list, nil
}

func objects(data []byte, key string) ([]map[string]any, error) {
	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, errors.New("install: invalid host inventory JSON")
	}
	if obj, ok := raw.(map[string]any); ok {
		raw = obj[key]
	}
	list, ok := raw.([]any)
	if !ok {
		return nil, errors.New("install: unsupported host inventory shape")
	}
	result := make([]map[string]any, 0, len(list))
	for _, item := range list {
		obj, ok := item.(map[string]any)
		if !ok {
			return nil, errors.New("install: invalid host inventory item")
		}
		result = append(result, obj)
	}
	return result, nil
}
func str(obj map[string]any, keys ...string) string {
	for _, key := range keys {
		if s, ok := obj[key].(string); ok && s != "" {
			return s
		}
	}
	return ""
}
