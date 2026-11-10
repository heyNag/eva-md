---
type: constraint
id: i01qcsw8g7h8j9
title: "Never push to main without the checks"
status: stable
state: active
state_at: "2026-11-04T09:30:00+01:00"
scope: project
project: p_00gb3367a19r
tag: stated
words: "never push to main unless make check passed"
generated: {"by": "claude-code/claude-haiku-5", "at": "2026-11-04T09:30:00+01:00"}
learned: "2026-11-04T09:28:05+01:00"
asserted_at: "2026-11-04T09:28:05+01:00"
sources:
  - {"id": "e1", "resource": "claude-code:7a8b9c0d-1e2f-4a3b-8c4d-5e6f7a8b9c0d#60", "pv": "typed", "supports": "stated", "hash": "sha256:0bb2a8b92f10baa1f4a5e67a9faae24d453635b80fe5daeab5f06a9d82f30602"}
locked: true  # keep this exactly as it is
---
Nothing is pushed to main unless `make check` passed.[^e1]

## Evidence
[^e1]: claude-code:7a8b9c0d-1e2f-4a3b-8c4d-5e6f7a8b9c0d#60
