package shapes

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// The facts the report settles in full or in part, and those it says
// shapes cannot settle, are together exactly the facts the host fact sheet
// marks "by the field report", each once.
func TestTheFactsAreTheSheets(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "v2", "host-facts.md"))
	if err != nil {
		t.Fatal(err)
	}
	header := regexp.MustCompile(`^#### (F-[0-9]{3})\.`)
	var marked []string
	fact := ""
	for _, line := range strings.Split(string(data), "\n") {
		if m := header.FindStringSubmatch(line); m != nil {
			fact = m[1]
			continue
		}
		if strings.HasPrefix(line, "- **Settled by.**") && strings.Contains(line, "**Mark: by the field report") && fact != "" {
			marked = append(marked, fact)
		}
	}
	if len(marked) != 51 {
		t.Fatalf("the sheet marks %d facts by the field report: %v", len(marked), marked)
	}
	var ours []string
	for _, l := range [][]string{settledInFull, settledInPart, notByShapes} {
		ours = append(ours, l...)
	}
	sorted := slices.Sorted(slices.Values(ours))
	if len(slices.Compact(slices.Clone(sorted))) != len(ours) {
		t.Errorf("a fact is listed twice: %v", sorted)
	}
	slices.Sort(marked)
	if !slices.Equal(sorted, marked) {
		t.Errorf("the report lists\n%v\nthe sheet marks\n%v", sorted, marked)
	}
}

// No list holds a name twice, other, or anything but printable ASCII; and
// Words holds every list's names.
func TestTheLists(t *testing.T) {
	words := Words()
	for _, l := range [][]string{claudeRowTypes, claudeRowFields, claudeMessageFields, claudeItemTypes, claudeItemFields,
		claudeSubtypes, claudeEntrypoints, turnOrigins, codexRowTypes, codexRowFields, codexPayloadTypes, codexPayloadFields,
		codexSources, codexOriginators} {
		if len(slices.Compact(slices.Sorted(slices.Values(l)))) != len(l) || slices.Contains(l, other) {
			t.Errorf("%v holds a name twice, or other", l)
		}
		for _, n := range l {
			if _, ok := slices.BinarySearch(words, n); !ok {
				t.Errorf("Words lacks %q", n)
			}
			for _, b := range []byte(n) {
				if b < 0x21 || b > 0x7e {
					t.Errorf("%q is not printable ASCII", n)
				}
			}
		}
	}
}
