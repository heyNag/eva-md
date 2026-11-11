---
type: system
title: Tide feed
description: Reads harbour stations into tide tables
generated: { by: claude-code/model-a, at: "2026-11-08T07:00:00Z" }
status: stable
stale_after: 2027-02-01
sources:
  - resource: "s:9c41aa07#30"
  - resource: "code:git.example.test:example-org/tide-tables@4e2bf4c:internal/feed"
aliases: ["tide feed","harbour feed","Gezeiten-Feed"]
as_of: "2026-11-08"
repos: ["git.example.test:example-org/tide-tables","git.example.test:example-org/feed-kit"]
state: "active"
---

# Tide feed

## What it is
Reads harbour stations into tide tables, every 10 minutes.

## How it fits
The feed kit parses; the tables service stores. See [Billing stub](billing-stub.md) for the fake ledger.

Part of the [system map](map.md).
