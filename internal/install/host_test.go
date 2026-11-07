package install

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// A host command's output is kept up to the limit, never past it, however
// much it prints: os/exec copies it through the buffer's Write, never a
// ReadFrom that would keep all of it. An inventory past the limit fails;
// diagnostics past it are cut.
func TestHostOutputIsCapped(t *testing.T) {
	host := filepath.Join(t.TempDir(), "codex")
	script := `#!/bin/sh
if [ "$EVA_MD_TEST_STREAM" = stderr ]; then /usr/bin/head -c "$EVA_MD_TEST_BYTES" /dev/zero >&2; exit 1; fi
exec /usr/bin/head -c "$EVA_MD_TEST_BYTES" /dev/zero
`
	if err := os.WriteFile(host, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	t.Setenv("EVA_MD_TEST_STREAM", "stdout")
	t.Setenv("EVA_MD_TEST_BYTES", fmt.Sprint(hostOutputLimit))
	if data, err := native(ctx, host, "plugin", "list", "--json"); err != nil || len(data) != hostOutputLimit {
		t.Fatalf("an inventory at the limit: %d bytes, %v", len(data), err)
	}
	t.Setenv("EVA_MD_TEST_BYTES", fmt.Sprint(hostOutputLimit+1))
	if data, err := native(ctx, host, "plugin", "list", "--json"); err == nil {
		t.Fatalf("an inventory past the limit was kept: %d bytes", len(data))
	}
	t.Setenv("EVA_MD_TEST_STREAM", "stderr")
	t.Setenv("EVA_MD_TEST_BYTES", fmt.Sprint(3*hostOutputLimit/2))
	_, err := native(ctx, host, "plugin", "list", "--json")
	var e *hostError
	if !errors.As(err, &e) || len(e.diagnostics) != hostOutputLimit {
		t.Fatalf("diagnostics past the limit: %v", err)
	}
}

// Installs, uninstalls and record reads that take the install lock of a new
// folder at once each get it in turn, never an error: on macOS, a file
// created through os.Root without O_EXCL can fail with ENOENT for the caller
// that loses the race.
func TestNewInstallLockTakesConcurrentCreators(t *testing.T) {
	base := t.TempDir()
	for i := 0; i < 20; i++ {
		dir := filepath.Join(base, fmt.Sprint("locks-", i))
		start := make(chan struct{})
		var wg sync.WaitGroup
		for worker := 0; worker < 8; worker++ {
			wg.Go(func() {
				<-start
				release, err := lock(context.Background(), dir)
				if err != nil {
					t.Errorf("lock %d: %v", i, err)
					return
				}
				release()
			})
		}
		close(start)
		wg.Wait()
		if t.Failed() {
			return
		}
	}
}
