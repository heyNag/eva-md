package hostfacts

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// Every name, address and token here is made up.
const (
	scanUser     = "quillowen"
	scanFull     = "Quill O'Wenderby"
	scanHome     = "/Users/quillowen"
	scanComputer = "Quills-Plum-Machine"
	scanEmail    = "quill.wenderby@madeup-mail.net"
	scanToken    = "sk-MadeUpTokenValue0123456789abcdefXYZ"
	scanSession  = "1a2b3c4d-5e6f-4a7b-8c9d-0e1f2a3b4c5d"
	scanLookup   = "r:0a1b2c3d"
)

func plantedRecording(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"events.jsonl": `{"cwd":"` + scanHome + `/scratch/alpha-orchard","session_id":"` + scanSession + `","answer":"done ` + scanLookup + `"}` + "\n" +
			`{"text":"by quill o’wenderby on quills-plum-machine.local","email":"` + scanEmail + `"}` + "\n",
		"notes.jsonl":     `{"text":"token ` + scanToken + `","other":"hf-test@machines.eva-md.invalid ` + scanSession + `"}` + "\n",
		"sub/marks.jsonl": `{"project":"-Users-quillowen-scratch"}` + "\n",
	}
	for name, text := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestScanFindsPlantedNamesAndCountsKnownValues(t *testing.T) {
	dir := plantedRecording(t)
	words := []ScanWord{{scanUser, "account"}, {scanFull, "account"}, {scanHome, "home"}, {scanComputer, "computer"}}
	res, err := Scan(dir, words, Known{})
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]int{}
	for _, h := range res.Hits {
		kinds[h.Kind]++
		if h.Line == 0 || h.File == "" {
			t.Errorf("a hit without its place: %+v", h)
		}
	}
	for _, k := range []string{"account", "home", "computer", "email address", "key or token"} {
		if kinds[k] == 0 {
			t.Errorf("no %s hit: %+v", k, res.Hits)
		}
	}
	if !slices.ContainsFunc(res.Hits, func(h Hit) bool { return h.Kind == "account" && strings.EqualFold(h.Text, "quill o’wenderby") }) {
		t.Errorf("the full name, with another apostrophe, is not found: %+v", res.Hits)
	}
	if res.Counts["uuid"] != 2 || res.Counts["lookup token"] != 1 || res.Counts["made-up email address"] != 1 {
		t.Errorf("counts: %v", res.Counts)
	}
	for _, h := range res.Hits {
		if strings.Contains(h.Text, scanSession) || strings.Contains(h.Text, scanLookup) {
			t.Errorf("a known value is shown: %+v", h)
		}
	}
	// A key hostrec made is counted, not shown.
	res, err = Scan(dir, nil, Known{Keys: []string{scanToken}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Counts["key hostrec made"] != 1 || slices.ContainsFunc(res.Hits, func(h Hit) bool { return h.Kind == "key or token" }) {
		t.Errorf("a key hostrec made: %v %+v", res.Counts, res.Hits)
	}
}

func TestScrubRemovesEachHitEverywhere(t *testing.T) {
	dir := plantedRecording(t)
	for _, text := range []string{scanUser, scanFull, scanComputer, scanEmail, scanToken} {
		if n, err := Scrub(dir, text); err != nil || n == 0 {
			t.Fatalf("scrub %q: %d %v", text, n, err)
		}
	}
	words := []ScanWord{{scanUser, "account"}, {scanFull, "account"}, {scanHome, "home"}, {scanComputer, "computer"}}
	res, err := Scan(dir, words, Known{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 0 {
		t.Errorf("hits after scrubbing: %+v", res.Hits)
	}
	err = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		for _, bad := range []string{scanUser, "wenderby", "plum-machine", "madeup-mail", scanToken} {
			if strings.Contains(strings.ToLower(string(data)), strings.ToLower(bad)) {
				t.Errorf("%s still holds %q", p, bad)
			}
		}
		if !strings.Contains(string(data), scanSession) && strings.HasSuffix(p, "notes.jsonl") {
			t.Errorf("scrub changed what it was not asked to: %s", data)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// A word inside another word is counted, not shown, and scrub leaves the
// word that holds it; a long hex run, such as a key of 64 hex digits, is
// shown, and so is the recording's own key.
func TestScanJudgesWholeWordsAndLongHex(t *testing.T) {
	dir := t.TempDir()
	const hex64 = "9f2c4a7e1b3d5f60a8c2e4b6d8f01a3c9f2c4a7e1b3d5f60a8c2e4b6d8f01a3c"
	const hex32 = "0123456789abcdef0123456789abcdef"
	const commit = "0123456789abcdef0123456789abcdef01234567"
	text := `{"event":"SessionStart","timestamp":"x","who":"tim","k":"` + hex64 + `","s":"` + hex32 + `","c":"` + commit + `"}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "events.jsonl"), []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	res, err := Scan(dir, []ScanWord{{"tim", "account"}, {"art", "account"}, {hex64, "the recording's key"}}, Known{})
	if err != nil {
		t.Fatal(err)
	}
	var shown []string
	for _, h := range res.Hits {
		shown = append(shown, h.Kind+"="+h.Text)
	}
	slices.Sort(shown)
	want := []string{"account=tim", "long hex run=" + hex32, "long hex run=" + hex64, "the recording's key=" + hex64}
	if !slices.Equal(shown, want) {
		t.Errorf("hits %v, want %v", shown, want)
	}
	if res.Counts["account, inside another word"] != 2 || res.Counts["commit hash"] != 1 {
		t.Errorf("counts: %v", res.Counts)
	}
	if n, err := Scrub(dir, "tim"); err != nil || n != 1 {
		t.Fatalf("scrub: %d %v", n, err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"event":"SessionStart","timestamp":"x"`) || strings.Contains(string(data), `"tim"`) {
		t.Errorf("scrub: %s", data)
	}
}

// The repository's fixtures hold no email address, home path or token,
// and every value of each is one the generator makes: the check needs no
// recording.
func TestCommittedFixtures(t *testing.T) {
	hits, err := ScanTestdata("testdata")
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	for _, h := range hits {
		t.Errorf("testdata/%s:%d holds %s", h.File, h.Line, h.Kind)
	}
	problems, err := CheckTestdata("testdata")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range problems {
		t.Errorf("testdata/%s", p)
	}
	// A fixture at any depth is checked, and a file no fixture.json
	// lists is a problem.
	td := t.TempDir()
	rec := madeUpRecording(t)
	deep := filepath.Join(td, "claude-code", "9.9.9", "extra")
	if _, err := WriteFixtures(FixtureOptions{Rec: rec, Out: deep, Seed: []byte("a made-up seed")}); err != nil {
		t.Fatal(err)
	}
	if problems, err := CheckTestdata(td); err != nil || len(problems) != 0 {
		t.Errorf("a fixture written by the generator: %v %v", problems, err)
	}
	for _, stray := range []string{filepath.Join(td, "codex", "stray.jsonl"), filepath.Join(deep, "unlisted.json")} {
		if err := os.MkdirAll(filepath.Dir(stray), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(stray, []byte(`{"a":"`+recPrompt+`"}`+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(filepath.Join(deep, EventsFile))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(deep, EventsFile), []byte(strings.Replace(string(data), "\"v\":1", "\"v\":1,\"x\":\""+recPrompt+"\"", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	problems, err = CheckTestdata(td)
	if err != nil {
		t.Fatal(err)
	}
	var files []string
	for _, p := range problems {
		files = append(files, p.File)
	}
	for _, want := range []string{"codex/stray.jsonl", "claude-code/9.9.9/extra/unlisted.json", "claude-code/9.9.9/extra/events.jsonl"} {
		if !slices.Contains(files, want) {
			t.Errorf("CheckTestdata finds no problem in %s: %v", want, problems)
		}
	}
	// The same patterns find what a committed fixture must not hold.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "events.jsonl"), []byte(`{"a":"`+scanEmail+`","b":"`+scanHome+`/x","c":"`+scanToken+`"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	hits, err = ScanTestdata(dir)
	if err != nil || len(hits) != 3 {
		t.Errorf("the testdata patterns found %d of 3: %+v %v", len(hits), hits, err)
	}
}

var bannedVocab = regexp.MustCompile(`(?i)^(claude|gpt|opus|sonnet|haiku|o[1-9]|codex-mini|gemini|llama|low|medium|high|xhigh|max|minimal|ultra|pro|plus|team|enterprise|free|business|.*\d+\.\d+.*|.*(cost|usd|token_count|tokens|rate_limit|ratelimit|usage).*)$`)

// The vocabulary holds host words only: no model name or alias, effort
// level, plan or tier, version, or usage, cost or rate-limit figure.
func TestVocabularyHoldsNoAccountWord(t *testing.T) {
	v := Vocab()
	if len(v.Entries()) < 100 {
		t.Fatalf("the vocabulary has %d entries", len(v.Entries()))
	}
	for _, w := range append(append(slices.Clone(v.Values), v.Tags...), v.BracketPrefixes...) {
		if bannedVocab.MatchString(w) && w != "token_count" {
			t.Errorf("the vocabulary keeps %q", w)
		}
	}
	for _, w := range v.Entries() {
		if EmailPattern.MatchString(w) || HomePattern.MatchString(w) || strings.Contains(w, "/") {
			t.Errorf("the vocabulary holds %q", w)
		}
	}
	for _, tag := range []string{"realtime_delegation", "system-reminder", "command-name"} {
		if !slices.Contains(v.Tags, tag) {
			t.Errorf("the vocabulary lacks the wrapper %s", tag)
		}
	}
	if tag, ok := v.LeadingTag("  <system-reminder>made up</system-reminder>"); !ok || tag != "system-reminder" {
		t.Errorf("LeadingTag: %q %v", tag, ok)
	}
	if tag, ok := UnlistedTag("[made_up-tag] text"); !ok || tag != "made_up-tag" {
		t.Errorf("UnlistedTag: %q %v", tag, ok)
	}
	if _, ok := UnlistedTag("<" + strings.Repeat("a", 25) + ">"); ok {
		t.Error("UnlistedTag takes a name of 25 characters")
	}
}
