"""pebblewick: a small to-do list, kept as a list of dicts."""


def open_items(items):
    """Return the open items by due day, in the order added on a tie."""
    return sorted((i for i in items if not i["done"]), key=lambda i: i["due"])


def cmd_add(items, out, text, due):
    items.append({"text": text, "due": int(due), "done": False})


def cmd_list(items, out):
    for item in open_items(items):
        out.write("%d %s\n" % (item["due"], item["text"]))


def cmd_done(items, out, n):
    open_items(items)[int(n) - 1]["done"] = True


def cmd_snooze(items, out, n, days):
    open_items(items)[int(n) - 1]["due"] += int(days)


def cmd_sweepdone(items, out):
    kept = [i for i in items if not i["done"]]
    out.write("%d\n" % (len(items) - len(kept)))
    items[:] = kept


COMMANDS = {"add": cmd_add, "list": cmd_list, "done": cmd_done,
            "snooze": cmd_snooze, "sweepdone": cmd_sweepdone}


def main(argv, items, out):
    """Run one command on items, writing what it prints to out.

    Return 0, or 2 for an unknown command.
    """
    if not argv or argv[0] not in COMMANDS:
        out.write("unknown command\n")
        return 2
    COMMANDS[argv[0]](items, out, *argv[1:])
    return 0
