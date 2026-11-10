# Critique of the six v2 designs: simplicity and scope

Lens: is each rebuild the smallest design that meets its bar. Read: the design brief, the lead's notes, all six designs, the capture audit with its verification, parts of the storage and product audits, the memory-frameworks and coding-memory-tools research notes, and the code at the current head (line counts by `wc`, v1 page layout in `docs/design.md`). Nothing in the repository was changed.

## 1. Verdict

Each design is careful, and each one alone reads as defensible. Together they are not a plan the owner can agree to or a team can finish. The six sections add up to a second system built beside the first: more parts, more commands, more states and more owner decisions than v1 has, justified mostly by reach and robustness that nobody has measured a need for.

The evidence the plan itself cites says what to build: a compact handoff brief, small cited statements checked by code, the raw record kept, and proof. Four things in the designs serve that directly and should be kept whole: the extraction contract with exact quotes and tags from the record, the typed brief built from a model-free handoff, a per-pass commit with progress stored beside the memory it describes, and a small harness that gives v1 a baseline. Most of the rest is second-order: multi-machine sync, background self-install with rollback, a health mesh, a job scheduler, rule enforcement, agent write tools, a lifecycle for candidates and disputes, lexical push tiers, and an evaluation program sized for a lab.

The plan needs a v2.0 cut line, a size budget that is tested, and a cut-over that is not four areas at once.

## 2. The census

| Count | Value | Source |
| --- | --- | --- |
| v1 non-test Go | 54,807 lines (tests 64,600) | `wc -l` over `cmd` and `internal` |
| Work packages | 128: 44 S, 71 M, 13 L | the six package tables |
| Rough effort at the designs' own bounds | about 100 agent-weeks, before the protocol's two reviews per package | S 2 days, M a week, L more |
| Owner decisions | 51 listed (8, 8, 8, 9, 8, 10), about 47 distinct | the six decision tables |
| `eva-md` verbs named | 43, against 19 in v1's usage line; hosts' own bar is "a dozen" | grep of the designs |
| Typed health conditions | about 30 | hosts 7.4, store S-H3, derive D-H3, deliver V-H5 |
| Evaluation suites, arms, model backends | 18, 5, 3 | evaluate sections 6, 4.5, 4.2 |
| Evaluation spend proposed | 100 M tokens a week standing, 140 M a minor release, 150 to 250 M per host for baselines, 350 M for the claim run | evaluate 9, EV-D2 |
| Line estimates given | two: store "about 3,600", deliver "about 5,000 out, about as many in" | store section 2, deliver 16.5 |
| Size of the designs | 608 KB, against a 24 KB bar for what an agent should read and a 390 KB v1 design called unusable | `wc -c`; evaluate section 1 |

No design gives a net line count. EV-D9 builds v2 "in new packages beside the old ones, behind flags that default to v1's behaviour", so the tree holds both systems until the cut-over packages (ST-13, DV-26, LG-21's deletions), and the store keeps the v1 page parsers in the tree afterwards as the migrator's readers. The evaluation harness adds five packages and a command that never ship and still have to be kept working. By my estimate the end state is 20 to 35% larger than v1 and the peak is close to double. The brief's own test is that a plan which doubles the codebase has failed.

## 3. Keep, shrink or drop, per rebuild

### Ledger

| Rebuild | Call | Simpler alternative |
| --- | --- | --- |
| Event model and one reader API (LG-10) | keep | make its v1 backend (seq equals line) the contract every other area builds on |
| Provenance by allowlist, one wrapper list, adapters (LG-13) | keep | none needed; it fixes a confirmed high finding |
| New kinds through hooks: dialog answers, plan decisions, subagent reports, failure digests (LG-23) | keep | hooks only at first; no segments |
| Spool, ingester, reconciliation, release rules R1 to R7, session-keyed layout (LG-20, LG-21) | shrink | move v1's transcript pass out of the hook into the detached job; build hold-and-release only if LG-01 and LG-02 show the rates evaluate's row 5 names |
| Project registry with aliases (LG-22) | keep | an alias table over v1 keys is enough until the layout moves |
| Privacy controls tightened (LG-24) | keep | small |
| Redaction driver, corpus, value classifier (LG-11, LG-12, LG-26) | keep | two policies (at rest, gate), not four |
| Secret handles, environment match, policy surface (LG-31, LG-32) | drop | nothing measured asks for them |
| Forget, tombstones, repair, expiry (LG-30) | shrink | forget by session, turns or event; no `--value`, no expiry in v2.0 |
| Backfill (LG-33) | drop | the owner ruled it out; derive's replay is a second history mechanism |

### Store

| Rebuild | Call | Simpler alternative |
| --- | --- | --- |
| Own codec, okf dropped (ST-03) | keep | six readers become one and a dependency goes |
| Owned repository, local-first (ST-05 part) | keep | it deletes more than it adds |
| Items as files (layout v2) | keep | note that v1's `knowledge/k<N>.md` is already one file per item; decisions and rules join that family |
| One commit per pass, coverage in the tree | keep | the one change that makes replay and undo true |
| Machine key, signing, trust set, quarantine (4.5, 8.6) | drop from v2.0 | unsigned commits under a machine author; decide signing after ST-00 says what the server accepts |
| Sync engine, join, migrate-then-merge, epoch and purge, checkout with `in/` branches (ST-09, ST-10, ST-11) | drop from v2.0 | push-only backup, as ST-D5's own fallback |
| Migrator framework proven on synthetic steps (ST-04) | shrink | one idempotent step 1 to 2 with a dry run, plus the `min_reader` check |
| Conformance suite with a reference model and 2 to 5 machines (ST-02, L) | shrink | single-machine properties only: crash safety, exactly once, protection, restore and replay |
| Evidence excerpts committed (ST-D6) | defer | references resolve on the capturing machine, which is the only machine in v2.0 |

### Derive

| Rebuild | Call | Simpler alternative |
| --- | --- | --- |
| Extraction contract v2: delta operations, exact quotes, tags from provenance, per-operation acceptance (DV-21) | keep | the best-evidenced rebuild in the plan; it replaces about 2,165 lines of word matching |
| Worker: computed pending work, one worker and one waiter, journal (DV-12) | keep | it ships on v1 first, which is the right pattern |
| Priorities, shares, quota gate, breaker probes, replay triggers (3.4 to 3.7, DV-22) | shrink | two priorities, one daily cap, one hold with backoff, the next real episode as the probe |
| Memory semantics: six kinds, lattice, candidates, disputes, archive, provisional (DV-23, DV-30, DV-31) | shrink | v1's four kinds, four states and two flags; anything without authority stays a cited note |
| Workstream chosen by a model on the default branch (6.6 rule 3) | shrink | branch and lineage binding only, until the field report gives the default-branch share |
| Review tier (DV-33) | drop | the designer doubts it; listing it as a package invites building it |
| Runner evolved (DV-11), system pages frozen (DV-35) | keep | both are small and one is a removal |

### Deliver

| Rebuild | Call | Simpler alternative |
| --- | --- | --- |
| Typed brief on the catalog and a compiled pack (DL-08, DL-10) | keep | the best-supported feature; three pack files, not five |
| Retrieval core with BM25F, gates G1 to G3 and a tier matrix (DL-06) | shrink | exact keys for push; v1's index scorer with the one tokenizer for pull |
| Context model by revision, planner, four logs, usage join (DL-09, DL-20) | shrink | ids per context window, reset on compaction, plus an owed flag and the delivery record |
| Pull tools `eva_recall` and `eva_open` (DL-12), access layer (DL-07) | keep | one small table |
| Tool-time delivery (DL-14) | shrink | a Claude Code experiment first; the Codex parser only after it earns it |
| Rule enforcement (DL-19) | drop | the hosts have this natively |
| Agent write tools (DL-17) | drop | the person's commands stay |
| Read-time delivery, host-native arms, resources, defaults from results (DL-21, DL-24, DL-22 part, DL-25) | drop | each is an idea with a trigger, not a package |

### Hosts

| Rebuild | Call | Simpler alternative |
| --- | --- | --- |
| Codex hooks in a frozen two-file plugin, tools in a free one (HO-20) | keep | fixes the one fault with a measured count |
| Generations, additive units, a renderer per generation (4.3) | shrink | freeze once; a later event is one more small plugin when it is needed |
| First run as `reconcile`, local-first (HO-21) | keep | removes about 1,000 lines of dialogue |
| Install manifest, host adapter and spec (HO-10, HO-11) | keep | static table; no per-session narrowing |
| Health: freshness ledger, conditions, channels (HO-12, HO-23) | shrink | lag against a budget per stage, one prepared line, three channels |
| Background install, soak, probation, rollback (HO-25, L) | shrink | check, verify, and an upgrade command that asks nothing |
| The tick (HO-22) | keep, one fallback | hook wake plus the OS tick; no timer in the MCP server |
| Reach beyond Linux, managed kit (HO-41 to HO-44) | drop from v2.0 | Linux assets only; the rest waits for the field report |

### Evaluate

| Rebuild | Call | Simpler alternative |
| --- | --- | --- |
| Black-box fidelity scorer, results rows, v1 baseline (EV-12, EV-14) | keep | this is the missing instrument |
| Retrieval set with negatives, latency gate (DL-04, EV-16) | keep | cheap and decisive |
| Continuation harness (EV-17, L) | shrink | 10 handoff and 10 hidden-decision tasks, arms off, eva and oracle |
| Scenario generator: 24 families, paraphrase bank, twins, simulated model with knobs, cassettes (EV-13, L) | shrink | v1's 12 scripts plus the six families the contract changes |
| Suites B, G, H, J, K and the act stage of I | defer | built when a decision waits on the number, as the designer's own rule says |
| Specs split, decision records (EV-10 part) | keep | |
| Guarantees as data with a named mutation each; six-step protocol per package (EV-10 part, EV-11) | drop, shrink | the test suite already pins the invariants; one review for small packages |
| Claim run, the word "best", a public evidence repository (EV-50, EV-D5, EV-D8) | drop | not a build decision |

## 4. Issues that would change the plan

### 1. No cut line and no size budget (cross-cutting, blocker)

What is wrong: the six designs are each complete, and nothing says which subset is v2.0. The census above is the result.

Evidence: 128 packages, 51 owner decisions, 43 verbs, two line estimates in 608 KB of design. v1 took 11 days. EV-D9 keeps both systems in the tree until cut-over.

Change: the plan names a v2.0 of roughly 40 packages and a "not in v2.0" appendix with the trigger that would promote each item. It adds one gate that a test enforces: shipped non-test Go at the end of each stage is no more than v1's 54,807 lines, and every rebuild package names the v1 files it deletes in the same change or the next.

### 2. Sync between machines drives most of the store, and is paid for before one machine works (store, blocker)

What is wrong: fetch and merge sits in Stage 4, but it shapes Stage 1 and Stage 2. The option table in store section 2 compares the three layouts on how two machines merge. The reference model and its 2 to 5 machines are an L package in Stage 1. The machine key, the trust set and the quarantine exist because "sync makes the remote an input". Committed excerpts exist because a reference "resolves only on the machine that captured the chat". Migration steps must be pure tree functions so two machines reach one tree.

Evidence: one install on one machine; store's field question 8 ("whether any second machine was ever pointed at this wiki") is unanswered; decision 1 rules sync out today. ST-D5's fallback says push-only "saves ST-09". It saves far more: ST-10, ST-11, the multi-machine half of ST-02, sections 8.5 to 8.7, ST-D4, ST-D6, DV-40, EV-40, HO-42 and contract S-D7.

Change: v2.0 is one writer with a push-only backup. Keep the cheap choices that leave the door open: ids that cannot collide, the writer in the episode file name, nothing derived in git. Sync becomes its own plan, opened when the owner has a second machine and single-machine v2 has run for a month.

### 3. A cut-over of four areas at once, whose binary the designs describe differently (cross-cutting, blocker)

What is wrong: store, derive and deliver "cut over together behind one flag"; the ledger cuts over from shadow and imports the inbox in the same window; hosts adds a swap, a day of probation on the old layout, then the migrations. That is the largest possible single change to a user's data.

Evidence: store section 13 point 9, derive section 12 step 7, ledger section 14, hosts 10.5. See contradiction 1: the cut-over binary must run a day on v1's layout and also has v1's paths deleted.

Change: shrink the bang to its core. Derive and deliver build on the ledger reader over the v1 inbox, so the ledger's layout change is a later, separate step. Hosts' packaging, health and upgrade work land on v1 and are not part of it. With background install dropped (issue 6), the migration runs when a person or an agent runs the upgrade, and the way back is the untouched old clone and inbox, which both designs already keep.

### 4. The evaluation program is a second product (evaluate, major)

What is wrong: principle C needs instruments. It does not need 18 suites, five arms, three model backends, a scenario generator with 24 families, cassettes, a results store that tracks staleness by git path, generated defaults, two canaries (one spending about 3 M tokens a day), and a claim run.

Evidence: 24 packages, two of them L, all in the path of Stage 2 ("EV-14, EV-15 and EV-17 before the Stage 2 package each one judges"). The designer's first risk is "the evaluation becomes a second product". The budget table asks for standing leave of 100 M tokens a week.

Change: Stage 1 builds four instruments and gives v1 a row on each: the black-box fidelity scorer on v1's 12 scripts and about six new families; the labelled retrieval set; the latency gate; and 20 continuation tasks under off, eva and oracle, with the host's own memory as a fourth arm only on the Mac. Everything else is built when a named decision waits on it. The signed-in canary runs when a host version changes, not daily.

### 5. The build process costs more than many of the packages it governs (evaluate, major)

What is wrong: every guarantee becomes a JSON record with tagged tests and "a named mutation that kills it"; every package, including 44 that are two days or less, gets a spec pull request, a second-agent review, a build, an adversarial review, integration and a live proof step.

Evidence: evaluate 12.1 and 12.2. The guarantee import covers v1's table, every audit's must-keep list and every verification bullet, which is several hundred records before any memory improves. v1's 64,600 lines of tests already pin these invariants, as the ledger design says of the redaction cases.

Change: keep the split of the v1 documents, dated decision records, per-component specs and the ownership check in CI. Drop guarantees as data and the mutation bookkeeping; each spec lists the must-keep items it touches and the tests that hold them. Spec and review steps apply to packages that define a contract between areas; a small package gets one review.

### 6. Background self-install with probation and rollback (hosts, major)

What is wrong: HO-25 is the area's one L package. It reverses a hard rule, adds a supply-chain channel, a hand-written signature parser, soak, yank, probation with four regression rules, automatic rollback, pinning, and the rule that releases N and N-1 read each other's state. It also forces "swap, probation, then migrate" onto every other area.

Evidence: hosts 10.2 says of soak that "with no telemetry this protects only if the maintainer sees a bad release". There is one install. The designer's own smaller change "gets the signature and the question-free upgrade".

Change: v2.0 ships `updates = "check"`: the signed manifest (HO-13), a daily check from the worker, one line after seven days, and `eva-md upgrade` that asks nothing so an agent can run it. The previous binary is kept for a manual rollback. Background install is reconsidered when there are users whose fixes are waiting.

### 7. A lifecycle built for a corpus nobody has seen (derive, major)

What is wrong: items get six kinds, five tags plus two more for outside and echoed text, a four-by-five lattice, eight states and flags, candidates that are stored but never shown, promotion at two independent sessions, expiry at 90 days, archive at 180 days fed by a usage join, disputes kept as records, and knowledge that is provisional until a branch merges. Eight housekeeping steps maintain it.

Evidence: the derive design calls PGS-02 "a prediction until a live run exists". Evaluate's row 8 allows that the real wiki holds "none or a handful" of knowledge pages. The research note records Mem0's walk-back to add-only writes, and the brief's fact 3 is that these systems fail by the form and volume of what they store.

Change: v2.0 stores as an item only what has authority (stated, accepted, or observed in a tool record). Everything else is a cited note in the session's log, where pull can find it. That removes the candidate state, support counting, expiry, archive and the usage join. Kinds are v1's four; `lesson` arrives with failure digests if suite C shows a gain; `preference` is a constraint. Housekeeping is exact duplicates and folding merged branches. The rest waits for the corpus counters (DV-32), the same gate the designer already applies to the review tier. DV-33 leaves the package list.

### 8. A job scheduler for one user (derive, major)

What is wrong: seven priority levels, round-robin fairness, budget shares of 30% and 15%, a quota gate at 25% and 10%, nine outcome classes, a breaker with half-open probes that spend model calls on a fixture, four replay triggers and a registration interface for other areas' jobs.

Evidence: the workload is tens of episodes a day, run one call at a time. Derive 3.5 says whether either host exposes its remaining allowance "is not established". The three findings behind the rebuild (lost wakes, unrecorded refusals, five places of job state) are fixed by computed pending work, the two locks and the journal alone.

Change: keep those three. Shrink the rest to two priorities (the chat that just ended, then newest first), one daily token cap, one hold with backoff after a rate limit or a sign-in failure, and a breaker that pauses after three host-shaped failures and resumes on a version change. The control rule already works without a fixture: a range is blamed only after another range succeeded in between.

### 9. The ingester is gated on rates that its dependents ignore (ledger, major)

What is wrong: the ledger design says "do not build the whole of this before the rates exist", and evaluate's row 5 would move the ingester behind derive. But DV-20 depends on LG-21, and DL-09, DL-10 and DL-11 depend on LG-20, LG-21 and LG-23. The gate cannot fire.

Evidence: the capture audit's verifier scoped capture-2 to medium: "No rate exists either way", capture measures 25 to 30 ms of a 300 ms budget, and the single ingester "takes the transcript as its source" while "the hook payload fields are the documented contract".

Change: LG-10's reader over the v1 inbox is what derive and deliver depend on. New kinds arrive through hooks. The transcript pass moves out of the hook into the detached job, which is an evolution of `internal/catchup` and removes the slicing code the design already lists. The spool, hold-and-release and the session-keyed layout are built only if the counted week shows 2% or more of events held or tool coverage under 95%.

### 10. Lexical push tiers built to sit in shadow (deliver, major)

What is wrong: gates G2 and G3, margins, priors, six field weights and a three-by-four matrix of statement, pointer and silence are designed, built and tuned, and by the design's own account may never be turned on.

Evidence: deliver section 15 point 1: "If that number is never reached, the fallback is start, first prompt, keys and pull, with no lexical push at all." The brief calls unsolicited per-prompt injection the riskiest feature. For push, only G1 (an exact path, symbol or command key) is on by default.

Change: v2.0 push is the start brief, first-prompt routing and exact keys. The labelled set (DL-04) is still built, and v1's recall is scored on it. Pull keeps `internal/searchindex`'s scorer with the one tokenizer and fields. BM25F and the lexical gates are proposed again only with a held-out number that asks for them.

### 11. Rule enforcement is a different product (deliver, major)

What is wrong: a check grammar, a command-prefix matcher that skips `cd`, environment assignments and `sudo`, `rule arm`, `disarm` and `list`, and a hook that answers a tool call with a permission question. It ends the rule that a hook never returns a decision.

Evidence: deliver section 8 itself notes both hosts point users to their own pre-tool hooks and permission rules for anything that must hold. No measurement on eva-md shows compliance is the limit.

Change: drop DL-19 and DL-D2 from v2. Suite G, when it exists, reports seen against obeyed. If that is the limit, the first step is to print the host's own rule for the person to add.

### 12. Agent write tools that by design do almost nothing (deliver, major)

What is wrong: `eva_note` and `eva_forget` need an authority check that searches ten minutes of typed prompts for an exact quote, a new ledger event kind, candidates, disputes and a narrowed "the server only reads".

Evidence: deliver section 11: "A typed 'remember that we never do X' is caught by capture and the fast pass with no tool at all"; a tool's remember is "a candidate, which is never pushed"; a tool's correct or forget of what the user owns "changes nothing".

Change: drop DL-17's tools and DL-D3. Keep the person's four commands, which an agent can run when asked, as it runs `eva-md exclude`. With issue 7's change there is no candidate state for a tool note to land in.

### 13. Delivery bookkeeping outweighs what is delivered (deliver, major)

What is wrong: a per-chat model of items held and seen by revision, lines that correct or retract an item in the middle of a chat, a decision log, a delivery record, a pull log joined to the chat by a token the capture adapter copies out of a tool result, a background usage join and a receipts command.

Evidence: after the design's own cuts a later prompt "usually" gets nothing. The usage counts feed only archive by disuse and tie order in the map (13). The ledger already marks a turn that made an eva-md lookup, which is all derive's echo rule needs.

Change: the context model is a set of ids per context window, reset on compaction, plus the owed-brief flag. The delivery record stays. The decision log exists only for a feature in shadow. No pull token, no usage join, no mid-chat correction or retraction lines.

### 14. Health is a monitoring mesh for a product whose bar is silence (hosts, major)

What is wrong: a freshness ledger, about 30 typed conditions with severity, relevance, a four-step decay, dormancy and acknowledgement, three remediation classes, five channels including OS notifications, three observers that watch each other, and the MCP server as an evaluator.

Evidence: the measured faults are three: missing work is never noticed (OPS-01), the first notice hides the rest and never expires (OPS-04), and health is computed in the chat-start hook. Lag against a budget per stage fixes the first, a relevance window and "most severe, once a day" fix the second, and one prepared file fixes the third.

Change: build those three. Channels are a chat-start line on either host, one line in the first pull result when that chat's hooks are silent, and `status`. Drop OS notifications (HO-D4), the evaluator and health writes in the MCP server (HO-D7), the decay ladder and `--quiet`.

### 15. Gated, optional and experimental features are still packages (cross-cutting, major)

What is wrong: about twenty packages are described as shadow only, built last, optional, an experiment, or built only if a number says so. Each still costs a spec, a flag, a test and a place in the dependency graph, and each `off | shadow | on` flag is a path that must be kept working.

Evidence: DL-21, DL-22's resources, DL-23, DL-24, DL-25, the steering sentence in DL-16, DV-33, DV-34, LG-31, LG-32, LG-33, ST-10, ST-11, HO-41 to HO-44, EV-40, EV-50, and the embedding trigger.

Change: move them to the appendix of issue 1 with one line each: the measurement that would open it. They get no package id until then.

### 16. Hook processes per turn rise and nobody budgets them (cross-cutting, major)

What is wrong: capture asks for PostToolUse on every tool with no matcher on both hosts, plus SubagentStop and PostCompact; deliver adds a synchronous PreToolUse on every edit and shell command and SubagentStart. v1 declares seven events with tool matchers. On Codex each run starts through a shell and re-hashes the plugin.

Evidence: ledger C-H1; hosts 4.3 and its risk 13; deliver 7.6. The ingester also reads the same tool rows from the host's record ("source: hook, record"), so successful tool calls have two sources. The only latency bars are per hook.

Change: before any declaration is frozen, the benchmark gains a per-turn figure: processes started and summed milliseconds for a turn with 50 tool calls, on each host's launch path. One source is chosen for successful tool calls. v1's matchers stay unless that figure allows more.

### 17. Generations of the Codex plugin, and reach beyond Linux (hosts, minor)

What is wrong: the two-plugin split is right. The framework around it (generation 0 and 1, additive frozen units, a renderer for every generation in the field, `upgrade --codex-hooks`) plans for an installed base that does not exist. Reach adds a generic adapter, `connect`, a desktop bundle, a cloud spike and a managed kit.

Evidence: one install; v2 is unpublished; hosts' own risk 11 is "reach consumes effort before the core is proven", and it calls the MCP-only floor's value "real and small".

Change: freeze the hooks plugin once, after Stage 0's hook-start measurement, with the bundle-hash test. A later event is a second small plugin, designed when needed. v2.0 reach is Linux release assets.

### 18. Redaction gains a policy system (ledger, minor)

What is wrong: three tiers by four policies, HMAC handles under a per-machine key, a match against the process environment, custom patterns, an allowlist and optional personal-data classes.

Evidence: the measured faults are cost, no deadline, masked `key: value` facts and the 19% hex rate. The design's own section 10.8 declines two other ideas because "no measurement has shown" the case; the same test removes these.

Change: keep LG-11, LG-12 and LG-26 with two policies. Drop LG-31, LG-32 and LG-D6.

### 19. Forgetting is three mechanisms in three areas, and capture keeps text nothing reads (ledger, store, deliver, minor)

What is wrong: tombstones, generations and repair in the ledger; retraction, epoch and a forced push in the store; retraction lines pushed into chats by deliver. Separately, assistant text between tool calls is captured although derive may not read it, and it brings an expiry rule and the largest growth in text at rest.

Evidence: ledger 9, store 8.7, deliver 6.1; ledger 4.1 ("the plan should not let derive read them until an evaluation shows they improve").

Change: v2.0 forgetting is a tombstone for the events and a retraction of the items that cite only them. No purge, no `--value`, no pushed retraction. Segments are not captured until an evaluation arm asks for them; `capture.detail` has two values.

### 20. Three ways to wake the worker (derive, hosts, minor)

What is wrong: a hook spawn, a launchd tick, and a timer in the per-chat MCP server that starts a tick every ten minutes.

Evidence: derive 3.8, hosts 6. If the owner allows the tick, the MCP timer adds nothing a hook wake does not. If the owner declines, hooks alone are v1's behaviour, which the owner has already accepted.

Change: two paths. The MCP server starts nothing.

## 5. Contradictions between designs

1. **What the cut-over binary contains.** Hosts 10.5: migrations run only after a clean probation, "during probation the new binary works on the old layout", and releases N and N-1 read each other's state. Store ST-13, derive DV-26 and ledger LG-21 delete v1's write paths at cut-over, and store 9.1 says no v1 point release is needed because "the binary was replaced". A binary cannot both lack v1's paths and run a day on v1's layout.
2. **Three kind tables.** Store has a kind `rule`, workstream states started, active, merged and archived, a maintained paragraph and an episode `Summary`. Derive has no `rule` kind, five states, one cited status line and a code-built trail. Deliver's brief draws a section from "items of kind rule" and another from constraints, and scores a workstream's "state paragraph". Derive notes the first two; nobody notes deliver's.
3. **Writes as ledger events.** Store S-L3 and derive D-L2 need remember, correct and confirm as ledger events; deliver expects a `note` event. The ledger's kind table has none of them, and the ledger is keyed by session while `eva-md remember` from a terminal has no session.
4. **The pull token.** Deliver needs the capture adapter to copy a token from the result of eva-md's own tool. The ledger's contract keeps no output of a successful call and its tool record has no field for it.
5. **One timer, two commands.** Derive's scheduler runs `eva-md work --tick` and has the MCP server call it. Hosts' runs `eva-md tick`, which wraps `work.Tick()`, and has the MCP server start `tick --from mcp`.
6. **The command surface.** Hosts' bar is a dozen verbs. The six designs name 43. `eva-md forget` is defined three times with different selectors (ledger: sessions, events, values; deliver: handles; store: `--purge`).
7. **"The server writes nothing."** Deliver narrows it to a pull log and adds write tools that call `store.Apply` in the server. Hosts narrows it to health lines and an evaluator. Derive and hosts make the server start background work. Four exceptions to one sentence, under two owner decisions.
8. **Which events are declared.** Ledger wants every tool with no matcher, declared once. Deliver wants PreToolUse in that one declaration. Hosts' rule is "a frozen unit declares only what is on by default" and would ship PreToolUse later as its own unit.
9. **How long v1's data stays.** Ledger removes the v1 inbox after 30 days. Store never modifies the old clone and allows an abandon for 14 days. Derive leaves the run log for 14 days. Hosts drops the previous binary once a migration has run.
10. **What Stage 0 can still change.** Evaluate says only Stage 2's scope waits on Stage 0. But LG-10, ST-01, DV-10 and DL-03 are Stage 1 specs that fix the spool, the merge rules, the lifecycle and the gates, which are the things Stage 0 is meant to size.
11. **Excerpts.** Derive's contract stores the verified quote in the item and promises deliver that a standing instruction can always be shown with it. Store makes committed excerpts an owner decision with "references only" as the fallback.
12. **Document size.** Evaluate sets 24 KB as the most an agent should read for one component. The six sections are 86 to 114 KB each.

## 6. What no design covers

1. **A total.** No one adds up packages, lines, model spend and calendar time, or says what is cut first when it slips.
2. **The owner's time.** Stage 0 alone asks the owner for two field reports, a point-release install, a payload recorder run on real hosts, three fact-sheet scripts, live runs on both hosts, a 180-session pilot, an evaluation home with both hosts signed in, and 51 decisions. Nobody budgets it, and the product's bar is that it asks a person for nothing.
3. **Plan B per area.** Evaluate's table says what each Stage 0 result changes. The five other designs' package lists do not have a smaller form for the case where v1 turns out to work (row 13: "the brief's form is tuned by suite K, not rebuilt on suspicion").
4. **A deletion ledger.** Which v1 files go, in which package, and when the frozen v1 parsers, the aliases "for one release" and the shadow flags are removed. No test stops the tree from growing.
5. **Flag and config growth.** New keys appear in every design (`capture.v2`, `capture.detail`, `retention.detail_days`, `organize.*`, `deliver.cross_project`, a three-valued flag per delivery point, `updates`, `identity`, `auth`). No one owns the count or the retirement of a flag.
6. **Upkeep after v2.** Host spec rows, the model policy table, cassettes, cached baselines and frozen plugin hashes all go stale as hosts ship weekly. The recurring cost is not estimated; evaluate prices one baseline rebuild at one to two days per host.
7. **What eva-md stops doing if the host's own memory is enough.** Every experiment has a host arm. No design says which feature is cut when that arm matches eva-md on same-host tasks.
8. **The per-turn cost of hooks**, as in issue 16.

## 7. The order I would run

**Stage 0, two weeks, no rebuild.** The field report. One v1 point release that adds only counters and changes no plugin byte. The live gates on v1. One sitting with the owner for every host fact, in place of four scripts. A pilot of 10 tasks under off, v1 and oracle. Then fix the cut line and write each area's smaller plan against evaluate's thresholds.

**Stage 1, instruments and changes that ship on v1.** The fidelity scorer, the retrieval set, the latency gate with a per-turn figure, and 20 continuation tasks, each with a v1 row. The worker with no lost wake, running v1's jobs. The runner's probe and typed outcomes, and a smaller model by default if the owner agrees and DV-03 supports it. The redaction driver and corpus. The health line prepared outside the hook. The frozen Codex hooks plugin. The question-free first run. The v1 documents split. Each change deletes what it replaces.

**Stage 2, one core rebuild and one cut-over.** The ledger reader over the v1 inbox with the new hook-carried kinds. The single-machine store: codec, item files, one commit per pass, coverage, the one migration. The extraction contract writing to it. The pack, the typed brief, exact keys and the two pull tools reading from it. One release, v1's pipeline deleted in it, the old clone and inbox kept as the way back. Gates: fidelity above v1's row, handoff not worse, latency inside the ceilings, shipped lines at or under v1's.

**Stage 3, only what a number asks for.** The ingester and spool if the capture rates say so. Lifecycle steps if the corpus counters say so. Tool-time delivery if suite C says so. Lexical push if the held-out set says so.

**Not in v2.0, each with its trigger written down.** Sync between machines, reach beyond Linux, background self-install, rule enforcement, agent write tools, the review tier, embeddings, the managed kit, the claim run.
