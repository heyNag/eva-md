package install

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/host/adapter"
	"github.com/heyNag/eva-md/internal/paths"
)

// ManifestSwitch decides only whether install starts an install manifest
// where none exists. A manifest on disk is read and kept current whatever
// it says, so a release can be rolled back over a later one's layout; off,
// with no manifest, install, rollback and uninstall do what v1 does.
var ManifestSwitch = config.NewBoolSwitch("install_manifest", false)

// manifestFile, in the data folder, is the install manifest: for each unit,
// the files eva-md rendered in its folder, so that any release can tell
// another release's files from foreign ones (OPS-07).
const manifestFile = "install-manifest.json"

const (
	manifestFormat = 1
	manifestLimit  = 1 << 20
	pluginID       = "eva-md@eva-md"
)

// unitKinds name each host's unit by what it holds: the Codex unit is the
// hooks unit whose bytes Codex approves (generation 0, the plugin as v1
// renders it), and the Claude Code unit is its whole plugin.
var unitKinds = map[adapter.ID]string{adapter.ClaudeCode: "plugin", adapter.Codex: "hooks"}

// v1Paths are the files each folder holds as releases 2026.11.5 to
// 2026.11.7 render it, spelled out rather than taken from render: with no
// manifest entry for a unit, they are its list, so a v1 folder is adopted.
var v1Paths = map[adapter.ID][]string{
	adapter.ClaudeCode: {
		".claude-plugin/marketplace.json",
		".eva-md-managed",
		"plugins/eva-md/.claude-plugin/plugin.json",
		"plugins/eva-md/.mcp.json",
		"plugins/eva-md/assets/icon.png",
		"plugins/eva-md/hooks/hooks.json",
		"plugins/eva-md/skills/eva-md-digest/SKILL.md",
		"plugins/eva-md/skills/eva-md-recall/SKILL.md",
	},
	adapter.Codex: {
		".agents/plugins/marketplace.json",
		".eva-md-managed",
		"plugins/eva-md/.codex-plugin/plugin.json",
		"plugins/eva-md/.mcp.json",
		"plugins/eva-md/assets/icon.png",
		"plugins/eva-md/hooks/hooks.json",
		"plugins/eva-md/skills/eva-md-digest/SKILL.md",
		"plugins/eva-md/skills/eva-md-recall/SKILL.md",
	},
}

// unitSpec is a unit this binary renders: a host's marketplace folder.
type unitSpec struct {
	id, host, root string
	hostID         adapter.ID
	hooks          bool
	order          int
}

// unitSpecs are the units this binary renders, in the adapter's order.
func unitSpecs() []unitSpec {
	var specs []unitSpec
	for i, h := range adapter.All() {
		kind := unitKinds[h.ID]
		if kind == "" {
			kind = "plugin"
		}
		specs = append(specs, unitSpec{
			id: string(h.ID) + "/" + kind, host: string(h.ID), hostID: h.ID,
			root: path.Join("marketplace", h.Names.InstallName), hooks: kind == "hooks", order: i,
		})
	}
	return specs
}

// unitOf is the unit of an install name its caller has checked.
func unitOf(installName string) unitSpec {
	root := path.Join("marketplace", installName)
	for _, s := range unitSpecs() {
		if s.root == root {
			return s
		}
	}
	return unitSpec{root: root}
}

func knownUnit(id string) (unitSpec, bool) {
	for _, s := range unitSpecs() {
		if s.id == id {
			return s, true
		}
	}
	return unitSpec{}, false
}

// fileEntry is one file of a unit: its slash-separated path under the
// unit's root, and the lower-case hex SHA-256 and size of its bytes.
type fileEntry struct {
	Path   string
	SHA256 string
	Size   int64
	extra  map[string]json.RawMessage
}

func entryOf(name string, data []byte) fileEntry {
	sum := sha256.Sum256(data)
	return fileEntry{Path: name, SHA256: hex.EncodeToString(sum[:]), Size: int64(len(data))}
}

// manifestUnit is one unit of the manifest. A unit this binary does not
// render keeps raw, and is written back as it was; its root and paths are
// still read, since its files may sit in a folder this binary renders.
type manifestUnit struct {
	ID, Host, Plugin, Root string
	Generation             *int
	Files                  []fileEntry
	extra                  map[string]json.RawMessage
	raw                    json.RawMessage
}

// manifest is the install manifest. Fields this binary does not know are
// kept in extra, at each level, and written back as they were.
type manifest struct {
	Units []manifestUnit
	extra map[string]json.RawMessage
}

// manifestError is a manifest install and uninstall can't read, in one line
// that names the file.
func manifestError(root *os.Root, why string) error {
	return fmt.Errorf("install: %s can't be read: %s; install the release that wrote it, or move it aside", filepath.Join(root.Name(), manifestFile), why)
}

// readManifest reads the manifest in the data folder, with its bytes; a
// manifest that isn't there gives nil. One that can't be read, isn't a
// regular file, is too large or breaks a rule of parseManifest is an error.
func readManifest(root *os.Root) (*manifest, []byte, error) {
	if root == nil {
		return nil, nil, nil
	}
	data, err := readAt(root, manifestFile, manifestLimit)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, manifestError(root, strings.TrimPrefix(err.Error(), "install: "))
	}
	m, err := parseManifest(data)
	if err != nil {
		return nil, nil, manifestError(root, err.Error())
	}
	return m, data, nil
}

// take moves key's value out of obj into v; a key that is missing leaves v
// as it is.
func take(obj map[string]json.RawMessage, key string, v any) (bool, error) {
	raw, ok := obj[key]
	if !ok {
		return false, nil
	}
	delete(obj, key)
	if err := json.Unmarshal(raw, v); err != nil {
		return true, fmt.Errorf("its %s is not what this release reads", key)
	}
	return true, nil
}

// cleanRel says whether p is a clean, relative, slash-separated path that
// stays under the folder it is relative to.
func cleanRel(p string) bool {
	return p != "" && p == path.Clean(p) && !path.IsAbs(p) && p != "." && p != ".." && !strings.HasPrefix(p, "../")
}

func isHex256(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// parseManifest reads a manifest's bytes. Format 1 is the only one this
// release reads; every unit's root is a folder under marketplace/, and
// every file's path is clean, relative and stays under its root. A unit
// this binary renders has its own host, plugin and root, and a hash and a
// size for each file.
func parseManifest(data []byte) (*manifest, error) {
	var top map[string]json.RawMessage
	if json.Unmarshal(data, &top) != nil || top == nil {
		return nil, errors.New("it is not a JSON object")
	}
	var format int
	if _, err := take(top, "format", &format); err != nil {
		return nil, err
	}
	if format > manifestFormat {
		return nil, fmt.Errorf("its format, %d, is newer than this release reads", format)
	}
	if format != manifestFormat {
		return nil, errors.New("it has no format this release reads")
	}
	var raws []json.RawMessage
	if _, err := take(top, "units", &raws); err != nil {
		return nil, err
	}
	m := &manifest{extra: top}
	ids := map[string]bool{}
	for _, raw := range raws {
		var obj map[string]json.RawMessage
		if json.Unmarshal(raw, &obj) != nil || obj == nil {
			return nil, errors.New("a unit is not a JSON object")
		}
		var u manifestUnit
		if _, err := take(obj, "id", &u.ID); err != nil {
			return nil, err
		}
		if u.ID == "" || ids[u.ID] {
			return nil, errors.New("a unit has no id, or one another unit has")
		}
		ids[u.ID] = true
		spec, known := knownUnit(u.ID)
		hasRoot, err := take(obj, "root", &u.Root)
		if err != nil {
			return nil, err
		}
		// A unit of a later release with no folder, such as a printed MCP
		// entry, has no root and lists no file.
		if (hasRoot || known || len(obj["files"]) > 0) && (!cleanRel(u.Root) || !strings.HasPrefix(u.Root, "marketplace/")) {
			return nil, fmt.Errorf("unit %s has its root outside marketplace/", u.ID)
		}
		if known {
			for _, kv := range []struct {
				key string
				v   *string
			}{{"host", &u.Host}, {"plugin", &u.Plugin}} {
				if _, err := take(obj, kv.key, kv.v); err != nil {
					return nil, err
				}
			}
			if u.Host != spec.host || u.Plugin != pluginID || u.Root != spec.root {
				return nil, fmt.Errorf("unit %s is not where this release renders it", u.ID)
			}
		}
		if known && spec.hooks {
			u.Generation = new(int)
			if _, err := take(obj, "generation", u.Generation); err != nil {
				return nil, err
			}
		}
		var files []map[string]json.RawMessage
		if _, err := take(obj, "files", &files); err != nil {
			return nil, err
		}
		seen := map[string]bool{}
		for _, f := range files {
			var e fileEntry
			if _, err := take(f, "path", &e.Path); err != nil {
				return nil, err
			}
			if !cleanRel(e.Path) || seen[e.Path] {
				return nil, fmt.Errorf("unit %s lists a path that is not clean, leaves its root or is listed twice", u.ID)
			}
			seen[e.Path] = true
			if known {
				if _, err := take(f, "sha256", &e.SHA256); err != nil {
					return nil, err
				}
				if _, err := take(f, "size", &e.Size); err != nil {
					return nil, err
				}
				if !isHex256(e.SHA256) || e.Size < 0 {
					return nil, fmt.Errorf("unit %s lists %s without its hash or size", u.ID, e.Path)
				}
				if len(f) > 0 {
					e.extra = f
				}
			}
			u.Files = append(u.Files, e)
		}
		if known {
			if len(obj) > 0 {
				u.extra = obj
			}
		} else {
			u.raw = slices.Clone(raw)
		}
		m.Units = append(m.Units, u)
	}
	return m, nil
}

// object writes a JSON object of the pairs in kv, in that order, then the
// keys of extra, sorted.
func object(extra map[string]json.RawMessage, kv ...any) (json.RawMessage, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	write := func(key string, v any) error {
		if b.Len() > 1 {
			b.WriteByte(',')
		}
		k, _ := json.Marshal(key)
		data, err := json.Marshal(v)
		if err != nil {
			return err
		}
		b.Write(k)
		b.WriteByte(':')
		b.Write(data)
		return nil
	}
	for i := 0; i+1 < len(kv); i += 2 {
		if err := write(kv[i].(string), kv[i+1]); err != nil {
			return nil, err
		}
	}
	for _, key := range slices.Sorted(maps.Keys(extra)) {
		if err := write(key, extra[key]); err != nil {
			return nil, err
		}
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// encode gives the manifest's bytes, indented, with the units this binary
// renders in the adapter's order and the others after them.
func (m *manifest) encode() ([]byte, error) {
	units := make([]json.RawMessage, 0, len(m.Units))
	for _, u := range m.Units {
		if u.raw != nil {
			units = append(units, u.raw)
			continue
		}
		files := make([]json.RawMessage, 0, len(u.Files))
		for _, f := range u.Files {
			obj, err := object(f.extra, "path", f.Path, "sha256", f.SHA256, "size", f.Size)
			if err != nil {
				return nil, err
			}
			files = append(files, obj)
		}
		kv := []any{"id", u.ID, "host", u.Host, "plugin", u.Plugin, "root", u.Root}
		if u.Generation != nil {
			kv = append(kv, "generation", *u.Generation)
		}
		obj, err := object(u.extra, append(kv, "files", files)...)
		if err != nil {
			return nil, err
		}
		units = append(units, obj)
	}
	top, err := object(m.extra, "format", manifestFormat, "units", units)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := json.Indent(&out, top, "", "  "); err != nil {
		return nil, err
	}
	out.WriteByte('\n')
	return out.Bytes(), nil
}

// clone copies m deeply enough for set and drop to change the copy only.
func (m *manifest) clone() *manifest {
	c := &manifest{extra: m.extra, Units: slices.Clone(m.Units)}
	for i := range c.Units {
		c.Units[i].Files = slices.Clone(c.Units[i].Files)
	}
	return c
}

func (m *manifest) find(id string) int {
	return slices.IndexFunc(m.Units, func(u manifestUnit) bool { return u.ID == id })
}

// set lists files, sorted, as spec's unit, keeping the unit's other fields,
// and a file's own when its bytes are the same. A hooks unit this binary
// renders is generation 0.
func (m *manifest) set(spec unitSpec, files []fileEntry) {
	files = slices.Clone(files)
	slices.SortFunc(files, func(a, b fileEntry) int { return strings.Compare(a.Path, b.Path) })
	i := m.find(spec.id)
	if i < 0 {
		m.Units = append(m.Units, manifestUnit{ID: spec.id, Host: spec.host, Plugin: pluginID, Root: spec.root})
		i = len(m.Units) - 1
	}
	u := &m.Units[i]
	for j, f := range files {
		for _, old := range u.Files {
			if old.Path == f.Path && old.SHA256 == f.SHA256 && old.Size == f.Size {
				files[j].extra = old.extra
			}
		}
	}
	u.Files = files
	if spec.hooks {
		u.Generation = new(int)
	}
	slices.SortStableFunc(m.Units, func(a, b manifestUnit) int { return unitOrder(a) - unitOrder(b) })
}

func unitOrder(u manifestUnit) int {
	if spec, ok := knownUnit(u.ID); ok && u.raw == nil {
		return spec.order
	}
	return len(unitSpecs())
}

// ownListed is what the manifest lists as spec's unit, by path; with no
// entry for the unit, which no release that writes one has rendered, it is
// v1's paths, with no entries.
func (m *manifest) ownListed(spec unitSpec) map[string]*fileEntry {
	if m == nil {
		return nil
	}
	listed := map[string]*fileEntry{}
	if i := m.find(spec.id); i >= 0 {
		for j := range m.Units[i].Files {
			listed[m.Units[i].Files[j].Path] = &m.Units[i].Files[j]
		}
		return listed
	}
	for _, p := range v1Paths[spec.hostID] {
		listed[p] = nil
	}
	return listed
}

// listedIn is every path, relative to spec's folder, that the manifest says
// is eva-md's there: the unit's own list, and the files of any other unit
// that fall in the folder. Without a manifest it is nil.
func (m *manifest) listedIn(spec unitSpec) map[string]bool {
	if m == nil {
		return nil
	}
	listed := map[string]bool{}
	for p := range m.ownListed(spec) {
		listed[p] = true
	}
	for _, u := range m.Units {
		if u.ID == spec.id {
			continue
		}
		for _, f := range u.Files {
			if rel, ok := strings.CutPrefix(path.Join(u.Root, f.Path), spec.root+"/"); ok {
				listed[rel] = true
			}
		}
	}
	return listed
}

// entries lists files, as rendered, sorted by path.
func entries(files map[string][]byte) []fileEntry {
	var list []fileEntry
	for _, name := range slices.Sorted(maps.Keys(files)) {
		list = append(list, entryOf(name, files[name]))
	}
	return list
}

// preInstall is the manifest install writes before its first change: each
// unit whose folder it is about to swap lists what it is about to render
// and what is listed now, the latter with the entry the manifest has or,
// for a v1 path, the file's bytes on disk.
func (m *manifest) preInstall(share *os.Root, plans []*hostPlan) (*manifest, error) {
	pre := m.clone()
	for _, p := range plans {
		if !p.changed {
			continue
		}
		spec := unitOf(p.host)
		list := entries(p.files)
		for name, e := range m.ownListed(spec) {
			if _, rendered := p.files[name]; rendered {
				continue
			}
			if e != nil {
				list = append(list, *e)
				continue
			}
			data, err := readAt(share, filepath.Join(filepath.FromSlash(spec.root), filepath.FromSlash(name)), 2<<20)
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			if err != nil {
				return nil, err
			}
			list = append(list, entryOf(name, data))
		}
		pre.set(spec, list)
	}
	return pre, nil
}

// installed is the manifest once the hosts verify: each unit this run
// installed lists only what it rendered.
func (m *manifest) installed(plans []*hostPlan) *manifest {
	done := m.clone()
	for _, p := range plans {
		done.set(unitOf(p.host), entries(p.files))
	}
	return done
}

// withoutKnown is what uninstall leaves: the units this binary does not
// render, as they were.
func (m *manifest) withoutKnown() *manifest {
	rest := &manifest{extra: m.extra}
	for _, u := range m.Units {
		if u.raw != nil {
			rest.Units = append(rest.Units, u)
		}
	}
	return rest
}

// putManifest writes m in the data folder, 0600 and atomically, unless its
// bytes are those on disk already, which it keeps current in onDisk. The
// caller holds the install lock.
func putManifest(ctx context.Context, share *os.Root, m *manifest, onDisk *[]byte) (bool, error) {
	data, err := m.encode()
	if err != nil {
		return false, err
	}
	if *onDisk != nil && bytes.Equal(data, *onDisk) {
		return false, nil
	}
	if err := paths.WriteFileAtomicAt(ctx, share, manifestFile, data, 0o600); err != nil {
		return false, err
	}
	*onDisk = data
	return true, nil
}

// restoreManifest puts back the manifest's bytes as they were before, or
// removes it when there was none.
func restoreManifest(ctx context.Context, share *os.Root, before []byte) error {
	if before != nil {
		return paths.WriteFileAtomicAt(ctx, share, manifestFile, before, 0o600)
	}
	if err := share.Remove(manifestFile); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
