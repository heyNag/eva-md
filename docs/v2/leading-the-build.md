# Leading the build

For whoever leads the v2 build next, a person or an agent that takes it over part way. [The plan](plan.md) says what is built and why; section 9 of it says how the build is run. This says how that has worked in practice, and what each lesson cost, so that it is paid for once.

## Where the state is

- **The plan** governs. A card that differs from it is wrong and is fixed.
- **The cards and their index**, [cards/README.md](cards/README.md): one card for each package, and for each its state (ready, building, merged, or what it waits for). Keep the index true at every merge: it is the only list of where the build stands.
- **`main`'s history.** Each package is one commit whose message says what it gives and what its reviewers found.
- **CI**, for each commit: the tests on macOS, which is the run of record; the latency gate in a job of its own; the checks on Linux.
- **Not in the repository:** the lead's working notes, and the branches of packages still being built, which exist only on the machine that builds them. A session that ends takes both with it. So what must outlive a session is written into a card, the index or this file, and a package is merged as soon as it is sound. One that is not merged when a session ends is built again from its card.

## How a package goes from card to main

1. **The card first, and pushed.** A builder starts in a worktree cut from `main` as the remote has it. A card that is only on the lead's machine is not there.
2. **A builder** makes a branch `pkg/<id>`, commits as it goes, runs `scripts/linux-check.sh`, and reports against the card's "Accepted when" list, item by item, with what shows each.
3. **Reviewers** get the card and the diff, and are told to break it. Approval is worth little; a defect with its evidence is worth a lot. One reviewer is enough for a package that changes nothing v1 does. Two for a hook, for redaction, for what is stored, and for anything that reads a person's chats: one looks at the contract and whether each test can fail, the other at what could be kept, printed or shown that should not be. A rule that is a matter of chance, as the latency gate's is, gets a reviewer who measures it by simulation with a harness of their own.
4. **A fixer** answers the reviews on a branch `pkg/<id>-r1`: every blocker and major is fixed with a test, and an issue it is sure is wrong is declined with its evidence.
5. **The lead** reads the reports, squashes the branch into one commit on `main`, and writes its message: what it gives, why, and what the reviewers found, with a trailer `Card: <ID>` that names the package's card, which the ownership check (EV-11) will read. The lead writes the lines of `AGENTS.md` and `README.md` the card kept back, marks the card merged in the index, pushes, reads CI for that commit, and fixes a failure forward at once.

The owner's leave for all of this, and its limits, are in `AGENTS.md` ("During the v2 build the owner has given standing leave"). A release is never the lead's: no tag, no release workflow.

## What a builder or a reviewer is told, every time

An agent that starts a task knows only what its order says. Each order carries:

- **Why the task is the owner's.** An agent that sees only the owner's latest words may take them for the whole task and refuse the rest. The order quotes the owner's leave, says which of their messages are settled constraints and not the task, and says that building or reviewing this package is what was asked.
- **Start from `main` as it is now:** fetch it, and if the worktree is behind and holds no work yet, reset to it.
- **The limits:** work only in your worktree; never push, tag or start a release; never run setup, install, update or the like against the real home; `internal/redact/baseline` is never edited; no new module; made-up values in fixtures.
- **Commit at every working state**, with plain messages that name no model or assistant.
- **The checks:** `scripts/linux-check.sh`, and `scripts/linux-gate.sh` for anything a hook runs; what a failure of the benchmark under load looks like, and that code is never changed to chase it.
- **A folder of your own for scratch files.** Never write into another agent's.
- **For a reviewer:** take no claim of the builder's on trust; run the checks yourself; change one line and see whether a test notices.

## What the first waves taught

Each of these happened, and each is now a rule in `AGENTS.md`, in the cards' README or in a script.

1. **Three builders started from a commit that lacked their cards**, because the cards were committed and not yet pushed. Push the cards, then start builders.
2. **Builders refused to build.** They read the owner's last message, which was about releases, as the whole task. Every order now says why its task is the owner's.
3. **The disk filled, and the machine was restarted.** Go's build cache keeps each package under the folder it was built in, every check ran in a snapshot with a folder of its own, and the cache reached 23 GB in a day. `linux-check.sh` now keeps a run's cache with its snapshot and removes both; `scripts/linux-space.sh` shows and frees the rest. Look at the space left before starting a wave.
4. **The restart lost a fixer three hours into its work.** Its commits were there; its report and its unfinished proof were not. What saved the proof was that it could be started again where it stopped. Long jobs are written that way, and agents commit as they go.
5. **A test passed on Linux and failed on macOS**, where a temporary folder is reached through an alias. No builder can run macOS, so tests are written for it, and CI is read after every push.
6. **A new script turned CI red.** A test reads every `go build` line of every script, and it read a closing parenthesis as part of the package's name. The tests that read scripts, workflows and documents take seconds: run them before pushing one.
7. **Editing a script while it ran broke that run** part way through.
8. **Timing under load proves nothing.** With several agents testing, the benchmark failed on code that was sound, and the latency gate said "not judged". In CI the gate first ran after the race tests and could not judge; it has a job of its own now.
9. **Twenty clean runs did not show what a simulation did.** The gate passed an unchanged binary 19 times of 20 on a quiet machine. A reviewer who fed its rule made-up samples under load found that it called a real slowdown of a fifth "not judged" two times in three, and failed an unchanged binary under some patterns of load. A rule of chance is reviewed by simulation, not only by running it.
10. **Two packages built at once wanted the same file.** Where that could be seen coming, a small registry removed it: commands, status rows and switches each register from a file of their own, and so do the evaluation command's instruments. Where it could not, as with one pinned line of text, the lead joins the two at the merge.
11. **A reviewer overwrote a builder's files** in a scratch folder they shared. Each agent now has its own.
12. **A prompt for the real install is reviewed like code.** The first one asked for three commands that failed on that machine. The draft of the second would have let model names, page paths and project names into its report, until a reviewer went through it command by command. Such a prompt is reviewed for privacy and for whether every command in it exists, and each command is run against the built release before the owner takes it there.

## What is the lead's alone

- The cards, the index and this file.
- `AGENTS.md` and `README.md`, beyond the lines a card names for its builder.
- The merge: the commit on `main`, its message, and CI's result after it.
- The stored latency baseline, taken from a pass of CI's own job, and the count of passes toward the twenty in twenty that make the gate blocking. A change to the gate's rule starts that count again.
- Telling the owner, in plain words, what was measured, what it changed and what is asked of them next.

## What is the owner's alone

- Every release, and the upgrade of the real install.
- Every live run on their machines or accounts: the host sittings, the live gates, the field evaluations.
- The decisions of section 7 of the plan that wait for the Stage 0 readout, and the answers only they can give (section 13.6).
