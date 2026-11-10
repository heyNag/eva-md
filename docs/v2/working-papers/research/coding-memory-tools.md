# Memory tools for coding agents, built-in memories, and what makes memory invisible or intrusive

Research notes, written 2026-11-10. Every fact carries its source URL. "Fetched" means I read the page on 2026-11-10; a date in brackets is the source's own date where it has one.

Confidence words: **high** = primary source read directly; **medium** = primary source read but the claim is a user's report, or a secondary source that agrees with a primary one; **low** = one secondary source, or a claim I could not check.

What I could not reach or confirm is listed at the end, under "Not confirmed".

---

## 1. The short version

1. **The hosts now ship memory, and the two this system supports do it differently.** Claude Code's auto memory is on by default, written by the agent in-session, stored as Markdown per repository on one machine. Codex's memories are off by default and are produced by a background two-phase pipeline (extract per chat, then consolidate) that is strikingly close to this system's own update job.
2. **Three vendors pulled back from automatic free-text memory in the last year.** Cursor removed Memories (Nov 2025) and sent users to rules. Devin's Knowledge is being retired in favor of Skills. Windsurf's (now Devin Desktop's) auto-generated memories page no longer documents memories in English. The direction is toward explicit, reviewable artifacts: rules, skills, and cited facts.
3. **The one vendor with published outcome numbers is GitHub.** Copilot Memory stores each repository fact with citations to code, checks the citations against the current branch before using the fact, votes facts up and down, and expires them. GitHub reports a 7-point rise in pull request merge rate in an A/B test.
4. **Independent evidence says memory helps only when it is the right memory.** A Sept 2026 benchmark found that verified-useful history raised task success by 1.1 to 4.5 points and cut steps, but four existing memory systems failed to beat memory-off in 11 of 12 pairings. An ETH Zurich paper found that repository context files do not improve success and raise cost by over 20%.
5. **Third-party tools with real adoption fail on trust and operations more than on recall.** The most-starred tool (claude-mem, 115k stars) has an open issue alleging it uploaded 30 days of prompts without opt-in, and its busiest issues are worker, port, version and install failures.
6. **What users ask the hosts for, and do not get:** memory that follows them across machines, memory a team shares, memory that works across agents, and a receipt showing what was loaded. These are open, heavily commented issues with no vendor reply. That is the gap a third-party layer can fill.
7. **Users' complaints about memory are stable across products:** stale facts trusted as current, abandoned approaches re-recorded, irrelevant recall, no way to see what was pulled in, token cost, and data leaving the machine.

---

## 2. Built-in memories of the hosts

### 2.1 Claude Code: CLAUDE.md, rules, auto memory

Source for this whole subsection unless noted: https://code.claude.com/docs/en/memory (fetched 2026-11-10). Confidence high.

- **Two systems.** CLAUDE.md files are written by the user; auto memory is "notes Claude writes itself based on your corrections and preferences". Both load at the start of every conversation and are treated "as context, not enforced configuration".
- **Capture.** In-session, by the main agent, as file writes. Four kinds, recorded as a `type` field in frontmatter: `user` (role, preferences), `feedback` (corrections and confirmed approaches), `project` (ongoing work, deadlines, decisions "that Claude can't derive from the code or git history"), `reference` (where to find things outside the project).
- **Write gate.** "Claude skips anything it can derive from the codebase, such as architecture, file paths, or debugging fixes. It also skips anything your CLAUDE.md files already say." "Claude doesn't save something every session."
- **Storage.** `~/.claude/projects/<project>/memory/` with a `MEMORY.md` index ("one line per memory") and one topic file per memory. The project key comes from the git repository, so worktrees share one directory. `autoMemoryDirectory` moves it; the value must be absolute or start with `~/`.
- **Scope limit.** "Auto memory is machine-local... Files are not shared across machines or cloud environments."
- **Injection.** "The first 200 lines of `MEMORY.md`, or the first 25KB, whichever comes first, are loaded at the start of every conversation." Topic files are not loaded at startup; "Claude reads them on demand using its standard file tools".
- **Self-maintenance.** After a write, Claude Code measures the index against the limits and "reminds Claude to shorten it: keep one line per entry, move detail into topic files, and merge or drop stale entries". Over the limit, the write succeeds but returns an error telling Claude to rewrite the index.
- **Freshness signal.** Claude Code records the write time in a `modified` frontmatter field; "The timestamp shows how current the fact is, both to you and to Claude when it reads the memory back."
- **Visibility.** The interface shows "Saved 2 memories" or "Recalled 2 memories". `/memory` browses files, toggles auto memory and opens the folder.
- **Controls.** On by default in local sessions. Off with `autoMemoryEnabled: false` (user or project settings) or `CLAUDE_CODE_DISABLE_AUTO_MEMORY=1`. Off by default in self-hosted environments.
- **Subagents.** The main conversation's auto memory is not loaded into subagents; a subagent can have its own memory directory.
- **CLAUDE.md guidance.** "target under 200 lines per CLAUDE.md file. Longer files consume more context and reduce adherence." Path-scoped rules in `.claude/rules/` load only when matching files are touched. Claude Code reads `AGENTS.md` directly from v2.1.277.
- **Commands near memory.** `/memory`, `/context` ("Shows optimization suggestions for context-heavy tools, memory bloat"), `/recap` (one-line session summary, and an automatic recap after the user has been away), `/insights`, `/resume`. Source: https://code.claude.com/docs/en/commands (fetched 2026-11-10). There is no `/dream` in the commands reference and no page about dreaming in the docs index (https://code.claude.com/docs/llms.txt, fetched 2026-11-10).
- **"Auto Dream".** Third-party posts from March 2026 onward describe a background consolidation sub-agent in Claude Code behind a server-side flag (for example https://www.implicator.ai/anthropic-adds-auto-dream-to-claude-code-fixing-memory-decay-between-sessions). I found **no official documentation** for it. Low confidence; treat as unconfirmed.
- **Recent fix worth knowing.** v2.1.311: "Fixed Claude seeing stale MEMORY.md and CLAUDE.md content right after automatic compaction". Source: https://raw.githubusercontent.com/anthropics/claude-code/main/CHANGELOG.md (fetched 2026-11-10; newest version at the top was 2.1.321).

**Hooks an external memory layer can use.** An Anthropic collaborator confirmed on 2026-08-17 that these exist: `PreCompact` (runs before manual or auto compaction, gets the session ID and transcript path, can block compaction), `PostCompact` (gets the generated `compact_summary`; added in v2.1.76), `SessionEnd` (with a `reason`), and `SessionStart` (on startup, resume and fork; can return `additionalContext`). The richer contract the community asked for (replace or append the compaction summary, a token budget in the payload) was not confirmed as shipped. Source: https://github.com/anthropics/claude-code/issues/47023 (opened 2026-04-12, closed 2026-08-17). Confidence high.

**What users complain about and ask for** (all from the anthropics/claude-code tracker, fetched 2026-11-10):

- **No receipt for what loaded.** Issue #82056, "A session cannot determine whether its auto-memory index loaded whole, truncated, or not at all" (opened 2026-07-28, open, about 76 comments, no staff reply visible). Reported cases: a 248-line index left 95 of 229 entries (41%) outside the load window; a session opened from a different path silently ran on an empty store; one store had 48 of 155 topic files with no link from the index; the truncation warning goes to the model, not the user. Requests: a load receipt (`complete | truncated | not_loaded`), a retrieval receipt, a write receipt (`created | updated | deduplicated | rejected`), `claude memory status --json`, and an explicit `unknown/incomplete` answer in place of a confident "no such rule". https://github.com/anthropics/claude-code/issues/82056. Confidence medium (user reports, consistent with each other).
- **Portable memory across machines.** Issue #25739 (opened 2026-02-14, open, 33 comments, latest activity 2026-11-08, no Anthropic response). Users lose memory on a second machine, after hardware failure, or when the path differs. One lost "130+ sessions of memory". Workarounds: symlink into a private git repo, `autoMemoryDirectory` per launch, sync tools. A commenter found 11 of 43 memory files contained absolute paths. https://github.com/anthropics/claude-code/issues/25739. Confidence high that the demand exists.
- **Shared team memory.** Issue #38536 (opened 2026-03-25, open, about 32 comments, no Anthropic response). Stories: handoffs, incident shift changes, onboarding, design rationale. Concerns raised by commenters: stale context treated as current (suggested fields: `status`, `observed_at`, `supersedes`, `needs_verification`), provenance receipts, offboarding, the difference between replaceable decisions and immutable events, and capture quality ("Hooking PostToolUse records commands and file edits, not reasoning"). https://github.com/anthropics/claude-code/issues/38536. Confidence high.
- **Other titles on the same tracker** (listing fetched 2026-11-10, https://github.com/anthropics/claude-code/issues?q=is%3Aissue+in%3Atitle+memory+%28auto+OR+MEMORY.md+OR+memories%29+sort%3Acomments-desc): #85557 "Portable experience memory across sessions and agents" (open, 21 comments); #63903 "autoMemoryEnabled=false doesn't suppress ~11–16k memory preamble" (closed 2026-08-17, 47 comments); #91188 "Make MEMORY.md compaction reminder threshold configurable" (open, 60 comments); #81833 "Auto-memory inconsistently loaded in git-worktree sessions" (open, 20); #33603 "CLAUDE.md hard rules and memory instructions consistently ignored" (open, 20); #34556 "Persistent Memory Across Context Compactions" (closed, 114 comments); #59248 "Silent retention cleanup deletes session transcripts with no warning, opt-in, or recovery" (open, 67 comments). Confidence high for titles and counts.

### 2.2 Claude Projects (cloud threads) and the Claude apps

- **Claude Code Projects** (public beta on Pro, Max and Ultra). A project is one conversation that runs tasks as parallel cloud threads. "Project memory: Notes Claude keeps about the project, such as requirements, decisions, and pitfalls, stored as files. Every cloud thread reads the index file `MEMORY.md` when it starts and opens the other files when it needs them". Users read, edit and delete the files in Project settings > Memory. It is "separate from the auto memory Claude Code keeps on your machine, even though both use a `MEMORY.md` index". The project conversation "works from recent messages, recent threads, and project memory rather than its full history". Source: https://code.claude.com/docs/en/claude-projects (fetched 2026-11-10). Confidence high.
- **Claude apps (web, desktop, mobile).** "Claude saves memory as a set of individual topics as you chat, rather than summarizing conversations after they end." Each project keeps its own memory, isolated from other projects. Users manage topics under Settings; incognito chats do not feed memory; chat search runs as visible tool calls. "Cowork sessions that run locally on your computer don't use memory." Memory import and export between assistants is "experimental". Source: https://support.claude.com/en/articles/11817273-using-claude-s-chat-search-and-memory-to-build-on-previous-context (fetched 2026-11-10). Confidence high.
- **Claude Managed Agents: memory stores and Dreams** (research preview). "Agents write to their memory stores as they work, but these writes are local and incremental: over many sessions a memory store accumulates duplicates, contradictions, and stale entries." A dream takes an existing store plus 1 to 100 session transcripts and "produces a new, reorganized memory store". "The input store is never modified, so you can review the output and discard it if you don't like the result." Dreams take "minutes to a few hours" and are billed at token rates. Source: https://platform.claude.com/docs/en/managed-agents/dreams (fetched 2026-11-10). Announced at Code with Claude, 2026-05-06, session "Memory and dreaming for self-learning agents" (https://claude.com/code-with-claude/session/sf-memory-and-dreaming-for-self-learning-agents). Confidence high.

### 2.3 Codex: memories and Computer History

Sources: https://learn.chatgpt.com/docs/customization/memories?surface=app (the page developers.openai.com/codex/memories now redirects to; fetched 2026-11-10) and https://raw.githubusercontent.com/openai/codex/main/codex-rs/memories/README.md (fetched 2026-11-10). Confidence high.

- **Default.** "Local Codex memories are off by default." Enable in the app (Settings > Personalization) or with `[features] memories = true`.
- **Capture and consolidation.** A background pipeline that starts when a root session begins (not ephemeral, not a sub-agent). **Phase 1** claims a bounded batch of eligible past chats (within an age window, idle long enough, not owned by another worker), sends each to a model in parallel, and expects `raw_memory`, `rollout_summary` and an optional slug; it "redacts secrets from these fields". **Phase 2** takes a global lock, loads a bounded set of phase-1 outputs, **ranks by usage count then recency**, ignores memories whose last use is outside `max_unused_days`, writes `raw_memories.md` and `rollout_summaries/`, writes a diff from the last baseline, and, only if something changed, runs one consolidation sub-agent "with no approvals, no network, and local write access only".
- **Timing.** It "skips active or short-lived sessions" and does not update "immediately at the end of every chat"; it waits until a chat has been idle. Generation can be skipped when the remaining rate limit is below `memories.min_rate_limit_remaining_percent`.
- **Storage.** Under `~/.codex`, in `memories/` or, for a fresh home, `memories_v2/`. The files are "generated state"; the docs say not to rely on hand-editing them.
- **Controls.** `memories.generate_memories` (may this chat feed memory), `memories.use_memories` (inject into future sessions), `memories.disable_on_external_context` (keep chats that used MCP tools or web search out of generation), per-chat `/memories`, and "Delete Codex memories".
- **Vendor's own guidance.** "Keep required team guidance in `AGENTS.md` or checked-in documentation." "Treat memories as a helpful recall layer, not as the only source for rules that must always apply."
- **Still moving.** Changelog entries from the last two weeks: 2026-11-06, CLI 0.167.0, "Update default models for memory extraction and consolidation"; 2026-10-29, CLI 0.166.0, "Bound memory baseline files and recover incomplete snapshots" and "Configure memory consolidation service tiers independently". Source: https://learn.chatgpt.com/codex/changelog (fetched 2026-11-10).
- **Computer History** (replaced the Chronicle research preview of 2026-04-20). macOS; off by default. It turns interaction events from allowed apps into text summaries and "plain-text Markdown files under `$CODEX_HOME/memories/extensions/skysight/`". "They are not encrypted by Computer History". Events are processed on OpenAI's servers. The page warns: "Computer History increases the risk of prompt injection from content in apps and websites" and "Computer History uses tokens while it summarizes activity and creates memories." Source: https://learn.chatgpt.com/docs/customization/computer-history.md (fetched 2026-11-10). Confidence high.
- **Complaints.** I found no heavily commented openai/codex issue about memories specifically (two listing searches, fetched 2026-11-10). The loudest Codex issues are about quota: #14593 "Burning tokens very fast" (630 comments), #28879 (211), #41220 "[Meta] Abnormal Codex usage/quota depletion" (84). Source: https://github.com/openai/codex/issues?q=is%3Aissue+memories+sort%3Areactions-%2B1-desc. That quota is the same quota a background memory job spends.

### 2.4 GitHub Copilot: Memory and Dreaming

- **Design** (GitHub engineering blog, 2026-01-15, https://github.blog/ai-and-ml/github-copilot/building-an-agentic-memory-system-for-github-copilot/). Memory creation is a tool call the agent makes when it finds something likely to matter later; each memory has a subject, a fact, **citations** to code locations, and a reason. **Just-in-time verification:** the agent checks the cited code when it meets a memory; verification "boils down to a small number of simple read operations". If the code contradicts the memory, the agent stores a corrected one; if it checks out, it re-stores it to refresh the timestamp. GitHub chose **not** to build offline curation at first, because it would bring "significant engineering complexity and LLM costs" and still need read-time reconciliation. Retrieval at the start: "the most recent memories for the target repository". Confidence high.
- **Measured results, same source.** Coding agent A/B: pull request merge rate 83% without memory, 90% with it. Code review A/B: positive feedback 75% to 77%. Offline code review: +3% precision, +4% recall. Both A/B results p < 0.00001. Adversarial test: repositories seeded with memories that contradicted the code; agents "verified citations, found contradictions, and updated the bad memories". These are vendor-run numbers. Confidence high that GitHub reports them.
- **Current behaviour** (https://docs.github.com/en/copilot/concepts/agents/copilot-memory and https://docs.github.com/en/copilot/concepts/prompting-and-context/copilot-memory, fetched 2026-11-10). Two kinds: repository-level facts (shared with collaborators, used only on that repository) and user-level preferences (follow the user). Copilot "can upvote a memory that it's able to validate, and downvote a memory if it's found to be wrong or outdated"; memories "downvoted past a threshold are no longer used". "The default expiry period is 90 days"; the clock "resets when Copilot upvotes it", and retrieval alone does not reset it. Owners can set 7 to 180 days for repository facts. Memories containing detected secrets are rejected, and so are memories containing model-control tokens, as a prompt-injection defence. In the app and interactive CLI Copilot "may ask you to approve storing a memory". Users cannot create or edit a memory directly on GitHub.com. For rules that must be followed, the page recommends custom instructions. Confidence high. (The VS Code page still says 28 days, https://code.visualstudio.com/docs/agents/memory; the GitHub page is the primary one.)
- **Copilot Dreaming** (public preview; https://docs.github.com/en/copilot/concepts/prompting-and-context/copilot-dreaming, fetched 2026-11-10). A background process that reads synced sessions and feedback on merged pull requests. It "adds, updates, and deletes memories without asking for approval for each change", but changes to checked-in files such as `AGENTS.md` and `.github/skills/**` "arrive as proposed pull requests". "Every Copilot Dreaming run uses AI credits", including runs that change nothing. Off unless enabled. Turning it off "does not undo past changes... or restore deleted memories". Confidence high. Secondary sources date the preview and the general availability of Copilot Memory to GitHub Universe, 2026-10-28 (https://getunblocked.com/blog/github-agent-memory-only-knows-github/); I did not find GitHub's own changelog post for that date. Confidence medium on the date.
- **VS Code local memory tool** (https://code.visualstudio.com/docs/agents/memory, fetched 2026-11-10). Three scopes as paths: `/memories/` (user, first 200 lines auto-loaded), `/memories/repo/`, `/memories/session/` (current conversation only; the Plan agent keeps `plan.md` there). Commands: "Chat: Show Memory Files", "Chat: Clear All Memory Files".

### 2.5 Gemini CLI

- **`save_memory`** appends a fact to `~/.gemini/GEMINI.md` under "## Gemini Added Memories". Source: https://geminicli.com/docs/tools/memory.
- **Auto Memory** (experimental, off by default; https://geminicli.com/docs/cli/auto-memory.md, fetched 2026-11-10). Runs as a background task at session startup. Eligible sessions "must be idle for at least three hours and have at least 10 user messages". The extraction agent "defaults to creating no artifacts unless the evidence is strong, so many runs produce nothing". It proposes **memory patches** (unified diffs) and **draft `SKILL.md` files** into a per-project inbox; `/memory inbox` reviews them; nothing is applied without approval. It "cannot directly edit active memory files, settings, credentials, or project `GEMINI.md` files". "Auto Memory does not extract memory or skills from the current session." Confidence high.

### 2.6 Cursor, Windsurf, Devin, Amp: the retreat from free-text auto memory

- **Cursor.** Staff reply, 2025-11-25: "The Memories feature was intentionally removed starting from version 2.1.x." Migration: an "Export memories" command writes an `.mdc` file to add to User or Project Rules. A user who first called memories "my competitive advantage" later wrote that they "can see the reasoning behind abandoning memories" since the two formats are nearly the same. Source: https://forum.cursor.com/t/are-my-memories-gone/144057. The current docs page for memories describes only rules (Project, User, Team, AGENTS.md) with four application modes: Always Apply, Apply Intelligently, Apply to Specific Files, Apply Manually (https://cursor.com/docs/context/memories, fetched 2026-11-10). Confidence high. A secondary source says Cursor Automations (March 2026) have their own notes across runs; not checked.
- **Devin Knowledge.** The docs page now opens with a deprecation notice: Knowledge is being retired in favor of Skills ("Use Skills for new instructions and context you want to share with Devin"), and existing items migrate automatically. Its model while it lived: every item needs a **trigger description**; Devin retrieves an item "when its current work is related to the specified triggers"; items can be pinned to a repo or all repos; Devin **suggests** Knowledge from chat feedback and the user edits, saves or dismisses. Guidance: "Content should be a handful of sentences". Source: https://docs.devin.ai/product-guides/knowledge.md (fetched 2026-11-10). Confidence high.
- **Windsurf, now Devin Desktop.** The memories URL (docs.windsurf.com/windsurf/cascade/memories) redirects to https://docs.devin.ai/desktop/cascade/memories, and the English page I fetched on 2026-11-10 describes rules, skills, plugins, MCP servers and hooks and **does not mention memories**. Search results for the localized pages say auto-generated memories apply only to the legacy Cascade agent and that the docs recommend a Rule or `AGENTS.md` for knowledge meant to be reused. Rules have four triggers: `always_on`, `glob`, `model_decision`, `manual`. Confidence medium (English page silent; legacy statement seen only through search snippets of localized pages).
- **Amp.** The manual page I could fetch mentions only threads ("You can save and share your interactions with Amp") and links to AGENTS.md; I found no automatic memory feature. Source: https://ampcode.com/manual (fetched 2026-11-10). Confidence low to medium: I did not read the threads or AGENTS.md sub-pages.
- **Others.** Search results (secondary only) say JetBrains Junie has guidelines files and no self-written notes, and Kiro relies on steering files. Not checked against primary docs. Low.

### 2.7 Summary table of built-ins

| Host | Automatic capture | Default | When it writes | Consolidation | Verification of old facts | User review before use |
|---|---|---|---|---|---|---|
| Claude Code (local) | Yes | On | In-session, by the agent | Index trimming on write; "Auto Dream" unconfirmed | `modified` timestamp only | No; visible "Saved N memories" |
| Claude Code Projects (cloud) | Yes | On in projects | In-thread | Not documented | Not documented | Edit in settings |
| Codex | Yes | Off | Background, after idle | Phase 2 agent, usage-ranked, unused pruned | None documented | No; delete-all and per-chat switches |
| Copilot | Yes | On (paid individual plans) | In-session tool call | Dreaming (preview, opt-in) | Citations checked before use; votes; 90-day expiry | Sometimes asks; list and delete in settings |
| Gemini CLI | Yes (experimental) | Off | Background, after 3 h idle | In the same extraction | None documented | Yes, inbox |
| Cursor | Removed | — | — | — | — | Rules only |
| Devin | Suggested Knowledge, being retired | — | — | — | — | Yes, suggestions; moving to Skills |

---

## 3. Third-party tools

### 3.1 claude-mem

- **Adoption.** 115.4k stars, 46 open issues, version 15.14.0, Apache-2.0. Source: https://github.com/thedotmack/claude-mem (fetched 2026-11-10). High.
- **Architecture** (https://docs.claude-mem.ai/architecture/overview, fetched 2026-11-10). Capture: hooks on SessionStart, UserPromptSubmit, PreToolUse (Read), PostToolUse (all tools), Stop, SessionEnd. Each hook calls a launcher that talks to a **local worker**: an Express server under Bun on port `37700 + (uid % 100)`. Consolidation: the worker hands tool events to an **observer agent** (Claude Agent SDK, or Gemini or OpenRouter) that writes structured observations, and the Stop hook writes a session summary. Storage: SQLite with FTS5 at `~/.claude-mem/claude-mem.db`; Chroma optional for vectors. Injection: an index of past observations at session start, then a three-step search skill (search, timeline, get_observations). Dependencies: Node 20+, Bun, uv. High.
- **What users value.** Zero-effort capture and a web viewer of what was captured; the star count shows the demand for "it just remembers". (Stars are weak evidence of use; see cautions.)
- **Trust problems.** Issue #5176, opened 2026-10-29, open: "Installing claude-mem uploads your last 30 days of prompts to cmem.ai, no opt-in". The reporter cites source lines from v15.5.0 and correspondence saying prompts were sent to a model through OpenRouter. A second commenter's review of v15.7.0 says setting `improveProduct=false` did not prevent the upload. Fix PR #5220 ("require explicit consent before uploading prompts") was open when I read it. No written maintainer reply. Source: https://github.com/thedotmack/claude-mem/issues/5176. The README also says anonymous analytics are on by default, sign-in is offered at install, and the creator "officially embraced" a third-party crypto token. Confidence: high that the issue and README say this; medium that the allegation is accurate.
- **Operational problems.** The most-commented issues are about the worker, not about memory quality: worker fails to start on its port (#380, #197), a DB migration broke the plugin (#121), a version mismatch killed the worker on every hook (#3857, #3940), a quota guard aborted and a restart discarded the queue (#4068), hooks not executing (#692), and the tool writing `CLAUDE.md` files into project subdirectories (#609, #641, #697). Source: https://github.com/thedotmack/claude-mem/issues?q=is%3Aissue+sort%3Acomments-desc (fetched 2026-11-10). High.
- **Token cost.** One user's comparison over 270 sessions reports 1.485M tokens for claude-mem against 20k for a transcript-search tool, and about 42,000 cache-creation tokens per session. Source: https://glama.ai/mcp/servers/@Vvkmnn/claude-historian/blob/5523c7a4febebd9cb3e54990eb6bc693ee206aa5/COMPARISON.md (seen through search; written by the author of the competing tool). Low.

### 3.2 basic-memory

4.5k stars, AGPL-3.0, 72 open issues. Markdown files are the source of truth; a SQLite index (Postgres optional) gives full-text and FastEmbed vector search with hybrid ranking. Notes are entities with observations and wikilink relations. **Capture is by explicit tool call** (the user asks; the model calls `bm_write`); a Claude Code plugin adds session-start briefings and pre-compaction checkpoints. Hosted version $15 a month. Source: https://github.com/basicmachines-co/basic-memory (fetched 2026-11-10). High. Value: files the user owns and can open in Obsidian. Weakness by design: nothing is remembered unless someone asks.

### 3.3 Beads

28.6k stars, MIT, now at github.com/gastownhall/beads; 1.1k open issues and 657 open pull requests. It is "a distributed graph issue tracker for AI agents, powered by Dolt": work items with dependencies, not prose. `bd ready` lists unblocked work, `bd prime` injects context, `bd remember` stores project memory that `bd prime` injects later, and "semantic 'memory decay' summarizes old closed tasks". Storage moved to Dolt; `.beads/issues.jsonl` is "an export for viewers and interchange, not the source of truth". The README documents schema-version mismatches with stale binaries and migrations that need one designated clone. Source: https://github.com/gastownhall/beads (fetched 2026-11-10). High. What it proves: agents use **structured work state** (what is ready, what blocks what) reliably. What it costs: a database, migrations and sync, which is where its issue volume is.

### 3.4 Serena memories

31.2k stars (https://github.com/oraios/serena). Memories are Markdown in `.serena/memories/`; global ones in `~/.serena/memories/global/`. The agent gets **the list of memory names** when it activates a project and is "instructed to read them when appropriate, inferring appropriateness from the file name". "The agent decides what to read and when"; the harness does not inject content. Onboarding writes the first memories. `read_only_memory_patterns` protects memories; `serena memories check` reports stale or broken references between memories. Keeping them current is on the user: "The agent should be instructed to update memories by the user when appropriate." Source: https://oraios.github.io/serena/02-usage/045_memories.html (fetched 2026-11-10). High.

### 3.5 memsearch (Zilliz)

2.8k stars, MIT. Markdown daily files (`memory/YYYY-MM-DD.md`) are the source of truth; Milvus is a "shadow index" rebuilt from them; search is dense vectors plus BM25 with RRF. The Claude Code plugin's Stop hook summarizes the last turn with a small model (Haiku by default) and appends to the daily file. Recall is a skill with three layers: search, expand the chunk to its section, parse the raw transcript. Local embedding is ONNX bge-m3, "about 558 MB download". Source: https://github.com/zilliztech/memsearch (fetched 2026-11-10). High.

### 3.6 Supermemory plugin for Claude Code

2.8k stars. Hooks: SessionStart (profile facts, `maxProfileItems` default 5, and a welcome-back notice after 6+ hours away), UserPromptSubmit (search and inject matches, "deduplicated within the session"), Stop (capture). Memories are stored in the vendor's cloud by default. Repositories are keyed by "a hash of its normalized Git remote", so clones share memory. A credential filter runs before saving and "cannot detect every secret". Source: https://github.com/supermemoryai/claude-supermemory (fetched 2026-11-10). High.

### 3.7 ByteRover (formerly Cipher)

A CLI (`brv`) that keeps a Markdown "context tree" under version control with `brv curate`, `brv query`, and a **review queue** (`brv review pending`, approve, reject), with cloud sync for teams. Its 92.19% retrieval accuracy figure is the vendor's own. Source: search results for docs.byterover.dev and listings, not read directly. Low to medium.

### 3.8 Cline Memory Bank

Not a feature; "a documentation methodology": six Markdown files (`projectbrief.md`, `productContext.md`, `activeContext.md`, `systemPatterns.md`, `techContext.md`, `progress.md`) and an instruction that says "I MUST read ALL memory bank files at the start of EVERY task - this is not optional." Source: https://docs.cline.bot/prompting/cline-memory-bank (fetched 2026-11-10). High. The known weakness is the read-everything rule and hand maintenance; secondary guides describe `activeContext.md` bloating and going stale. Low on the complaints (vendor blogs).

### 3.9 Letta Code: Context Repositories (MemFS)

Published 2026-02-12. Agent memory is a git-backed folder of Markdown files with a description in frontmatter. The **file tree is always in the system prompt**, so names act as navigation; files under `system/` are always fully loaded. Memory subagents: **init** (explores the codebase and reviews Claude Code and Codex histories), **reflection** (a background pass over recent conversations), and **defragmentation** (reorganizes into "roughly 15–25 focused files"). Each subagent works in its own git worktree and merges back, which is how concurrent memory writes are coordinated. The post reports no measurements. Source: https://www.letta.com/blog/context-repositories. High.

### 3.10 OpenClaw memory (built into a widely used agent; several coding tools copy it)

Source: https://docs.openclaw.ai/concepts/memory.md (fetched 2026-11-10). High.

- Files: `USER.md`, `MEMORY.md` (curated, loaded at session start), `memory/YYYY-MM-DD.md` (daily notes, searched, not injected each turn), `DREAMS.md` (summaries for human review). "The model only remembers what gets saved to disk; there is no hidden state."
- **Pre-compaction flush**, on by default: a silent turn prompts the agent to save important context before compaction; it writes to the daily note only.
- **Recall budget.** "Automatic project recall has a one-second preparation budget. Late results are discarded."
- **Search** is hybrid, with a keyword-only setting (`memory.search.provider: "none"`).
- **Dreaming** is on by default. Promotion into `MEMORY.md` is gated on score, recall frequency and query diversity. "Invalid decisions fall back to append-only."
- **Taint gating.** "untrusted and system-derived candidates never enter the consolidation prompt or durable promotion path."
- **Truncation is shown**: if `MEMORY.md` exceeds its budget, `/context list` and `openclaw doctor` show it.

### 3.11 MemPalace and the long tail

- **MemPalace** went viral in April 2026 (tens of thousands of stars within weeks). Its 100% LongMemEval claim was revised to 96.6% after an issue; an arXiv analysis concluded its retrieval performance "derives from standard vector database operations, not from its spatial metaphor". Sources: https://arxiv.org/html/2604.21284v1 and https://vectorize.io/articles/mempalace-review (seen through search). Medium. Lesson: stars and benchmark headlines are not evidence.
- **Long tail.** Hacker News lists dozens of 2026 "memory for coding agents" projects with single-digit points (https://hn.algolia.com/api/v1/search?query=agent%20memory%20coding%20agents&tags=story, fetched 2026-11-10). The category is crowded and mostly unproven.
- Not examined in depth: mem0 and OpenMemory, Hindsight, cognee, Graphiti. Their coverage in search results was mostly vendor-written.

---

## 4. What users say (Hacker News threads, read 2026-11-10)

Quotes are verbatim as returned by the page reader, with usernames.

**"Show HN: Stop Claude Code from forgetting everything"** (202 points, 225 comments, 2025-12-29), https://news.ycombinator.com/item?id=46426624
- saberience: "tried probably 10-20 other open source projects and closed source projects" and "nothing works better than simply keeping my own library of markdown files for each project specification."
- gbnwl: "there's never any evidence or even attempt at measuring any metric of performance improved by it." and "Doesn't mean it's time to send our data to strangers."
- floatrock: "why do I need an API key for what can be local markdown files."
- minimaxir: "With Opus 4.5 in Claude Code, I'm doing fine with just a (very detailed) CLAUDE.md."
- hasperdi: "the model may forget to update the log... It usually happens when the context window >50% filled."
- gck1: "just let the code itself scream at it and feed the context."

**"Show HN: Recall – Local project memory for Claude Code"** (138 points, 85 comments, 2026-06-21), https://news.ycombinator.com/item?id=48622590
- suprjami: "possibly just inflates context which is harmful."
- rkochanowski: "From my experience, removing from context works more often then adding."
- zihotki: "Are there any benchmarks/evals to back the claims?"
- cadamsdotcom: "the need to manually update the context is a huge hurdle."
- The author: "Currently there is no way to handle stale memory".
- drivebyhooting: "Compaction already drops nuanced and salient information."
- Reported in the summary: agents keep recording abandoned approaches; several commenters pointed out that Claude Code already has memory.

**"Show HN: Total Recall – write-gated memory for Claude Code"** (67 points, 2026-02-05), https://news.ycombinator.com/item?id=46907183
- 4b11b4: "At this point, I'd argue any \"automated memory\" is a failure over time."
- ejae_dev: "the daily-log-first approach is a smart design choice — it decouples capture from commitment."
- iamleppert: "I just want an infinite scroll in Claude Code, where I can easily search and view all my past conversations."
- Three commenters criticized the README for being LLM-written; Bolwin: "it's a strong signal for low effort".

**"Jevmem – automatic project memory for Claude Code"** (62 points, 49 comments, 2026-09-25), https://news.ycombinator.com/item?id=49846391
- anthuswilliams: "I still don't understand what this thing is for and why it is to be preferred over the harness's native memory management tools."
- rafram, on keeping superseded lines in the file: "You're polluting context with false/outdated information".
- rearclabs: "Keeping history and using history are two different things though."
- 0x457: "they can't reliably classify if something is useful or not."
- qwertox: "your whole memory file go to TypeSafe AI, a young vendor".

**Thread on a git-native memory tool** (81 points, 32 comments, 2026-09-05), https://news.ycombinator.com/item?id=49581240
- langs: "I don't get it. Why benchmark the latency instead of recall/precision?"
- glub: "What nobody has gotten close to solving is maintenance and provenance".
- olenzma: "Invalidation is not an algorithm, but a judgment made in context".
- vshulcz: "The funny thing is BM25 basically ties embeddings here at 1/100th the cost."
- opwizardx: "adding vector search, in my experience, adds more hassle and failures then helps".
- mbreese: "I have had mixed results getting LLMs to consistently use third party tools."

**"Claude Memory"** launch thread (559 points, 312 comments, 2025-10-23), https://news.ycombinator.com/item?id=45684134
- godelski: "I don't know what's being pulled into context or not."
- cruffle_duffle: "They never give you much control over when the system uses them and little control over what gets stored."
- abustamam: "until it starts hallucinating memories, or using memories in an incorrect context."
- helloplanets: "memory can be especially sneakily bad when trying to get creative outputs."

**Simon Willison, "I really don't like ChatGPT's new memory dossier"** (2025-05-21), https://simonwillison.net/2025/May/21/chatgpt-new-memory/
- An image request picked up a location he had mentioned in another chat. He objected to material "being dumped into the model's context without my explicit permission or control" and asked for memory scoped to projects: "I need control over what older conversations are being considered, on as fine-grained a level as possible."

**Themes, with where each shows up**

| Complaint | Where |
|---|---|
| Stale facts trusted as current; no invalidation | Recall, Jevmem and git-native threads; Claude Code #38536 |
| Abandoned approaches recorded and replayed | Recall and Jevmem threads |
| Irrelevant recall, context inflation | Recall thread; Claude Memory thread; Willison |
| Cannot see what was loaded or why | Claude Code #82056; Claude Memory thread |
| Token and quota cost | claude-mem comparison; Copilot Dreaming docs; Codex quota issues |
| Data leaves the machine, or might | claude-mem #5176; first thread; Jevmem thread |
| Setup and moving parts (worker, port, runtimes) | claude-mem issue list; Beads README |
| No measurement | every Show HN thread |
| The host already does this | Recall and Jevmem threads |

---

## 5. Does memory help? The measured evidence

- **GitHub's A/B tests** (vendor, 2026-01-15): merge rate 83% to 90% with cited, verified memory. https://github.blog/ai-and-ml/github-copilot/building-an-agentic-memory-system-for-github-copilot/
- **VibeMemBench** (arXiv 2609.23570, submitted 2026-09-20), https://arxiv.org/abs/2609.23570. 111 coding targets from 90 repositories and 3,634 history trajectories. Directly injecting experience whose usefulness was confirmed by execution "raised task resolution on four of them by 1.1 to 4.5 percentage points and reduced agent steps on all five" held-out solvers. But when four existing memory systems had to build and retrieve from the same history, "in 11 of 12 solver-system pairings, the result did not exceed the matched memory-off baseline". The abstract does not name the four systems. Confidence high for the abstract.
- **"Evaluating AGENTS.md"** (ETH Zurich, arXiv 2602.11988, v1 2026-02-12, v3 2026-09-29, listed at ICLR 2026), https://arxiv.org/abs/2602.11988. Context files "generally do not improve task success" and raise inference cost by over 20% on average. Agents follow the instructions in the files well, but "repository overviews, although popular and recommended by model providers, are not helpful." Search summaries add: LLM-generated files lowered success by about 0.5 to 2 points; developer-written files gave about 4 points on one benchmark. Confidence high for the abstract, medium for the per-benchmark numbers.
- **DreamBench-SWE** (arXiv 2608.20664, 2026-08-21, single author), https://arxiv.org/abs/2608.20664. On tasks that depend on non-inferable evidence from earlier sessions: no external memory 21/180; deterministic verbatim event memory 82/180; a hosted memory product 97/180. The differences among the memory conditions were not confirmed. Reading: for facts the code cannot supply, having any memory matters far more than which one. Medium.

Taken together: the value is in a small number of non-inferable, verified, relevant facts. Volume and repository overviews cost tokens and can lower success.

---

## 6. Answers to the questions

### 6.1 What makes memory feel invisible rather than intrusive

Drawn from what the surviving designs share and what the complaints name:

1. **No user step at capture.** Hooks or a background job. "the need to manually update the context is a huge hurdle" (Recall thread). Explicit-capture tools (basic-memory, Cline's bank, Serena) depend on discipline and decay.
2. **Never delay a turn.** Claude Code writes in-session; Codex and Gemini wait until a chat has been idle for hours and run at the next startup; OpenClaw gives recall one second and drops late results.
3. **Inject little, and only what the code cannot say.** Claude Code's gate ("skips anything it can derive from the codebase"), its 200-line index, supermemory's five profile items, Devin's "handful of sentences". The ETH paper and the Recall thread ("removing from context works more often") say the same from the other side.
4. **Be right, or say how old it is.** Copilot checks citations before use; Claude Code stamps `modified`. A wrong memory is the most visible thing a memory system can do.
5. **A quiet, truthful trace.** "Recalled 2 memories" is enough when it is right; the complaint in #82056 is that there is no trace when it is wrong.
6. **No new moving parts.** claude-mem's top issues are its worker, port and runtime. A memory layer that breaks the host's startup is the opposite of invisible.
7. **Spend the user's quota carefully.** Codex skips generation near the rate limit; Copilot warns that every Dreaming run costs credits.
8. **Stay in scope.** Project-scoped by default; nothing from another project appears unasked (Willison; Claude's project memory).
9. **Do not fight the host.** Where the host has its own memory, a second layer that repeats or contradicts it is noise ("why it is to be preferred over the harness's native memory management tools").

### 6.2 Controls users want

| Control | Evidence |
|---|---|
| See what is stored, as plain files | Claude Code `/memory`; Copilot settings lists; "I specifically want everything to be stored as plain text" (git-native thread) |
| See what was loaded this session, and whether it was complete | Claude Code #82056 (load, retrieval and write receipts) |
| See why something was recalled | "I don't know what's being pulled into context or not" |
| Correct in place | Claude apps "edit or delete topics"; Copilot cannot edit directly, which is a gap |
| Forget, and have it stick | Copilot points to the right place and down-votes; Codex delete-all; OpenClaw "Forgetting removes indexed text and vectors" |
| Scope: per project, per chat off-the-record, exclude tool-assisted chats | Claude incognito and project isolation; Codex `/memories` and `disable_on_external_context` |
| Review before it becomes standing guidance | Gemini inbox; Devin suggestions; Copilot Dreaming pull requests for instruction files; ByteRover review queue |
| Undo a consolidation | Managed Agents dreams never modify the input store; Letta and Serena lean on git history; Copilot Dreaming cannot undo, which its docs state |
| Portability across machines, agents, teammates | Claude Code #25739, #85557, #38536 |
| Local by default, no telemetry | claude-mem #5176; first HN thread |

### 6.3 Proven by use, and hype

**Proven by use (shipped at scale, kept, or measured):**
- Plain Markdown the user can read and edit, with a small always-loaded index and details on demand (Claude Code, Claude Projects, Codex, OpenClaw, Letta, Serena).
- A strict write gate: corrections, confirmed approaches, decisions the code cannot show (Claude Code's four types; Gemini's "default to nothing").
- Citations plus verification at use time, with reinforcement and expiry (Copilot; the only design with outcome numbers).
- Background consolidation after idle, not at the end of every chat (Codex, Gemini, Copilot Dreaming, Managed Agents dreams).
- Explicit rules files for anything that must always apply; every vendor says so (Codex, Copilot, Windsurf, Cursor).
- Procedures becoming skills that load on demand (Gemini drafts, Codex memory skills, Devin's move to Skills, Copilot Dreaming's skill pull requests).
- Keyword search as the baseline; vector search optional (OpenClaw's keyword-only mode; BM25 in memsearch and basic-memory; commenters' reports).
- Structured work state for agents (Beads).
- Saving state before compaction (OpenClaw flush; Claude Code's PreCompact hook, which third-party authors say "made external persistent memory viable").

**Hype, or unproven:**
- Benchmark headlines on conversational memory tests (MemPalace; vendor accuracy figures). They do not measure coding outcomes.
- "Capture everything, compress with a model, inject an index" as a token saver: no independent measurement, and one report of the opposite.
- Knowledge graphs and spatial metaphors as the reason for retrieval quality (the MemPalace analysis).
- Star counts as proof of value.
- Auto-generated repository overviews (the ETH paper).
- Always-on screen or activity capture for coding memory (Computer History): new, opt-in, with a prompt-injection warning from its own vendor.
- "Dreaming" as a quality guarantee: it makes a store coherent, not correct; no vendor has published outcome numbers for it.

### 6.4 Where the hosts' built-in memories are heading

1. **From free-text memories to typed, reviewable artifacts**: rules, skills, cited facts (Cursor, Devin, Windsurf, Copilot Dreaming, Gemini).
2. **Background curation as a named product** ("dreaming"): Anthropic Managed Agents (May 2026), GitHub (Oct 2026), OpenClaw, and Codex's phase 2.
3. **Verification and decay built in**: Copilot's citations, votes and 90-day expiry; Codex's usage ranking and `max_unused_days`.
4. **Memory moving to the vendor's cloud for multi-session work**: Claude Projects, Copilot Memory, ChatGPT memory. Local stores (Claude Code, Codex) stay per machine.
5. **Ambient capture beyond chats**: Codex Computer History.
6. **Each vendor's memory stays inside that vendor.** None reads another's. None of the two hosts here offers sync across machines or a team store for local sessions.

**So a third-party layer has to add what they do not have:**
- One memory across Claude Code and Codex, and across machines, with history the user owns (git).
- Team sharing with review, which a git repository gives almost for free.
- Receipts: what loaded, what was recalled and why, what was written.
- Verification against the code, which neither Claude Code's nor Codex's local memory does.
- Work continuity per branch (what was in progress, what was tried and dropped). Host memories store preferences and facts; none stores a branch's working state.
- A measured claim. Nobody in the third-party field publishes outcome numbers.
- A clean trust story: local, no telemetry, no daemon, few dependencies.

And it must **not** duplicate what the host already injects, or it becomes the noise users complain about.

---

## 7. Ideas worth taking (detail)

1. **Cite and verify.** Every knowledge fact carries the code locations or commits it rests on. Before a fact is injected, a cheap local check sees whether the cited code moved; a moved citation downgrades the fact to "unverified" or holds it back, and queues it for the next background update. Copilot's design and numbers are the evidence.
2. **Reinforce and decay by use.** Record each injection and whether the chat confirmed, ignored or contradicted the fact. Rank by use; retire what is never used. Copilot votes and expiry; Codex usage count and `max_unused_days`.
3. **Receipts.** A one-line, user-visible note of what was loaded and why, a `status` view of the same data, and an honest "unknown" when the store could not be read. Claude Code #82056 is the specification.
4. **Stage consolidation as a reviewable change.** The background update writes to a branch or produces a diff; low-risk changes merge on their own, changes to standing rules wait for the user. Managed Agents dreams (new store, input untouched), Copilot Dreaming (pull requests for instruction files), Gemini (inbox).
5. **Default to writing nothing.** Promote only corrections, confirmed approaches, decisions and constraints the code cannot show. Claude Code's skip rule; Gemini's strong-evidence default; the ETH paper.
6. **Keep superseded and abandoned work out of active context, but keep it.** A typed "tried and dropped, because" record that is recalled only when the agent heads the same way. Jevmem and Recall threads.
7. **Work with the host's own memory.** Detect Claude Code auto memory and Codex memories; do not inject what they already hold; offer to import them. Position on what they lack.
8. **Emit procedures as host skills.** A procedure page becomes a `SKILL.md` the host loads on demand through its own mechanism. Gemini, Codex, Devin, Copilot.
9. **Use the compaction hooks.** Save state on PreCompact; put the thread's current state back on PostCompact or the next SessionStart. Issue #47023; OpenClaw's flush.
10. **Gate by provenance.** Text that came from tool output, web pages or MCP calls never becomes a standing rule without the user's confirmation. OpenClaw taint gating; Codex `disable_on_external_context`; Copilot's rejection of model-control tokens.
11. **Budget recall and never repeat it.** A hard time budget, a small item cap, and no second injection of the same fact in one session. OpenClaw; supermemory.
12. **Respect the quota.** Check remaining rate limit before a background model run; report what each run spent. Codex; Copilot Dreaming's cost warning; the size of the quota issues.
13. **Measure outcomes.** A with-and-without harness on real repository tasks, plus seeded wrong memories to test self-correction. VibeMemBench's method; GitHub's adversarial test.
14. **Scope and forget controls.** Per-project isolation by default, an off-the-record switch per chat, exclusion of tool-heavy chats, and a forget that removes the fact, its index entries and its recall.
15. **Trust as a feature.** No telemetry, no account, no worker, a human-written README, and a published statement of what leaves the machine (nothing but the host's own model calls).

---

## 8. Cautions

- **Stars are not use.** The most-starred tool carries a crypto token and an unresolved privacy allegation; another viral project had to revise its benchmark claim.
- **Double memory.** Claude Code's auto memory is on by default. A second layer injecting at SessionStart can repeat or contradict it. One issue reported an 11 to 16k-token memory preamble even with auto memory off (#63903, closed).
- **More context can lower success.** Overviews and generated context files cost over 20% more and did not help (ETH). Memory systems as built did not beat memory-off (VibeMemBench).
- **Model-run consolidation drifts.** "they can't reliably classify if something is useful or not." Copilot Dreaming deletes memories without approval and cannot undo. A consolidation step needs a diff and a way back.
- **Host contracts move.** Codex changed its memory defaults twice in the last two weeks. Hook behaviour has changed across Claude Code versions. Cursor removed a whole feature.
- **Background runs spend the user's subscription.** Quota is the most-commented complaint on both hosts' trackers.
- **Plain files are readable by anything running as the user.** OpenAI says so about its own memory files. Redaction "cannot detect every secret" (supermemory). Memory is also a prompt-injection path.
- **Vector search has a real cost**: a 558 MB model or a network call, an index to keep in step, and no coding-task evidence that it beats keyword search. The supporting reports are anecdotes.
- **A team store raises new problems**: stale context from a colleague read as current, offboarding, and who approved what.
- **Some facts here are secondary**: see below.

---

## 9. Not confirmed, and open questions

- **Claude Code "Auto Dream".** Described by third parties; absent from the official docs index and commands reference on 2026-11-10. I did not search the full changelog (1.2 MB; only the newest part was read).
- **Whether Claude Code selects topic files with a side model.** The docs say Claude reads them "on demand using its standard file tools". "Recalled N memories" is documented as a UI message only.
- **Copilot Memory general availability date** (2026-10-28) comes from secondary sources. The VS Code docs (28 days) and GitHub docs (90 days) disagree on expiry; I took GitHub's.
- **Windsurf memories as legacy**: the English page no longer mentions memories; the legacy statement was seen only in search snippets of localized pages.
- **Amp**: no memory feature found, but the threads and AGENTS.md sub-pages were not read.
- **claude-mem #5176**: an allegation with cited source lines and an open fix; no maintainer statement.
- **claude-mem token overhead**: one comparison by a competitor's author. No independent measurement exists that I could find.
- **Reddit**: search returned only snapshot mirrors; I read no Reddit thread directly. claudeissues.com (a mirror of Claude Code issues) could not be reached from this session; I used GitHub directly instead.
- **Codex numeric defaults** (idle hours, age window, batch sizes, `max_unused_days`) are not in the docs or the README I read; third-party posts give 6 hours idle and a 256-rollout cap, unverified.
- **VibeMemBench**: which four memory systems, and why they failed, is in the full paper, not the abstract.
- **How Claude Code's auto memory and an external SessionStart injection interact in practice**: no measurement found of duplication or conflict rates.
- **Claude Desktop**: the help article says local Cowork sessions "don't use memory"; whether plugins and hooks behave the same in the desktop app's Code tab as in the CLI was outside what I read.
- **User reaction to Copilot's voting and expiry**: I found the design and the vendor's numbers, but no body of user complaints or praise about them.
- **mem0, OpenMemory, Hindsight, cognee, Graphiti**: not examined beyond search summaries.

---

## Fact check

Checked 2026-11-10 by a second reader, against primary sources fetched that day. I tried to break the facts a design would lean on hardest. 18 were checked: 12 confirmed, 6 partly, none refuted. GitHub's issue API was not reachable from this session, so issue comment totals and "no staff reply" rest on the rendered pages, which hide part of each long thread.

Sources used more than once:
- Claude Code memory docs: https://code.claude.com/docs/en/memory
- Claude Code hooks reference: https://code.claude.com/docs/en/hooks
- Claude Code context window docs: https://code.claude.com/docs/en/context-window
- Claude Code changelog, searched whole (1.19 MB, newest 2.1.321): https://raw.githubusercontent.com/anthropics/claude-code/main/CHANGELOG.md
- Codex hooks reference: https://learn.chatgpt.com/docs/hooks
- Codex configuration reference: https://learn.chatgpt.com/docs/config-file/config-reference

### Verdicts

| Fact | Verdict | What the source says, and the correction |
|---|---|---|
| F1 Claude Code auto memory: default, storage, load limit, machine-local | confirmed | Every part is verbatim in the memory docs. Two additions: it is off by default in self-hosted environments, and it cannot be turned back on from a background session. The changelog also shows a background "memory extraction" writer (2.1.77, 2.1.273) and a model-backed "memory recall" (2.1.286, 2.1.288), so capture is not only in-session file writes. |
| F2 Write gate, four types, `modified`, "Saved N memories", `/memory` | confirmed | Verbatim in the memory docs. `modified` arrived in 2.1.214 (changelog). |
| F3 Users cannot tell what memory loaded (#82056) | partly | The issue, the requests and the three figures are real, but the figures are commenters' own: 95 of 229 entries (41.5%) outside the window; 48 of 155 orphaned topic files is one store (another had 68 of 155; 122 of 847 overall); the "empty store" is a project-folder name mismatch. "Cannot tell" is too strong: the docs say `/context` lists "which CLAUDE.md and auto memory files loaded", 2.1.210 made an over-limit index write an explicit error and 2.1.268 made the truncation warning say how many lines were cut. The page showed 23 comments plus 71 hidden items, so "about 76 comments" and "no staff reply" could not be checked in full; no staff badge appears in what loaded. Source: https://github.com/anthropics/claude-code/issues/82056 |
| F4 Most-requested features: portable, team, cross-agent memory | partly | #25739 (open, 33 comments, only a bot replied) and #38536 (open, about 33, no staff reply) are as described, and the stale-context, `supersedes`, `needs_verification` and receipt comments are there. Two things are off. #85557 does have an Anthropic reply: on 2026-08-17 bcherny wrote that auto memory is "plain Markdown, so it's portable — you can point it at a shared or checked-in location with the `autoMemoryDirectory` setting", and left the issue open only for the structured, validated extraction step. And "most-requested" is not supported: by the notes' own listing #34556 (114 comments), #59248 (67) and #91188 (60) drew more. Sources: https://github.com/anthropics/claude-code/issues/25739, https://github.com/anthropics/claude-code/issues/38536, https://github.com/anthropics/claude-code/issues/85557 |
| F5 Claude Code lifecycle hooks | partly | The four events and the collaborator comment of 2026-08-17 are confirmed. Three omissions change a design. (1) SessionStart also fires with source `clear` and `compact`, and the `compact` run is the only way to put context back after compaction: PostCompact has "no decision control" and cannot add context, and context that hooks added earlier is "Summarized with the rest of the conversation". (2) A hook cannot replace the compaction summary, but it can steer it: "Claude Code uses its stdout as instructions for the compaction summary" for a PreCompact hook that exits 0. (3) SessionEnd hooks share a 1.5-second default budget, and "Timeouts set on plugin-provided hooks don't raise the budget". Sources: hooks reference, context window docs, https://github.com/anthropics/claude-code/issues/47023 |
| F6 Claude Projects memory; apps' memory; local Cowork | confirmed | Verbatim in both sources. Additions from the Projects page: a thread run on the user's own computer "Starts with the project's instructions, but not with its memory files loaded"; cloud threads do not run hooks from `.claude/settings.json`, and plugins reach them only through Project settings. Sources: https://code.claude.com/docs/en/claude-projects, https://support.claude.com/en/articles/11817273-using-claude-s-chat-search-and-memory-to-build-on-previous-context |
| F7 Managed Agents Dreams | confirmed | Research preview; a memory store plus 1 to 100 sessions; a new output store; "The input store is never modified"; "minutes to a few hours"; billed at standard token rates. Source: https://platform.claude.com/docs/en/managed-agents/dreams |
| F8 Codex memories: off by default, two-phase pipeline | confirmed | Docs and the repository README say all of it. The defaults the notes could not find are in the configuration reference: idle 6 hours, age window 30 days, `max_unused_days` 30, 16 chats per startup, 256 raw memories kept for consolidation, rate-limit floor 25%. Sources: https://learn.chatgpt.com/docs/customization/memories?surface=app, https://raw.githubusercontent.com/openai/codex/main/codex-rs/memories/README.md, configuration reference |
| F9 OpenAI's guidance; controls; defaults still moving | confirmed | Both quotes, "generated state", the per-chat `/memories`, `disable_on_external_context` and delete-all are on the page. The changelog shows CLI 0.167.0 (2026-11-06, "Update default models for memory extraction and consolidation") and 0.166.0 (2026-10-29, two memory items). Source: https://learn.chatgpt.com/codex/changelog |
| F11 Copilot Memory: citations, votes, 90-day expiry | confirmed | Every part is on the page, including "Simply retrieving a memory does not reset this period" and "You cannot directly create or edit an individual memory". The down-vote threshold is not published. Source: https://docs.github.com/en/copilot/concepts/agents/copilot-memory |
| F12 GitHub's outcome numbers | confirmed | 90% against 83% merge rate, 77% against 75% positive feedback, p < 0.00001, the adversarial test and the "significant engineering complexity and LLM costs" reason are all in the post. These are GitHub's own figures. GitHub has since published two more for memory (see "Missed", item 12). Source: https://github.blog/ai-and-ml/github-copilot/building-an-agentic-memory-system-for-github-copilot/ |
| F13 Copilot Dreaming | confirmed | Public preview; "adds, updates, and deletes memories automatically, without approval for each change"; file changes arrive as pull requests; every run uses AI credits; turning it off does not undo or restore. "Dreaming is opt-in" is in the changelog post of 2026-10-28. Sources: https://docs.github.com/en/copilot/concepts/prompting-and-context/copilot-dreaming, https://github.blog/changelog/2026-10-28-github-intelligence-puts-your-code-context-and-history-to-work/ |
| F14 Gemini CLI Auto Memory | confirmed | Experimental, off by default, runs at startup, three hours idle and 10 user messages, "defaults to creating no artifacts unless the evidence is strong", inbox with approval. Source: https://geminicli.com/docs/cli/auto-memory.md |
| F23 OpenClaw's protective patterns | confirmed | All five are on the page, and both the pre-compaction flush and dreaming are on by default. Source: https://docs.openclaw.ai/concepts/memory.md |
| F24 ETH study of context files | confirmed | The abstract says all three things. Correct section 5's side numbers from version 3: files written by a model lowered success by about 0.5 points (SWE-bench Lite) and 2 points (the paper's own benchmark, CTXbench); developer-written files raised it by about 2.4 points, not significant (p = 21%), not "about 4 points". Authors are from ETH Zurich and LogicStar.ai. The arXiv page names no venue, so "listed at ICLR 2026" is unverified. Tested with Sonnet 4.5, GPT-5.2 and Qwen3 coder, which are older than today's models. Source: https://arxiv.org/html/2602.11988v3 |
| F25 VibeMemBench | confirmed | 1.1 to 4.5 points on four of five solvers, fewer steps on all five, eleven of twelve pairings at or below memory-off. Caveats the design should carry: the paper calls the gains "directional evidence" because all five intervals cross zero; targets were kept only if injected history helped; and the four systems are Mem0, SimpleMem, MemoryOS and A-MEM, general conversational memory systems, none built for coding. Source: https://arxiv.org/html/2609.23570v1 |
| F29 Quota complaints; the 11 to 16k memory preamble | partly | Claude Code #16157 has 1,499 comments (confirmed). Codex #14593 is closed, and its exact count was not shown (25 visible, 651 hidden items). The preamble claim is weaker than stated: 11.3k and 16.2k tokens were measured on 2.1.149; a commenter on 2.1.280 (2026-09-24) could not reproduce it and put the unexplained overhead at about 230 tokens; the author closed the issue as "no longer relevant to our workflow", with no Anthropic statement. Do not cite it as current behaviour. Sources: https://github.com/anthropics/claude-code/issues/63903, https://github.com/anthropics/claude-code/issues?q=is%3Aissue+sort%3Acomments-desc, https://github.com/openai/codex/issues/14593 |
| F30 "Auto Dream" described only by third parties | partly | No "dream" or "consolidat" appears in the docs index, the commands reference or anywhere in the full changelog, which is stronger than the notes claim. But Anthropic's own changelog does describe automated memory work the docs leave out: "memory-extraction writes" (2.1.77), "Changed the `Improved N memories` line" (2.1.181), and "memory recall" that failed "in organizations that cannot use the default Sonnet model" (2.1.286). So background extraction, a pass that improves memories and model-backed recall exist in some form. This also answers the notes' open question about a side model: recall makes a model call of its own (2.1.288 lists it with session titles among the features that need structured outputs). Sources: changelog, https://code.claude.com/docs/llms.txt, https://code.claude.com/docs/en/commands |

Not checked: F10, F15 (the staff quote and the export path were confirmed in passing; the staff reply gives no reason for the removal), F16, F17, F18 (issue re-read: opened 2026-10-29, closed and reopened 2026-11-02, fix pull request #5220 still open, no written maintainer reply), F19 to F22, F26 to F28.

### Missed: facts the design needs that the notes lack

1. **Only a SessionStart hook with source `compact` restores context after compaction in Claude Code.** "Context that hooks added earlier" is "Summarized with the rest of the conversation"; for SessionStart hooks that match `compact`, "Claude Code runs them and adds their output to the compacted context". Auto memory and the root CLAUDE.md are re-read from disk. Idea 9 in section 7 names PostCompact as a place to put state back; it cannot. https://code.claude.com/docs/en/context-window
2. **A PreCompact hook can steer the summary.** Its stdout becomes "instructions for the compaction summary", cut at 10,000 characters per hook (2.1.314). https://code.claude.com/docs/en/hooks
3. **Injected context has hard size caps on both hosts.** Claude Code caps each `additionalContext` or stdout string at 10,000 characters; past that it saves the text to a file and passes a preview of the first 2,000 characters, and "doesn't ask Claude to read the file". Codex limits each hook's `additionalContext` to about 2,500 tokens by default (`additionalContextLimit`). https://code.claude.com/docs/en/hooks and https://learn.chatgpt.com/docs/hooks
4. **End-of-session hooks get about one second.** Claude Code: 1.5 seconds shared by all SessionEnd hooks, and a plugin's own timeout does not raise it. Codex: SessionEnd defaults to 1 second, 3 at most, and its `reason` "is always `other`" for now. Work at session end has to be handed off or left for the next start. Same two URLs.
5. **Codex now has the same lifecycle hooks as Claude Code**, so one design can serve both: SessionStart (source `startup`, `resume`, `clear`, `compact`, `fork`), SessionEnd, UserPromptSubmit, Stop, PreCompact, PostCompact. "Hooks are enabled by default", but "Non-managed hooks must be reviewed and trusted before they run", trust is tied to the hook's hash, and "If a plugin update changes a bundled script, Codex requires a new review". `transcript_path` may be null. https://learn.chatgpt.com/docs/hooks
6. **A per-prompt hook in Claude Code also runs for prompts no person typed**: scheduled task firings, background subagent reports and messages from other sessions. Its default timeout is 30 seconds, and on timeout its output is discarded and the prompt goes ahead. https://code.claude.com/docs/en/hooks
7. **Where the layer can and cannot run in the Claude desktop app.** The Code tab's local sessions use the CLI's files: "Hooks and skills defined in settings apply to both", and plugins install from a plugin manager. Cowork "sources its skills, plugins, and connectors from this Customize configuration, which syncs through your claude.ai account, not from the CLI's `~/.claude` directory". Cloud sessions do not run hooks from `.claude/settings.json` and do not install plugins a repository declares. Plugins are not available in WSL sessions. https://code.claude.com/docs/en/desktop and https://code.claude.com/docs/en/claude-projects
8. **Anthropic's own answer to portability is `autoMemoryDirectory`** (the bcherny reply quoted under F4). A layer with a git store can hold Claude's auto memory folder inside it, in place of competing with it. The value must be absolute or start with `~/`, and one set by a repository's settings needs workspace trust. https://github.com/anthropics/claude-code/issues/85557 and https://code.claude.com/docs/en/memory
9. **Claude Code already ships pieces of the features users ask for, on its cloud surfaces.** The changelog names "mounted team memory stores (`CLAUDE_MEMORY_STORES`) in remote sessions" (2.1.172) and workspace and channel memory files in Claude Tag (2.1.268, 2.1.296). It also cleans what it loads: "invisible characters and tags that imitate Claude Code's own markup are neutralized in `MEMORY.md` and recalled memory notes" (2.1.284). Changelog URL above.
10. **Injected text that changes between requests costs a full prompt-cache miss.** Fixes for this in 2.1.275 (a memory file's "age note"), 2.1.277 (SessionStart hook output) and 2.1.295 (a hook's unchanged context added again on every resume). What a hook injects should be byte-stable within a session. On resume, SessionStart input carries `seconds_since_last_response`, `context_tokens` and `prompt_cache_likely_expired` (2.1.251 or later), which say when a fresh brief is cheap. Changelog and hooks reference.
11. **How Codex measures use, and what its store looks like.** The read path does "memory citation parsing, and read-usage telemetry classification", which is the likely source of `usage_count`; the README does not say so outright. The memory folder is "a git-baseline directory"; consolidation runs only when the git-style diff against the last baseline is not empty; its outputs are `MEMORY.md`, `memory_summary.md` and `skills/`. https://raw.githubusercontent.com/openai/codex/main/codex-rs/memories/README.md
12. **GitHub published newer numbers, for memory and none for dreaming.** Copilot Memory is "now generally available on paid Copilot plans" (2026-10-28, which settles the notes' open question on the date). Of reviewed CLI sessions that used memory, "93% showed better adherence to developer preferences" and "42% showed more efficient workflows". https://github.blog/changelog/2026-10-28-github-intelligence-puts-your-code-context-and-history-to-work/
13. **Why the memory systems lost in VibeMemBench: the form of the record, not the search.** Ranking misses were 1.3% of failing pairings; degraded form was 69.3%. Records tied to a directory or identifier helped; record length did not predict the outcome; irrelevant injected text lowered resolution by 0.7 to 3.4 points; the same record helped weaker solvers and hurt stronger ones. https://arxiv.org/html/2609.23570v1
14. **Text a person wrote beats text a model wrote, in the ETH study**: developer-written files "outperform LLM-generated ones by a significant margin of 7% on average". The authors advise leaving model-written files out for now. A memory layer's pages are model-written. https://arxiv.org/html/2602.11988v3
