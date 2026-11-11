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

// manifest is what the runner and the reader rule take from a store.toml:
// only the keys they need, read by hand, since toml is read only in
// internal/config. Zero means absent.
type manifest struct {
	schema, minReader, epoch int
	storeID                  string
}

var (
	reKey     = regexp.MustCompile(`^([A-Za-z0-9_-]+)[ \t]*=[ \t]*(.*)$`)
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
// blank lines and comments are ignored; a key asked for twice, or a value
// of another shape, makes the file unreadable.
func readKeys(b []byte, ints, strs []string) (map[string]int, map[string]string, error) {
	gotInt, gotStr := map[string]int{}, map[string]string{}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			break
		}
		m := reKey.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		key, value := m[1], m[2]
		isInt, isStr := slices.Contains(ints, key), slices.Contains(strs, key)
		if !isInt && !isStr {
			continue
		}
		if _, dup := gotInt[key]; dup {
			return nil, nil, fmt.Errorf("%w: %s twice", errManifest, key)
		}
		if _, dup := gotStr[key]; dup {
			return nil, nil, fmt.Errorf("%w: %s twice", errManifest, key)
		}
		value = stripComment(value)
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

// stripComment drops a comment after a value. The values read here hold
// no '#', so the first one starts the comment.
func stripComment(v string) string {
	if i := strings.IndexByte(v, '#'); i >= 0 {
		v = v[:i]
	}
	return strings.TrimSpace(v)
}

// parseManifest reads a store.toml: schema and min_reader are required
// and positive, store_id required, epoch optional.
func parseManifest(b []byte) (manifest, error) {
	ints, strs, err := readKeys(b, []string{"schema", "min_reader", "epoch"}, []string{"store_id"})
	if err != nil {
		return manifest{}, err
	}
	m := manifest{schema: ints["schema"], minReader: ints["min_reader"], epoch: ints["epoch"], storeID: strs["store_id"]}
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
