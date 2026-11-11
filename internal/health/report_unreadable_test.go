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

// A config that is there but can't be read leaves the wiki's part out, as
// unreadable, and gives the notice's kind as config_unreadable; the other
// parts are still counted, and nothing of the config reaches the output.
func TestFieldReportWithAConfigThatCantBeRead(t *testing.T) {
	d, _ := reportHome(t)
	put(t, d.ConfigFile(), []byte("wiki_path = [\""+planted+"\n"))

	got, out := fieldOf(t, d)
	want := map[string]string{"operations": "counted", "hooks": "counted", "updates": "counted", "backlog": "counted", "record": "counted", "wiki": "unreadable"}
	if !reflect.DeepEqual(got.Parts, want) {
		t.Errorf("parts: %v, want %v", got.Parts, want)
	}
	if got.Wiki != nil || got.Operations.Notice != "config_unreadable" {
		t.Errorf("the wiki's part %+v and the notice %q, want none and config_unreadable", got.Wiki, got.Operations.Notice)
	}
	if got.Operations.DailyJobDays != 3 || got.Operations.CommitDays != 5 || got.Record.Sessions["claude-code"] != 66 {
		t.Errorf("the other parts' figures: %+v %+v", got.Operations, got.Record.Sessions)
	}
	noPersonalText(t, out, d.ConfigFile())
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
