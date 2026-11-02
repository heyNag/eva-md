package release

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Tests check what the requests do, not how fast the machine answers them:
// the time limits are raised before any test runs.
func TestMain(m *testing.M) {
	LookupTimeout, DownloadTimeout = 10*time.Minute, 10*time.Minute
	os.Exit(m.Run())
}

// A tag is the release's day with no zero padding, then .2 and on for more
// releases that day; versions order by day, then that number; a source
// build is its day with number 0, and replaces nothing of its day unforced.
func TestVersionsOrder(t *testing.T) {
	for _, tag := range []string{"2026.11.3", "2026.1.31", "2026.11.3.2", "2026.11.3.10"} {
		if !ValidTag(tag) {
			t.Errorf("%s refused", tag)
		}
	}
	for _, tag := range []string{"v2026.11.3", "2026.11.03", "2026.0.3", "2026.11.3.1", "2026.11.3.01", "2026.13.1", "2026.11.32", "26.11.3", "2026.11.3+0915.9e4f609", "dev", ""} {
		if ValidTag(tag) {
			t.Errorf("%s accepted", tag)
		}
	}
	order := []string{"dev", "2026.11.2", "2026.11.3+0915.9e4f609", "2026.11.3", "2026.11.3.2", "2026.11.3.10", "2026.12.1", "2027.1.1"}
	for i := range order {
		for j := range order {
			want := 0
			if i < j {
				want = -1
			} else if i > j {
				want = 1
			}
			if got := Compare(Parse(order[i]), Parse(order[j])); got != want {
				t.Errorf("Compare(%s, %s) = %d, want %d", order[i], order[j], got, want)
			}
		}
	}
	if v := Parse("2026.11.3+0915.9e4f609.dirty"); !v.Source || v.N != 0 || v.Day != 3 {
		t.Errorf("a dirty source build: %+v", v)
	}
	for _, c := range []struct {
		release, current string
		replaces         bool
	}{
		{"2026.11.3", "2026.11.2", true},
		{"2026.11.3.2", "2026.11.3", true},
		{"2026.11.3", "2026.11.3", false},
		{"2026.11.2", "2026.11.3", false},
		{"2026.11.3", "2026.11.3+0915.9e4f609", false},
		{"2026.11.3.2", "2026.11.3+2359.9e4f609.dirty", false},
		{"2026.11.4", "2026.11.3+2359.9e4f609", true},
		{"2026.11.3", "2026.12.1+0001.9e4f609", false},
		{"2026.11.3", "dev", false},
	} {
		tag, _ := ParseTag(c.release)
		if got := Replaces(tag, Parse(c.current)); got != c.replaces {
			t.Errorf("Replaces(%s, %s) = %v", c.release, c.current, got)
		}
	}
}

// The checksum file is read as shasum writes it; a missing line or another
// sum fails.
func TestVerify(t *testing.T) {
	file := filepath.Join(t.TempDir(), "eva-md")
	if err := os.WriteFile(file, []byte("binary"), 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("binary"))
	good := hex.EncodeToString(sum[:])
	other := strings.Repeat("0", 64)
	for _, c := range []struct {
		sums, err string
	}{
		{good + "  eva-md-darwin-arm64\n", ""},
		{other + "  eva-md-darwin-amd64\n" + good + " *eva-md-darwin-arm64\r\n", ""},
		{good + "  eva-md-darwin-amd64\n", "has no line for eva-md-darwin-arm64"},
		{other + "  eva-md-darwin-arm64\n", "doesn't match"},
		{"", "has no line"},
	} {
		err := Verify([]byte(c.sums), "eva-md-darwin-arm64", file)
		if c.err == "" && err != nil || c.err != "" && (err == nil || !strings.Contains(err.Error(), c.err)) {
			t.Errorf("sums %q: %v", c.sums, err)
		}
	}
}

// The latest release is read from the redirect, which is not followed; a
// download follows a redirect on GitHub's own host, refuses one elsewhere,
// and refuses an answer that is not 200 or larger than its limit.
func TestLatestAndDownload(t *testing.T) {
	var followed bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/heyNag/eva-md/releases/latest":
			http.Redirect(w, r, "/heyNag/eva-md/releases/tag/2026.11.3", http.StatusFound)
		case "/heyNag/eva-md/releases/tag/2026.11.3":
			followed = true
		case "/heyNag/eva-md/releases/download/2026.11.3/eva-md-darwin-arm64":
			http.Redirect(w, r, "/assets/arm64", http.StatusFound)
		case "/assets/arm64":
			_, _ = w.Write([]byte("binary"))
		case "/heyNag/eva-md/releases/download/2026.11.3/elsewhere":
			http.Redirect(w, r, "https://downloads.example.com/x", http.StatusFound)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	saved := Base
	Base = srv.URL + "/heyNag/eva-md"
	t.Cleanup(func() { Base = saved })
	ctx := context.Background()
	if tag, err := Latest(ctx); err != nil || tag != "2026.11.3" || followed {
		t.Fatalf("latest %q %v, followed %v", tag, err, followed)
	}
	dir := t.TempDir()
	if err := Download(ctx, "2026.11.3", "eva-md-darwin-arm64", filepath.Join(dir, "a"), 100); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "a")); string(b) != "binary" {
		t.Fatalf("downloaded %q", b)
	}
	for name, want := range map[string]string{"elsewhere": "isn't GitHub", "missing": "404"} {
		if err := Download(ctx, "2026.11.3", name, filepath.Join(dir, name), 100); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if err := Download(ctx, "2026.11.3", "eva-md-darwin-arm64", filepath.Join(dir, "small"), 3); err == nil || !strings.Contains(err.Error(), "larger") {
		t.Errorf("over the limit: %v", err)
	}
	// A redirect that names no tag is refused.
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/heyNag/eva-md/releases/tag/v1.0", http.StatusFound)
	}))
	defer srv2.Close()
	Base = srv2.URL + "/heyNag/eva-md"
	if _, err := Latest(ctx); err == nil {
		t.Fatal("a tag of another form accepted")
	}
}
