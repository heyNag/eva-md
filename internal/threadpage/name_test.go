package threadpage_test

import (
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/resume"
	tp "github.com/heyNag/eva-md/internal/threadpage"
)

func TestThreadNameMatchesResume(t *testing.T) {
	isolate(t)
	corpus := []string{
		"", "main", "feature/x", "a/b/c", "trailing/", "/leading", "a//b",
		".hidden", "-dash", ".", "..", "-", "_ok",
		"index", "INDEX", "Index", "log", "LOG", "Log", "AGENTS", "agents", "Agents", "agents.md",
		"a b", "tab\tname", "ctl\x00x", "esc\x1b[31m", "\x7f", "bidi\u202eevil", "zw\u200bj", "\u200b", "\ufeffbom",
		"über", "日本/ブランチ", "🙂", "with.dots", "under_score", "plus+sign", "colon:x", "percent%20", `a\b`,
		"release-1.2.3", "dependabot/npm_and_yarn/lodash-4.17.21", strings.Repeat("x", 300),
	}
	for _, branch := range corpus {
		if got, want := tp.ThreadName(branch), resume.ThreadName(branch); got != want {
			t.Errorf("ThreadName(%q) = %q, resume.ThreadName = %q", branch, got, want)
		}
	}
	for branch, want := range map[string]string{"": "default", "main": "main", "release-1.2.3": "release-1.2.3"} {
		if got := tp.ThreadName(branch); got != want {
			t.Errorf("ThreadName(%q) = %q, want %q", branch, got, want)
		}
	}
	for branch, prefix := range map[string]string{"feature/x": "feature-x-", ".hidden": "_.hidden-", "-dash": "_-dash-", "INDEX": "INDEX-thread-", "bidi\u202eevil": "bidievil-", "\u200b": "thread-"} {
		if got := tp.ThreadName(branch); !strings.HasPrefix(got, prefix) || len(got) != len(prefix)+6 {
			t.Errorf("ThreadName(%q) = %q, want %s and a 6-character hash", branch, got, prefix)
		}
	}
}
