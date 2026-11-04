# Releasing

A release is an annotated git tag on a commit of main and the GitHub release the release workflow publishes for it. The maintainer cuts one in either of two ways: with the Cut a release workflow, from GitHub, which needs no Mac, or with `scripts/release.sh` from a Mac. This page says who cuts one, what each way checks, the GitHub settings Cut a release needs, how to verify a release by hand and how to pull a bad one.

## Who releases, and how

The maintainer releases. Before each release, check both desktop apps by hand, as design.md's live resume test asks; neither way checks them.

Both ways make the same checks of main with `scripts/release.sh`, all but the signing check from GitHub, and name the same tag and notes; both push only the tag, never a branch and never with force; both have `.github/workflows/release.yml` build, attest and publish the release, and both then check what it published. They differ in who says yes and how that is recorded. Cut a release waits for a required reviewer of the repository's `release` environment to approve it on GitHub, which records who did, and makes an unsigned tag. `scripts/release.sh` asks on the Mac's terminal and makes a tag signed with the maintainer's key.

Cut a release is the usual way. `scripts/release.sh` is the fallback: for a release when Actions can't cut one, as when GitHub refuses the workflow's tag push (see "Cut a release"), or for a signed tag.

## Cut a release

`.github/workflows/cut-release.yml` cuts a release from GitHub. It needs write access to the repository, to run the workflow, a required reviewer of the `release` environment to approve it, and the settings in "The GitHub settings Cut a release needs", made once.

To run it, open the repository's Actions tab, choose Cut a release, then Run workflow, with "Use workflow from" on main. Tick "Only check main and show the release; tag nothing" for a dry run. With gh, the same is:

```sh
gh workflow run cut-release.yml --repo heyNag/eva-md --ref main -f dry_run=true    # a dry run
gh workflow run cut-release.yml --repo heyNag/eva-md --ref main                    # a release
```

A dry run makes every check and shows the release in the run's summary, then stops: nothing waits for an approval, and nothing is tagged or pushed. A release does the same, then waits for the approval. GitHub tells the environment's required reviewers that a run is waiting; open the run and choose Review deployments, tick `release` and approve, in a browser or the GitHub app. Rejecting it stops the run with nothing tagged. Each approved run also shows as a deployment to `release` on the repository's Deployments page.

Its jobs run in this order, each only after the one before it passes. The workflow may do nothing by default (`permissions: {}`), and each job asks for only what it needs:

1. **plan** (Ubuntu, `actions: read`, `contents: read`). Each of its failures shows as an annotation on the run, with what is wrong and the fix. It fails unless the run is on main. It reads the `release` environment and fails unless the environment exists with at least one required reviewer: without it, GitHub would make the environment at the approve job, unprotected, and the release would go out with no approval. It warns when the environment allows any branch. It checks out main and fails when main has moved since the run started, since the build provenance names the commit the run started on; run it again to release main as it is now. Then it runs `scripts/release.sh --plan`, which makes the checks in "What the checks are, and why" and names the tag and notes. It writes the release to the run's summary: the tag, a link to the commit, the notes, and either "Dry run: nothing was tagged or pushed." or "Approve the release environment to tag and publish <tag>." A dry run stops here.
2. **approve** (no permissions, in the `release` environment). GitHub holds it until a required reviewer approves, and records who did. It adds "Approved releasing <tag> from <commit>." to the summary.
3. **tag** (Ubuntu, `contents: write`). It fails unless the plan gave a tag in the release format and a full commit hash. It checks out the planned commit, not main's tip, since main may have moved while the release waited for its approval. When origin doesn't have the tag, it makes an unsigned annotated tag on the commit with the notes as its message (`git tag -a --no-sign`), whose tagger is who started the run, and pushes only that tag (`git push --no-follow-tags origin refs/tags/<tag>`), never with force, so the push fails if the tag appeared meanwhile. When origin has the tag already, it fetches it, and goes on without making or pushing a tag only when origin's tag is annotated, on the planned commit and has the planned notes as its message, as when the job is re-run after a failure that followed its push. Otherwise it fails: origin's tag is on another commit or has other notes, and a tag is never moved. Either way, it fails unless origin's tag is then the one it has here.
4. **publish**. It calls the release workflow with the tag (see "What the workflow builds and publishes"), granting it `contents: write`, `id-token: write` and `attestations: write`, exactly what that workflow's jobs ask for. A tag pushed with a workflow's token starts no workflow, so the run calls it itself.
5. **check** (macOS, `contents: read`, `attestations: read`). It downloads the published release and checks it as `scripts/release.sh` does: every asset is there, `SHA256SUMS` lists exactly the other five and matches them, `gh attestation verify` confirms each of those five was built by this repository's release workflow (`--signer-workflow heyNag/eva-md/.github/workflows/release.yml`) in a run on main (`--source-ref refs/heads/main`, since a called workflow's source ref is its caller's) from the planned commit (`--source-digest <commit>`), and only then `eva-md-darwin-arm64` runs on the Apple silicon runner to show it prints the tag as its version. Nothing downloaded runs before its provenance verifies. It adds the release's URL and the install command to the summary.

Every job but publish has a time limit, and so does each job of the release workflow publish calls; the wait for an approval doesn't count toward the approve job's. One Cut a release run goes at a time. A run awaiting its approval is waiting, in GitHub's terms, and holds the `cut-release` concurrency group. A run started meanwhile, a dry run included, is queued (pending) until the waiting run is approved, rejected or cancelled. GitHub keeps only the newest queued run, cancelling an older queued one, and never cancels the waiting run for it. So reject or cancel a release you won't approve rather than leave it waiting: until then, every run started after it stays queued.

When a job fails, its error says what is wrong. Before the tag job pushes, nothing is published: fix what the plan named and run it again. When the push fails, either origin has the tag now, made meanwhile, or GitHub refused to let the workflow's token create the tag, which it can do when main's workflow files changed after the planned commit; run it again, or release from a Mac.

After the push, the tag stays, as with `scripts/release.sh`. A failure a re-run can get past is worth a re-run of the run's failed jobs, with Re-run jobs > Re-run failed jobs on the run's page or `gh run rerun <run id> --failed --repo heyNag/eva-md`: a runner outage in publish or check, a download that failed, or a provenance lookup that failed. A re-run keeps the tag, and re-running failed jobs uses the same commit and inputs. When the tag job fails after its push, as when its check of origin's tag fails, its time limit ends it or the run is cancelled, re-run the run's failed jobs too: the tag job finds origin's tag on the planned commit with the planned notes and goes on to publish. Don't start a new run, whose plan would stop at "already released". Making the release a draft and releasing a fix is for a release that is actually wrong: when publish fails again or for a reason a re-run can't change, release a fix under the next tag; when check fails again, its error says to make the release a draft and release a fix (see "Pull a bad release").

### A tag cut from GitHub

A tag Cut a release makes is annotated but not signed: GitHub's record of who approved the release stands in for the signature. The run that pushed the tag shows who approved it, and GitHub's API lists the same: `gh api repos/heyNag/eva-md/actions/runs/<run id>/approvals`. The release's notes end with a line that links that run: "Cut on GitHub in run <run URL>, which records who approved it." The tag itself doesn't name the run, so its message stays the notes alone. Follow that link to find the run from the release. The run is also among Cut a release's runs, which `gh run list --repo heyNag/eva-md --workflow cut-release.yml` lists, where a dry run is named "Cut a release (dry run)" and a release "Cut a release"; in the run, the plan's summary names the tag. The jobs' names don't name it: GitHub doesn't evaluate the name of a job it skips, as a dry run skips the approve and tag jobs, so they have fixed names, "Approve the release" and "Tag the release". The tagger the tag names is who started the run, which the tag alone doesn't prove. `git tag -v` checks only a tag cut from a Mac. docs/known-limitations.md says what the approval doesn't prove.

### The GitHub settings Cut a release needs

Made once, in the repository's settings, by someone with admin access:

1. **The environment.** Settings > Environments > New environment, named exactly `release`.
2. **Required reviewers.** In it, tick Required reviewers and add the maintainer. Leave "Prevent self-review" unticked, since the maintainer approves the runs they start. "Allow administrators to bypass configured protection rules" is ticked by default and stays ticked, so the maintainer, an admin, can bypass the environment's rules when needed. It doesn't let a run off main through, since the plan job refuses those itself. Environments with required reviewers are free for a public repository. Without an environment named `release` that has a required reviewer, the plan job fails.
3. **Deployment branches and tags.** Choose "Selected branches and tags" and add the branch rule `main`, so only a run on main can reach the approval. With "Selected branches and tags" but no rule `main`, the approve job fails at once with GitHub's message that main can't deploy to `release`, and nothing is tagged; "Protected branches only" fails the same way while main isn't a protected branch. With "No restriction", the plan job only warns.
4. **Tag rulesets.** Leave release tags out of any ruleset that restricts creating tags: the tag job pushes them with the workflow's token, and such a ruleset refuses that push.

## scripts/release.sh from a Mac

The maintainer releases from a Mac with a clone of `github.com/heyNag/eva-md`, push access to it, `gh` signed in to github.com and git set up to sign tags. One command does it, from the checkout's root on main:

```sh
scripts/release.sh --dry-run   # check everything and show the release, then stop
scripts/release.sh             # the same, then ask, tag, push, watch and verify
```

The script shows the tag, the commit and the notes, then asks `Release <tag> from <short commit>? [y/N]` on the terminal. Only `y` or `yes` goes on, also written `Y`, `Yes` or `YES`; anything else, Enter included, stops with nothing tagged. The answer is read from the terminal itself, never from stdin, so piping `yes` into the script answers nothing. No flag skips the question, and without a terminal the script stops before asking. `--dry-run` runs every check and shows the same summary, then stops before the question. Its third mode, `--plan`, is Cut a release's (see "What the checks are, and why"); it can't be combined with `--dry-run`.

After a yes, the script:

1. makes a signed, annotated tag on the commit (`git tag -s`), with the notes as its message;
2. pushes only that tag (`git push --no-follow-tags origin refs/tags/<tag>`), never a branch and never with force;
3. finds the release workflow's run for the tag and watches it to the end (`gh run watch --exit-status`);
4. downloads the published release into a temporary folder and checks it: every asset is there, `SHA256SUMS` lists exactly the other five and matches them, `gh attestation verify` confirms each of those five was built by this repository's release workflow in the run the tag started (`--source-ref refs/tags/<tag>`), and only then the binary for this Mac's architecture runs to show it prints the tag as its version. The sums come from the same release as the assets, so only the provenance shows they are what the workflow built, and nothing downloaded runs before it verifies;
5. prints the release's URL and the install command.

It never moves, deletes or reuses a tag. When a step after the push fails, the tag stays where it is, and the script says how to look at the run or pull the release. When the release workflow failed for a reason a re-run can get past, such as a runner outage or a download that failed, re-run its failed jobs (`gh run rerun <run id> --failed --repo heyNag/eva-md`), which keeps the tag and uses the same commit, then check the release as "Verify a release by hand" shows, since the script has stopped. The same goes for a provenance lookup that failed in the script's own check: check by hand again first. Making the release a draft and releasing a fix is for a release that is actually wrong. When the push itself fails, the tag is made here only: push it with `git push --no-follow-tags origin refs/tags/<tag>`. The script then checks nothing, so when the workflow's run for the tag ends, check the release as "Verify a release by hand" shows. Running the script again stops with that same fix rather than make a second tag for the commit, and once origin has the tag it stops at "already released".

## What the checks are, and why

`scripts/release.sh` makes these checks in all three modes, but for the two noted below, and stops at the first that fails, saying what is wrong and the exact fix:

| Check | Why |
|---|---|
| It runs from the root of the eva-md checkout. | Every path it reads is relative to the root. |
| HEAD is main. | Releases come from main only; the workflow refuses a tag whose commit isn't on main. |
| The tree has no changes and no untracked files. | The tag names a commit, so what is released must be exactly what was committed. |
| HEAD has `.github/workflows/ci.yml`, `.github/workflows/release.yml`, `scripts/install.sh`, `scripts/uninstall.sh` and `scripts/LICENSES.txt`. | The workflow builds from the tagged tree and copies the install scripts and the licenses from it. |
| `user.signingkey` is set, and the program for `gpg.format` (gpg, ssh-keygen or gpgsm, or the one `gpg.<format>.program` names) is installed. Not in a plan. | The tag is signed, so anyone can check who made it. The key itself is never printed. A plan makes no tag: Cut a release makes an unsigned one once the release is approved. |
| origin is `github.com/heyNag/eva-md` (`https://github.com/heyNag/eva-md`, `git@github.com:heyNag/eva-md` or `ssh://git@github.com/heyNag/eva-md`, with or without `.git`), and so is any push URL it has. | The tag is pushed to origin and the workflow runs only on GitHub, so a tag pushed to a mirror or a fork would release nothing and stay there. The URLs are compared, never printed. |
| After `git fetch`, HEAD is origin/main. | The workflow builds what origin has; a commit only this clone has can't be released. |
| `gh` is installed and signed in to github.com (`gh auth status --hostname github.com`). In a plan, `gh` can read the repository (`gh api repos/heyNag/eva-md`) instead. | The script reads CI runs, watches the workflow and downloads the release with it. In a workflow, gh signs in with the workflow's token, which `gh auth status` may not recognise though it works. |
| The CI workflow passed for HEAD (`gh run list --workflow ci.yml --commit <HEAD>`). | Only a commit whose CI run passed is released, and a run passes only when both its jobs do: the macOS job, which runs every check and test, the race detector included, and the Linux job, which builds eva-md and runs its tests on Linux, where cloud sessions work on it. |
| No release tag is on HEAD already. | One commit is released once. A release tag on HEAD that origin has means it is released. One only this clone has never left it, as after a failed push, so the script says to push it; one whose name origin has on another commit is never moved. |

A plan asks nothing and shows the same summary as a dry run, ending "Plan: nothing was tagged or pushed." When `$GITHUB_OUTPUT` is set, as in a workflow, it also writes `tag`, `commit` (the full hash), `since` (`since <previous tag>` or `the first release`) and `notes` there, the notes in GitHub's multi-line form between two lines of a random delimiter that no note can be. A plan that fails writes nothing there. In a workflow, where `GITHUB_ACTIONS` is `true`, every failure the script stops at is also printed to stdout as a workflow error command, `::error::<problem>. Fix: <fix>`, with `%`, carriage returns and newlines encoded as GitHub reads them, so the plan's failures appear as annotations on the run. The plan is the one place the tag and notes are worked out: Cut a release tags and publishes what its plan job wrote.

## The tag

A release's tag is its version: the UTC day of the released commit's commit date, as year.month.day without zero padding, such as `2026.11.3`. That is the same day `eva-md version` prints for a source build of that commit (`2026.11.3+0915.9e4f609`). Tags have no `v` prefix.

When that day's tag already exists, here or on origin, the next release of the day is `2026.11.3.2`, then `2026.11.3.3`, and so on. The day comes from the commit, not the clock: a commit made late on 2 November in a zone behind UTC may already be 3 November in UTC, and a commit released a day after it was made keeps its own day.

As a regular expression, a release tag is:

```
^[0-9]{4}\.[1-9][0-9]?\.[1-9][0-9]?(\.[2-9]|\.[1-9][0-9]+)?$
```

The notes are the subjects of the commits since the previous release tag on main, one line each, or of every commit for the first release. Both ways of releasing get the tag and notes from `scripts/release.sh`, so a release cut from GitHub is named as one from the Mac would be.

## What the workflow builds and publishes

`.github/workflows/release.yml` runs in two ways: when a tag is pushed, as `scripts/release.sh` pushes one, and when Cut a release calls it (`workflow_call`) with the tag, its one input, which is required. Cut a release has to call it, since a tag pushed with a workflow's token starts no workflow. A called run's ref is the caller's branch, so the workflow reads the tag from its input when called and from the pushed ref otherwise, for its checks, its checkout and its concurrency group: one run per tag at a time. Its jobs each have only the permissions they need, a time limit, and actions pinned to full commit SHAs, as Cut a release's do.

1. **build** (Ubuntu, `contents: read`). It fails unless the tag matches the format above exactly (and, run by a push, the pushed ref is a tag), is an annotated tag, its commit is on main, and no release for it exists yet. It writes the notes from the tag's annotation, without its signature when it has one; called by Cut a release, it adds a blank line and "Cut on GitHub in run <run URL>, which records who approved it.", since a called workflow's run is its caller's, the run that holds the approval. A pushed tag's notes are the annotation alone. It builds `eva-md-darwin-arm64` and `eva-md-darwin-amd64` with `CGO_ENABLED=0`, `-trimpath`, `-mod=readonly` and `-ldflags "-X main.version=<tag>"`. It copies `scripts/install.sh`, `scripts/uninstall.sh` and `scripts/LICENSES.txt` from the tagged tree and writes `SHA256SUMS` for those five files, one `shasum -a 256` line each.
2. **verify** (macOS, no permissions). On an Apple silicon runner, it checks `SHA256SUMS` and that `eva-md-darwin-arm64 version` prints `eva-md <tag>`.
3. **attest** (`id-token: write`, `attestations: write`). It records the build provenance of the five files with `actions/attest-build-provenance`. Since this job of `release.yml` makes it, the provenance names `release.yml` as its signer whichever way the release was cut, called by Cut a release or run by a pushed tag. `scripts/release.sh`, Cut a release's check job and "Verify a release by hand" all check against that one signer, and `TestReleaseChecksProvenanceAgainstOneSigner` keeps the script and the check job so.
4. **release** (`contents: write`). It checks again, now able to see drafts, that no release exists for the tag, then creates the release named by the tag, with the notes and six assets: the two binaries, `install.sh`, `uninstall.sh`, `LICENSES.txt` and `SHA256SUMS`. It never replaces a release.

`LICENSES.txt` holds eva-md's license, then the license of each module the binaries are built with, Go's license, and the notices in Go's own code that ask to go with a copy of the program: those of the fdlibm code in package math and, for the Intel binary, of Inferno's code in the runtime. The binaries contain all of that code, and each of those licenses and notices asks to go with every copy. `TestReleaseLicenses` fails when `scripts/LICENSES.txt` lacks one of them, as after a module is added or upgraded or a new Go brings a notice, or has a section for a module the binaries aren't built with, and its message gives the text to add. It reads Go's license and notices from the Go that runs it, so the file holds those of each Go the tests run with, the release's included.

The assets are plain files, not archives. The newest release's are at `https://github.com/heyNag/eva-md/releases/latest/download/<name>`, and a given release's at `https://github.com/heyNag/eva-md/releases/download/<tag>/<name>`.

## Verify a release by hand

In a new, empty folder:

```sh
tag=2026.11.3
commit=$(gh api "repos/heyNag/eva-md/commits/$tag" --jq .sha)
gh release download "$tag" --repo heyNag/eva-md
shasum -a 256 -c SHA256SUMS
gh attestation verify eva-md-darwin-arm64 --repo heyNag/eva-md \
  --signer-workflow heyNag/eva-md/.github/workflows/release.yml \
  --source-ref refs/heads/main --source-digest "$commit"
chmod +x eva-md-darwin-arm64 && ./eva-md-darwin-arm64 version
```

Use `eva-md-darwin-amd64` on an Intel Mac. Without gh, download the assets with `curl -fsSLO https://github.com/heyNag/eva-md/releases/download/$tag/<name>` and check them with `shasum`; checking the provenance needs gh. The signer is `release.yml` for every release, cut from GitHub or from a Mac. The source ref depends on the way it was cut: `refs/heads/main` for a release cut from GitHub, as above, since the release workflow ran called by Cut a release on main, and `refs/tags/$tag` for one cut from a Mac, since the pushed tag started the release workflow's run. A release cut from GitHub has notes that end "Cut on GitHub in run …". gh matches `--signer-workflow` as a prefix, so without `--source-ref` a run of `release.yml` on any branch would pass.

The provenance proves the files were built by this repository's `release.yml` from that commit. It doesn't prove who released them: a Mac tag's signature or a GitHub run's approval shows that. For a tag cut from a Mac, `git tag -v <tag>` checks its signature when your git knows the maintainer's key; a tag cut from GitHub has no signature, and the Cut a release run that pushed it shows who approved it (see "A tag cut from GitHub").

## Pull a bad release

Hide it first, then release a fix:

```sh
gh release edit 2026.11.3 --repo heyNag/eva-md --draft=true   # hide it; it can be published again
gh release delete 2026.11.3 --repo heyNag/eva-md --yes        # or remove the release; the tag stays
```

A draft or deleted release drops out of `releases/latest`, so new installs get the previous release until the fix is out. Fix main, let CI pass, and release again, either way: with Cut a release or `scripts/release.sh`. A tag is never reused, moved or deleted: the fix gets the next tag, such as `2026.11.3.2` on the same day.

## How versions read

- **A release build** prints its tag: `eva-md version` says `eva-md 2026.11.3`.
- **A source build** prints the UTC day of its commit, then as build metadata the commit's UTC time and short hash, such as `2026.11.3+0915.9e4f609`, with `.dirty` for a build of a changed tree, or `dev` when Go recorded no commit, as for a test binary.

A release and a source build of the same commit print the same day.
