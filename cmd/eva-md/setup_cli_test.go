package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// What gh or glab prints is kept up to the limit and the command fails past
// it, however much the CLI prints and whether or not its output is also
// sorted: os/exec copies it through the buffer's Write, never a ReadFrom
// that would keep all of it.
func TestCLIOutputIsCapped(t *testing.T) {
	isolated(t)
	if err := os.WriteFile(filepath.Join(pathFolder(), "gh"), []byte("#!/bin/sh\nexec /usr/bin/head -c \"$EVA_MD_TEST_BYTES\" /dev/zero\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	c, ctx := repoCLI{name: "gh"}, context.Background()
	t.Setenv("EVA_MD_TEST_BYTES", fmt.Sprint(cliOutputLimit))
	if out, err := c.run(ctx, repoCLITimeout, "repo", "view"); err != nil || len(out) != cliOutputLimit {
		t.Fatalf("output at the limit: %d bytes, %v", len(out), err)
	}
	t.Setenv("EVA_MD_TEST_BYTES", fmt.Sprint(cliOutputLimit+1))
	if out, err := c.run(ctx, repoCLITimeout, "repo", "view"); err == nil {
		t.Fatalf("output past the limit was kept: %d bytes", len(out))
	}
	if out, err := c.runSorted(ctx, repoCLITimeout, &sortBuffer{}, "repo", "create"); err == nil {
		t.Fatalf("sorted output past the limit was kept: %d bytes", len(out))
	}
}

// Setup's signing line reads git's settings with the environment the daily
// commit's git gets, so signing that only config carried in the
// environment, or another repository's config, turns on doesn't count, as
// it doesn't for the commit.
func TestGitSigningReadsWithTheCommitsEnvironment(t *testing.T) {
	isolated(t)
	home := os.Getenv("HOME")
	if err := os.WriteFile(os.Getenv("GIT_CONFIG_GLOBAL"), []byte("[commit]\n\tgpgsign = false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(home, "other.git")
	for _, args := range [][]string{{"init", "--bare", "-q", other}, {"-C", other, "config", "commit.gpgsign", "true"}} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	for _, env := range [][][2]string{
		{{"GIT_CONFIG_COUNT", "1"}, {"GIT_CONFIG_KEY_0", "commit.gpgsign"}, {"GIT_CONFIG_VALUE_0", "true"}},
		{{"GIT_CONFIG_PARAMETERS", "'commit.gpgsign'='true'"}},
		{{"GIT_DIR", other}},
	} {
		t.Run(env[0][0], func(t *testing.T) {
			for _, kv := range env {
				t.Setenv(kv[0], kv[1])
			}
			if got := gitSigning(context.Background(), home); got != "" {
				t.Fatalf("signing %q, from %s", got, env[0][0])
			}
		})
	}
}
