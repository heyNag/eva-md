package install

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

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
	Reason      string `json:"reason,omitempty"`
}

type runner func(context.Context, string, ...string) ([]byte, error)

// Inspect checks exactly eva-md's registration through read-only host commands.
func Inspect(ctx context.Context, dirs paths.Dirs, host string) (Status, error) {
	return inspect(ctx, dirs, host, native)
}

func native(ctx context.Context, host string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, host, args...)
	cmd.WaitDelay = time.Second
	var stdout limitedBuffer
	cmd.Stdout = &stdout
	// Native commands can include paths or credentials in diagnostics. Keep
	// errors generic; the user can run the named command for host details.
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("install: %s failed", commandDescription(host, args))
	}
	if stdout.overflow {
		return nil, errors.New("install: host inventory exceeded limit")
	}
	return stdout.Bytes(), nil
}

type limitedBuffer struct {
	bytes.Buffer
	overflow bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if b.Len()+n > 2<<20 {
		b.overflow = true
		return n, nil
	}
	_, _ = b.Buffer.Write(p)
	return n, nil
}

func inspect(ctx context.Context, dirs paths.Dirs, host string, run runner) (Status, error) {
	s := Status{Host: host}
	if host != "claude" && host != "codex" {
		return s, errors.New("install: host must be claude or codex")
	}
	if _, err := exec.LookPath(host); err != nil {
		s.Reason = "host CLI not found"
		return s, nil
	}
	s.Available = true
	data, err := run(ctx, host, "plugin", "list", "--json")
	if err != nil {
		return s, err
	}
	list, err := objects(data, "installed")
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
			return s, errors.New("install: another eva-md plugin copy is registered; remove that copy through its host first")
		}
		s.Installed = true
		s.Enabled, _ = p["enabled"].(bool)
		s.Valid = s.Enabled
		if errs, ok := p["errors"].([]any); ok && len(errs) > 0 {
			s.Valid = false
			s.Reason = "host reports plugin errors"
		}
		if host == "claude" {
			if str(p, "readFromFolder") != pluginPath(dirs.Share, host) || str(p, "folderVersion") != "1" {
				s.Valid = false
				s.Reason = "Claude source folder or folder version differs"
			}
		} else {
			source, _ := p["source"].(map[string]any)
			if str(source, "path") != pluginPath(dirs.Share, host) {
				s.Valid = false
				s.Reason = "Codex source folder differs"
			}
		}
	}
	if count > 1 {
		return s, errors.New("install: multiple eva-md plugin copies are registered")
	}
	data, err = run(ctx, host, "plugin", "marketplace", "list", "--json")
	if err != nil {
		return s, err
	}
	markets, err := objects(data, "marketplaces")
	if err != nil {
		return s, err
	}
	for _, m := range markets {
		if str(m, "name") != "eva-md" {
			continue
		}
		if s.Marketplace {
			return s, errors.New("install: duplicate eva-md marketplaces")
		}
		s.Marketplace = true
		root := str(m, "root", "path", "installLocation")
		if root == "" {
			source, _ := m["marketplaceSource"].(map[string]any)
			root = str(source, "source")
		}
		if root == "" {
			source, _ := m["source"].(map[string]any)
			root = str(source, "path", "source")
		}
		if filepath.Clean(root) != marketplacePath(dirs.Share, host) {
			return s, errors.New("install: eva-md marketplace name belongs to another folder")
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
