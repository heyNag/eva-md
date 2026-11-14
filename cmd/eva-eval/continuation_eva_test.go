package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// argsFor is the rig's run with the condition cond in place of off.
func (r contRig) argsFor(cond string, more ...string) []string {
	args := r.args(more...)
	i := slices.Index(args, "--condition")
	args[i+1] = cond
	return args
}

// nothingRan fails when the fake host ran or the run made a folder.
func (r contRig) nothingRan(t *testing.T) {
	t.Helper()
	if calls := r.calls(t); len(calls) != 0 {
		t.Fatalf("the fake ran: %+v", calls)
	}
	for _, p := range []string{filepath.Join(r.base, "work"), filepath.Join(r.base, "out")} {
		if _, err := os.Lstat(p); !os.IsNotExist(err) {
			t.Fatalf("%s was made", p)
		}
	}
}

// continuation run --condition eva, given leave to spend, refuses an
// eva-md that is not the evaluation build, a --pass-env of either of the
// seam's variables, and no --eva-md at all, and starts nothing.
func TestContinuationRunEvaRefuses(t *testing.T) {
	r := newContRig(t)
	t.Setenv("CLAUDE_CODE_ENTRYPOINT", "cli")
	t.Setenv("EVA_MD_EVAL_ACTOR", "person")
	for _, c := range []struct {
		args  []string
		usage bool
		want  string
	}{
		{r.argsFor("eva", "--eva-md", r.cli, "--spend-model-quota"), false, "the condition eva: --eva-md is not the evaluation build: it was built without the tag evamd_eval"},
		{r.argsFor("eva", "--eva-md", r.cli, "--pass-env", "CLAUDE_CODE_ENTRYPOINT", "--spend-model-quota"), false, "--pass-env CLAUDE_CODE_ENTRYPOINT is the evaluation seam's"},
		{r.argsFor("eva", "--eva-md", r.cli, "--pass-env", "EVA_MD_EVAL_ACTOR", "--spend-model-quota"), false, "--pass-env EVA_MD_EVAL_ACTOR is the evaluation seam's"},
		{r.argsFor("eva", "--spend-model-quota"), true, "--condition eva needs --eva-md"},
		{r.argsFor("eva", "--eva-md=", "--spend-model-quota"), true, "--condition eva needs --eva-md"},
	} {
		code, out, errOut := cmd(c.args...)
		if code != 2 || !strings.Contains(errOut, c.want) || strings.Contains(errOut, "usage:") != c.usage {
			t.Fatalf("%v: exit %d\n%s\n%s", c.args, code, out, errOut)
		}
		if strings.Contains(out, "run ") {
			t.Fatalf("a run started:\n%s", out)
		}
	}
	r.nothingRan(t)
}

// --sign-in is taken out of run's arguments, in either form, before run
// reads them, and is refused given twice, with no path, or with another
// condition; the usage lists it.
func TestContinuationRunSignInFlag(t *testing.T) {
	r := newContRig(t)
	program := filepath.Join(r.base, "sign-in")
	for _, args := range [][]string{
		r.argsFor("eva", "--eva-md", r.cli, "--sign-in", program, "--spend-model-quota"),
		r.argsFor("eva", "--eva-md", r.cli, "-sign-in="+program, "--spend-model-quota"),
	} {
		// The run gets as far as the condition's check, which refuses the
		// untagged eva-md: the flag was not read as one of run's.
		code, _, errOut := cmd(args...)
		if code != 2 || !strings.Contains(errOut, "evaluation build") {
			t.Fatalf("%v: exit %d\n%s", args, code, errOut)
		}
	}
	for args, want := range map[*[]string]string{
		ptr(r.argsFor("eva", "--eva-md", r.cli, "--sign-in", program, "--sign-in", program)): "--sign-in is given twice",
		ptr(r.argsFor("eva", "--eva-md", r.cli, "--sign-in")):                                "--sign-in needs a program's path",
		ptr(r.argsFor("eva", "--eva-md", r.cli, "--sign-in=")):                               "--sign-in needs a program's path",
		ptr(r.args("--sign-in", program, "--spend-model-quota")):                             "--sign-in is for --condition eva alone",
		ptr(r.argsFor("oracle", "--sign-in", program)):                                       "--sign-in is for --condition eva alone",
	} {
		code, _, errOut := cmd(*args...)
		if code != 2 || !strings.Contains(errOut, want) || !strings.Contains(errOut, "usage:") {
			t.Fatalf("%v: exit %d\n%s", *args, code, errOut)
		}
	}
	code, _, errOut := cmd("continuation")
	if code != 2 || !strings.Contains(errOut, "[--sign-in PATH, with --condition eva]") {
		t.Fatalf("exit %d\n%s", code, errOut)
	}
	r.nothingRan(t)

	rest, path, msg := takeSignIn([]string{"--condition", "eva", "--sign-in", "/made/up", "--", "--sign-in", "x"})
	if msg != "" || path != "/made/up" || !slices.Equal(rest, []string{"--condition", "eva", "--", "--sign-in", "x"}) {
		t.Fatalf("%q %q %q", rest, path, msg)
	}
	if got := lastValue([]string{"--condition", "off", "-condition=eva", "--", "--condition", "oracle"}, "condition"); got != "eva" {
		t.Fatalf("the last condition: %q", got)
	}
}

func ptr(s []string) *[]string { return &s }
