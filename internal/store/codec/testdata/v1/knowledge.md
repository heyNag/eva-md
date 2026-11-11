---
type: knowledge
title: Station ids are five digits
description: "Harbour station ids are five digits, zero-padded: 00412, not 412."
generated: { by: claude-code/model-a, at: "2026-11-06T10:02:33Z" }
status: stable
sources:
  - resource: "s:9c41aa07#14"
  - resource: "s:51d0e2b4#3"
  - resource: "code:internal/feed/station.go"
as_of: "2026-11-06"
kind: "fact"
project: "git.example.test:example-org/tide-tables"
scope: "project"
state: "active"
---

## Statement
Harbour station ids are five digits, zero-padded: 00412, not 412. [observed] s:9c41aa07#14 s:51d0e2b4#3 <!-- t:2026-11-06T09:58:12.25+01:00 -->

## History
- 2026-11-05: Station ids were read as numbers. s:51d0e2b4#3

Part of the [knowledge map](map.md).
