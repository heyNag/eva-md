package health

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// The doctor reads Codex's hooks feature from codex features list, as Codex
// 0.167 prints it, "hooks stable <state>", and as earlier ones did, with
// "experimental": on passes, off fails with the command that turns it on,
// and a list without it is unknown.
func TestCodexHooksFeatureRow(t *testing.T) {
	d := isolated(t)
	bin := t.TempDir()
	t.Setenv("PATH", bin)
	for _, c := range []struct{ list, status, detail, fix string }{
		{"hooks stable true", "PASS", "enabled", ""},
		{"hooks stable false", "FAIL", "disabled", "run codex features enable hooks"},
		{"hooks experimental false", "FAIL", "disabled", "run codex features enable hooks"},
		{"multi_agent stable true", "UNKNOWN", "not reported by this version", ""},
	} {
		script := "#!/bin/sh\n[ \"$*\" = 'features list' ] || exit 2\nprintf '%s\\n' '" + c.list + "'\n"
		if err := os.WriteFile(filepath.Join(bin, "codex"), []byte(script), 0o700); err != nil {
			t.Fatal(err)
		}
		var r Report
		inspectHosts(context.Background(), &r, d, inventoryTimeout)
		if row := findRow(t, r, "codex hooks feature"); row.Status != c.status || row.Detail != c.detail || row.Fix != c.fix {
			t.Errorf("%q: %+v", c.list, row)
		}
	}
}

// A host command, such as codex features list, may print up to the
// doctor's limit and no more, however much it prints: os/exec copies its
// output through the buffer's Write, never a ReadFrom that would keep all
// of it, so a command past the limit counts as unavailable.
func TestHostCommandOutputIsCapped(t *testing.T) {
	isolated(t)
	host := filepath.Join(t.TempDir(), "codex")
	if err := os.WriteFile(host, []byte("#!/bin/sh\nexec /usr/bin/head -c \"$EVA_MD_TEST_BYTES\" /dev/zero\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	t.Setenv("EVA_MD_TEST_BYTES", fmt.Sprint(hostOutputLimit))
	if data, err := hostCommand(ctx, host, "features", "list"); err != nil || len(data) != hostOutputLimit {
		t.Fatalf("output at the limit: %d bytes, %v", len(data), err)
	}
	t.Setenv("EVA_MD_TEST_BYTES", fmt.Sprint(hostOutputLimit+1))
	if data, err := hostCommand(ctx, host, "features", "list"); err == nil {
		t.Fatalf("output past the limit was kept: %d bytes", len(data))
	}
}
