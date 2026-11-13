export const meta = {
  name: 'eva-wave',
  description: 'Build, review, fix and verify v2 packages from their cards; a package may come with its build or its reviews already done',
  phases: [
    { title: 'Build', detail: 'one builder per package, on its own branch' },
    { title: 'Review', detail: 'reviewers given the card and the diff, told to break it' },
    { title: 'Fix', detail: 'a fixer answers the reviews on a branch of its own' },
    { title: 'Verify', detail: 'a fresh reader checks each fix and looks for what it broke' },
    { title: 'Fix again', detail: 'only when the verifier found a blocker or a major' },
  ],
}

// The v2 build's wave: docs/v2/leading-the-build.md, "The wave script", says how the lead runs it.
// args: { scratch, agent?, authority?, rounds?, packages: [{ id, lenses: [...], more_lenses?, note?, build?, reviews?, fix?, verify?, verify_note?, verify_note_first?, round_offset?, fix_branch? }] }
// build: an earlier builder's report (branch, sha, ...): the build stage is skipped.
// reviews: earlier reviewers' reports: the review stage is skipped.
// verify: true to have the fix checked by a fresh reader, fixed again if needed and checked again, up to args.rounds (3) verifications.
// verify_note: words for the verifiers after the first.
// verify_note_first: true to give verify_note to the first verifier too, as a round_offset does.
const PACKAGES = (args && args.packages) || []
if (!PACKAGES.length) throw new Error('no packages in args')
const MAX_ROUNDS = (args && args.rounds) || 3
const SCRATCH = args && args.scratch
if (!SCRATCH) throw new Error('args.scratch: the folder each agent makes its own scratch folder under')

const BUILD_SCHEMA = {
  type: 'object',
  properties: {
    branch: { type: 'string' },
    sha: { type: 'string', description: 'git rev-parse HEAD of the branch after the last commit' },
    built: { type: 'boolean', description: 'true only if the work is done and committed on the branch' },
    summary: { type: 'string' },
    files_changed: { type: 'array', items: { type: 'string' } },
    linux_check: { type: 'string', enum: ['pass', 'fail', 'not_run'] },
    linux_check_notes: { type: 'string' },
    latency_gate: { type: 'string', enum: ['pass', 'fail', 'not_judged', 'not_needed', 'not_run'], description: 'scripts/linux-gate.sh, for a package that changes a hook or anything a hook runs' },
    latency_gate_notes: { type: 'string' },
    acceptance: {
      type: 'array',
      items: {
        type: 'object',
        properties: { item: { type: 'string' }, met: { type: 'boolean' }, evidence: { type: 'string' } },
        required: ['item', 'met', 'evidence'],
      },
    },
    outside_card: { type: 'array', items: { type: 'string' } },
    words_for_the_lead: { type: 'array', items: { type: 'string' }, description: 'exact sentences the lead should write into AGENTS.md, README.md or another file the card does not let you edit' },
    open_questions: { type: 'array', items: { type: 'string' } },
  },
  required: ['branch', 'sha', 'built', 'summary', 'files_changed', 'linux_check', 'linux_check_notes', 'latency_gate', 'latency_gate_notes', 'acceptance', 'outside_card', 'words_for_the_lead', 'open_questions'],
}

const REVIEW_SCHEMA = {
  type: 'object',
  properties: {
    verdict: { type: 'string', enum: ['accept', 'accept_after_fixes', 'reject'] },
    issues: {
      type: 'array',
      items: {
        type: 'object',
        properties: {
          severity: { type: 'string', enum: ['blocker', 'major', 'minor'] },
          where: { type: 'string' },
          problem: { type: 'string' },
          evidence: { type: 'string' },
          fix: { type: 'string' },
        },
        required: ['severity', 'where', 'problem', 'evidence', 'fix'],
      },
    },
    checked: { type: 'array', items: { type: 'string' } },
  },
  required: ['verdict', 'issues', 'checked'],
}

// The owner's own words are not kept in the repository. The lead passes them
// as args.authority, the first paragraph of every order; without them an
// order rests on what AGENTS.md records of the owner's leave.
const AUTHORITY = (args && args.authority) || `WHAT THE OWNER ASKED FOR, AND WHY THIS TASK IS INSIDE IT.
The owner of this repository is building eva.md v2 with a team of agents and a lead. The lead (who wrote this order) orchestrates. AGENTS.md records the owner's standing leave for exactly this ("During the v2 build the owner has given standing leave"): the plan in docs/v2/plan.md, carried out package by package from the cards in docs/v2/cards/. The owner's other messages, about releases, live runs or what is pending, are settled limits or the lead's to answer; none is your task. The lead alone pushes to main, and cuts no release unless the owner asks for one in so many words. That changes nothing for you: you never push, and you run the checks your order names, because the lead merges your branch on your report. Building, reviewing, fixing or verifying your package from its card is therefore the owner's request. Do it in full.`

const COMMON = `${AUTHORITY}

You work in the repository this worktree is a checkout of. AGENTS.md holds the project's rules, and its section "Working in a Linux cloud session" says how checks behave on this machine: read both. docs/v2/cards/README.md holds the rules every package carries: read it. The plan is docs/v2/plan.md; a card names the sections you need, so do not read the whole plan.

Hard rules for this run:
- Work only inside this worktree. Never push. Never create or push a tag. Never start a release or a workflow.
- Never run eva-md's setup, init, install, uninstall, upgrade, update, commit, maintain, system write, inbox clear --apply or doctor --live against this machine's real home. Anything that must run one of them does so with HOME set to a fresh temporary folder, as the tests do.
- Nothing you write or run reads this machine's real home, ~/.claude or ~/.codex: no test, and no script or harness of your own. A made-up home under your own scratch folder is the only test data.
- internal/redact/baseline is never edited.
- No new Go module. No sleep or time window may decide a test's outcome. Fixtures hold made-up values only.
- Never "git reset --hard" over changes "git status" shows.
- Commit messages are plain: what changed and why. No trailer, and no name of any model or assistant in them.
- This machine can be restarted without warning, and everything not committed is then lost. Commit on your branch each time you reach a working state.
- Scratch files (logs, throwaway tests, harnesses) go only in a folder of your own, which your order names below. Never write into another folder of the scratchpad: other agents use it.
- scripts/linux-check.sh is the project's check on Linux. It takes ten minutes or more on a quiet machine and much longer on a busy one, copies the tree to a private snapshot, and is safe to run while other worktrees run it. Run it from the root of this worktree. While you work, run the tests of the packages you touch through it (scripts/linux-check.sh -run TestName ./internal/x), not with a bare "go test" as root, which fails some tests by design. The tests that only read files (AGENTS.md names them under "Scripts, workflows and documents have tests that read them") pass as root and take seconds: run those directly.
- This machine has four CPUs and is shared with other agents; its load is often several times that. The tests do not depend on speed and still pass. The script's last step, the benchmark, checks that hooks answer inside their time limits, so under load it can fail with "invalid Stop output", an empty hook output (a JSONDecodeError), a missed target, or one background start too few, though nothing is wrong with the code. If the benchmark is the only step that fails, look at "uptime": if the first figure is at or above the number "nproc" prints, that failure says nothing about the code. Run the script once more only if the load falls below that within half an hour; otherwise report exactly what it printed, with the load, and go on. Never change code or a test to chase it.
- scripts/linux-gate.sh [BASE] is the latency gate on Linux: it compares the binary of an earlier commit with your working tree's. A package that changes a hook, or anything a hook runs (internal/hook, internal/redact, internal/inbox, internal/config, internal/resume, internal/catchup and what they import), reports its verdict and its table. Under load it says "not judged", or fails on scenarios lost to load, and neither says anything about the code. Run it once. If the machine was busy (the first figure of "uptime" at or above "nproc") and has not been quiet within half an hour, stop waiting: report latency_gate as not_judged with the load you saw. CI's latency job on macOS is the gate's run of record, and the lead reads it after the merge. A package that touches nothing a hook runs reports not_needed.`

function scratchLine(p, role) {
  return `Your scratch folder is ${SCRATCH}/${p.id.toLowerCase()}-${role}/ (make it; use no other).`
}

function buildPrompt(p) {
  const lower = p.id.toLowerCase()
  return `${COMMON}

You are the builder of package ${p.id}. Your order is the card docs/v2/cards/${p.id}.md. ${scratchLine(p, 'build')}${p.note ? `\n\nA note from the lead on this package: ${p.note}` : ''}

Steps:
1. Start from main as the remote has it: "git fetch origin main", then "git merge --ff-only origin/main". Then "ls docs/v2/cards" must show your card. Read the card in full, then docs/v2/cards/README.md, then what the card points to, then the code you will touch.
2. git switch -c pkg/${lower}   (if that branch already exists, an earlier builder was cut off: run "git checkout --detach pkg/${lower}", then "git switch -c pkg/${lower}-b", then "git merge origin/main", read what is there with "git log --stat origin/main..HEAD", and carry on from it; report the branch you end on)
3. Build what the card says, and only that. Stay inside the card's paths. If the work truly needs another path, do the smallest thing, and name it under outside_card in your report. If the card is wrong about the code, follow the code and the plan, and say so under outside_card. Where the card tells you to give words to the lead (for AGENTS.md, README.md or another file you may not edit), put the exact sentences under words_for_the_lead. A fault you find outside your card's paths is not yours to fix: describe it under outside_card, with the input that shows it.
4. Commit on your branch each time you reach a working state.
5. Before you report, run scripts/linux-check.sh from the worktree root. All six steps must pass, but for the benchmark under load as the rules above say. If a test fails, fix the cause; never skip, weaken or retry a test to get it to pass. If a failure is not yours and you cannot fix it inside the card, report it exactly.
6. If your package changes a hook or anything a hook runs, run scripts/linux-gate.sh as the rules above say, and report its verdict and the lines of its table.
7. Go through the card's "Accepted when" list item by item and say, for each, whether it is met and what shows it (a test name, a command and its output).
8. Make a last commit, then report: the branch, git rev-parse HEAD, built (true only if the package is built and committed), the files changed (git diff --stat origin/main...HEAD), and anything the lead must know.

Quality bar: this code will be reviewed by agents told to break it, and then merged to main. Write the tests you would want if you were the reviewer: each must be able to fail. Tests are judged on macOS, which no one here can run: resolve a path under t.TempDir() with filepath.EvalSymlinks before comparing it with one git or the system resolved, and lean on no two names that differ only in letter case. Keep the code plain and small; match the style of the package you are in.`
}

const LENS = {
  general: 'Check everything: behaviour, the card\'s acceptance list, the project\'s rules.',
  contract: 'Your lens is the contract and its tests: does the code do exactly what the card and the design say, on every input, and can each test fail? Hunt for the input the builder did not think of.',
  privacy: 'Your lens is privacy and safety: can anything be kept, returned, printed, logged or shown that should not be (captured text, a path under a home folder, a project or branch name, a session id, text off the record, a secret, text under the wrong author)? Does anything weaken a guarantee v1 has today? Is any rule of AGENTS.md crossed?',
  protection: 'Your lens is protection that fails closed: look for a document, a line or a file that the code reads as unprotected, as readable or as absent, which a YAML reader, v1 or the written format would call locked, verified, unreadable or present. Write such inputs yourself and feed them in; each one you find is a defect with its evidence.',
  rule: 'Your lens is the rule itself where it is a matter of chance or of counting: measure it with a harness of your own on made-up inputs, and say how often it is wrong each way.',
}
for (const p of PACKAGES) for (const lens of [...(p.lenses || []), ...(p.more_lenses || [])]) {
  if (!LENS[lens]) throw new Error(`${p.id}: no lens "${lens}"; the lenses are ${Object.keys(LENS).join(', ')}`)
}

function reviewPrompt(p, lens, build) {
  return `${COMMON}

You are a reviewer of package ${p.id}. Reviewing it is part of the build the owner asked for. You have the card docs/v2/cards/${p.id}.md and the builder's branch, and nothing else to go on. Your job is to break it. Approval is worth little; a real defect with its evidence is worth a lot. ${LENS[lens] || LENS.general} ${scratchLine(p, 'review-' + lens)}${p.note ? `\n\nA note from the lead on this package: ${p.note}` : ''}

The builder reports this (treat every claim as unproven):
${JSON.stringify(build, null, 1)}

Steps:
1. "git fetch origin main", then "git checkout --detach ${build.branch}". This worktree is yours: change anything you like in it, but push nothing. The branch may have been cut from an older main, whose check scripts are older: take main's with "git checkout origin/main -- scripts/linux-check.sh scripts/linux-gate.sh scripts/linux-space.sh" before you run a check (leave that change uncommitted; it is not part of the package).
2. Read the card, then the diff: git diff origin/main...${build.branch}
3. Check each "Accepted when" item yourself. Run the package's tests. Run scripts/linux-check.sh yourself once: do not take the builder's word for a pass.
4. Try to break it: write throwaway tests and inputs; change one line of the code and see whether a test notices (a test that cannot fail is an issue); look for a diff outside the card's paths (git diff --stat origin/main...${build.branch}); look for behaviour that changed where the card says it must not; look for anything that depends on the machine's speed, on the real home, or on the network; look for a test that would fail on macOS (a path compared without resolving it, a file system that tells letter case apart).
5. Report each issue with its severity (blocker: must not merge; major: wrong or unsafe, fix before merge; minor: worth fixing), where it is, what shows it, and the fix. Do not report style preferences. Say also what you checked and found sound. A fault you find outside the package's paths goes in too, marked as outside the package.`
}

function fixPrompt(p, build, reviews, round) {
  const issues = reviews.flatMap((r, i) => r.issues.map(x => ({ reviewer: i + 1, ...x })))
  const lower = p.id.toLowerCase()
  // round_offset: for a package that has had fix rounds before, so that its branches go on from the last.
  // fix_branch: the branch an earlier fixer of the first round was cut off on; the fixer finds it and carries on.
  const branch = (p.fix_branch && round === 1) ? p.fix_branch : `pkg/${lower}-r${round + (p.round_offset || 0)}`
  return `${COMMON}

You are the fixer of package ${p.id}: you answer what its reviewers found. The card is docs/v2/cards/${p.id}.md. The branch under review is ${build.branch} at ${build.sha}. ${scratchLine(p, 'fix' + round)}${p.note ? `\n\nA note from the lead on this package: ${p.note}` : ''}

Steps:
1. "git fetch origin main", then: git switch -c ${branch} ${build.branch}   (if ${branch} already exists, an earlier fixer was cut off: "git checkout --detach ${branch}", then "git switch -c ${branch}b", and carry on from what is there; report the branch you end on)
2. Bring main in, so that you run today's check scripts and the lead's merge is clean: "git merge origin/main" (a merge, not a rebase: the lead squashes your branch). If it conflicts, resolve it so that both sides' changes hold, run the tests of the files you resolved, and say in your report what conflicted and how you joined it.
3. For each issue below: reproduce it, then fix it. Fix every blocker and major. Fix a minor when the fix is small and safe. If you are sure an issue is wrong, do not change the code: say why, with evidence, under open_questions as "declined: <issue> because <reason>". An issue marked as outside the package is not yours to fix: list it under outside_card for the lead.
4. Add or strengthen a test for each thing you fix, so the fault cannot come back unseen: take the fix out again and see the test fail. Commit as you go.
5. Run scripts/linux-check.sh from the worktree root. All six steps must pass, but for the benchmark under load as the rules above say. If the package changes a hook or anything a hook runs, run scripts/linux-gate.sh origin/main once, as the rules above say.
6. Commit, then report as a builder does, with the new branch, git rev-parse HEAD and built set to true. Carry over into words_for_the_lead every sentence the builder gave the lead that still holds, corrected where your fixes changed what it says.

The builder's report, for its words to the lead and what it did outside the card:
${JSON.stringify({ outside_card: build.outside_card, words_for_the_lead: build.words_for_the_lead, open_questions: build.open_questions }, null, 1)}

The issues:
${JSON.stringify(issues, null, 1)}`
}

function verifyPrompt(p, build, reviews, fix) {
  const issues = reviews.flatMap((r, i) => r.issues.map(x => ({ reviewer: i + 1, severity: x.severity, where: x.where, problem: x.problem, fix: x.fix })))
  return `${COMMON}

You are the verifier of package ${p.id}. Reviewers found faults in the builder's branch ${build.branch}; a fixer answered them on ${fix.branch} at ${fix.sha}. You have not seen any of it before. The lead merges this branch to main on your word, and from there it goes into a release that runs on a person's machine, so a fault you miss is a fault they meet. ${scratchLine(p, 'verify')}

Your job, in this order:
1. "git fetch origin main", then "git checkout --detach ${fix.branch}". This worktree is yours; push nothing.
2. For each issue below, decide for yourself whether it is truly fixed: read the fix, find its test, take the fix out and see the test fail, put it back. An issue the fixer declined: decide whether the reason holds.
3. Look for what the fixes broke or left half done: read the whole diff of the fix round (git diff ${build.sha}...${fix.sha}), and the package's whole diff against main (git diff origin/main...${fix.branch}) for anything outside the card's paths, anything that changes what v1 does with every switch off, anything printed, stored or logged that could hold a person's text, a path, a project, branch or session name, or a model's name.
4. Run the package's tests through scripts/linux-check.sh -run ... for the packages it touches, and the quick tests that read documents and scripts. A full scripts/linux-check.sh run is the fixer's to have done: run it yourself only if the fixer's report does not show a pass of every step but the benchmark.
5. Report as a reviewer does: verdict accept when every blocker and major is fixed and you found nothing new of that weight; accept_after_fixes when something of that weight remains or is new; reject only if the branch should not be merged even after another round. List each remaining or new issue with its evidence and the fix. Under checked, list each original issue with "fixed", "declined, rightly" or "not fixed".

The fixer's report (treat every claim as unproven):
${JSON.stringify(fix, null, 1)}

The issues the reviewers raised:
${JSON.stringify(issues, null, 1)}`
}

// args.agent: the options every agent of the wave gets besides its worktree,
// as the lead's own instructions set them.
const OPTS = { ...((args && args.agent) || {}), isolation: 'worktree' }

const results = await pipeline(
  PACKAGES,
  async (p) => {
    if (p.build && p.build.branch) return { id: p.id, build: p.build }
    const build = await agent(buildPrompt(p), { ...OPTS, schema: BUILD_SCHEMA, label: `build:${p.id}`, phase: 'Build' })
    return { id: p.id, build }
  },
  async (prev, p) => {
    if (!prev || !prev.build || !prev.build.built || !prev.build.branch) {
      log(`${p.id}: not built, so not reviewed`)
      return prev
    }
    const given = (p.reviews && p.reviews.length) ? p.reviews : []
    // more_lenses: reviews still owed for a package that comes with some of its reviews done.
    if (given.length && !(p.more_lenses && p.more_lenses.length)) return { ...prev, reviews: given }
    const lenses = given.length ? p.more_lenses : p.lenses
    const reviews = (await parallel(lenses.map(lens => () =>
      agent(reviewPrompt(p, lens, prev.build), { ...OPTS, schema: REVIEW_SCHEMA, label: `review:${p.id}:${lens}`, phase: 'Review' })
    ))).filter(Boolean)
    return { ...prev, reviews: [...given, ...reviews] }
  },
  async (prev, p) => {
    if (!prev || !prev.reviews || !prev.reviews.length) return prev
    const any = prev.reviews.some(r => r.issues.length > 0)
    const unchecked = prev.build.linux_check !== 'pass'
    if (!any && !unchecked) return { ...prev, final: prev.build }
    // fix: an earlier fixer's finished report: the fix stage is skipped and the verification starts from it.
    if (p.fix && p.fix.built && p.fix.branch) return { ...prev, fix: p.fix, final: p.fix }
    const fix = await agent(fixPrompt(p, prev.build, prev.reviews, 1), { ...OPTS, schema: BUILD_SCHEMA, label: `fix:${p.id}`, phase: 'Fix' })
    return { ...prev, fix, final: fix || prev.build }
  },
  async (prev, p) => {
    if (!prev || !prev.fix || !prev.fix.built || !p.verify) return prev
    // Rounds of verify, then fix, ending on a verification: a wave never ends on a fix nobody checked.
    // Round 1's calls keep the same words whatever the number of rounds, so a resumed run replays them.
    let base = prev.build, reviews = prev.reviews, fix = prev.fix
    const rounds = []
    let out = prev
    for (let n = 1; n <= MAX_ROUNDS; n++) {
      const note = (n > 1 || p.round_offset || p.verify_note_first) && p.verify_note ? `\n\nA note from the lead for this round: ${p.verify_note}` : ''
      const role = n === 1 ? 'verify' : 'verify' + n
      const prompt = n === 1 ? verifyPrompt(p, base, reviews, fix) + note : verifyPrompt(p, base, reviews, fix).replace(scratchLine(p, 'verify'), scratchLine(p, role)) + note
      const verdict = await agent(prompt, { ...OPTS, schema: REVIEW_SCHEMA, label: n === 1 ? `verify:${p.id}` : `verify${n}:${p.id}`, phase: 'Verify' })
      if (!verdict) { log(`${p.id}: verification ${n} gave nothing back; ${fix.branch} is not verified`); return { ...out, final: fix, verified: false } }
      rounds.push({ round: n, on: fix.branch, verdict })
      out = n === 1 ? { ...out, verdict } : { ...out, rounds }
      const heavy = verdict.issues.filter(x => x.severity !== 'minor')
      if (!heavy.length) return { ...out, rounds, final: fix, verified: true }
      if (n === MAX_ROUNDS) { log(`${p.id}: verification ${n} still found ${heavy.length} blocker or major issue(s); no round is left, so ${fix.branch} is not verified`); return { ...out, rounds, final: fix, verified: false } }
      log(`${p.id}: verification ${n} found ${heavy.length} blocker or major issue(s); fix round ${n + 1} follows`)
      const next = await agent(fixPrompt(p, fix, [verdict], n + 1), { ...OPTS, schema: BUILD_SCHEMA, label: `fix${n + 1}:${p.id}`, phase: 'Fix again' })
      if (!next || !next.built) { log(`${p.id}: fix round ${n + 1} did not finish`); return { ...out, rounds, final: fix, verified: false } }
      out = n === 1 ? { ...out, fix2: next } : { ...out, ['fix' + (n + 1)]: next }
      base = fix; reviews = [verdict]; fix = next
    }
    return out
  },
)

const done = results.filter(Boolean)
log(`packages through the pipeline: ${done.length} of ${PACKAGES.length}`)
return done
