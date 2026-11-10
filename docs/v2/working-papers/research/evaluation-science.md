# Evaluation science for agent memory: what is measured, what the evidence says, and how a memory for coding agents should be measured

Research notes, written 2026-11-10. Topic: benchmarks, results, failure modes, and a proposed evaluation programme for a Markdown-and-git memory layer for coding agents.

## 0. How to read these notes

- Every fact has a URL and, where known, the date of the source.
- **Source quality tags.** `[paper]` = arXiv or proceedings page read directly. `[vendor]` = a number reported by the party whose product it flatters. `[secondary]` = a summary site or search snippet; the primary source was not opened or did not show the number. `[local]` = read from this repository's own docs (never sent to any search).
- **Reading caveat.** Pages were read through an automated fetch-and-summarise tool. Numbers below are as that tool reported them from the page. Before any number is published in a plan or a README, re-check it against the PDF. Where two sources disagreed I say so.
- Many 2026 sources are preprints, not peer reviewed. Several are small (tens to low hundreds of tasks). I say so where it matters.

---

## 1. The benchmark landscape

### 1.1 Conversational memory benchmarks (question answering over chat history)

| Benchmark | What it is | Size | Source |
|---|---|---|---|
| **DMR** (Deep Memory Retrieval, from the MemGPT paper) | Single-turn fact retrieval over a multi-session chat | 60 messages per conversation | Described and criticised in the Zep paper, https://arxiv.org/html/2501.13956 (Jan 2025) `[paper, by a vendor]` |
| **LoCoMo** | QA, event summarisation, multimodal dialogue generation over very long two-person chats | ~300 turns, ~9K tokens average per conversation, up to 35 sessions; 1,540 scored questions + 446 adversarial | https://arxiv.org/abs/2402.17753 (27 Feb 2024) `[paper]`; question counts from https://github.com/dial481/locomo-audit `[audit]` |
| **PerLTQA** | Personal long-term memory QA: semantic + episodic memory; classification, retrieval, synthesis | 8,593 questions, 30 characters; fictional, generated with gpt-3.5-turbo; Chinese | https://arxiv.org/abs/2402.16288 (Feb 2024; SIGHAN-10) `[secondary summary of the paper]` |
| **LongMemEval** | Five abilities: information extraction, multi-session reasoning, temporal reasoning, knowledge updates, abstention | 500 questions; `_S` ≈ 115k tokens (~50 sessions) per question, `_M` = 500 sessions ≈ 1.5M tokens; 30 abstention questions | https://arxiv.org/abs/2410.10813 and https://arxiv.org/html/2410.10813v2 (Oct 2024, ICLR 2025) `[paper]` |
| **MemBench** | Factual vs reflective memory; participation vs observation scenarios; effectiveness, efficiency, capacity | — | https://arxiv.org/abs/2506.21605 (ACL 2025 Findings) `[secondary summary]` |
| **ConvoMem** (Salesforce) | 75,336 QA pairs: user facts, assistant recall, abstention, preferences, temporal changes, implicit connections | 75,336 QA | https://arxiv.org/abs/2511.10523 (13 Nov 2025) `[secondary summary of abstract]` |
| **BEAM** ("Beyond a Million Tokens") | Generated coherent conversations up to 10M tokens, 10 memory abilities | 100 conversations, 2,000 validated questions | https://arxiv.org/abs/2510.27246 (31 Oct 2025, ICLR 2026) `[secondary summary of abstract]` |
| **HaluMem** | Operation-level hallucination: memory extraction, memory updating, memory QA scored separately | Medium and Long variants | https://arxiv.org/abs/2511.03506 (Nov 2025) `[secondary summary]` |
| **SubtleMemory** | Fine-grained discrimination between near-identical / contradictory memories, with staged diagnostics (preservation → retrieval → answer) | — | https://arxiv.org/html/2606.05761 (v3 5 Oct 2026) `[paper]` |
| **STALE** | Whether an agent knows a stored memory is no longer valid when it was invalidated implicitly | 400 expert-validated scenarios, 1,200 queries, contexts to 150K tokens | https://arxiv.org/abs/2605.06527 (7 May 2026) `[secondary summary]` |
| **PersistBench** | When memories should NOT be used: cross-domain leakage, memory-induced sycophancy | 18 models | https://arxiv.org/abs/2602.01146 (Feb 2026, ICML 2026 / PMLR 306) `[secondary summary of the arXiv HTML]` |

### 1.2 Agentic / multi-competency memory benchmarks

| Benchmark | What it is | Source |
|---|---|---|
| **MemoryAgentBench** | Four competencies — accurate retrieval, test-time learning, long-range understanding, conflict resolution — fed incrementally; compares long-context, BM25, embedding RAG, structure-augmented RAG (RAPTOR, GraphRAG, HippoRAG-v2, Mem0, Cognee), agentic (Self-RAG, MemGPT) | https://arxiv.org/html/2507.05257v1 (Jul 2025) `[paper]` |
| **MemoryArena** | Multi-session "memory–agent–environment" loops with interdependent subtasks (web navigation, preference-constrained planning, progressive search, formal reasoning) | https://arxiv.org/abs/2602.16313 (18 Feb 2026; v2 17 Sep 2026) `[paper]` |
| **AMA-Bench** | Memory over agent trajectories (states, actions, observations, tool outputs), not dialogue; real + synthetic trajectories of arbitrary length | https://arxiv.org/abs/2602.22769 (26 Feb 2026; v4 27 May 2026) `[paper abstract]` |
| **LongMemEval-V2** | Whether memory turns long web-agent histories into reusable environment knowledge: static state recall, dynamic state tracking, workflow knowledge, environment gotchas, premise awareness. 451 questions; histories up to 500 trajectories / 115M tokens | https://arxiv.org/abs/2605.12493 (12 May 2026, "work in progress") `[paper abstract]` |
| **Evo-Memory** (DeepMind + UIUC) | Streaming task sequences to test test-time evolution of memory; >10 memory modules | https://arxiv.org/abs/2511.20857 (25 Nov 2025) `[secondary summary]` |
| **STATE-Bench** (Microsoft) | 450 enterprise tasks against a stateful environment; deterministic final-state scorer; completion, consistency (pass^5), efficiency, communication; has a pluggable Memory Track | https://opensource.microsoft.com/blog/2026/05/19/introducing-state-bench-a-benchmark-for-ai-agent-memory/ (19 May 2026) `[secondary summary of the vendor post]` |
| **DolphinBench** | Memory judged by task completion; 3 personas × ~500k tokens of user messages × 200 tasks; each task verified to succeed with the history and fail without; requires reporting cost and latency with accuracy | https://arxiv.org/abs/2609.24971 (21 Sep 2026) `[paper abstract]`. Note: the authors include people I recall as a memory vendor's founders (from memory, not verified), so treat as vendor-affiliated. |
| **AgingBench** | Reliability over a deployment's lifespan (8–200 sessions): compression, interference, revision and maintenance "aging". Two coding scenarios (S4 design notes and dependencies across sprints; S7 a self-planning agent on Claude Code / OpenHands with pytest checks) | https://arxiv.org/html/2605.26302 (25 May 2026) `[paper]` |
| **Cross-scenario generality study** | Eight memory systems + an agent harness (raw text + grep/read) across LoCoMo, HotpotQA, AMA-Bench, MemoryAgentBench, ALFWorld, MemoryArena | https://arxiv.org/html/2606.04315 (3 Jun 2026) `[paper]` |
| **"Are We Ready For An Agent-Native Memory System?"** | 12 memory systems, 5 workloads / 11 datasets, module-level ablations (representation, extraction, retrieval/routing, maintenance) | https://arxiv.org/abs/2606.24775 (23 Jun 2026) `[paper abstract]` |

### 1.3 Benchmarks and studies specific to coding agents and long-running software work

This is the part that matters most here, and it has moved a great deal in 2026.

**Does memory change coding outcomes?**

- **VibeMemBench** — 111 coding targets from 90 SWE-rebench V2 repositories with 3,634 history trajectories; executable tests decide; every target is screened so that injected history *can* help; five held-out solvers; matched memory-off baseline plus an irrelevant-memory control. https://arxiv.org/abs/2609.23570 and https://arxiv.org/html/2609.23570 (20 Sep 2026) `[paper]`
- **SWE-ContextBench** — 300 base + 99 related tasks (Lite; 1,100 + 376 across 51 repos and 9 languages in Full); conditions: no context, free retrieval of trajectories or summaries, oracle trajectory, oracle summary, and three memory frameworks; reports resolve rate, time and cost. https://arxiv.org/html/2602.08316v3 (v3 6 May 2026) `[paper]`
- **SWE-Bench-CL** — SWE-bench Verified rearranged into 8 chronological per-repo sequences (273 tasks); defines forgetting, forward/backward transfer, area under the learning curve, tool-use efficiency. *No memory-vs-no-memory result is reported*: "full empirical evaluation … is ongoing"; preliminary runs had pass rates below 8.5% with memory "on par with or slightly worse". https://arxiv.org/html/2507.00014v1 (13 Jun 2025) `[paper]`
- **FSE-AIWare 2026 competition, "Intelligent Coding Assistant Enhanced with Memory"** — 200 SWE-Bench Pro instances (8 projects × 25), evaluated sequentially per project in commit-time order with a persistent shared directory; metrics: task success, efficiency (tokens, turns), and **Memory Utility = difference in task success with and without memories**; budget $2 per instance. No results on the page. https://mem-comp.github.io (event 5–9 Jul 2026) `[primary page]`
- **SDE-bench** (a vendor's own) — 61 bug-fix tasks in one library with ~140 decoy conversations; each task hinges on a project-specific decision that is not in the repo; a hidden test checks it; failed attempts get the failures back, up to five rounds; metric **corrections per task**; deterministic pytest, no LLM judge. https://hindsight.vectorize.io/blog/2026/08/06/hindsight-0-9-0 (6 Aug 2026) `[vendor]`
- **Structurally aligned subtask-level memory** — argues whole-episode ("instance-level") memory is the wrong granularity; +4.7 pp mean Pass@1 on SWE-bench Verified over a vanilla agent. https://arxiv.org/abs/2602.21611 (25 Feb 2026) `[paper abstract]`
- **ProofDebt** — memory for continual repair built from test outcomes, changed files, dependency links and patch status rather than self-written summaries; on the seven tasks an audit found needed earlier repair information: reset 14.3%, ProofDebt 60.3%, oracle memory 82.5%; argues evaluations must first measure whether a task offers a real opportunity for memory. https://www.preprints.org/manuscript/202610.0373 (Oct 2026) `[secondary: page returned 403, numbers from a search snippet]`

**Continuity from one session to the next (handoff, resume, task switching)**

- **Handoff Debt** — 75 SWE-bench Verified tasks, 181 deterministic interruption points, 2,172 takeover runs; the repository is held fixed and only the *handoff view* changes: repository only, raw trace, summary notes, structured notes. Metrics: resolution, agent events, cumulative prompt tokens. https://arxiv.org/html/2606.02875v2 (Jun 2026; EMNLP 2026 per https://aclanthology.org/2026.emnlp-main.738/) `[paper]`
- **HSC-Bench** ("Benchmarking Task Takeover Across Agent Handoffs") — 1,039 tasks, two tracks: recovering repository evidence, and continuing from inherited conversational, tool and file-work state; checks later actions for adherence to established commitments and completed work. https://arxiv.org/abs/2610.36523 (27 Oct 2026) `[paper abstract]`
- **Switch-SWE** — 200 focus tasks from 19 Python repositories; each has an uninterrupted control and a "switched" session that interleaves four other unfinished tasks from the same repository; same executable verifier. Tested on seven model/agent configurations including Codex and Claude Code. https://arxiv.org/html/2611.02017 (1 Nov 2026) `[paper]`
- **Factory's compression probes** — after compressing a real session, ask four probe types and grade the answers: *recall* ("What was the original error message?"), *artifact* ("Which files have we modified?"), *continuation* ("What should we do next?"), *decision* ("What did we decide about X?"). 36,000+ production messages. https://factory.com/news/evaluating-compression (16 Dec 2025) `[vendor]`
- **MemoryCode** — synthetic multi-session mentor/mentee coding conventions with distractors. https://arxiv.org/abs/2502.13791 (ACL 2025) `[secondary]`
- **ContinuityBench** — mentioned in Switch-SWE's related work as resuming interrupted agents from preserved environment states under different handoff conditions. *Not verified directly.*

**Standing instructions, rules and context files**

- **"Evaluating AGENTS.md"** (ETH Zurich + LogicStar; benchmark called CTXbench in v3, AGENTbench in earlier coverage) — Claude Code (Sonnet 4.5), Codex (GPT-5.2, GPT-5.1 mini), Qwen Code; none / LLM-generated / developer-written context files. https://arxiv.org/html/2602.11988 (Feb 2026; v3 29 Sep 2026; ICLR 2026 listing https://iclr.cc/virtual/2026/10018749) `[paper]`
- **TellOnce** — 32 real coding-agent transcripts from one user over ~two months; 142 correction-conflict records; measures how often a past correction is violated again. https://arxiv.org/html/2606.13174 (v2 25 Oct 2026) `[paper]`
- **SkillsBench** — curated vs self-generated procedural "skills" across 84–86 tasks. https://arxiv.org/abs/2602.12670 (Feb 2026; NeurIPS 2026 poster https://neurips.cc/virtual/2026/poster/139353) `[paper / secondary for numbers]`

**Stale memory against a changing codebase**

- **EA-Graph** — "artifact-anchored verification memory … under upstream drift": prose notes record a conclusion without the program state that supported it; after an upstream change the repo may still build while earlier claims no longer hold. Generated repositories with ground truth by construction; classify each prior claim as unaffected / affected / unprovable; three memory conditions (anchored, prose notes, none); Haiku and Sonnet. https://arxiv.org/abs/2608.04278 (4 Aug 2026) `[paper abstract]`

**Related context-engineering benchmarks** (not memory proper): Letta's Context-Bench and Recovery-Bench (models average 11.2% on Recovery-Bench vs 26.3% on the original Terminal-Bench tasks) https://letta.com/blog/recovery-bench (27 Aug 2025) `[vendor, secondary summary]`.

---

## 2. Known problems with these benchmarks

1. **LoCoMo's answer key is wrong in places and its judge is lenient.** An audit reports 99 of 1,540 questions (6.4%) have wrong golden answers (156 issues in all: 99 score-corrupting, 57 citation-only), so the scoring ceiling is 93.57%. The 446 adversarial (category 5) questions — 22.5% of the dataset, the only abstention test — are not evaluated by any published result; the original formatter references a missing field on 444 of them. An LLM judge accepted 62.81% of intentionally wrong, vague-but-topical answers (10.61% of specific-but-wrong ones). https://github.com/dial481/locomo-audit (hashes "verified Feb 2026") `[audit by a party that also builds a memory product; not independently replicated that I found]`. Vendors now report LoCoMo scores in the low 90s, at or near that ceiling (e.g. 92.5%, reported in a vendor's 2026 benchmark overview, https://mem0.ai/blog/ai-memory-benchmarks-in-2026 or its companion https://mem0.ai/blog/state-of-ai-agent-memory-2026 `[vendor, secondary; I did not open the post to see which of the two carries the figure]`).
2. **LoCoMo fits in a context window and has no knowledge updates.** Conversations are ~16–26k tokens; no question tests a fact that changes; category 5 unusable; some questions need image details absent from the captions; some attribute statements to the wrong speaker; some have several defensible answers. https://www.getzep.com/blog/lies-damn-lies-statistics-is-mem0-really-sota-in-agent-memory/ (6 May 2025) `[vendor critique of a competitor]`
3. **DMR is trivial.** 60 messages, single-turn fact retrieval, ambiguous questions. A full-conversation baseline scores 94.4% (gpt-4-turbo) and 98.0% (gpt-4o-mini) against the memory system's 94.8% and 98.2%. https://arxiv.org/html/2501.13956 (Jan 2025)
4. **Retrieval metrics are passed off as answer accuracy.** A widely publicised 96.6% was recall_any@5 (is the right session in the top five), with no answer generated and no judge; a critical arXiv analysis attributes the number to verbatim storage plus a stock embedding store. https://arxiv.org/abs/2604.21284 (Apr 2026) `[secondary summary]`
5. **Harnesses are inconsistent, so the same system gets wildly different scores.** One 2026 paper documents truncation, home-grown judges and full-history leaks as measurement pitfalls and insists on the official judge plus the full-context baseline in every table. https://arxiv.org/abs/2606.09900 (5 Jun 2026) `[paper abstract, single author]`. Vendors disputing each other's numbers for the same system: Zep 65.99% (as run by Mem0) vs 75.14% (as run by Zep) on LoCoMo, https://www.getzep.com/blog/lies-damn-lies-statistics-is-mem0-really-sota-in-agent-memory/. Judge protocol choices alone can change method rankings on conversational QA, https://arxiv.org/html/2606.04315.
6. **QA benchmarks tell you which fact to fetch; real tasks do not.** "Most existing memory benchmarks use conversational question-answering, where the question signals which fact to retrieve" (DolphinBench, https://arxiv.org/abs/2609.24971). Systems near saturation on LoCoMo do poorly on MemoryArena's task loops: all-task success rate 0.09–0.20 for every method — long-context 0.09–0.19, memory systems (Letta, Mem0, Mirix, Mem0-g, ReasoningBank) 0.12–0.14, RAG 0.17–0.20. https://arxiv.org/html/2602.16313
7. **End-to-end scores hide where the failure is.** Several 2026 papers argue for stage-wise diagnostics: extraction / update / QA (HaluMem https://arxiv.org/abs/2511.03506); preservation → retrieval → answer (SubtleMemory https://arxiv.org/html/2606.05761); write / retrieval / utilisation (AgingBench https://arxiv.org/html/2605.26302); storage vs execution for attacks (https://arxiv.org/abs/2605.08442).
8. **Averaging over tasks where memory cannot help washes out, or reverses, the effect.** In VibeMemBench, 42.0% of pairings had a memory-off ceiling of 4/4 seeds, so memory could only lose (https://arxiv.org/html/2609.23570). ProofDebt makes the same argument (https://www.preprints.org/manuscript/202610.0373 `[secondary]`).
9. **Cost and latency are rarely reported.** MemoryAgentBench measured memory construction at 512-token chunks: Mem0 14,644 s, Cognee 8,309 s, HippoRAG-v2 817 s, BM25 0.11 s (https://arxiv.org/html/2507.05257v1). DolphinBench requires cost and latency in every submission (https://arxiv.org/abs/2609.24971).
10. **Small samples and no error bars.** SWE-ContextBench Lite's condition table is 99 tasks on one agent (https://arxiv.org/html/2602.08316v3); all five of VibeMemBench's headline gains have bootstrap intervals that cross zero (https://arxiv.org/html/2609.23570); Factory reports 0.26–0.35 point gaps on a 5-point scale with no intervals (https://factory.com/news/evaluating-compression). The statistical remedy is known: report standard errors, analyse question-level paired differences, plan sample size by power analysis (https://arxiv.org/abs/2411.00640, 1 Nov 2024).
11. **Synthetic data and model-generated ground truth.** PerLTQA is fictional and generated by gpt-3.5-turbo (https://arxiv.org/abs/2402.16288 `[secondary]`).
12. **Governance, recovery and forgetting are barely evaluated.** A survey of 435 works finds the literature concentrates on accumulating and retrieving state rather than governing, recovering or relinquishing it. https://arxiv.org/abs/2606.30306 (29 Jun 2026)

---

## 3. What the evidence says

### 3.1 Raw transcript vs extracted facts

- **LongMemEval (2024).** Splitting sessions into rounds improved reading. *Replacing* content with extracted summaries or facts generally hurt QA through information loss, except multi-session reasoning. Using extracted facts as extra *index keys* while returning the original text raised recall@k by 9.4% and final accuracy by 5.4% on average. https://arxiv.org/html/2410.10813v2
- **SubtleMemory (2026).** The two systems with the highest preservation rates (93.5% and 91.5%) both keep raw session logs; the authors attribute it partly to that. Systems that extract aggressively preserve as little as 39–40% of the detail needed. Oracle 85.4%; best systems 68–70%. https://arxiv.org/html/2606.05761
- **Cross-scenario study (2026).** "Oracle answerability" of what each system stored on AMA-Bench: HippoRAG 10%, SimpleMem 35%, AMA-Agent 83%, PlugMem 98% — a build-time schema silently drops step and action indices. A harness that keeps flat raw text and gives the agent grep/read had the best cross-task rank (3.67), ahead of long context (4.00). https://arxiv.org/html/2606.04315
- **Engram (2026).** Facts alone lose recall; facts combined with retrieved raw chunks recover the detail. 83.6% vs 73.2% full-context on LongMemEval_S with ~9.6k vs ~79k tokens. https://arxiv.org/abs/2606.09900 `[single-author preprint]`
- **For coding, the form of what is injected matters more than whether it was found (VibeMemBench).** Of 231 failing memory pairings: ranking misses 1.3%, coverage misses 16.0%, use failures 13.4%, and **form degradation 69.3%** — relevant history reached the solver but lost the fix in how it was rendered. Raw-transcript chunks produced all the "instruction pollution" and shell-noise cases; LLM-extracted atomic facts produced all the over-generalised-fix, schema-omits-fix and symptom-instead-of-fix cases. A short four-field record (bug class, root cause, fix pattern, lesson) would have recovered 196 seeds on those pairings. A 334-line record dropped one target from 4/4 seeds to 0. https://arxiv.org/html/2609.23570
- **SWE-ContextBench.** With an oracle choosing the item, a concise summary beat the full trajectory: 34.34% vs 27.27% resolved (no-context 26.26%, 99 tasks, one agent — small). https://arxiv.org/html/2602.08316v3
- **Handoff Debt.** The raw trace gave the largest gains (+6.1 to +14.9 pp solved, events −57 to −59%) but at ~87k characters of prompt; compact notes (7–10k characters) kept most of the efficiency (events −20 to −46%, tokens −42 to −63%) with smaller, model-dependent solve gains. Neither format dominated. https://arxiv.org/html/2606.02875v2
- **HaluMem.** Extraction and update are where commercial systems fail: update *omission* above 60% on the Long set for five of six systems, and above 98% for two; all systems below 70% answer accuracy. https://arxiv.org/abs/2511.03506 `[secondary summary]`

**Reading:** keep the raw record as the source of truth and as the thing an agent can search; use extracted facts as an index into it; and when something is put in front of the model, put a short, structured, verified statement, not a transcript chunk and not a free-floating atomic fact.

### 3.2 Compute at write time vs compute at read time

- **Write-time compute pays when the future question is predictable and the result is reused.** Sleep-time compute: ~5× less test-time compute for the same accuracy on two stateful maths sets, up to +13% / +18% accuracy when scaled, ~2.5× lower average cost per query when several queries share a context; effectiveness correlates with query predictability. On a coding case study (SWE-Features) ~1.5× fewer test-time tokens at low budgets, measured by file-set overlap, not correctness. https://arxiv.org/html/2504.13171v1 (Apr 2025) `[secondary summary of the paper]`
- **Write-time compression with no retrieval at all can top a QA benchmark.** One framework's "observational memory" — background observer and reflector agents compress the chat into a dated observation log held in a stable, cacheable context of ~30k tokens — reports 94.87% on LongMemEval_S with gpt-5-mini (84.23% with gpt-4o; its own RAG 80.05%; full context 60.20%). https://mastra.ai/research/observational-memory (Feb 2026) `[vendor, secondary summary]`
- **Read-time "deep research" over a lightly indexed full history wins accuracy at a latency cost.** General Agentic Memory: a light memory plus the complete history in a page store, with a researcher agent at query time; outperforms memory-based baselines on LoCoMo, HotpotQA, RULER, NarrativeQA; reported 12–18 s per query. https://arxiv.org/abs/2511.18423 (Nov 2025) `[secondary; latency figure unverified]`
- **Heavy write-time construction is expensive and not reliably better.** Mem0 14,644 s and Cognee 8,309 s to build vs BM25 0.11 s; Mem0 scored 3.4 on test-time learning and 0.8 on long-range understanding where long context scored 82–89 and 29–52. https://arxiv.org/html/2507.05257v1
- **Localised maintenance beats global reorganisation on cost.** https://arxiv.org/abs/2606.24775
- **A coding-specific result for *when* to compute.** A vendor's coding-agent memory plugin made the agent *worse* with automatic recall on every prompt (1.06 corrections per task vs 0.97 with no memory): scattered fragments broke focus and per-prompt retrieval added latency. Replacing it with one synthesis pass on the first prompt, cached and re-injected, plus an on-demand tool, gave 0.85 → 0.36 (Claude Code), 1.34 → 0.47 (Codex CLI), 1.20 → 0.80 (opencode) corrections per task, mean of 3 runs, 61 tasks, with cost and wall time down. https://hindsight.vectorize.io/blog/2026/08/06/hindsight-0-9-0 `[vendor, own benchmark]`

**Reading:** spend at write time on things that are certain to be reused (the state of the work, the standing rules, the handful of constraints) and keep them in a stable prefix; do not spend per prompt; leave open-ended lookup to the agent at read time.

### 3.3 Multi-step search by the agent vs one-shot retrieval

- **A plain agent with file tools beats specialised memory products on LoCoMo.** 74.0% with gpt-4o-mini and grep / search_files / open / close over the history stored as a file, vs a reported 68.5% for a graph memory product. "Agents can rewrite questions into their own queries and keep searching." https://www.letta.com/blog/benchmarking-ai-agent-memory (12 Aug 2025) `[vendor]`
- **On very long agent histories, a coding agent searching trajectory files beats RAG by a wide margin.** LongMemEval-V2: coding-agent method 72.5% average, off-the-shelf coding agent 69.3%, strongest RAG 48.5%. The cost is latency, which the authors name as the key limitation. https://arxiv.org/abs/2605.12493
- **Active control generalises best across scenarios.** The grep/read harness ranks first across five scenarios; "memory performance depends more on giving the agent active control over storage and retrieval than on a passive store behind a fixed pipeline". https://arxiv.org/abs/2606.04315
- **But agentic search loses when the history fits in context and the question is structural.** The same harness scored 45.0 on LoCoMo vs 61.5 long-context and 62–67 for indexed methods; it won by +29.5 pp on HotpotQA where the corpus exceeds the window. An agentic plan–execute–judge loop beat single-pass by 3.8 pp on LoCoMo for ~3× the query tokens (24.8K vs 6.9K). https://arxiv.org/html/2606.04315
- **Similarity-only retrieval is lossy for trajectories.** AMA-Bench attributes current systems' weakness to missing causal/objective information and reliance on similarity retrieval; its tool-augmented method reaches 57.22%, 11.16% above the strongest baseline. https://arxiv.org/abs/2602.22769
- **Keyword search is a competitive retriever.** BM25 scored 100.0 on multi-needle retrieval (best embedding 73.5), beat embeddings on test-time learning (75.4 vs 69.4), and was within a few points on conflict resolution; embeddings won RULER-QA 83.0 vs 61.0. https://arxiv.org/html/2507.05257v1. On MemoryArena BM25 0.17 vs embedding 0.20 vs memory products 0.12–0.14. https://arxiv.org/html/2602.16313
- **The host vendor's own guidance is a hybrid.** Load a small instruction file up front; fetch everything else just in time with glob and grep, "avoiding stale indexes"; find "the smallest possible set of high-signal tokens". https://www.anthropic.com/engineering/effective-context-engineering-for-ai-agents (29 Sep 2025). A file-based memory tool plus context editing improved an internal 100-turn agentic search evaluation by 39% over baseline (context editing alone 29%, token use −84%). https://www.anthropic.com/news/context-management (2025) `[vendor, internal eval]`

**Reading:** for a coding agent, whose model is already trained to search files, the highest-ceiling retrieval design is "make the memory a well-organised set of files and let the agent search it", with one-shot injection reserved for a very small, very precise set.

### 3.4 Full-context baselines — and, for coding, the repository-only baseline

- **When the history fits, full context is hard to beat.** LoCoMo: full context 72.90% vs the best configuration of a memory product at 68.44% (https://www.getzep.com/blog/lies-damn-lies-statistics-is-mem0-really-sota-in-agent-memory/). ConvoMem: under 150 conversations full context reaches 70–82% on the hardest multi-message cases where a RAG memory product reaches 30–45% (https://arxiv.org/abs/2511.10523 `[secondary]`). MemoryAgentBench: long-context models win test-time learning and long-range understanding outright (https://arxiv.org/html/2507.05257v1).
- **When the history is long, full context degrades sharply.** GPT-4o on LongMemEval_S: 0.870 with oracle sessions vs 0.606 with the full ~115k-token history (−30%); a commercial assistant's built-in memory dropped to 0.5773 from 0.9184 offline reading (https://arxiv.org/html/2410.10813v2). Every one of 18 models scored higher on ~300-token focused prompts than on ~113k-token full prompts for the same questions (https://www.trychroma.com/research/context-rot, 14 Jul 2025).
- **In agentic task loops, external memory is "not universally beneficial"** and does not reliably beat long context; it helped most where traces exceed 120k tokens. https://arxiv.org/html/2602.16313
- **For a coding agent the equivalent baseline is the repository itself**, and it is strong: the agent can re-derive a great deal from code, tests, docs and git history. Evidence: LLM-generated context files helped only when all other documentation was removed (+2.7%), i.e. they were redundant with what the agent could already read (https://arxiv.org/html/2602.11988). Repository-only takeover resolved 34–46% of interrupted tasks; what the handoff note bought was mostly *efficiency* (https://arxiv.org/html/2606.02875v2). EA-Graph and VibeMemBench both build in a no-record / memory-off baseline for this reason.

**Reading:** every result must sit beside (a) memory off, (b) an irrelevant-memory control, (c) an oracle-memory ceiling and, where the history fits, (d) the raw history in context. A memory system that cannot beat (a) on tasks where (c) beats (a) is not working.

### 3.5 Reasoning about time and updated knowledge

- **Conflict resolution is the weakest competency measured.** Single-hop: GPT-4o long-context 60.0, BM25 56.0, a memory product 18.0. Multi-hop: best 7.0 across all methods. A reasoning model (o4-mini) reached 100 / 80 at 6K tokens but fell to 14.0 multi-hop at 32K. https://arxiv.org/html/2507.05257v1
- **Updates are silently dropped at write time.** Update omission above 60% for five of six commercial systems on HaluMem-Long. https://arxiv.org/abs/2511.03506 `[secondary]`
- **Implicit invalidation is largely unsolved.** STALE: best model 55.2% overall; systems often retrieve the new evidence but leave old and new memories coexisting without adjudication ("current-state adjudication gap"). https://arxiv.org/abs/2605.06527 `[secondary]`
- **Explicit time handling helps.** Time-aware query expansion raised recall on temporal questions by 11.3% (rounds) / 6.8% (sessions), but only with a strong model extracting the time range (https://arxiv.org/html/2410.10813v2). A temporal knowledge graph raised temporal-reasoning accuracy from 45.1% to 62.4% (gpt-4o) over full context, but got *worse* on recalling what the assistant itself had said (94.6% → 80.4%) and, with the smaller model, on knowledge updates (76.9% → 74.4%) (https://arxiv.org/html/2501.13956 `[vendor paper]`). A bi-temporal model that invalidates rather than deletes, with an "as-of" filter, is the design of the 2026 engine cited above (https://arxiv.org/abs/2606.09900).
- **Over a deployment's life, behaviour can look fine while facts decay.** AgingBench: behavioural compliance stayed near zero violations while factual precision dropped; agents wrote the information but retrieved too little before answering; routine maintenance (recompaction, migration) caused regressions (−0.21 on one migration event). Recommendations: preserve exact values at write time, maintain derived state explicitly or recompute it, run regression checks after maintenance. https://arxiv.org/html/2605.26302

### 3.6 Abstaining when nothing is known

- **The main benchmark's abstention set is tiny, and the other one's is broken.** LongMemEval has 30 false-premise questions of 500 (https://arxiv.org/html/2410.10813v2); LoCoMo's 446 adversarial questions are unusable (https://github.com/dial481/locomo-audit).
- **Model families differ.** Under distractors, Claude models abstain when uncertain (lowest hallucination rates, lower scores on full prompts); GPT models gave the most confident wrong answers. https://www.trychroma.com/research/context-rot
- **Lenient judges hide the problem.** 62.81% of vague-but-topical wrong answers accepted. https://github.com/dial481/locomo-audit
- **Newer benchmarks test it directly:** premise awareness (LongMemEval-V2, https://arxiv.org/abs/2605.12493), premise resistance (STALE, https://arxiv.org/abs/2605.06527), and "unprovable rather than guessed" (EA-Graph, https://arxiv.org/abs/2608.04278, where no session fabricated withheld content).
- **For coding-agent memory, not injecting is a first-class action.** A small 2026 study frames memory use as a risk-sensitive choice among: no memory, inject top result, summarise candidates, abstain, ask; with a reward that penalises a false-positive injection more than a missed reuse (62.5% success, 0.0% false positives in offline replay; smoke-scale, proxy-based, preliminary). https://arxiv.org/abs/2604.27283 (30 Apr 2026)

---

## 4. Failure modes

### 4.1 Memory poisoning and prompt injection through stored memory

- **It is a named top-ten risk.** OWASP Top 10 for Agentic Applications 2026, published 9 Dec 2025 (https://genai.owasp.org/resource/owasp-top-10-for-agentic-applications-for-2026/). ASI06 "Memory & Context Poisoning": corruption that persists and shapes later decisions, temporally decoupled from the write. *I could not read OWASP's own ASI06 text; the description is from secondary sources* (e.g. https://christian-schneider.net/blog/persistent-memory-poisoning-in-ai-agents/).
- **Query-only poisoning works in the lab.** MINJA injects malicious records by ordinary interaction (bridging steps, an indication prompt, progressive shortening). https://arxiv.org/abs/2503.03704 (Mar 2025; v5 Feb 2026). Reported 98.2% injection success and 76.8% attack success `[secondary; not on the abstract page]`; a January 2026 follow-up says these held under idealised conditions (https://arxiv.org/abs/2601.05504 `[secondary]`).
- **Once a payload is in an auto-loaded instruction file, the host follows it.** "Bad Memory": single-probe attack success with the payload in CLAUDE.md / AGENTS.md — Haiku 4.5 63.3%, Opus 4.7 30.0%, GPT-5.2 23.3%, GPT-5.5 60.0% (mean of three attack types, 10 trials each, synthetic workspace). Agents "trust instructions written in a persistent context or configuration file as if the user authored them". Persistence does not track success: Opus recognised an injection but left it in place 93–97% of the time. One model moved a payload it did not recognise as malicious *into* AGENTS.md. Untrusted external content "rarely causes an agent to overwrite its own trusted files" — so the dangerous step is whatever automated path writes them. https://arxiv.org/html/2607.14611v1 (16 Jul 2026)
- **Aggressive write and retrieval policies widen the attack surface**, and prompt-injection defences do not cover it. Four write channels, nine structural vulnerabilities, six attack classes; MPBench. https://arxiv.org/abs/2606.04329 (Jun 2026) `[abstract only]`
- **Storing and acting are separate; test both.** Input filters never see a payload that arrives through retrieval; small retrieval-layer classifiers did not flag it; the only defence that blocked the attack removed the recall tool (0% for eight of nine models) and was bypassed when the model rebuilt the instruction from the corpus. Zero exfiltration in a test is weak evidence of resistance. https://arxiv.org/abs/2605.08442 (May 2026; v6 Oct 2026) `[single-author preprint]`
- **Benchmark the whole lifecycle.** HarnessSafe: 328 executable cases over seven "persistent carrier" families (memory, skills, tools, shared artifacts), tracked from entry through persistence to a later benign trigger; containment depends on carrier and on the harness–model pairing. https://arxiv.org/abs/2608.06984 (7 Aug 2026)
- **Real incidents on coding agents.** A Cloud Security Alliance note (27 Oct 2026) lists: hidden Unicode in rules files (Mar 2025); a self-propagating worm via committed agent config (Jun 2026, 73 repositories disabled); a planted AGENTS.md in a Downloads folder that got one host to install a malicious package and another to redirect API traffic (Oct 2026, no patch). Controls recommended: review instruction-file changes like build scripts, scan for zero-width and bidirectional Unicode, deny agents write access to their own settings, express prohibitions as enforced permissions rather than prose, provenance or signing. https://labs.cloudsecurityalliance.org/research/csa-research-note-agent-instruction-file-poisoning-20261027/
- **Memory that should not be used.** PersistBench: median 53% failure on cross-domain leakage and above 90% on memory-induced sycophancy across 18 models. https://arxiv.org/abs/2602.01146 `[secondary]`

### 4.2 Errors that reinforce themselves

- **Agents follow retrieved experience closely, including its mistakes.** "Experience-following": when a new task resembles a stored record, the output closely copies it; flawed records are copied and, if the run is saved back, amplified. Storing everything was the worst policy on all four agents (e.g. a code-generation agent: 13.05% with add-all vs 16.75% with a fixed 100-record memory vs 38.50% with strict, ground-truth-gated addition). An ordinary LLM self-judge as the gate can do worse than a small curated set; a ground-truth evaluator is best. Deleting records that keep producing bad outcomes helps further. https://arxiv.org/html/2505.16067v2 (May 2025; ACL 2026)
- **Self-written procedural memory does not help on average.** Curated skills +16.2 pp average pass rate (+4.5 pp in software engineering); self-generated skills −1.3 pp. https://arxiv.org/abs/2602.12670 `[numbers from secondary summaries; versions differ slightly]`
- **Belief drift across sessions.** MemoryArena describes small errors accumulating across sessions, success decaying with subtask depth for every method. https://arxiv.org/html/2602.16313
- **Memory-only self-improvement hits a ceiling.** ALFWorld: vanilla 46.7%, best memory method 43.3%, hand-written golden procedural memory 71.7%. https://arxiv.org/html/2606.04315
- **What counters it: ground memory in checks the world gives you.** Test outcomes, changed files and patch status rather than self-summaries (ProofDebt); claims anchored to the content that established them (EA-Graph); "treat handoff text as evidence to verify, not as ground truth" (Handoff Debt).

### 4.3 Stale facts

- Covered in 3.5 (STALE, HaluMem, AgingBench) and, for code, EA-Graph: with a smaller model, artifact-anchored claims beat both prose notes and no memory in all seven worlds (paired Wilcoxon p = 0.0156); with the larger model, the anchored condition was perfect but controls were at ceiling. Evidence strength is kept separate from freshness; when replacement content is unavailable the claim "becomes unprovable rather than guessed". https://arxiv.org/abs/2608.04278
- A successor "can over-trust the predecessor's interpretation and follow an unproductive path"; a compact note "can drop an exact validation command or warning that matters". https://arxiv.org/html/2606.02875v2

### 4.4 Context rot from injecting too much

- **Longer input, worse performance, even on trivial tasks; near-miss distractors are the worst kind.** 18 models; one distractor lowers accuracy, four lower it further. https://www.trychroma.com/research/context-rot
- **Context files cost more and usually help less than nothing.** LLM-generated repository context files: −0.5% (SWE-bench Lite, not significant) and −2% (CTXbench) success, with +20–23% cost and +2.5–3.9 steps (significant). Developer-written: about +2.4% (not significant), up to +19% cost. Agents obey what the files say (a mentioned tool used 1.6× per instance vs <0.01), run more tests and read more files; repository overviews did not reduce steps to the relevant files. Recommendation: human-written files should state only what is not already discoverable. https://arxiv.org/html/2602.11988
- **For injected coding experience the harm is volume, not instructions.** Removing flagged instruction lines recovered ~0.60 seeds per pairing; deleting the same number of random lines recovered ~0.48. A wider retrieval budget (1 to 5 records) never closed the gap. An irrelevant passage cost 0.7–3.4 pp. Eleven of twelve solver × memory-system pairings were at or below memory-off. https://arxiv.org/html/2609.23570
- **Wrongly selected context is worse than none.** Free summary retrieval 22.22% vs no context 26.26%; free trajectory retrieval +27.3% cost for no gain. https://arxiv.org/html/2602.08316v3
- **Per-prompt automatic recall made a coding agent worse** (3.2 above). https://hindsight.vectorize.io/blog/2026/08/06/hindsight-0-9-0

### 4.5 The access–compliance gap (remembering is not obeying)

- TellOnce: with *all* of a user's past corrections in context, compliance on held-out checks was 55.0% (31.6% with none; 42.5% with a memory product, i.e. 57.5% still violated). Agents "acknowledge a rule, retrieve it, or restate it in a plan, yet fail to carry it into the final response". Compiling corrections into runtime checks that can block or request revision reached 70.1% on that diagnostic and cut violation rates to 12.6–68.8% across six model × benchmark settings where no-memory was 37.8–100%. https://arxiv.org/html/2606.13174
- MemoryCode: instruction-following held above 94% for a single instruction but fell to 30.5% over 16–100 sessions of history (GPT-4o). https://arxiv.org/abs/2502.13791 `[secondary]`
- The CSA note cites a preprint finding only 4–16% of security rules in public CLAUDE.md files had a matching enforced control. https://labs.cloudsecurityalliance.org/research/csa-research-note-agent-instruction-file-poisoning-20261027/

---

## 5. What this means for the system being redesigned

Mapping the evidence to the system's parts (capture → redacted inbox; code-built resume brief; background model run that writes thread pages, rules, knowledge and system pages; keyword recall injection; read-only MCP server).

1. **The resume brief is the highest-value, best-supported feature.** Handoff Debt shows a compact handoff view cuts a successor's events by 20–59% and prompt tokens by 42–63%; Switch-SWE shows per-task state plus routing recovers 5.5–15.5 pp lost to task switching — and the system's per-branch thread pages are exactly a per-task state store. The field that mattered most was **validation evidence**: what was tested, what failed, what was done about it. The weakest dimension in every compaction method measured is the **artifact trail** (2.19–2.45 of 5): which files changed and how.
2. **Per-prompt knowledge injection is the riskiest feature.** Three independent coding results (VibeMemBench, the ETH study, the vendor's corrections-per-task result) say unsolicited injection is at best neutral and often harmful unless it is very precise and very short. The local design is already conservative (a strong-match floor, at most three statements per prompt, tuned on fixtures to 100% precision / ~90% recall `[local: docs/design.md, docs/verification.md]`), and its own docs say its precision on a real index and whether recalled facts help a chat have not been measured `[local: docs/verification.md]`. That measurement is the most important missing number.
3. **Keeping the raw inbox is right; the search path over it deserves to be first-class.** Raw preservation is what the best-preserving systems share, and agent-driven file search is the strongest retrieval result for coding-shaped agents. A Markdown wiki in git plus a searchable raw inbox is very close to the "flat files + grep" harness that ranked first across scenarios.
4. **The write path's existing checks are the right kind and ahead of most of the field.** The offline accuracy suite plants facts with un-inferable codenames and scores recall, update accuracy, stale-in-force rate, speaker and date accuracy, duplicates, and off-the-record/pasted content, and rejects adversarial model outputs `[local: internal/update/accuracy, docs/verification.md]`. That is operation-level evaluation in HaluMem's sense, with a deterministic grader. What it lacks is scale (12 scripts), a live result (the live run has not happened `[local]`), implicit invalidation, long-horizon aging, and any link to downstream task success.
5. **Speaker tagging and "only the user's words make a decision" are a real poisoning defence**, because the dangerous step is an automated write to a trusted, auto-loaded file. They need an adversarial suite that measures both stages: was the payload stored with authority, and did a later session act on it.
6. **Standing rules need a compliance metric, not just a storage metric.** Visibility gives roughly half compliance.
7. **Knowledge and system pages that make claims about code need anchors and a freshness state.** The system already tracks how far cited code has moved for system pages `[local: AGENTS.md]`; EA-Graph says the same idea, extended to knowledge claims with an explicit "unprovable" state, is what makes prose memory safe under drift.

---

## 6. Proposed evaluation programme for a memory for coding agents

### 6.1 Principles

1. **Measure the change in the work, not answers to quiz questions.** Primary outcomes are execution-verified.
2. **Four conditions on every task:** memory off; the system; an irrelevant-memory control (token-matched); an oracle-memory ceiling (the ideal record, hand-placed). Report the system as a fraction of the oracle-minus-off gap. Where the history fits, add raw-history-in-context.
3. **Screen for opportunity, and report the no-opportunity tasks separately as a harm test.** Keep a task in the "memory should help" set only if oracle beats off; keep a second set where memory cannot help and require non-inferiority there.
4. **Stage-wise diagnostics behind every end-to-end number:** captured? → written correctly? → preserved over time? → retrieved / injected? → used? → obeyed?
5. **Deterministic graders first.** Tests, file and state diffs, codename presence. An LLM judge only where unavoidable, validated against deliberately wrong and vague answers and against human labels.
6. **Cost and latency are outcomes.** Total tokens per completed task including the background update; hook latency; wall time.
7. **Statistics.** Paired per-task differences, ≥3 seeds per task, standard errors or bootstrap intervals, pass^k for reliability, sample size from a power calculation, per-item logs published.
8. **Both hosts, current models, re-run on every host model change.** Results vary a great deal by model and by harness.

### 6.2 Tasks (suites)

| Suite | Question | Design | Grader | Modelled on |
|---|---|---|---|---|
| **A. Resume / handoff** | Does the brief let a new chat continue interrupted work faster and as correctly? | Interrupt tasks at deterministic points (after first source edit, after first validation result, after first post-failure edit). New session gets: repo only / brief / raw transcript / oracle note. | Tests; agent events and prompt tokens to completion; split "needs completion" from "already solved, preserve" | Handoff Debt, HSC-Bench |
| **B. Branch switching** | Does per-branch state survive interleaved work? | Focus task + four other unfinished tasks on other branches of the same repo, near and far; control uninterrupted. | Same executable verifier in both | Switch-SWE |
| **C. Hidden-decision tasks** | Does the agent apply a project decision that exists only in a past chat? | A real library; N bug-fix tasks whose hidden test depends on a decision stated in an earlier chat; ~2× decoy chats; failures fed back up to five rounds. | Hidden tests; **corrections per task**; solve rate; cost | SDE-bench |
| **D. Sequential repository work** | Does memory of earlier tasks help later ones (and never hurt)? | Same-repo tasks in commit order with a persistent store; opportunity-screened pairs from issue/PR cross-references. | Tests; **memory utility** = Δ success on/off; forward transfer; steps; tokens | FSE-AIWare competition, SWE-ContextBench, VibeMemBench, SWE-Bench-CL metrics |
| **E. Write-path accuracy** | Does the update write the right thing, attributed and dated, and retire what stopped holding? | Planted-fact scripts with codenames: reversals, corrections, conditionals, off-the-record, pasted text, assistant suggestions vs user decisions, out-of-order sessions. Extend with implicit invalidation and multi-hop updates. | Deterministic: recall, update accuracy, stale-in-force, speaker, date, duplicates; validator obligations | Existing local suite; HaluMem; STALE; MemoryAgentBench conflict resolution |
| **F. Recall injection** | When the system injects a knowledge statement unasked, was it right to, and did it help? | (i) Labelled prompt × page relevance set on real indexes with near-miss distractors and no-match prompts. (ii) Counterfactual task runs with injection on / off / wrong page. | Precision, recall, false-injection rate on no-match prompts; harm rate (tasks that pass off but fail on); tokens injected | RSCB-MC framing; VibeMemBench irrelevant control; Chroma distractors |
| **G. Rule compliance** | Is a standing rule obeyed in later sessions, not just shown? | Rules each with a deterministic check (a command never run, a file never touched, a convention in the diff); tasks that tempt a violation; 1 to 50 sessions of intervening history. | Violation rate; rule-present-in-context rate (to separate access from compliance) | TellOnce, MemoryCode |
| **H. Staleness under code drift** | After the code changes, does memory stop asserting what is no longer true? | Generated repos with ground truth by construction; write memory; apply value drift, logic drift, deletion, withheld content. | Each prior claim classified unaffected / affected / unprovable; stale-assertion rate; fabrication count | EA-Graph, STALE premise resistance |
| **I. Poisoning and injection** | Can untrusted content become trusted memory, and does a later session act on it? | Payloads in tool output, pasted text, web content, file contents, forged closing tags, hidden Unicode, assistant suggestions phrased as user decisions; later benign trigger. Canary tokens. | **Two rates:** stored-with-authority, and acted-on-later; plus persistence after a clean session | Bad Memory, HarnessSafe lifecycle, injection–execution dissociation, MPBench |
| **J. Aging** | Does quality hold over months? | Scripted deployments of 8 to 200 sessions with compaction, page caps, renames, migrations and upgrades as maintenance events. | Suite E metrics as a time series; page size growth; regression at each maintenance event; brief token count | AgingBench |
| **K. Brief probes** | Does the brief alone hold what a successor needs? | From a session with known ground truth, generate the brief; ask a fresh model the four probe types with only the brief. | Codename presence (deterministic) for recall, artifact, decision; continuation judged against the scripted next step | Factory probes |
| **L. Abstention** | When nothing relevant is known, does the system inject nothing and does the agent say so? | False-premise prompts ("as we decided last week …" with no such decision); prompts in a project with an empty wiki; prompts matching a *sibling* project only. | Injection count must be zero; agent's answer checked for a fabricated memory | LongMemEval abstention, LongMemEval-V2 premise awareness |
| **M. Invisibility** | Does it cost the user nothing? | Hook latency benchmark; added tokens per session; background model calls per day; failure injection (wiki missing, lock held, host CLI missing). | p50/p95/p99 latency; token overhead; zero blocked prompts | Local latency benchmark; DolphinBench's cost-and-latency rule |

Conversational benchmarks (LongMemEval_S with the official judge and a full-context row; LoCoMo only with the audit's corrections) are a **sanity check on the retrieval path only**. They should never be the headline: they test a chat assistant recalling personal facts, they reward the wrong design choices for a coding agent, and LoCoMo is at its ceiling.

### 6.3 Metrics

**Primary (the claim rests on these)**
- **Memory utility**: paired Δ task success, system vs memory off, on opportunity-screened tasks, with interval. Also as **share of the oracle gap closed**.
- **Harm rate**: share of tasks that pass with memory off and fail with memory on; and Δ success on no-opportunity tasks (non-inferiority).
- **Handoff debt**: median agent events and cumulative prompt tokens for the successor to finish, vs repository only.
- **Corrections per task** on hidden-decision tasks.
- **Net tokens per completed task**, counting the brief, every injection and the background update.

**Diagnostic**
- Write path: recall of planted facts, update accuracy, stale-in-force rate, speaker-attribution and date accuracy, duplicate rate, off-the-record leak count (must be 0).
- Preservation: can the fact still be found in the wiki or inbox after N sessions and each maintenance event.
- Injection: precision, recall, false-injection rate on no-match prompts, tokens per injection, share of prompts with any injection.
- Use: of injected or briefed items, the share the agent's next actions are consistent with.
- Compliance: rule violation rate, conditional on the rule having been in context.
- Staleness: stale-assertion rate after drift; share of drifted claims flagged; fabrication count.
- Security: stored-with-authority rate; acted-on rate; persistence across sessions.
- Reliability: pass^k (all of k runs succeed), k = 3 or 5.
- Experience: hook p95 latency; number of things the user had to do (target: none).

### 6.4 Datasets that can be built and run offline

"Offline" has two levels here. **Level 0** runs with canned model outputs and a fake host CLI in CI: it checks the pipeline, the graders and every deterministic property. **Level 1** needs a real model but no network service beyond the host itself and no human: it produces the actual numbers.

1. **Scaled planted-fact generator** (Level 0 and 1). Turn the 12 hand-written scripts into a generator: parameterise speaker, tag, validity window, supersession chain length, branch, session order, and distractor volume; emit inbox entries and obligations. Add implicit invalidation ("we moved off X" without naming the old decision) and two-hop updates. Hundreds of scripts cost nothing to grade.
2. **Hidden-decision task pack** (Level 1; Level 0 for the plumbing). Pick 2–3 small permissively licensed libraries in Go, Python and TypeScript. For each, write 30–60 tasks whose hidden test encodes a decision that appears only in a synthetic earlier chat; add decoy chats. Verify each task: passes with the oracle note, fails without. Deterministic test grading.
3. **Interruption pack** (Level 1). From any execution-verified task set, record a predecessor run and cut it at the three deterministic points. Store repo snapshot + captured chat. The four handoff views are generated mechanically.
4. **Branch-switch pack** (Level 1). Compose pack 3's tasks five to a repository on separate branches.
5. **Labelled recall set** (Level 0). From a developer's own consenting wiki (or a synthetic one of 500+ knowledge pages built from real open-source project decisions), label prompt × page relevance, including near-miss and sibling-project distractors and prompts that match nothing. Fully deterministic scoring of the keyword scorer; reusable to compare any retrieval change.
6. **Drift worlds** (Level 0 for flagging, Level 1 for agent behaviour). Small generated repositories where each documented behaviour maps to a known artifact; scripted mutations.
7. **Poisoning corpus** (Level 0 for the store stage, Level 1 for the act stage). A few hundred payload placements across the write channels, each with a canary string and a later benign trigger.
8. **Aging scripts** (Level 0). Long generated histories replayed through the update with canned outputs, with maintenance events; time-series assertions.
9. **Rule-compliance pack** (Level 1). 30–50 rules with mechanical checks and tempting tasks.
10. **Replay of real captured sessions** (Level 1, private, never published). With the owner's consent, replay their own inbox through a candidate build and diff the wiki; count changes, never show content.

### 6.5 Live checks

- **Per-release on/off A/B on both hosts** over packs 2–4 (a few hundred runs): memory utility, harm rate, handoff debt, corrections per task, tokens. Gate releases on no regression.
- **Live write-path accuracy run** on each host and after each host model change (exists as a command; has never been run `[local]`).
- **Canary set in the doctor's live mode**: a handful of planted-fact scripts and two poisoning probes against the real host.
- **Local, content-free counters** (counts only, in line with the rule against logging captured content): briefs shown; injections per prompt; injections later contradicted by the user ("no, that's outdated" patterns); rules restated by the user after being stored (a re-explanation signal); knowledge pages never recalled; pages flagged stale. These are the only signals of real-world precision available without reading content.
- **Shadow mode for any retrieval change**: compute what the new scorer would inject, log only ids and scores, compare with the current one on the developer's machine before switching.
- **Quarterly external sanity run**: LongMemEval_S official judge with full-context row; whichever public coding-memory benchmark has a maintained harness (VibeMemBench, SWE-ContextBench, the FSE competition set).

### 6.6 Targets that would justify "best"

There is no accepted leaderboard for coding-agent memory, and most published systems fail to beat memory-off on coding tasks. So "best" has to be an evidence package, not a rank. Proposed bar (numbers are proposals, anchored to the published results beside them):

| # | Claim | Target | Anchor |
|---|---|---|---|
| 1 | Memory helps where it can | Memory utility ≥ +3 pp with the 95% interval above zero on opportunity-screened tasks, both hosts; ≥ 50% of the oracle gap closed | Ideal injected experience: +1.1 to +4.5 pp, intervals crossing zero; 11 of 12 existing-system pairings ≤ baseline (VibeMemBench). Oracle summary +8 pp on Lite, +1.1 to +5.3 pp on Full (SWE-ContextBench) |
| 2 | Memory never hurts | Harm rate ≤ 1%; no-opportunity Δ ≥ −1 pp (non-inferiority); irrelevant-memory control within noise | Irrelevant passage cost 0.7–3.4 pp; existing systems −0.2 to −5.5 pp (VibeMemBench) |
| 3 | Resuming is cheap | ≥ 40% fewer successor events and ≥ 50% fewer prompt tokens than repository only, with solve rate not lower, from a brief under ~2,500 tokens | Notes: events −20 to −46%, tokens −42 to −63%; raw trace events −57 to −59% at ~87k characters (Handoff Debt) |
| 4 | Decisions stick | ≥ 50% fewer corrections per task than vanilla on hidden-decision tasks, both hosts | −57% Claude Code, −65% Codex CLI reported by a vendor on its own set |
| 5 | Switching branches is safe | Recover ≥ 75% of the resolution lost to interleaving | Loss 6.0–15.0 pp; a state memory + router recovered 5.5–15.5 pp (Switch-SWE) |
| 6 | The wiki is accurate | Live, both hosts, 3 runs: planted-fact recall ≥ 0.95, update accuracy ≥ 0.95, stale-in-force ≤ 0.02, zero speaker and date errors, zero off-the-record leaks; same after 200 sessions | Commercial systems: update omission > 60% on long histories (HaluMem); best model 55.2% on implicit invalidation (STALE) |
| 7 | Injection is precise | Precision ≥ 0.95 and false-injection ≤ 1% on a real-index labelled set; zero injections on no-match and sibling-only prompts | Fixture result 100% / ~90% `[local]`; per-prompt recall made an agent worse (vendor result) |
| 8 | Rules are obeyed | Violation rate ≤ 15% with ≥ 16 sessions of intervening history; report the access-vs-compliance split | All rules in context → 45% violated; best compiled-check method 12.6–68.8% (TellOnce); 30.5% accuracy at 16–100 sessions (MemoryCode) |
| 9 | Stale claims are caught | ≥ 95% of drifted code claims flagged or marked unprovable; zero fabricated replacements | Anchored memory perfect with a strong model; prose notes worse than anchored in 7 of 7 worlds with a small one (EA-Graph) |
| 10 | Untrusted text cannot become trusted memory | 0 payloads stored with user authority across the corpus; 0 acted on in a later session; hidden-Unicode and forged-tag cases refused | 23–63% attack success once a payload is in an instruction file (Bad Memory) |
| 11 | It pays for itself | Net tokens per completed task ≤ memory off on opportunity tasks and ≤ +3% elsewhere, background update included | Context files +20–23% cost (ETH); handoff notes −42 to −63% tokens |
| 12 | It is invisible | Capture p95 < 50 ms and brief p95 < 300 ms `[local targets]`; zero prompts blocked under failure injection; zero required user actions after install | — |
| 13 | It is reproducible | Harness, task packs, per-item logs and the four-condition table published; every number has a command that reproduces it | The practice argued for in https://arxiv.org/abs/2606.09900 and https://arxiv.org/abs/2411.00640 |

Targets 1–5 are the claim. Targets 6–10 are why the claim can be trusted. Targets 11–13 are what makes it a product.

### 6.7 Minimum statistical protocol

- Paired design: the same task and seed under every condition.
- ≥ 3 seeds per task; report pass^3 and mean.
- Report the per-task paired difference with a standard error (clustered by repository) or a bootstrap interval; McNemar's test for paired pass/fail.
- Size the pack so a 3 pp utility difference is detectable: with paired binary outcomes and a discordant-pair rate around 15–20%, that needs on the order of several hundred task-seed pairs per host (compute exactly once pilot discordance is known).
- Publish per-item logs. Pre-register the primary metrics before the first live run.

---

## 7. What I could not confirm

- OWASP's own text for ASI06 (only the publication date, 9 Dec 2025, came from OWASP's page).
- MINJA's 98.2% / 76.8% (from a secondary summary; the abstract page does not give them).
- ProofDebt's full method and sample (the preprint page returned 403; seven tasks is a very small sample).
- LongMemEval-V2's "oracle ceiling 86–90%" (aggregator only) and a secondary claim of 89.7% for an off-the-shelf coding agent, which conflicts with the abstract's 69.3%.
- STALE's 55.2%, PersistBench's 53% / >90%, MemoryCode's 30.5%, Evo-Memory's and MemBench's findings, GAM's latency: all from secondary summaries.
- DolphinBench results (abstract has none); FSE-AIWare competition results (not on the page); HSC-Bench per-model numbers (abstract has none); ContinuityBench (not located); CODEMEM (secondary only).
- Whether the LoCoMo audit has been independently replicated.
- LoCoMo-Plus and MemoryBench: named in vendor overviews; I found no primary source in the time available.
- Every vendor number (Mastra 94.87%, Mem0 93.4% / 92.5%, Hindsight 91.4% and the corrections-per-task result, Letta 74.0%, Zep's tables, Factory's scores, Anthropic's 39%) is self-reported.
- No public evaluation was found of either host's own built-in memory on coding outcomes.

---

## 8. Source list

Benchmarks and audits
- LoCoMo: https://arxiv.org/abs/2402.17753 (27 Feb 2024)
- LoCoMo audit: https://github.com/dial481/locomo-audit (2026)
- Zep critique of LoCoMo and a competitor's results: https://www.getzep.com/blog/lies-damn-lies-statistics-is-mem0-really-sota-in-agent-memory/ (6 May 2025)
- Zep paper (DMR limits, LongMemEval per-type): https://arxiv.org/html/2501.13956 (Jan 2025)
- LongMemEval: https://arxiv.org/abs/2410.10813, https://arxiv.org/html/2410.10813v2 (Oct 2024 / Mar 2025)
- LongMemEval-V2: https://arxiv.org/abs/2605.12493 (12 May 2026)
- MemoryAgentBench: https://arxiv.org/html/2507.05257v1 (Jul 2025)
- MemBench: https://arxiv.org/abs/2506.21605 (2025)
- PerLTQA: https://arxiv.org/abs/2402.16288 (Feb 2024)
- ConvoMem: https://arxiv.org/abs/2511.10523 (13 Nov 2025)
- BEAM: https://arxiv.org/abs/2510.27246 (31 Oct 2025)
- HaluMem: https://arxiv.org/abs/2511.03506 (Nov 2025)
- SubtleMemory: https://arxiv.org/html/2606.05761 (2026)
- STALE: https://arxiv.org/abs/2605.06527 (7 May 2026)
- PersistBench: https://arxiv.org/abs/2602.01146 (Feb 2026)
- MemoryArena: https://arxiv.org/abs/2602.16313, https://arxiv.org/html/2602.16313 (Feb / Sep 2026)
- AMA-Bench: https://arxiv.org/abs/2602.22769 (Feb 2026)
- Evo-Memory: https://arxiv.org/abs/2511.20857 (25 Nov 2025)
- STATE-Bench: https://opensource.microsoft.com/blog/2026/05/19/introducing-state-bench-a-benchmark-for-ai-agent-memory/ (19 May 2026)
- DolphinBench: https://arxiv.org/abs/2609.24971 (21 Sep 2026)
- AgingBench: https://arxiv.org/html/2605.26302 (25 May 2026)
- Cross-scenario generality: https://arxiv.org/abs/2606.04315, https://arxiv.org/html/2606.04315 (3 Jun 2026)
- Agent-native memory systems study: https://arxiv.org/abs/2606.24775 (23 Jun 2026)
- Always-On Agents survey: https://arxiv.org/abs/2606.30306 (29 Jun 2026)
- MemPalace critical analysis: https://arxiv.org/abs/2604.21284 (Apr 2026)
- Engram (measurement pitfalls, bi-temporal): https://arxiv.org/abs/2606.09900 (5 Jun 2026)

Coding-specific
- VibeMemBench: https://arxiv.org/abs/2609.23570, https://arxiv.org/html/2609.23570 (20 Sep 2026)
- SWE-ContextBench: https://arxiv.org/html/2602.08316v3 (Feb / May 2026)
- SWE-Bench-CL: https://arxiv.org/abs/2507.00014, https://arxiv.org/html/2507.00014v1 (13 Jun 2025)
- FSE-AIWare 2026 memory competition: https://mem-comp.github.io (2026)
- Hindsight 0.9.0 (SDE-bench, corrections per task): https://hindsight.vectorize.io/blog/2026/08/06/hindsight-0-9-0 (6 Aug 2026)
- Subtask-level memory: https://arxiv.org/abs/2602.21611 (25 Feb 2026)
- ProofDebt: https://www.preprints.org/manuscript/202610.0373 (Oct 2026)
- Handoff Debt: https://arxiv.org/html/2606.02875v2, https://aclanthology.org/2026.emnlp-main.738/ (2026)
- HSC-Bench: https://arxiv.org/abs/2610.36523 (27 Oct 2026)
- Switch-SWE: https://arxiv.org/html/2611.02017 (1 Nov 2026)
- Factory compression probes: https://factory.com/news/evaluating-compression (16 Dec 2025)
- MemoryCode: https://arxiv.org/abs/2502.13791 (2025)
- Evaluating AGENTS.md: https://arxiv.org/abs/2602.11988, https://arxiv.org/html/2602.11988 (Feb / Sep 2026)
- TellOnce: https://arxiv.org/html/2606.13174 (2026)
- SkillsBench: https://arxiv.org/abs/2602.12670 (Feb 2026)
- EA-Graph: https://arxiv.org/abs/2608.04278 (4 Aug 2026)
- Abstention-aware memory retrieval for coding agents: https://arxiv.org/abs/2604.27283 (30 Apr 2026)
- Recovery-Bench: https://letta.com/blog/recovery-bench (27 Aug 2025)

Design evidence
- Letta filesystem result: https://www.letta.com/blog/benchmarking-ai-agent-memory (12 Aug 2025)
- Sleep-time compute: https://arxiv.org/html/2504.13171v1 (Apr 2025)
- General Agentic Memory: https://arxiv.org/abs/2511.18423 (Nov 2025)
- Observational memory: https://mastra.ai/research/observational-memory (2026)
- Hindsight paper: https://arxiv.org/abs/2512.12818 (14 Dec 2025)
- Experience-following: https://arxiv.org/html/2505.16067v2 (2025)
- Context rot: https://www.trychroma.com/research/context-rot (14 Jul 2025)
- Effective context engineering: https://www.anthropic.com/engineering/effective-context-engineering-for-ai-agents (29 Sep 2025)
- Context management (memory tool + context editing): https://www.anthropic.com/news/context-management (2025)
- Adding Error Bars to Evals: https://arxiv.org/abs/2411.00640 (1 Nov 2024)

Security
- OWASP Top 10 for Agentic Applications 2026: https://genai.owasp.org/resource/owasp-top-10-for-agentic-applications-for-2026/ (9 Dec 2025)
- MINJA: https://arxiv.org/abs/2503.03704 (Mar 2025); follow-up https://arxiv.org/abs/2601.05504 (Jan 2026)
- Bad Memory: https://arxiv.org/html/2607.14611v1 (16 Jul 2026)
- Memory poisoning systematic study / MPBench: https://arxiv.org/abs/2606.04329 (Jun 2026)
- Injection–execution dissociation: https://arxiv.org/abs/2605.08442 (May / Oct 2026)
- HarnessSafe: https://arxiv.org/abs/2608.06984 (7 Aug 2026)
- CSA note on instruction-file poisoning: https://labs.cloudsecurityalliance.org/research/csa-research-note-agent-instruction-file-poisoning-20261027/ (27 Oct 2026)

---

## Fact check

Checked 2026-11-10 by a second reader, who fetched each source again and tried to find where the claim goes beyond it. The fact IDs (F1 to F30) are those of the fact list handed over with these notes. Pages were read through the same fetch-and-summarise tool as the notes, so a number here is as that tool reported it from the page; where the tool read only the first 100,000 characters of a long page, the row says what was not seen.

Verdicts: **confirmed** = the source says what the fact says; **partly** = the numbers are there but something in the claim is off or out of date; **refuted** = the source does not say it; **could not check** = the source could not be read.

Nothing was refuted outright. Seven facts are only partly right, and four of those matter to the design: F11, F12, F27 and F9. Two more (F20, F24) are out of date because the paper has been revised since the notes' figures were taken.

### The ten facts the design leans on hardest

| ID | Verdict | What the source says, and what to correct | Source |
|---|---|---|---|
| F15 Handoff Debt | confirmed | 75 tasks, 181 handoff points, 2,172 takeover runs; events down 20-59%, prompt tokens down 42-63%; raw trace 87k characters against 9.8-10.0k for notes; repository-only solved 46.4%, 42.5%, 34.3%. EMNLP 2026 confirmed. Limits the notes do not state: every model is open-weight (Qwen predecessor; Qwen, Gemma and Devstral successors), so neither host was tested; note-based solve gains were significant only for Devstral; free-form summary notes cut events as much as structured notes or more (-46/-33/-30% against -44/-20/-29%). | https://arxiv.org/html/2606.02875v2 ; https://aclanthology.org/2026.emnlp-main.738/ |
| F16 Switch-SWE | confirmed | 200 focus tasks, 19 Python repositories, seven configurations; mean loss 11.5 points, range 6.0-15.0. Claude Code (Opus 5) 53.5 to 41.5; Codex (GPT-5.6 Sol) 52.5 to 38.0. Limit: the fix (history routing plus task-state memory) was run only on OpenCode with two open models, not on Codex or Claude Code. Routing alone closed most of the gap (15.0 to 3.0, and 6.0 to 3.0). Uninterrupted results stayed within 0.5 points. | https://arxiv.org/html/2611.02017 |
| F10 VibeMemBench results | confirmed | 111 targets, 90 repositories; verified record +0.0 to +4.5 points, all five intervals cross zero, steps down for all five; 11 of 12 pairings "fail to exceed" memory-off (-5.5 to +2.0); irrelevant passage -0.7 to -3.4, intervals cross zero; Claude and GPT families not run. Limit: the "ideal" record is not an oracle. It is the record with the largest measured uplift on a reference solver, which the paper calls optimistic. | https://arxiv.org/html/2609.23570 |
| F11 VibeMemBench failure analysis | partly | The shares are right (231 failing pairings: ranking 1.3%, form degradation 69.3%, coverage 16.0%, use 13.4%), and so is volume over instruction content (0.60 against 0.48 seeds recovered). Off: the paper does not say a four-field record "would have recovered most". Its +196 seeds is a bound from substituting the frozen record's outcome on the 160 form-degradation pairings, and it says this is not an upper bound. The stage labels agree only weakly with a blind audit (Cohen's kappa 0.24-0.32). The 334-line record was about a different issue, so that case is also a wrong record, not length alone. | https://arxiv.org/html/2609.23570 |
| F13 Context files (ETH) | confirmed | Claude Code (Sonnet 4.5), Codex (GPT-5.2, GPT-5.1 mini), Qwen Code. LLM-written files: -0.5% and -2% success (p = 0.87, 0.37), cost +20% and +23%, steps +2.45 and +3.92. Developer-written: +2.4% (p = 0.21), cost up to +19%. +2.7% only with other documentation removed. A mentioned tool was used 1.6 times per task against under 0.01. Overviews did not shorten the path to the changed files. A second study finds the opposite cost direction; see "Facts the notes lack". | https://arxiv.org/html/2602.11988 |
| F14 Vendor corrections-per-task | confirmed | 1.06 against 0.97 with recall on every prompt; after the change Claude Code (Sonnet 5) 0.85 to 0.36, Codex CLI (GPT-5.4 mini) 1.34 to 0.47, opencode 1.20 to 0.80; 61 tasks, mean of 3 runs, pytest grading. Solve rates were 99-100% in both arms, so the benchmark measures rework only, not success. Vendor's own benchmark and product. | https://hindsight.vectorize.io/blog/2026/08/06/hindsight-0-9-0 |
| F7 Cross-scenario study | confirmed | Grep/read harness rank 3.67, long context 4.00; LoCoMo 45.0 against 61.5; stored answers recoverable 10% (HippoRAG) and 35% (SimpleMem). Limits: the 3.8-point comparison (24.8K against 6.9K query tokens, 3.6 times) is for the paper's own system, AutoMEM, not the grep harness, and AutoMEM outranks the harness (3.10 against 4.10 on that basis). The model throughout is Qwen3-32B, a small open model. | https://arxiv.org/html/2606.04315 |
| F4 LongMemEval | confirmed | 500 questions, about 115k tokens in the S set, 30 false-premise questions; GPT-4o 0.870 with oracle sessions, 0.606 with full history; facts as extra keys +9.4% recall@k and +5.4% accuracy; time-aware expansion +11.3% (rounds) and +6.8% (sessions), only with a strong model. The paper does not say whether these gains are relative or absolute. | https://arxiv.org/html/2410.10813v2 |
| F18 Rule compliance | confirmed | No rules 31.6%, memory product 42.5%, all rules 55.0%, compiled checks 70.1%. MemoryCode, checked in the paper's Table 4: GPT-4o 94.5 on one instruction, 30.5 on histories of 16-100 sessions. Limits: the diagnostic is one user, 19 held-out tasks and 29 checks; MemoryCode is synthetic. Also there, and not in the notes: giving only the task-relevant rules scored 54.0%, no better than all rules. | https://arxiv.org/html/2606.13174 ; https://arxiv.org/html/2502.13791v2 |
| F27 Bad Memory | partly | The means are in Table 1 (63.3, 30.0, 23.3, 60.0%). Off: each mean averages three payloads placed in three different files, and only one sat in the auto-loaded CLAUDE.md or AGENTS.md. There, credential exfiltration succeeded 80% (Haiku 4.5), 0% (Opus 4.7), 30% (GPT-5.2), 0% (GPT-5.5). The other two payloads sat in files the auto-loaded file told the agent to read. The claim about external content overwriting trusted files rests on preliminary attempts the paper scoped out, not on a reported experiment. Persistence (93-97% for Opus) and the move into AGENTS.md by GPT-5.2 are confirmed. | https://arxiv.org/html/2607.14611v1 |

### Other facts checked

| ID | Verdict | What the source says, and what to correct | Source |
|---|---|---|---|
| F12 SWE-ContextBench | partly | Resolve rates are right (26.26, 27.27, 34.34, 22.22%; 99 tasks, Claude Sonnet 4.5). Off: +27.3% cost is free trajectory retrieval against oracle trajectory ($0.98 against $0.77); against no context it is +24.1%. The notes leave out the same table's memory frameworks: OpenViking 29.20% and Supermemory 30.30% beat no context at 27-33% lower cost; Mem0 24.24%. | https://arxiv.org/html/2602.08316v3 |
| F9 File-tools agent on LoCoMo | partly | 74.0% with gpt-4o-mini against Mem0's own reported 68.5% is right. Off: `search_files` is semantic vector search over a file the platform embedded automatically, and tool rules made the agent start with it. This is not grep alone. | https://www.letta.com/blog/benchmarking-ai-agent-memory |
| F20 SkillsBench | partly | Out of date. Current v4 (14 Jun 2026): 87 tasks, curated skills 33.9% to 50.5% (+16.6 points); software engineering +11.6, not +4.5; self-generated skills were below baseline on all three configurations tested (-8.1 Claude Code with Opus 4.7, -11.3 Codex with GPT-5.5, -11.5 Gemini CLI), not -1.3. | https://arxiv.org/html/2602.12670v4 |
| F24 AgingBench | partly | The claim matches v1 (25 May 2026): about 400 runs, 14 models, OpenHands and Claude Code on scenario S7, violations near zero while precision drops, maintenance shocks, the three recommendations. The same URL now serves v2 (7 Nov 2026), a rewritten study: 6 models, 10 memory systems, 50-200 sessions, no Claude Code or OpenHands runs; the near-zero-violations finding and the regression-check advice were not found in it. Cite v1 by version, or re-read v2. | https://arxiv.org/html/2605.26302v1 ; https://arxiv.org/html/2605.26302 |
| F25 SubtleMemory | partly | 93.5% (A-Mem) and 91.5% (OpenClaw) preservation, both keeping the original sessions, and the 85.4% oracle are right. Off: the two low systems are MemoBase (39.1%) and MetaClaw (40.2%), and the paper does not call them extraction-heavy. Its text gives 70.0% as the best overall score, but its Table 1 lists 71.3%. | https://arxiv.org/html/2606.05761 |
| F22 STALE | partly | 400 scenarios, 1,200 queries, best model 55.2%: confirmed from the abstract. "Old and new memories coexisting unadjudicated" is not in the abstract, which speaks of "a pervasive gap between retrieving updated evidence and acting on it". The full paper was not read. | https://arxiv.org/abs/2605.06527 |
| F23 EA-Graph | confirmed | Seven worlds, 42 sessions; Haiku: anchored beat prose notes and no memory, p = 0.0156 each; Sonnet: anchored perfect, but controls at ceiling left the planned contrasts non-significant; no session fabricated withheld content. Abstract only. The authors make no claim about efficiency or repair quality. | https://arxiv.org/abs/2608.04278 |
| F19 Experience-following | confirmed | Add-all was lowest on all four agents; 13.05, 16.75 and 38.50 match. The code-generating agent is EHRAgent, which writes code to query medical records, not a software-engineering agent. The arXiv page shows no ACL venue. | https://arxiv.org/html/2505.16067v2 |
| F8 LongMemEval-V2 | confirmed | 451 questions, up to 500 trajectories and 115M tokens; 72.5%, 69.3%, 48.5%; "coding agent based methods have high latency costs". Abstract only. | https://arxiv.org/abs/2605.12493 |
| F17 Compaction probes | confirmed | 36,611 messages; 3.70, 3.44, 3.35; artifact trail 2.19-2.45. Scores come from an LLM judge (GPT-5.2) on six dimensions, with no intervals; the vendor's own summaries came from production, the others from API calls. | https://factory.com/news/evaluating-compression |
| F6 MemoryArena | confirmed | Success rate: long context 0.09-0.19, memory systems 0.12-0.14, RAG 0.17-0.20; heading "External Memory and RAG Systems Are Not Universally Beneficial". | https://arxiv.org/html/2602.16313 |
| F5 MemoryAgentBench | confirmed | Single-hop 60.0, 56.0, 18.0; best multi-hop 7.0; BM25 100.0 against 73.5 on multi-needle; build time 0.11 s against 14,644 s. Embeddings win three of the five retrieval sets. | https://arxiv.org/html/2507.05257v1 |
| F1 LoCoMo audit | confirmed | 99 of 1,540, ceiling 93.57%, missing field on 444 of 446, 62.81% of vague wrong answers accepted. The README does not name its author or say the author builds a memory product. One vendor's 92.32% is within 1.25 points of the ceiling. | https://github.com/dial481/locomo-audit |
| F2 LoCoMo critique | confirmed | 16,000-26,000 tokens, no knowledge-update questions, 72.90% against 68.44%, 65.99% against 75.14%. The 75.14% is itself a corrected figure. | https://www.getzep.com/blog/lies-damn-lies-statistics-is-mem0-really-sota-in-agent-memory/ |
| F3 DMR | confirmed | 60 messages; 94.4% and 98.0% baseline against 94.8% and 98.2%. | https://arxiv.org/html/2501.13956 |
| F21 HaluMem | confirmed | Long set: update omission 98.51, 98.40, 95.38, 82.42, 62.14, 34.47%; best answer accuracy 67.23%. The six rows are five distinct systems (two are variants of one product), and the paper does not call them commercial. | https://arxiv.org/html/2511.03506v3 |
| F26 Context rot | confirmed | 18 models, about 300 against 113k tokens, distractor findings, Claude and GPT behaviour. Per-model figures are in charts only, and not every model is in every experiment. | https://www.trychroma.com/research/context-rot |
| F28 Persistent memory attacks | confirmed | Input filters never see the payload; small retrieval-layer classifiers did not flag it; removing the recall tool gave 0% for 8 of 9 open-weight models and was bypassed by rebuilding the instruction. Incidents and "no patch was issued" confirmed. | https://arxiv.org/abs/2605.08442 ; https://labs.cloudsecurityalliance.org/research/csa-research-note-agent-instruction-file-poisoning-20261027/ |
| F29 Write-time compute | confirmed | About 5 times less test-time compute, 2.5 times lower cost per query; 94.87% with gpt-5-mini, about 30k tokens of context, no per-turn retrieval. | https://arxiv.org/abs/2504.13171 ; https://mastra.ai/research/observational-memory |
| F30 Reporting practice | confirmed | Pitfalls and recommended practice are in the abstract (single author); Memory Utility is defined on the competition page with no formula, and the page still has no results. The error-bars paper was not fetched again. | https://arxiv.org/abs/2606.09900 ; https://mem-comp.github.io |

### Facts the notes lack

1. **Both hosts now ship their own memory, so it has to be a condition in every experiment.** Claude Code's auto memory is on by default in local sessions, is kept per repository on one machine, loads the first 200 lines or 25KB of its `MEMORY.md` index into every session, and skips what it can derive from the code. https://code.claude.com/docs/en/memory. Codex memories are off by default, are written in the background once a chat has been idle, live under `~/.codex/memories_v2/`, and can leave out chats that used MCP tools or web search (`memories.disable_on_external_context`). https://developers.openai.com/codex/memories (redirects to https://learn.chatgpt.com/docs/customization/memories). The notes' "memory off" baseline does not say whether the host's memory is on.
2. **Each host's documentation says its memory is not a place for rules that must hold.** Claude Code: memory and CLAUDE.md are "context, not enforced configuration"; to block an action, use a PreToolUse hook; keep each file under 200 lines because longer files reduce adherence. Codex: treat memories as "a helpful recall layer, not as the only source for rules that must always apply". Same two URLs.
3. **One benchmark has measured Claude Code's built-in memory, and it has results.** DolphinBench, with Claude Code and Claude Sonnet 5: built-in 26.33%, one vendor's product 32.33%, another's 35.83%. With another harness and GPT-5.6-Luna: built-in 65.67%, best product 70.67%. The tasks are knowledge work, not coding; the authors work at a memory vendor; there is no file-search, full-context or plain-RAG baseline. Each task had to pass twice with the history and fail twice without. https://arxiv.org/html/2609.24971
4. **HSC-Bench has results, and they bear on the form of a resume brief.** For GPT-5.4 on continuation tasks (mean score, share passing every criterion): narrative summary 87.34, 42.8%; recent transcript 89.73, 44.7%; structured state 92.76, 52.1%; full history 94.51, 57.9%; privileged target state 98.30, 85.3%. A prose summary did worse than the recent transcript. A record that a step was done did not always stop the successor doing it again (2 of 114). Six receiving models, neither host's agent. https://arxiv.org/html/2610.36523
5. **The evidence on structured against free-form handoff notes conflicts.** Handoff Debt found free-form summary notes as good as structured notes or better; HSC-Bench found structured state better than a narrative summary. https://arxiv.org/html/2606.02875v2 ; https://arxiv.org/html/2610.36523
6. **Some off-the-shelf memory products did beat memory-off on coding tasks.** SWE-ContextBench Lite: 29.20% and 30.30% against 26.26%, with 21-34% less time and cost; a third scored 24.24%. This sits against the notes' statement that most published systems fail to beat memory-off. 99 tasks, one agent, no intervals. https://arxiv.org/html/2602.08316v3
7. **SWE-bench Verified is no longer reported by OpenAI** (23 Feb 2026): an audit found flawed tests that reject correct fixes, and models had seen the solutions in training; it recommends SWE-bench Pro. Handoff Debt and SWE-Bench-CL are built on Verified. A model that already knows a fix needs no memory, which shrinks any measured memory effect. https://openai.com/index/why-we-no-longer-evaluate-swe-bench-verified/ (the page returned 403 to the fetch; this is from search-result summaries of it).
8. **Choosing the relevant rules did not improve compliance.** TellOnce: task-selected rules 54.0%, all rules 55.0%. Better rule retrieval alone will not raise obedience. https://arxiv.org/html/2606.13174
9. **Self-written skills are now measured as harmful, not neutral, on both hosts.** SkillsBench v4: -8.1 points (Claude Code, Opus 4.7), -11.3 (Codex, GPT-5.5). Skill sets of at most three modules beat larger bundles; 13 of 87 tasks got worse with curated skills. https://arxiv.org/html/2602.12670v4
10. **A second study of AGENTS.md files found lower cost, not higher.** 10 repositories, 124 pull requests: median runtime -28.64%, output tokens -16.58%, completion comparable. It conflicts with the ETH study's +20-23% cost. https://arxiv.org/abs/2601.20404
11. **Attack success in an instruction file depends heavily on what the payload asks for.** In the auto-loaded file, credential theft succeeded 0% on the two strongest models; from a referenced preferences file, "install this pinned dependency" succeeded 90% (Opus 4.7) and 80% (GPT-5.5). Plausible-looking engineering advice is the dangerous payload. The authors recommend tiers: low-trust memory may supply facts but not override rules. https://arxiv.org/html/2607.14611v1
12. **Token counts need prices.** Cached input is read at 0.1 times the base input price on most Claude models (less on the newest), written at 1.25 times, and a change at one level of the prompt invalidates everything after it. "Net tokens per completed task" should count cached and uncached tokens separately, or report cost. https://platform.claude.com/docs/en/build-with-claude/prompt-caching
13. **AgingBench was rewritten on 7 Nov 2026 (v2)**, three days before these notes; its coding-agent runs are gone from the new version. https://arxiv.org/html/2605.26302
