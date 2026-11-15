# go.mod names the newest patch of its Go

- **Status:** Accepted
- **Date:** 2026-11-15

## Context

`go.mod` named Go 1.26.0, and CI and the release workflow install exactly what it names. On 2026-11-15 that was eleven patch releases behind 1.26.11.
Several of those releases carry security fixes to packages eva-md's binary holds, `crypto/tls`, `crypto/x509`, `net/http` and `os` among them, which `eva-md upgrade` uses to fetch a release.
One carries the fix for a fault that failed CI's macOS job four times: from 1.26.0 to 1.26.4 the race detector on darwin/arm64 at times crashes a child between fork and exec (golang/go#79804, fixed in 1.26.5).

## Decision

`go.mod` names Go 1.26.11, the newest patch of its line on that day, and every job of CI and the release workflow run it. The owner agreed on 2026-11-15, at the lead's recommendation.
Before each release, whoever prepares it looks for a newer patch of the same line and proposes it. A move to a new line, such as 1.27, is a decision of its own.

## Consequences

A release is built with a Go that has the fixes of its line.
Building from source needs Go 1.26.11 or newer, or an older Go that may fetch it.
The macOS job's own later patch, set earlier the same day for the race detector's fault, is gone again: `go.mod`'s version is past the fix, and must stay at 1.26.5 or later.

## Reopens

One line of `AGENTS.md`, which named `go 1.26.0`.

## Reopened by

None.
