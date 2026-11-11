# Leading the build

For whoever leads the v2 build next, a person or an agent that takes it over part way. [The plan](plan.md) says what is built and why; section 9 of it says how the build is run, and section 15 gives the order of work and the owner's calendar. This says how that has worked in practice, and what each lesson cost, so that it is paid for once.

## Where the state is

- **The plan** governs. A card that differs from it is wrong and is fixed.
- **The cards and their index**, [cards/README.md](cards/README.md): one card for each package, and for each its state (ready, building, in review, merged, or what it waits for). Keep the index true at every change of state: mark a card `building` in the push that starts its builder, `in review` when its reviewers start, and `merged` at the merge. It is the only list of where the build stands.
- **`main`'s history.** Each package is one commit whose message says what it gives and what its reviewers found.
- **CI**, for each commit: the tests on macOS, which is the run of record; the latency gate in a job of its own; the checks on Linux.
- **Not in the repository:** the lead's working notes, and the branches of packages still being built, which exist only on the machine that builds them. A session that ends takes both with it. So what must outlive a session is written into a card, the index or this file, and a package is merged as soon as it is sound. One that is not merged when a session ends is built again from its card. On that machine `git branch --list 'pkg/*'` and `git worktree list` show what is in progress, but a squash merge leaves its branch behind, so a branch alone proves nothing.

## How a package goes from card to main

1. **The card first, and pushed.** A builder starts in a worktree cut from `main` as the remote has it. A card that is only on the lead's machine is not there.
2. **A builder** makes a branch `pkg/<id>`, commits as it goes, runs `scripts/linux-check.sh`, and reports against the card's "Accepted when" list, item by item, with what shows each.
3. **Reviewers** get the card and the diff, and are told to break it. Approval is worth little; a defect with its evidence is worth a lot. One reviewer is enough for a package that changes nothing v1 does. Two for a hook, for redaction, for the record and the store, for the pause switch, and for anything that reads a person's chats or puts text in one (plan section 9): one looks at the contract and whether each test can fail, the other at what could be kept, printed or shown that should not be. A rule that is a matter of chance, as the latency gate's is, gets a reviewer who measures it by simulation with a harness of their own.
4. **A fixer** answers the reviews on a branch `pkg/<id>-r1`: every blocker and major is fixed with a test, and an issue it is sure is wrong is declined with its evidence.
5. **The lead** reads the reports, checks the diff's paths against the card's "Owns and may touch" with `git diff --stat main...pkg/<id>-r1` (no CI job checks them yet) and asks for every path outside it to be named in the report, runs the quick document and script tests `AGENTS.md` names on the result, squashes the branch into one commit on `main`, and writes its message: what it gives, why, and what the reviewers found, with a trailer `Card: <ID>` that names the package's card, which the ownership check (EV-11) will read. The lead writes the lines of `AGENTS.md` and `README.md` the card kept back, marks the card merged in the index, pushes, reads CI for that commit, and fixes a failure forward at once. CI is read with `gh run list --commit <sha>` and `gh run view <id> --log-failed` where gh is there, or with the session's own GitHub tools. A green run says nothing of the latency gate, whose step is advisory: its verdict is in that step's log or in the run's `latency` artifact, and the verdict of every run that counts goes into the table in [baselines/README.md](baselines/README.md).

The owner's leave for all of this, and its limits, are in `AGENTS.md` ("During the v2 build the owner has given standing leave"). A release is never the lead's: no tag, no release workflow. Within the build only the lead pushes; builders, reviewers and fixers commit on their own branches.

## What a builder or a reviewer is told, every time

An agent that starts a task knows only what its order says. Each order carries:

- **Why the task is the owner's.** An agent that sees only the owner's latest words may take them for the whole task and refuse the rest. The order quotes the owner's leave, says which of their messages are settled constraints and not the task, and says that building or reviewing this package is what was asked.
- **Start from `main` as it is now:** fetch it, and `git merge --ff-only origin/main`, which never discards anything; a branch with commits of its own rebases on it instead. A fix to a script reaches a builder only when its branch holds it: a builder on an older branch keeps running the old script. After pushing one, tell every running builder to rebase on `origin/main` before its next check.
- **The limits:** work only in your worktree; never push, tag or start a release; never run setup, install, update or the like against the real home, and never read the real home, `~/.claude` or `~/.codex` with a script of your own; `internal/redact/baseline` is never edited; no new module; made-up values in fixtures.
- **Commit at every working state**, on its own branch, with plain messages that name no model or assistant beyond the trailer the session gives.
- **The checks:** `scripts/linux-check.sh`, and `scripts/linux-gate.sh` for anything a hook runs; what a failure of the benchmark under load looks like, and that code is never changed to chase it.
- **A folder of your own for scratch files.** Never write into another agent's.
- **For a reviewer:** take no claim of the builder's on trust; run the checks yourself; change one line and see whether a test notices.

An order can start from this, with the package's own words put in:

> The owner of this repository asked for the v2 build and gave standing leave for it (`AGENTS.md`, "During the v2 build the owner has given standing leave"). Building <id> from `docs/v2/cards/<id>.md` is that request; the owner's other messages, about releases or live runs, are settled limits, not your task. Run `git fetch origin main`, then `git merge --ff-only origin/main`; if it refuses because your branch has commits of its own, rebase on `origin/main`, and never `git reset --hard` over changes `git status` shows. Work on branch `pkg/<id>`, commit at every working state, and keep scratch files only in <folder>. Run `scripts/linux-check.sh`; a benchmark failure under load is run again when the machine is quiet, never chased in code. Work only in your worktree; never run setup, install, update or the like against the real home; never edit `internal/redact/baseline`; add no module; fixtures hold made-up values. For anything a hook runs, also run `scripts/linux-gate.sh`. Never push, tag or start a release. Report against the card's "Accepted when", item by item. (For a reviewer: take no claim on trust, run the checks yourself, and change one line to see whether a test notices.)

## What the first waves taught

Each of these happened. Items 3 to 8 and 11 are now rules in `AGENTS.md` or in a script; item 10 is in the cards' README; items 1, 2, 9 and 12 are held by the steps and orders above, so they hold only as long as each order carries them.

1. **Three builders started from a commit that lacked their cards**, because the cards were committed and not yet pushed. Push the cards, then start builders.
2. **Builders refused to build.** They read the owner's last message, which was about releases, as the whole task. Every order now says why its task is the owner's.
3. **The disk filled, and the machine was restarted.** Go's build cache keeps each package under the folder it was built in, every check ran in a snapshot with a folder of its own, and the cache reached 23 GB in a day. `linux-check.sh` now keeps a run's cache with its snapshot and removes both, and refuses to start unless 2,500 MB are free plus 2,000 MB for each check already running; `scripts/linux-space.sh` shows and frees the rest. Before starting a wave, run `scripts/linux-space.sh`, start no more builders than (free MB − 500) / 2,000, since each may run a check at once, and prune first if that is fewer than you need. A builder on a branch from before that fix still fills the old shared cache: have it rebase, and prune between waves until all have.
4. **The restart lost a fixer three hours into its work.** Its commits were there; its report and its unfinished proof were not. What saved the proof was that it could be started again where it stopped. Long jobs are written that way, and agents commit as they go.
5. **A test passed on Linux and failed on macOS**, where a temporary folder is reached through an alias. No builder can run macOS, so tests are written for it, and CI is read after every push. It happened a second time, with the capture audit: a rule that compared a folder with `/tmp` was right on Linux and wrong on macOS, where `/tmp` is a link and a folder that is gone cannot be resolved. A test of such a rule names its root through a link, so that Linux shows what macOS would.
6. **A new script turned CI red.** A test reads every `go build` line of every script, and it read a closing parenthesis as part of the package's name. The tests that read scripts, workflows and documents take seconds: run them before pushing one.
7. **Editing a script while it ran broke that run** part way through.
8. **Timing under load proves nothing.** With several agents testing, the benchmark failed on code that was sound, and the latency gate said "not judged". In CI the gate first ran after the race tests and could not judge; it has a job of its own now.
9. **Twenty clean runs did not show what a simulation did.** The gate passed an unchanged binary 19 times of 20 on a quiet machine. A reviewer who fed its rule made-up samples under load found that it called a real slowdown of a fifth "not judged" two times in three, and failed an unchanged binary under some patterns of load. A rule of chance is reviewed by simulation, not only by running it.
10. **Two packages built at once wanted the same file.** Where that could be seen coming, a small registry removed it: commands, status rows and switches each register from a file of their own, and so do the evaluation command's instruments. Where it could not, as with one pinned line of text, the lead joins the two at the merge.
11. **A reviewer overwrote a builder's files** in a scratch folder they shared. Each agent now has its own.
12. **A prompt for the real install is reviewed like code.** The first one, [field-report-prompt.md](field-report-prompt.md), asked for three commands that failed on that machine. The draft of the second would have let model names, page paths and project names into its report, until a reviewer went through it command by command. Such a prompt is reviewed for privacy and for whether every command in it exists. The second is [field-report-2-prompt.md](field-report-2-prompt.md), with the message that goes with it in [field-report-2-message.md](field-report-2-message.md): before the owner takes it there, each command it names is run with the release stand-in's binary (the release's code, built with the tests' `evamd_testlimits` tag so that it runs on Linux), in a home of its own, never the real home.
13. **Builders waited hours for a quiet machine.** Six agents on four CPUs kept the load at eight times the CPUs. The latency gate said "not judged", the rule told each builder to run it again when the machine was quiet, and it never was, so the packages on the way to a release stood still. A builder now waits half an hour at most and reports the load; the lead reads CI's `latency` job on the merge. And a wave is no larger than the machine: the next one starts when the last one's checks are over, and the packages a release waits for go first.
14. **A review found a fault in a package already merged.** The codec's reviewers found nine ways to write a lock that the format checker, merged before it, read as no lock. A finding outside a package's paths is not fixed in that package: the builder reports it, and the lead writes a card for it, as ST-01b is for this one.
15. **An agent tried its script on the build machine's own host folders.** Asked to write a reader of the hosts' session files, a writer ran it over the real `~/.claude` of the machine it worked on, which holds the record of real sessions. It printed only counts, and none of it reached the repository, but the rule that tests never touch the real machine had not been said to an agent that was writing no test. Every order now says it: nothing an agent writes or runs reads the real home, `~/.claude` or `~/.codex`; a made-up home under its own scratch folder is the only test data.
16. **A file that has an agent write its own scripts over a person's records leaked in three trials running.** It was meant to settle host facts on the real install with no sitting. Each trial, in a made-up home with markers planted in every value and key, found new leaks, also after the rule became "print only names on a fixed list": a path in a traceback, a version's suffix, a word in another script, digits that were not ASCII. Reading a person's records belongs in tested code with fixed lists (card HF-01), as the report and the capture audit are, and a prompt for the real install only runs such commands and copies their numbers. The trials were what showed it: a privacy review alone had passed the first version with fixes.
17. **A list of failures carries no times.** A wave reported ten agents failed on a usage limit when it ended, two hours after they had failed and after the limit had been lifted. The lead read it as a limit in force and told the owner the build had stopped. Before acting on a failure a workflow reports, read the last line of a failed agent's transcript for its time, and look at whether other agents are answering now.
18. **Built branches live on one machine until they merge.** A dozen packages, built and waiting for a review or for the release, existed only as local branches on a cloud machine that nothing promises to keep. The lead now keeps their tips as the parents of one commit, with `main`'s tree, on the session's own branch on the remote: it is no branch to merge, and a tip is fetched from it by its id. Renew it when a wave ends, with the last such commit as a parent, so that the push is a fast-forward.

## What is the lead's alone

- The cards, the index and this file.
- `AGENTS.md` and `README.md`, beyond the lines a card names for its builder.
- The merge: the commit on `main`, its message, and CI's result after it.
- The stored latency baseline, taken from a pass of CI's own job, and the count of passes toward the twenty with no fail between them that make the gate blocking, kept in [baselines/README.md](baselines/README.md). A change to the gate's rule starts that count again.
- Telling the owner, in plain words, what was measured, what it changed and what is asked of them next.

## What is the owner's alone

- Every release, and the upgrade of the real install.
- Every live run on their machines or accounts: the host sittings, the live gates, the field evaluations. The host sittings follow [host-sitting-runbook.md](host-sitting-runbook.md), whose Part 1 is the core (about three hours of steps in three sittings) and whose Part 2 is asked for group by group; [host-facts.md](host-facts.md) says for each fact how it is settled, and the lead writes each result into it. The sittings wait for the recorder ([cards/HF-00.md](cards/HF-00.md)).
- The decisions of section 7 of the plan that wait for the Stage 0 readout, and the answers only they can give (section 13.6).
- The two API keys of decision 28. Until the owner says they exist, no live evaluation runs in containers.

## Before the owner is told the counters release is ready

- CT-1 to CT-4 merged. Each that touches a hook has its latency comparison reported: by the gate's second-round rule if EV-16b has merged; if not, by `scripts/linux-gate.sh` on a quiet machine and by CI's latency job.
- The second field report's prompt finished as its draft's first paragraph says.
- The three sentences for v1's documents written (plan section 8, "Three sentences go into v1's documents with it").
- CI green for the commit on both test jobs.

Then the lead tells the owner it is ready, and the owner cuts it. The lead never cuts it.
