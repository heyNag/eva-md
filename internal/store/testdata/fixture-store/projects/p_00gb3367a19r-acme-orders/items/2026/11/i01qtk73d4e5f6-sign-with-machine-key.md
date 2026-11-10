---
type: decision
id: i01qtk73d4e5f6
title: "Sign store commits with a machine key"
description: "Store commits are signed with a per-machine SSH key, never the person's."
status: stable
state: active
state_at: "2026-11-09T15:02:11+01:00"
scope: here
project: p_00gb3367a19r
workstream: w4w1gxsy1dkz19
tag: accepted
generated: {"by": "claude-code/claude-haiku-5", "at": "2026-11-09T15:02:11+01:00"}
learned: "2026-11-09T14:58:40+01:00"
asserted_at: "2026-11-09T14:59:02+01:00"
supersedes: ["i01q7krma1b2c3"]
branch: "feature/signing"
anchors: ["github.com/acme/orders@4e2bf4c:internal/store/sign.go"]
cues: ["commit signing prompt", "machine key"]
sources:
  - {"id": "e1", "resource": "claude-code:5f0c9a1e-2b7d-4c1a-9e3f-0a1b2c3d4e5f#142", "pv": "agent", "supports": "suggested", "hash": "sha256:35b555288e03bad67cd145efc0725544c1d70562fa8822aa9b409e1a64779108"}
  - {"id": "e2", "resource": "claude-code:5f0c9a1e-2b7d-4c1a-9e3f-0a1b2c3d4e5f#143", "pv": "chosen", "supports": "accepted", "hash": "sha256:99eccb96ff42f9dd1866a5b10dcb8b3ca6fe1d0ff52021ff55fc86ed9467bb9b"}
---
Store commits are signed with a per-machine SSH key, never the person's.[^e1][^e2]

## Detail
Reason: the daily approval prompt. Rejected: the person's key with a cached approval.

## Evidence
[^e1]: claude-code:5f0c9a1e-2b7d-4c1a-9e3f-0a1b2c3d4e5f#142
[^e2]: claude-code:5f0c9a1e-2b7d-4c1a-9e3f-0a1b2c3d4e5f#143
