package main

import (
	"strings"
	"testing"
)

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
