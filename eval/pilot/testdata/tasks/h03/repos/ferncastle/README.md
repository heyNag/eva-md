# ferncastle

Parse reads a settings text, line by line:

- `key = value` sets key; white space around both is dropped.
- A line that starts with `#` is a comment, and a blank line is skipped.
- A line `[name]` opens a stanza: the keys after it, up to the next
  stanza, are stored as `name.key`.
- Setting a key twice in the same stanza fails with ErrTwiceSet.
- Any other line fails with ErrNoEquals.

Each failure is a *LineError that names the line, counted from 1.
