package resume

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/paths"
)

// Show resolves an unambiguous physical source line, or a wiki page/section.
// It is read-only and never follows symlinks or returns secret-bearing text.
func Show(ctx context.Context, dirs paths.Dirs, cfg config.Config, ref string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if strings.HasPrefix(ref, "s:") {
		return showSource(ctx, dirs, ref)
	}
	page, section, _ := strings.Cut(ref, "#")
	if strings.Contains(page, "\\") || !filepath.IsLocal(page) || page == "." || strings.HasPrefix(page, ".") || strings.Contains(page, "/.") {
		return "", errors.New("resume: invalid page path")
	}
	if !strings.HasSuffix(page, ".md") {
		page += ".md"
	}
	root, err := paths.OpenRoot(cfg.WikiPath, false)
	if err != nil {
		return "", err
	}
	defer root.Close()
	index, err := regular(root, "index.md")
	if err != nil {
		return "", errors.New("resume: wiki root index missing")
	}
	index.Close()
	if cfg.Instance != "" {
		schema, err := config.LoadSchema(filepath.Join(cfg.WikiPath, ".eva-md", "schema.toml"))
		if err != nil || schema.Instance != cfg.Instance {
			return "", errors.New("resume: wiki instance mismatch")
		}
	}
	body, err := pageText(root, page)
	if err != nil {
		return "", err
	}
	if section != "" {
		var lines []string
		active := false
		level := 0
		for _, line := range strings.Split(body, "\n") {
			n := len(line) - len(strings.TrimLeft(line, "#"))
			heading := n > 0 && n <= 6 && len(line) > n && line[n] == ' '
			if active && heading && n <= level {
				break
			}
			if heading && strings.EqualFold(strings.TrimSpace(line[n:]), section) {
				active = true
				level = n
			}
			if active {
				lines = append(lines, line)
			}
		}
		if !active {
			return "", errors.New("resume: section not found")
		}
		body = strings.Join(lines, "\n")
	}
	return clean(body), nil
}

func showSource(ctx context.Context, dirs paths.Dirs, ref string) (string, error) {
	sid, num, ok := strings.Cut(strings.TrimPrefix(ref, "s:"), "#")
	line, err := strconv.Atoi(num)
	if !ok || err != nil || line < 1 || len([]rune(sid)) < 1 || len([]rune(sid)) > 8 || strings.ContainsAny(sid, "/\\\x00") {
		return "", errors.New("resume: invalid source reference")
	}
	for _, r := range sid {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf) {
			return "", errors.New("resume: invalid source reference")
		}
	}
	root, err := paths.OpenRoot(dirs.Inbox(), false)
	if err != nil {
		return "", err
	}
	defer root.Close()
	d, err := root.Open(".")
	if err != nil {
		return "", err
	}
	projects, err := d.ReadDir(-1)
	d.Close()
	if err != nil {
		return "", err
	}
	var found []string
	for _, p := range projects {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if !p.IsDir() {
			continue
		}
		d, err := root.Open(p.Name())
		if err != nil {
			continue
		}
		files, _ := d.ReadDir(-1)
		d.Close()
		for _, f := range files {
			if !f.Type().IsRegular() || !strings.HasSuffix(f.Name(), ".jsonl") {
				continue
			}
			name := f.Name()
			for _, host := range []string{"claude-code-", "codex-"} {
				if strings.HasPrefix(name, host) {
					name = strings.TrimPrefix(name, host)
					break
				}
			}
			if !strings.HasPrefix(name, sid) {
				continue
			}
			file, err := regular(root, filepath.Join(p.Name(), f.Name()))
			if err != nil {
				continue
			}
			entry, err := sourceLine(ctx, file, line)
			file.Close()
			if err != nil {
				continue
			}
			if inbox.Ref(entry.Session, line) != ref {
				continue
			}
			if filepath.Base((inbox.Store{Root: dirs.Inbox()}).Path(entry.Project, entry.Host, entry.Session)) != f.Name() {
				continue
			}
			if project, _ := paths.SafeName(entry.Project); project != p.Name() {
				continue
			}
			found = append(found, sourceDescription(record{Entry: entry, line: line}))
		}
	}
	if len(found) == 0 {
		return "", errors.New("resume: source not found")
	}
	if len(found) > 1 {
		return "", errors.New("resume: ambiguous source reference; inspect the inbox session explicitly")
	}
	return found[0], nil
}

func sourceLine(ctx context.Context, f *os.File, line int) (inbox.Entry, error) {
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 64<<10), 2<<20)
	for n := 1; s.Scan(); n++ {
		if err := ctx.Err(); err != nil {
			return inbox.Entry{}, err
		}
		if n != line {
			continue
		}
		var e inbox.Entry
		if err := json.Unmarshal(s.Bytes(), &e); err != nil {
			return e, err
		}
		return e, nil
	}
	return inbox.Entry{}, fmt.Errorf("resume: source line %d unavailable", line)
}
