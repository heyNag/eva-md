---
type: workstream
id: w4w1gxsy1dkz19
title: "Commit signing"
status: stable
state: active
state_at: "2026-11-09T15:02:11+01:00"
project: p_00gb3367a19r
branch: "feature/signing"
generated: {"by": "claude-code/claude-haiku-5", "at": "2026-11-09T15:02:11+01:00"}
sources:
  - {"id": "e1", "resource": "claude-code:5f0c9a1e-2b7d-4c1a-9e3f-0a1b2c3d4e5f#150", "pv": "agent", "supports": "suggested", "hash": "sha256:78226d5610d9b907b6d1bc3028227d4035255262528884582d634a97bab755f3"}
---
Machine-key signing is decided; generating the key at install is next.[^e1]

## Next steps
- Generate the key at install <!-- id:n01qtkccf8g9h0 -->
- Pass the key on each git call <!-- id:n01qtke8j1k2m3 -->

## Open questions
- Does the self-hosted server accept SSH signatures? <!-- id:q01qtkg4n4p5q6 -->

## Decisions in force
- [Sign store commits with a machine key](/projects/p_00gb3367a19r-acme-orders/items/2026/11/i01qtk73d4e5f6-sign-with-machine-key.md)
