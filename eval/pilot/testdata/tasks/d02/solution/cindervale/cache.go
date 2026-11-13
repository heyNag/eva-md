// Package cindervale keeps downloaded pages in a cache folder.
package cindervale

import (
	"os"
	"path/filepath"
)

// Dir returns the cache folder, made if missing: the folder TOVRANE_CACHE_DIR names, or one under the home folder.
func Dir() (string, error) {
	if dir := os.Getenv("TOVRANE_CACHE_DIR"); dir != "" {
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
