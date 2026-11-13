// Package cindervale keeps downloaded pages in a cache folder.
package cindervale

import (
	"os"
	"path/filepath"
)

// Dir returns the cache folder, under the home folder.
func Dir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".cindervale", "cache")
	return dir, nil
}
