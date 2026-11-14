# The headless runners: their offline checks

- **Component:** Updater: the headless runners' offline checks
- **Paths:** `internal/update`
- **Status:** v1 as built, frozen on 2026-11-14

The blocks below are v1's verification text, moved here word for word when the two documents were split, then out of [update-model.md](update-model.md) on 2026-11-14 to make room for its list of what must be kept; [INDEX.md](INDEX.md) lists every spec.

<!-- v1 docs/verification.md 017 -->
- **Headless runners** (`internal/update`).
  Fake `claude` and `codex` scripts on a PATH of only them and the system folders record what they are given.
  The tests check the exact arguments of both commands, with `--strict-mcp-config` after `--tools ""` on Claude and `codex --no-daemon exec` with the plan tool off on Codex; the cleared environment (no API key, no inherited CLAUDECODE or CLAUDE_CODE_* variable, `TERM=dumb`, `EVA_MD_INNER=1` and
  `CLAUDE_CODE_DISABLE_AUTO_MEMORY=1` added, the CA, proxy and config-folder variables kept); the batch on stdin only, never in an argument; Claude's envelopes classified (the structured output, a missing one, error subtypes, permission denials as `tool_used`, API error statuses, token counts) and
  Codex's event stream (a tool, including a todo_list item, stopping the run at once, unknown events refused, error items kept as diagnostics, a stale answer never reused); failure wording sorted into `auth`, `rate_limit`, `host_error`, `model_rejected` and `mcp_failed`; a refused effort stepping down
  the host's table one entry at a time, then running without the effort flag, then without the model, within one shared timeout and with a Degraded note; a timeout or cancel killing the CLI's whole process group; a run whose time ran out before its CLI started a timeout, not an error, with the CLI
  never run (`TestTimeoutBeforeTheCLIStartsIsATimeout`); a missing CLI as `cli_missing`; and the retry without `--strict-mcp-config` under a managed MCP config.
  The fake `codex` answers `codex features list`: only the tool features it lists are disabled, whether on or off, an unlisted name and listed features that give no tool are not, and an empty list, one with no feature line or a list command that fails gives the fixed five
  (`TestCodexDisablesTheToolFeaturesCodexLists`); a list that hangs ends at its own held timeout and the run goes on with the fixed five (`TestCodexFeaturesListThatHangsFallsBack`); the command carries a `--disable` for exactly the features the list names, the fixed five first, and none for a feature the list lacks (`TestCodexArgumentsMatchTheDesign`).

<!-- /v1 -->

## Must keep

The items this component must keep, and the tests that hold them, are listed in [update-model.md](update-model.md#must-keep).

## Since the split

No change yet.
