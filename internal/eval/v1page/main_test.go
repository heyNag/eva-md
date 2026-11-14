package v1page_test

import (
	"os"
	"testing"

	"github.com/heyNag/eva-md/internal/testgitenv"
)

// TestMain clears the git configuration and commit identity the environment
// carries before any test runs, since the tests link code that starts git.
func TestMain(m *testing.M) {
	testgitenv.Clear()
	os.Exit(m.Run())
}
