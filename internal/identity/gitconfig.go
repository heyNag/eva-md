package identity

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// globalConfigs lists the global config files git reads, as
// git_global_config_paths in git's config.c names them.
func globalConfigs() []string {
	if v, ok := os.LookupEnv("GIT_CONFIG_GLOBAL"); ok {
		if v == "" {
			return nil
		}
		return []string{v}
	}
	home, hasHome := os.LookupEnv("HOME")
	var out []string
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		out = append(out, xdg+"/git/config")
	} else if hasHome {
		out = append(out, home+"/.config/git/config")
	}
	if hasHome {
		out = append(out, home+"/.gitconfig")
	}
	return out
}

// unknownSystem marks a git that can't name its system config. Such a git
// is always resolved live.
const unknownSystem = "?"

// systemConfig asks git for its system config file. It returns "" when git
// reads none, as with GIT_CONFIG_NOSYSTEM, unknownSystem for a git too old to
// say (git var learned GIT_CONFIG_SYSTEM in 2.42), and false when the answer
// may be different next time.
func systemConfig(ctx context.Context) (string, bool) {
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	out, err := git(ctx, "/", "var", "GIT_CONFIG_SYSTEM")
	var ee *exitError
	switch {
	case err == nil && filepath.IsAbs(out) && !strings.ContainsAny(out, "\n\x00"):
		return out, true
	case err == nil:
		return unknownSystem, true
	case errors.As(err, &ee) && ee.code == 1:
		return "", true
	case errors.As(err, &ee) && ee.code == 129:
		return unknownSystem, true
	}
	return "", false
}

// includeTarget expands an include path the way git's handle_path_include
// does. "~user/" and "%(prefix)/" paths are left to live resolution.
func includeTarget(v, from string) (string, bool) {
	if v == "" || strings.HasPrefix(v, "%(prefix)/") || strings.ContainsRune(v, 0) {
		return "", false
	}
	if v[0] == '~' {
		if len(v) > 1 && v[1] != '/' {
			return "", false
		}
		home, ok := os.LookupEnv("HOME")
		if !ok {
			return "", false
		}
		v = home + v[1:]
	}
	if !strings.HasPrefix(v, "/") {
		v = from[:strings.LastIndexByte(from, '/')+1] + v
	}
	return v, true
}

// configIncludes returns the values of every path key in include and
// includeIf sections, conditional or not, parsing the file the way git's
// config.c does. It reports false where git would reject the file.
func configIncludes(data []byte) ([]string, bool) {
	p := configParser{data: data}
	if strings.HasPrefix(string(data), "\xef\xbb\xbf") {
		p.pos = 3
	}
	var includes []string
	include, comment := false, false
	for {
		c := p.next()
		switch {
		case c == '\n':
			if p.eof {
				return includes, true
			}
			comment = false
		case comment || gitSpace(c):
		case c == '#' || c == ';':
			comment = true
		case c == '[':
			name, ok := p.section()
			if !ok || name == "" {
				return nil, false
			}
			base, _, _ := strings.Cut(name, ".")
			include = base == "include" || base == "includeif"
		case isAlpha(c):
			key, value, ok := p.entry(c)
			if !ok {
				return nil, false
			}
			if include && key == "path" {
				if value == nil {
					return nil, false
				}
				includes = append(includes, *value)
			}
		default:
			return nil, false
		}
	}
}

type configParser struct {
	data []byte
	pos  int
	eof  bool
}

// next mirrors get_next_char: CRLF reads as LF and the end reads as LF.
func (p *configParser) next() byte {
	if p.pos >= len(p.data) {
		p.eof = true
		return '\n'
	}
	c := p.data[p.pos]
	p.pos++
	if c == '\r' && p.pos < len(p.data) && p.data[p.pos] == '\n' {
		p.pos++
		c = '\n'
	}
	return c
}

// section mirrors get_base_var and get_extended_base_var.
func (p *configParser) section() (string, bool) {
	var name strings.Builder
	var c byte
	for {
		c = p.next()
		if p.eof {
			return "", false
		}
		if c == ']' {
			return name.String(), true
		}
		if gitSpace(c) {
			break
		}
		if !keyChar(c) && c != '.' {
			return "", false
		}
		name.WriteByte(lower(c))
	}
	for gitSpace(c) {
		if c == '\n' {
			return "", false
		}
		c = p.next()
	}
	if c != '"' {
		return "", false
	}
	name.WriteByte('.')
	for {
		c := p.next()
		if c == '\n' {
			return "", false
		}
		if c == '"' {
			break
		}
		if c == '\\' {
			if c = p.next(); c == '\n' {
				return "", false
			}
		}
		name.WriteByte(c)
	}
	return name.String(), p.next() == ']'
}

// entry mirrors get_value and parse_value. The value is nil for a key
// without '='.
func (p *configParser) entry(first byte) (string, *string, bool) {
	key := []byte{lower(first)}
	c := p.next()
	for !p.eof && keyChar(c) {
		key = append(key, lower(c))
		c = p.next()
	}
	for c == ' ' || c == '\t' {
		c = p.next()
	}
	if c == '\n' {
		return string(key), nil, true
	}
	if c != '=' {
		return "", nil, false
	}
	var value []byte
	quote, comment := false, false
	trim := -1
	for {
		c := p.next()
		if c == '\n' {
			if quote {
				return "", nil, false
			}
			if trim >= 0 {
				value = value[:trim]
			}
			v := string(value)
			return string(key), &v, true
		}
		if comment {
			continue
		}
		if gitSpace(c) && !quote {
			if trim < 0 {
				trim = len(value)
			}
			if len(value) > 0 {
				value = append(value, c)
			}
			continue
		}
		if !quote && (c == ';' || c == '#') {
			comment = true
			continue
		}
		trim = -1
		switch c {
		case '\\':
			switch c = p.next(); c {
			case '\n':
				continue
			case 't':
				c = '\t'
			case 'b':
				c = '\b'
			case 'n':
				c = '\n'
			case '\\', '"':
			default:
				return "", nil, false
			}
		case '"':
			quote = !quote
			continue
		}
		value = append(value, c)
	}
}

func gitSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }
func isAlpha(c byte) bool  { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }
func keyChar(c byte) bool  { return isAlpha(c) || c >= '0' && c <= '9' || c == '-' }
func lower(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + 'a' - 'A'
	}
	return c
}
