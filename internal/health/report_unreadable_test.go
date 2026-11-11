package health

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/coderef"
	"github.com/heyNag/eva-md/internal/paths"
)

// fieldOf is the report of d at reportNow, decoded from the JSON
// fieldJSON gives, so that two reports compare as the person reads them.
func fieldOf(t *testing.T, d paths.Dirs) (FieldReport, string) {
	t.Helper()
	data := fieldJSON(t, d)
	var r FieldReport
	if err := json.Unmarshal(data, &r); err != nil {
		t.Fatal(err)
	}
	return r, string(data)
}

// noPersonalText fails when the report's output holds a path under the
// test's home, the folder named, the system's words for an error, or the
// marker the fixture planted in every text a counter reads.
func noPersonalText(t *testing.T, out string, folder string) {
	t.Helper()
	real := folder
	if r, err := filepath.EvalSymlinks(filepath.Dir(folder)); err == nil {
		real = filepath.Join(r, filepath.Base(folder))
	}
	for _, text := range []string{os.Getenv("HOME"), folder, real, filepath.Base(folder) + "/", "permission denied", "denied",
		"no such file", "error", planted, "/"} {
		if strings.Contains(out, text) {
			t.Errorf("the report holds %q:\n%s", text, out)
		}
	}
}

// A folder of the daily job's attempt markers that can't be listed makes
// the operations part say it couldn't read all it counts: its days of the
// daily job are a floor of zero, and every other figure of the report, the
// rest of the operations part's included, is what it is when the folder
// can be read. Nothing of the folder, its path or the system's error
// reaches the output.
func TestFieldReportWithAttemptMarkersThatCantBeListed(t *testing.T) {
	d, _ := reportHome(t)
	want, _ := fieldOf(t, d)
	if want.Parts["operations"] != PartCounted || want.Operations.DailyJobDays != 3 {
		t.Fatalf("the fixture's operations: %s with %d days of the daily job, want counted with 3", want.Parts["operations"], want.Operations.DailyJobDays)
	}

	markers := filepath.Join(d.State, "commit")
	if err := os.Chmod(markers, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(markers, 0o700) })
	if _, err := os.ReadDir(markers); err == nil {
		t.Fatal("the attempt markers' folder can still be listed with mode 0000; the checks run as a user other than root")
	}

	got, out := fieldOf(t, d)
	if got.Parts["operations"] != PartIncomplete {
		t.Errorf("the operations part is %q, want %q", got.Parts["operations"], PartIncomplete)
	}
	if got.Operations.DailyJobDays != 0 {
		t.Errorf("the daily job's days are %d, want 0", got.Operations.DailyJobDays)
	}

	// Every other figure is the readable fixture's.
	got.Parts["operations"], got.Operations.DailyJobDays = want.Parts["operations"], want.Operations.DailyJobDays
	if !reflect.DeepEqual(got, want) {
		gotJSON, _ := json.MarshalIndent(got, "", "  ")
		wantJSON, _ := json.MarshalIndent(want, "", "  ")
		t.Errorf("a folder of attempt markers that can't be listed changed other figures:\n%s\nwant\n%s", gotJSON, wantJSON)
	}
	noPersonalText(t, out, markers)
}

// A config that is there but can't be read, as one that is not valid TOML
// or one that can't be opened, leaves the wiki's part out, as unreadable,
// and gives the notice's kind as config_unreadable; every other figure of
// the report, the rest of the operations part's included, is what it is
// when the config can be read, and nothing of the config, its path or the
// system's error reaches the output.
func TestFieldReportWithAConfigThatCantBeRead(t *testing.T) {
	d, _ := reportHome(t)
	want, _ := fieldOf(t, d)
	if want.Parts["wiki"] != PartCounted || want.Wiki == nil || want.Operations.Notice == "config_unreadable" {
		t.Fatalf("the fixture's wiki part is %q with %+v and the notice %q, want counted, given and not config_unreadable",
			want.Parts["wiki"], want.Wiki, want.Operations.Notice)
	}
	if !want.Operations.Trust.Present || len(want.Operations.Reasons) == 0 || allHookRuns(want.Operations.HookRuns) == 0 || want.Parts["hooks"] != PartCounted {
		t.Fatalf("the fixture gives no trust flag, commit reason, hook run or hooks part a config that can't be read could drop: %+v, hooks %q",
			want.Operations, want.Parts["hooks"])
	}
	readable, err := os.ReadFile(d.ConfigFile())
	if err != nil {
		t.Fatal(err)
	}

	check := func(t *testing.T) {
		t.Helper()
		got, out := fieldOf(t, d)
		if got.Parts["wiki"] != PartUnreadable || got.Wiki != nil || got.Operations.Notice != "config_unreadable" {
			t.Errorf("the wiki's part is %q with %+v and the notice %q, want unreadable, none and config_unreadable",
				got.Parts["wiki"], got.Wiki, got.Operations.Notice)
		}

		// Every other figure is the readable fixture's.
		got.Parts["wiki"], got.Operations.Notice, got.Wiki = want.Parts["wiki"], want.Operations.Notice, want.Wiki
		if !reflect.DeepEqual(got, want) {
			gotJSON, _ := json.MarshalIndent(got, "", "  ")
			wantJSON, _ := json.MarshalIndent(want, "", "  ")
			t.Errorf("a config that can't be read changed other figures:\n%s\nwant\n%s", gotJSON, wantJSON)
		}
		noPersonalText(t, out, d.ConfigFile())
	}

	t.Run("not valid TOML", func(t *testing.T) {
		put(t, d.ConfigFile(), []byte("wiki_path = [\""+planted+"\n"))
		check(t)
	})

	// Its error names its path and says permission denied, which the
	// output must not hold.
	t.Run("can't be opened", func(t *testing.T) {
		put(t, d.ConfigFile(), readable)
		if err := os.Chmod(d.ConfigFile(), 0o000); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(d.ConfigFile(), 0o600) })
		if _, err := os.ReadFile(d.ConfigFile()); err == nil {
			t.Fatal("the config can still be read with mode 0000; the checks run as a user other than root")
		}
		check(t)
	})
}

// allHookRuns is how many hook runs the operations part counts in all.
func allHookRuns(runs map[string]map[string]HookRunsCounts) int {
	n := 0
	for _, events := range runs {
		for _, c := range events {
			n += c.Runs
		}
	}
	return n
}

// A code status that is there but can't be read makes the wiki's part
// unreadable, with no code figures, while its page counts and the other
// parts are still given, and nothing of the file reaches the output.
func TestFieldReportWithACodeStatusThatCantBeRead(t *testing.T) {
	d, _ := reportHome(t)
	want, _ := fieldOf(t, d)
	status := filepath.Join(d.State, coderef.File)
	put(t, status, []byte("not json "+planted))

	got, out := fieldOf(t, d)
	if got.Parts["wiki"] != PartUnreadable || got.Wiki == nil {
		t.Fatalf("the wiki's part is %q with %+v, want unreadable and given", got.Parts["wiki"], got.Wiki)
	}
	if got.Wiki.Code.Found || got.Wiki.Code.Pages != 0 || len(got.Wiki.Code.Entries) != 0 {
		t.Errorf("the code figures of a status that can't be read: %+v", got.Wiki.Code)
	}
	if !reflect.DeepEqual(got.Wiki.Pages, want.Wiki.Pages) {
		t.Errorf("pages: %v, want %v", got.Wiki.Pages, want.Wiki.Pages)
	}
	got.Parts["wiki"], got.Wiki, want.Wiki = want.Parts["wiki"], nil, nil
	if !reflect.DeepEqual(got, want) {
		t.Errorf("a code status that can't be read changed other parts:\n%+v\nwant\n%+v", got, want)
	}
	noPersonalText(t, out, status)
}
