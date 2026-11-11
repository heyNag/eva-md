---
# A person's note above every key.
type: fact
id: i01qa78gk3m9pa
title: "The orders service listens on port 8443" # checked on 3 November
  # an indented comment under the title
status: stable

# a blank line above, and this comment, belong to no key
state: active   # trailing comment after a plain value
cues: ["port", "orders"]  # and after JSON
sources:
  # a comment inside a block list
  - {"id": "e1", "resource": "claude-code:7a8b9c0d#31", "pv": "tool", "supports": "observed", "hash": "sha256:f6b28acd05c2aa9ab36778da1c8ebe7175c52eb54b4da50d0fabcd5f11bbb767"}
# the last comment
---
The orders service listens on port 8443.[^e1]

<!-- a comment in the body -->

## Evidence
[^e1]: claude-code:7a8b9c0d#31
