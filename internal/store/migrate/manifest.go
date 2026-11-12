package migrate

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// The four files the runner owns. A step's Change may not name them: the
// runner writes each from the generation it migrates to.
const (
	attributesPath = ".gitattributes"
	ignorePath     = ".gitignore"
	manifestPath   = ".eva-md/store.toml"
	markerPath     = ".eva-md/schema.toml"
)

var owned = []string{attributesPath, ignorePath, manifestPath, markerPath}

// gitFiles are the names git reads settings from inside a work tree. A
// step writes none, at any depth: one would change the bytes git takes in
// for the files beside it, or what it hides.
var gitFiles = []string{".gitattributes", ".gitignore", ".gitmodules"}

// manifest is what the runner and the reader rule take from a store.toml:
// only the keys they need, read by hand, since toml is read only in
// internal/config. Zero means absent.
type manifest struct {
	schema, minReader, epoch int
	storeID                  string
}

var (
	reInt     = regexp.MustCompile(`^(0|[1-9][0-9]{0,8})$`)
	reStr     = regexp.MustCompile(`^"([A-Za-z0-9_-]{1,64})"$`)
	reStoreID = regexp.MustCompile(`^s[0-9a-hjkmnp-tv-z]{13}$`)
	reWriter  = regexp.MustCompile(`^w-[0-9a-hjkmnp-tv-z]{8}$`)
	reLabel   = regexp.MustCompile(`^[a-z0-9-]{1,32}$`)
	rePass    = regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{26}$`)
	reClass   = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)
)

var errManifest = errors.New("not a readable manifest")

// readKeys reads the top-level keys of a TOML file that the caller asks
// for, each at most once: an integer for each of ints, a plain string for
// each of strs. Lines before the first table header that name another key,
// blank lines and comments are ignored. It fails closed on whatever a line
// reader cannot be sure a TOML reader reads the same way: a key asked for
// twice, quoted or dotted, a quoted key with an escape, a multi-line
// string, an array or inline table left open at the end of its line, a
// line that is no key, and a value of another shape.
func readKeys(b []byte, ints, strs []string) (map[string]int, map[string]string, error) {
	gotInt, gotStr := map[string]int{}, map[string]string{}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSuffix(line, "\r")
		if strings.Contains(line, `"""`) || strings.Contains(line, `'''`) {
			return nil, nil, fmt.Errorf("%w: a multi-line string", errManifest)
		}
		line = strings.Trim(line, " \t")
		if line == "" || line[0] == '#' {
			continue
		}
		if line[0] == '[' {
			break
		}
		k, err := splitKey(line)
		if err != nil {
			return nil, nil, err
		}
		isInt, isStr := slices.Contains(ints, k.first), slices.Contains(strs, k.first)
		if !isInt && !isStr {
			if !closedValue(k.value) {
				return nil, nil, fmt.Errorf("%w: a value runs past its line", errManifest)
			}
			continue
		}
		// A key asked for in another form, quoted or dotted, would be
		// the same key, or clash with it, to a TOML reader: unreadable.
		if k.quoted || k.dotted {
			return nil, nil, fmt.Errorf("%w: %s is not a plain key", errManifest, k.first)
		}
		key := k.first
		if _, dup := gotInt[key]; dup {
			return nil, nil, fmt.Errorf("%w: %s twice", errManifest, key)
		}
		if _, dup := gotStr[key]; dup {
			return nil, nil, fmt.Errorf("%w: %s twice", errManifest, key)
		}
		value := stripComment(k.value)
		if isInt {
			if !reInt.MatchString(value) {
				return nil, nil, fmt.Errorf("%w: %s is not an integer", errManifest, key)
			}
			gotInt[key], _ = strconv.Atoi(value)
			continue
		}
		s := reStr.FindStringSubmatch(value)
		if s == nil {
			return nil, nil, fmt.Errorf("%w: %s is not a plain string", errManifest, key)
		}
		gotStr[key] = s[1]
	}
	return gotInt, gotStr, nil
}

// keyLine is one key = value line: its key's first segment, whether that
// segment was quoted, whether the key has more segments, and the value.
type keyLine struct {
	first          string
	quoted, dotted bool
	value          string
}

var errNotKey = fmt.Errorf("%w: a line that is not a plain key", errManifest)

// splitKey reads a key = value line's key segment by segment, as TOML
// does: bare segments of A-Za-z0-9_-, quoted ones, and dots between them
// with optional blanks around. A quoted segment with an escape is refused,
// since its name cannot be told without reading the escape.
func splitKey(line string) (keyLine, error) {
	var k keyLine
	rest := line
	for i := 0; ; i++ {
		var seg string
		quoted := false
		switch {
		case rest == "":
			return keyLine{}, errNotKey
		case rest[0] == '"' || rest[0] == '\'':
			end := strings.IndexByte(rest[1:], rest[0])
			if end < 0 {
				return keyLine{}, errNotKey
			}
			seg, rest, quoted = rest[1:1+end], rest[end+2:], true
			if strings.Contains(seg, `\`) {
				return keyLine{}, errNotKey
			}
		default:
			n := strings.IndexFunc(rest, func(r rune) bool {
				return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-')
			})
			if n == 0 {
				return keyLine{}, errNotKey
			}
			if n < 0 {
				n = len(rest)
			}
			seg, rest = rest[:n], rest[n:]
		}
		if i == 0 {
			k.first, k.quoted = seg, quoted
		}
		rest = strings.TrimLeft(rest, " \t")
		switch {
		case strings.HasPrefix(rest, "."):
			k.dotted = true
			rest = strings.TrimLeft(rest[1:], " \t")
		case strings.HasPrefix(rest, "="):
			k.value = strings.Trim(rest[1:], " \t")
			return k, nil
		default:
			return keyLine{}, errNotKey
		}
	}
}

// closedValue reports whether a value ends on its line: no string left
// open, and every array and inline table closed before any comment.
func closedValue(v string) bool {
	depth := 0
	var quote byte
	for i := 0; i < len(v); i++ {
		c := v[i]
		switch {
		case quote == '"' && c == '\\':
			i++
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '[' || c == '{':
			depth++
		case c == ']' || c == '}':
			depth--
		case c == '#':
			return depth == 0
		}
	}
	return quote == 0 && depth == 0
}

// stripComment drops a comment after a value. The values read here hold
// no '#', so the first one starts the comment.
func stripComment(v string) string {
	if i := strings.IndexByte(v, '#'); i >= 0 {
		v = v[:i]
	}
	return strings.TrimSpace(v)
}

// parseManifest reads a store.toml as the runner does: schema and
// min_reader are required and positive, store_id required, epoch optional.
func parseManifest(b []byte) (manifest, error) {
	return readManifestKeys(b, true)
}

// readManifestKeys reads schema, min_reader and store_id, and epoch when
// asked. The reader rule reads only the first three, so a newer store's
// epoch, of whatever shape, never hides that it needs a newer reader.
func readManifestKeys(b []byte, epoch bool) (manifest, error) {
	ints := []string{"schema", "min_reader"}
	if epoch {
		ints = append(ints, "epoch")
	}
	got, strs, err := readKeys(b, ints, []string{"store_id"})
	if err != nil {
		return manifest{}, err
	}
	m := manifest{schema: got["schema"], minReader: got["min_reader"], epoch: got["epoch"], storeID: strs["store_id"]}
	if m.schema < 1 || m.minReader < 1 || m.storeID == "" {
		return manifest{}, fmt.Errorf("%w: schema, min_reader or store_id is missing", errManifest)
	}
	return m, nil
}

// parseMarker reads v1's schema.toml for its schema_version.
func parseMarker(b []byte) (int, error) {
	ints, _, err := readKeys(b, []string{"schema_version"}, nil)
	if err != nil {
		return 0, err
	}
	if ints["schema_version"] < 1 {
		return 0, fmt.Errorf("%w: schema_version is missing", errManifest)
	}
	return ints["schema_version"], nil
}

// ownedFiles are the four runner-owned files after a step to generation
// to, as the format's section 5 writes them: the store id and the epoch
// are kept, the minimum reader is the higher of the old and the step's.
func ownedFiles(old manifest, to, minReader int, storeID string) map[string][]byte {
	if old.storeID != "" {
		storeID = old.storeID
	}
	epoch := old.epoch
	if epoch < 1 {
		epoch = 1
	}
	return map[string][]byte{
		attributesPath: []byte("* -text -filter -ident\n"),
		ignorePath:     []byte(".tmp-*\n"),
		manifestPath:   fmt.Appendf(nil, "schema = %d\nmin_reader = %d\nstore_id = %q\nepoch = %d\n", to, max(old.minReader, minReader), storeID, epoch),
		markerPath:     fmt.Appendf(nil, "schema_version = %d\n", to),
	}
}
