# The contract spike's run plan

The calls and tokens of every arm of the contract spike's live run (plan section 8, Stage 0, row DV-03), as `eva-eval spike plan` works them out before anything is called. `eval/spike/run-plan.md` is this plan at the defaults. Each arm is run by `eva-eval spike run`, which calls nothing without `--spend-model-quota`.

## Rules and defaults

- **Arms.** Every host eva-md knows (claude-code, codex), every tier (small, middle, large), every effort asked for (low, medium; `--efforts`, default low,medium) and both forms of the contract (quotes, segments): 24 arms. The tier is all a row says of its model.
- **Calls.** An arm makes one first call for each pass of the spike's 19 scripts, 28 passes, in each round, and runs 1 round (`--repeat`, default 1). It makes at most one repair call for each first call, and this plan counts every one.
- **Tokens.** A first call's input is the bytes of the prompt, of the pass rendered with nothing shown and of the answer's schema, divided by 4 and rounded up, plus the host's overhead per call. A repair call's input adds the output allowance to its first call's. Each call's output is the output allowance, 2,000 tokens (`--output-allowance`, default 2,000). Tokens are input and output together.
- **Overhead.** The host's own input per call: claude-code 0, codex 0 (`--overhead host=n,...`, default 0). It is unknown until DV-02's recordings show what each host adds to a call.
- **Spoken scripts.** They are not counted: they wait on the wrapper HF-00 records, and the row allows about a tenth more for them.
- **The row's allowance** is about 1,900 calls and 25,000,000 tokens over both hosts (plan section 8, row DV-03). Where the run happens, and its price, are the lead's to state; the run is the owner's to allow.

## Scripts

| Script | Passes | First calls' input tokens, quotes | First calls' input tokens, segments |
| --- | --- | --- | --- |
| v1.01 | 1 | 2,571 | 2,533 |
| v1.02 | 1 | 2,561 | 2,522 |
| v1.03 | 2 | 5,100 | 5,020 |
| v1.04 | 2 | 5,104 | 5,023 |
| v1.05 | 1 | 2,564 | 2,524 |
| v1.06 | 1 | 2,598 | 2,563 |
| v1.07 | 1 | 2,594 | 2,559 |
| v1.08 | 2 | 5,150 | 5,078 |
| v1.09 | 2 | 5,113 | 5,034 |
| v1.10 | 3 | 7,665 | 7,547 |
| v1.11 | 3 | 7,695 | 7,583 |
| v1.12 | 2 | 5,134 | 5,056 |
| assent.01 | 1 | 2,891 | 2,908 |
| assent.02 | 1 | 2,907 | 2,921 |
| assent.03 | 1 | 2,874 | 2,885 |
| assent.04 | 1 | 2,903 | 2,921 |
| assent.05 | 1 | 2,867 | 2,878 |
| assent.06 | 1 | 2,873 | 2,885 |
| bounds.01 | 1 | 3,143 | 3,223 |

Overhead is left out here; the arms add it.

## Arms

| Host | Tier | Effort | Form | First calls | Repair calls, at most | Tokens in | Tokens out | Tokens |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| claude-code | small | low | quotes | 28 | 28 | 204,614 | 112,000 | 316,614 |
| claude-code | small | low | segments | 28 | 28 | 203,326 | 112,000 | 315,326 |
| claude-code | small | medium | quotes | 28 | 28 | 204,614 | 112,000 | 316,614 |
| claude-code | small | medium | segments | 28 | 28 | 203,326 | 112,000 | 315,326 |
| claude-code | middle | low | quotes | 28 | 28 | 204,614 | 112,000 | 316,614 |
| claude-code | middle | low | segments | 28 | 28 | 203,326 | 112,000 | 315,326 |
| claude-code | middle | medium | quotes | 28 | 28 | 204,614 | 112,000 | 316,614 |
| claude-code | middle | medium | segments | 28 | 28 | 203,326 | 112,000 | 315,326 |
| claude-code | large | low | quotes | 28 | 28 | 204,614 | 112,000 | 316,614 |
| claude-code | large | low | segments | 28 | 28 | 203,326 | 112,000 | 315,326 |
| claude-code | large | medium | quotes | 28 | 28 | 204,614 | 112,000 | 316,614 |
| claude-code | large | medium | segments | 28 | 28 | 203,326 | 112,000 | 315,326 |
| codex | small | low | quotes | 28 | 28 | 204,614 | 112,000 | 316,614 |
| codex | small | low | segments | 28 | 28 | 203,326 | 112,000 | 315,326 |
| codex | small | medium | quotes | 28 | 28 | 204,614 | 112,000 | 316,614 |
| codex | small | medium | segments | 28 | 28 | 203,326 | 112,000 | 315,326 |
| codex | middle | low | quotes | 28 | 28 | 204,614 | 112,000 | 316,614 |
| codex | middle | low | segments | 28 | 28 | 203,326 | 112,000 | 315,326 |
| codex | middle | medium | quotes | 28 | 28 | 204,614 | 112,000 | 316,614 |
| codex | middle | medium | segments | 28 | 28 | 203,326 | 112,000 | 315,326 |
| codex | large | low | quotes | 28 | 28 | 204,614 | 112,000 | 316,614 |
| codex | large | low | segments | 28 | 28 | 203,326 | 112,000 | 315,326 |
| codex | large | medium | quotes | 28 | 28 | 204,614 | 112,000 | 316,614 |
| codex | large | medium | segments | 28 | 28 | 203,326 | 112,000 | 315,326 |

## Totals

| Host | Arms | Calls, at most | Tokens in | Tokens out | Tokens |
| --- | --- | --- | --- | --- | --- |
| claude-code | 12 | 672 | 2,447,640 | 1,344,000 | 3,791,640 |
| codex | 12 | 672 | 2,447,640 | 1,344,000 | 3,791,640 |
| all | 24 | 1,344 | 4,895,280 | 2,688,000 | 7,583,280 |

Against the row's allowance: 1,344 calls at most, under it, the row's being about 1,900; 7,583,280 tokens at most, under it, the row's being about 25,000,000.
