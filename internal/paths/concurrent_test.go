package paths

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestConcurrentRootOpen(t *testing.T) {
	isolate(t)
	dir := filepath.Join(t.TempDir(), "root")
	var wg sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		wg.Go(func() {
			for i := 0; i < 1000; i++ {
				root, err := OpenRoot(dir, true)
				if err != nil {
					t.Error(err)
					return
				}
				if err := root.MkdirAll("general", 0o700); err != nil {
					root.Close()
					t.Error(err)
					return
				}
				f, err := root.OpenFile("general/file", os.O_CREATE|os.O_EXCL|os.O_APPEND|os.O_WRONLY, 0o600)
				if os.IsExist(err) {
					f, err = root.OpenFile("general/file", os.O_APPEND|os.O_WRONLY, 0)
				}
				if err != nil {
					_, serr := root.Stat("general")
					_, perr := os.Stat(filepath.Join(dir, "general"))
					t.Errorf("open failed iteration %d: %v; rooted stat %v; absolute stat %v", i, err, serr, perr)
					root.Close()
					return
				}
				f.Close()
				root.Close()
			}
		})
	}
	wg.Wait()
}
