# pebblewick

A to-do list kept as a list of dicts. `todo.main(argv, items, out)` runs one
command on the list and writes what it prints to out:

- `add <text> <due>` adds an open item due on day <due>, a whole number.
- `list` prints the open items by due day, as `<due> <text>`.
- `done <n>` marks the n-th item of list's order as done.
- `snooze <n> <days>` moves the n-th item of list's order <days> later.
- `sweepdone` removes the done items and prints how many it removed.
