"""The shape of the real install, as its first field report found it.

An inbox of 600 session files in one area: one of 2 MB, a long chat, and
599 sharing 2.3 MB, most of them holding an end event and little else; 47
of the 600 have a recorded start. A wiki of 54 pages: 39 knowledge pages, 4
thread pages, a rules page, 4 system pages and 6 others (README.md, log.md,
the general area's now page, the project's now page and the two maps),
besides the index files. Every word is generated and made up.

There it times a chat start, a prompt and a capture, as the hooks run them,
and the brief built by command, which the report found at 0.92 s.
"""
from datetime import datetime, timedelta, timezone
import hashlib
import json
import os
import random
import subprocess
import time

from evabench import Home, context, nearest_rank, scenario

MIB = 1 << 20
FILES, STARTS = 600, 47
LARGEST, REST = 2*MIB, int(2.3*MIB)
WORDS = ("ledger settlement quartz walker index segment cursor brief thread fixture retry budget window marker digest "
         "schema rollup replay shard queue worker limit batch journal anchor beacon cobalt delta ember falcon garnet "
         "harbor island juniper kestrel lantern meadow nimbus orchid pebble quill raven saffron tundra umber velvet "
         "willow yarrow zephyr parser renderer validator watcher scheduler").split()


def sentence(rng, n):
    return " ".join(rng.choice(WORDS) for _ in range(n)).capitalize() + "."


def stamp(t):
    return t.astimezone(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")


def session_id(rng):
    return "%08x-%04x-%04x-%04x-%012x" % tuple(rng.getrandbits(b) for b in (32, 16, 16, 16, 48))


class Inbox:
    """Writes session files in the area, with the fields the hooks gave the
    seed chat's entries."""

    def __init__(self, area, template, rng):
        self.area, self.template, self.rng = area, template, rng

    def line(self, host, sid, t, role, kind, text):
        return (json.dumps(dict(self.template, ts=stamp(t), host=host, session=sid, role=role, kind=kind, text=text)) + "\n").encode()

    def write(self, host, sid, lines, t):
        path = self.area/("%s-%s.jsonl" % (host, sid))
        path.write_bytes(b"".join(lines)); path.chmod(0o600)
        os.utime(path, (t.timestamp(), t.timestamp()))
        return path.stat().st_size

    def end_only(self, host, t):
        sid = session_id(self.rng)
        return self.write(host, sid, [self.line(host, sid, t, "event", "event", "session_end")], t)

    def chat(self, host, end, size):
        """A chat of prompts, tool calls and replies, of at most size bytes,
        that ends at end."""
        rng, sid = self.rng, session_id(self.rng)
        # Seconds before the next line, and the line's role, kind and text.
        rows = [(0, "event", "event", "session_start: startup")]
        used = 2*len(self.line(host, sid, end, "event", "event", "session_start: startup"))
        while True:
            turn = [(rng.randint(60, 1200), "user", "prompt", sentence(rng, rng.randint(12, 40)))]
            for _ in range(rng.randint(1, 5)):
                w = rng.choice(WORDS)
                turn.append((rng.randint(2, 40), "tool", "tool", rng.choice((
                    "Bash go test ./internal/%s/... (exit 0)" % w, "apply_patch internal/%s/%s.go" % (w, rng.choice(WORDS)),
                    "Bash rg -n %s internal (exit 0)" % w))))
            turn.append((rng.randint(5, 90), "assistant", "reply", sentence(rng, rng.randint(30, 120))))
            more = sum(len(self.line(host, sid, end, role, kind, text)) for _, role, kind, text in turn)
            if used + more > size:
                break
            rows += turn
            used += more
        rows.append((rng.randint(5, 60), "event", "event", "session_end"))
        t = end - timedelta(seconds=sum(r[0] for r in rows))
        lines = []
        for seconds, role, kind, text in rows:
            t += timedelta(seconds=seconds)
            lines.append(self.line(host, sid, t, role, kind, text))
        return self.write(host, sid, lines, end)


def fill_inbox(area, template, rng, now):
    """The 600 files: the 2 MB chat, 46 more chats and 553 end events."""
    inbox = Inbox(area, template, rng)
    largest = inbox.chat("codex", now-timedelta(hours=1), LARGEST)
    when = lambda: now - timedelta(minutes=rng.randint(90, 30*24*60))
    rest = [inbox.end_only("codex" if i % 10 else "claude-code", when()) for i in range(FILES-STARTS)]
    chats = STARTS-1
    for i in range(chats):
        budget = (REST - sum(rest)) // (chats-i)
        rest.append(inbox.chat("claude-code" if i % 8 == 0 else "codex", when(), budget))
    if not (LARGEST-8192 <= largest <= LARGEST and abs(sum(rest)-REST) <= REST//100 and len(rest) == FILES-1):
        raise RuntimeError("the inbox is not the real install's shape")
    return largest, sum(rest)


def coined(rng):
    """A made-up name, such as a fact's title starts with."""
    return "".join(rng.choice("bdfgklmnprstvz") + rng.choice("aeiou") for _ in range(3)).capitalize()


def ref(rng):
    return "s:%08x#%d" % (rng.getrandbits(32), rng.randint(1, 400))


def item(rng, text, tag, k, t):
    return "- %s [%s] %s <!-- i:n%d t:%s -->" % (text, tag, ref(rng), k, t)


def thread_page(rng, slug, about, branch, head, now, summary, decisions, timeline_days):
    t = now.isoformat(timespec="seconds")
    sources = "".join('  - resource: "s:%08x"\n' % rng.getrandbits(32) for _ in range(20))
    front = ('---\ntype: thread\ntitle: "%s · %s"\ndescription: "Branch %s of %s"\ngenerated: { by: codex/model-x, at: "%s" }\n'
             'status: stable\nsources:\n%sas_of: "%s"\nbranch: "%s"\nhead: "%s"\nlocked: false\nmodel: "model-x"\nmodel_note: ""\n'
             'parent: ""\nproject: "%s"\nstate: "active"\nwriter: "eva-md/2026.11.7"\n---\n') % (
        slug, branch, branch, about, stamp(now), sources, t, branch, head, slug)
    k = iter(range(1, 10000))
    body = ["", "# %s · %s" % (slug, branch), "", "## Now", "<!-- eva:now:start -->", summary, "",
            "### Last chat", "%s · codex/model-x · %s" % (now.date(), sentence(rng, 14)), "", "### Done"]
    body += ["- %s [observed] %s" % (sentence(rng, 10), ref(rng)) for _ in range(6)]
    body += ["", "### Next"] + [item(rng, sentence(rng, 12), "stated", next(k), t) for _ in range(8)]
    body += ["", "### Open questions"] + [item(rng, sentence(rng, 10)[:-1]+"?", "stated", next(k), t) for _ in range(6)]
    body += ["", "### Decisions in force"] + [item(rng, sentence(rng, 14), "stated", next(k), t) for _ in range(decisions)]
    body += ["<!-- eva:now:end -->", "", "## Timeline"]
    for d in range(timeline_days, 0, -1):
        body.append("### %s" % (now-timedelta(days=d)).date())
        body += ["- %s [stated]: %s %s" % (rng.choice(("decision", "done", "finding", "change")), sentence(rng, 12), ref(rng)) for _ in range(6)]
    return front + "\n".join(body) + "\n"


def thread_name(branch):
    """threadpage.ThreadName, for the branches a checkout has."""
    if branch == "":
        return "default"
    s = "".join(c if c.isalnum() or c in "_-." else "-" for c in branch) or "thread"
    if s[0] in ".-":
        s = "_" + s
    if s.lower() in ("index", "log", "agents"):
        s += "-thread"
    return s if s == branch else s + "-" + hashlib.sha256(branch.encode()).hexdigest()[:6]


def write(path, text):
    path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    path.write_text(text); path.chmod(0o600)


def fill_wiki(wiki, key, branch, head, rng, now):
    """The 54 pages, their index files and the three indexes the hooks read."""
    if key.startswith("local-"):
        slug, remote = key[len("local-"):], ""
    elif "__" in key:
        slug, remote = key.split("__")[-1], key.replace("__", "/")
    else:
        slug, remote = key, ""
    day = now.date().isoformat()
    t = now.isoformat(timespec="seconds")
    write(wiki/"index.md", "# Knowledge Base\n\n* [README](README.md) - How to read this wiki\n* [General](general/index.md)\n"
          "* [Projects](projects/index.md)\n* [Systems](systems/index.md)\n* [Knowledge](knowledge/index.md)\n")
    write(wiki/"README.md", "---\ntype: guide\ntitle: README\ndescription: How to read this wiki\nstatus: stable\n---\n\n"
          "# eva.md wiki\n\nStart at [general/now.md](general/now.md).\n")
    write(wiki/"log.md", "".join("## %s\n- %s\n\n" % ((now-timedelta(days=d)).date(), sentence(rng, 8)) for d in range(30, 0, -1)))
    write(wiki/"general/index.md", "# General\n\n* [Now](now.md) - The general area\n* [Threads](threads/index.md)\n")
    write(wiki/"general/now.md", "---\ntype: now\ntitle: General\ndescription: The general area\nstatus: stable\n---\n\n"
          "## Threads\n* [default](threads/default.md) — No chats yet · last active %s\n" % day)
    write(wiki/"general/threads/index.md", "# Threads\n\n* [general · default](default.md) - Branch default of general\n")
    write(wiki/"general/threads/default.md", thread_page(rng, "general", "general", "default", "", now, "No chats yet.", 0, 0))
    # The project: its now page, rules and three threads, the checkout's
    # branch first.
    names = [branch] + [b for b in ("main", "search-index", "release-notes") if b != branch][:2]
    project = wiki/"projects"/slug
    write(wiki/"projects/index.md", "# Projects\n\n* [%s](%s/index.md) - %s\n" % (slug, slug, remote or key))
    write(project/"index.md", "# %s\n\n* [Now](now.md)\n* [Rules](rules.md)\n* [Threads](threads/index.md)\n" % slug)
    write(project/"now.md", ('---\ntype: now\ntitle: %s\ndescription: Threads of %s\ngenerated: { by: eva-md/2026.11.7 }\nstatus: stable\n'
                             'locked: false\nremote: "%s"\nwriter: "eva-md/2026.11.7"\n---\n\n## Rules\n* [Standing rules](rules.md)\n\n## Threads\n') %
          (slug, slug, remote) + "".join("* [%s](threads/%s.md) — %s · last active %s\n" % (b, thread_name(b), sentence(rng, 8), day) for b in names))
    write(project/"threads/index.md", "# Threads\n\n" + "".join("* [%s · %s](%s.md) - Branch %s\n" % (slug, b, thread_name(b), b) for b in names))
    for i, b in enumerate(names):
        summary = " ".join(sentence(rng, 12) for _ in range(4))
        write(project/"threads"/(thread_name(b)+".md"),
              thread_page(rng, slug, remote or key, b, head, now - timedelta(days=3*i), summary[:600], 30 if i == 0 else 12, 25 if i == 0 else 8))
    rules = ["- Rule %d: %s [stated] %s <!-- i:r%d t:%s -->" % (r, sentence(rng, 12), ref(rng), r, t) for r in range(1, 9)]
    write(project/"rules.md", '---\ntype: rules\ntitle: "%s · rules"\ndescription: Standing rules of %s\nstatus: stable\n---\n\n# %s · rules\n\n'
          '## Rules\n%s\n' % (slug, slug, slug, "\n".join(rules)))
    # Four system pages that list the project, so its brief points at them.
    systems, maplines, names_rows = [], [], []
    for i, (title, alias) in enumerate((("Inbox Walker", "inbox walk"), ("Search Segments", "segment store"),
                                        ("Brief Builder", "startup brief"), ("Update Worker", "update job"))):
        s = title.lower().replace(" ", "-")
        systems.append(s)
        write(wiki/"systems"/(s+".md"), ('---\ntype: system\ntitle: %s\ndescription: %s\nstatus: stable\nsources:\n'
                                         '  - resource: "https://example.com/docs/%s"\nas_of: "%s"\nstate: active\nrepos: ["%s"]\naliases: ["%s"]\n---\n\n'
                                         '## Summary\n%s\n\n## Interfaces\n%s\n\nPart of the [system map](map.md).\n') %
              (title, sentence(rng, 8)[:-1], s, day, key, alias, " ".join(sentence(rng, 14) for _ in range(3)), sentence(rng, 20)))
        maplines.append("- [%s](%s.md): %s · repos: %s · as of %s\n" % (title, s, sentence(rng, 8)[:-1], key, day))
        names_rows += ["%s\tsystems/%s\t%s\n" % (n, s, title) for n in (title, s, alias)]
    write(wiki/"systems/map.md", "---\ntype: map\ntitle: System map\n---\n\n# System map\n\n" + "".join(maplines))
    write(wiki/"systems/index.md", "# Systems\n\n* [System map](map.md)\n" + "".join("* [%s](%s.md)\n" % (s, s) for s in systems))
    write(wiki/".eva-md/names.tsv", "".join(sorted(names_rows)))
    # 39 knowledge pages: 12 facts, 10 decisions, 7 procedures and 10
    # constraints, 5 of them for every project and 2 citing code.
    kinds = ["fact"]*12 + ["decision"]*10 + ["procedure"]*7 + ["constraint"]*10
    rows, kmap = [], []
    for n, kind in enumerate(kinds, 1):
        scope = "all" if n % 8 == 0 else "project"
        title = "%s %s %s" % (coined(rng), rng.choice(WORDS), rng.choice(WORDS))
        statement = sentence(rng, 18)
        paths = ["internal/%s" % rng.choice(WORDS)] if n in (3, 17) else []
        code = "".join('  - resource: "%s@%s:%s"\n' % (key, head or "0"*12, p) for p in paths if head)
        write(wiki/"knowledge"/("k%d.md" % n), ('---\ntype: knowledge\ntitle: %s\ndescription: %s\ngenerated: { by: codex/model-x, at: "%s" }\n'
                                                'status: stable\nsources:\n  - resource: "%s"\n%sas_of: "%s"\nkind: "%s"\nproject: "%s"\nscope: "%s"\n'
                                                'state: "active"\n---\n\n## Statement\n%s [stated] %s <!-- t:%s -->\n\nPart of the [knowledge map](map.md).\n') %
              (title, statement, stamp(now), ref(rng), code, day, kind, key, scope, statement, ref(rng), t))
        rows.append("\t".join(["k%d" % n, kind, "stated", scope, key, day, t, "", title, statement, ",".join(paths)]) + "\n")
        kmap.append("- [%s](k%d.md): %s · %s\n" % (title, n, kind, statement))
    write(wiki/"knowledge/map.md", "---\ntype: map\ntitle: Knowledge map\n---\n\n# Knowledge map\n\n" + "".join(kmap))
    write(wiki/"knowledge/index.md", "# Knowledge\n\n* [Knowledge map](map.md)\n" + "".join("* [k%d](k%d.md)\n" % (n, n) for n in range(1, 40)))
    write(wiki/".eva-md/knowledge.tsv", "# eva-md knowledge index 1\n" + "".join(rows))
    pages = [p for p in wiki.rglob("*.md") if p.name != "index.md"]
    if len(pages) != 54:
        raise RuntimeError("the wiki has %d pages, not the real install's 54" % len(pages))
    # What a brief must hold to show it read the project's thread page, its
    # rules and its constraints.
    marks = ("projects/%s/threads/%s#Now" % (slug, thread_name(branch)), "Standing rules, the user's own words", "\nConstraint: ")
    return marks, [r.split("\t")[8] for r in rows], ["Inbox Walker", "segment store", "Brief Builder", "update job"]


@scenario(300, "real-install")
def real_install(run):
    rng = random.Random(600)
    now = datetime.now().astimezone()
    with Home(run.binary, run.cwd, prefix="eva-md-ri-") as h:
        # A seed chat gives the area and the fields the hooks write; its file
        # is then removed, so the area holds the 600 alone.
        h.hook("codex", "SessionStart", "ri-seed", source="startup")
        h.hook("codex", "UserPromptSubmit", "ri-seed", turn_id="0", prompt="Seed prompt for the shape of the real install.")
        seeds = list((h.state/"inbox").glob("*/codex-ri-seed.jsonl"))
        if len(seeds) != 1:
            raise RuntimeError("cwd is a temporary folder, which hooks skip, or capture failed")
        area = seeds[0].parent
        template = json.loads(seeds[0].read_bytes().splitlines()[0])
        seeds[0].unlink()
        largest, rest = fill_inbox(area, template, rng, now)
        head = subprocess.run(["git", "-C", str(run.cwd), "rev-parse", "HEAD~1"], capture_output=True, text=True, env=h.env)
        head = head.stdout.strip() if head.returncode == 0 else ""
        marks, titles, systems = fill_wiki(h.wiki(), template["project"], template.get("branch", ""), head, rng, now)
        run.put("real_install_inbox_files", len(list(area.glob("*.jsonl"))))
        run.put("real_install_inbox_largest_bytes", largest)
        run.put("real_install_inbox_rest_bytes", rest)
        run.put("real_install_wiki_pages", 54)
        # The day's first chat start asks for the sweep, untimed.
        h.hook("codex", "SessionStart", "ri-first", source="startup")
        h.take_sweep()
        starts = []
        for i in range(run.loops(20)):
            output, elapsed = h.hook("codex", "SessionStart", "ri-start-%d" % i, source="startup")
            if not all(m in context(output) for m in marks):
                raise RuntimeError("a chat start at the real install's shape did not brief the project's thread")
            starts.append(elapsed)
        h.hook("codex", "SessionStart", "ri-turns", source="startup")
        prompts, captures = [], []
        for i in range(50):
            prompt = ("How does the %s handle a retry, and what did we decide about %s?" % (systems[i % 4], titles[i % len(titles)].lower())
                      if i % 2 else "Continue with the next step: %s" % sentence(rng, 14))
            _, elapsed = h.hook("codex", "UserPromptSubmit", "ri-turns", turn_id=str(i), prompt=prompt)
            prompts.append(elapsed)
            _, elapsed = h.hook("codex", "Stop", "ri-turns", turn_id=str(i), last_assistant_message=sentence(rng, 40))
            captures.append(elapsed)
        briefs = []
        for _ in range(10):
            start = time.perf_counter()
            r = subprocess.run([str(run.binary), "brief", "--cwd", str(run.cwd), "--host", "codex"],
                               capture_output=True, text=True, env=h.env, timeout=30)
            briefs.append((time.perf_counter()-start)*1000)
            if r.returncode or not all(m in r.stdout for m in marks):
                raise RuntimeError("eva-md brief at the real install's shape failed")
        run.figure("real_install_startup", starts, target=300)
        run.figure("real_install_prompt", prompts, target=50)
        run.figure("real_install_capture", captures, target=50)
        run.put("real_install_brief_command_samples", len(briefs))
        run.put("real_install_brief_command_median_ms", round(nearest_rank(briefs, 0.5), 3))
        run.put("real_install_brief_command_p95_ms", round(nearest_rank(briefs), 3))
