// Package cindervale keeps downloaded pages in a cache folder.
package cindervale

import (
	"os"
	"path/filepath"
)

// Dir returns the cache folder, made if missing: the folder CINDERVALE_CACHE names, or one under the home folder.
func Dir() (string, error) {
	if dir := os.Getenv("CINDERVALE_CACHE"); dir != "" {
		return dir, os.MkdirAll(dir, 0o755)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".cindervale", "cache")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}
