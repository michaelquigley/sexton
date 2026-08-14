---
title: semantic differs
state: researching
created: 2026-08-13
tags: [feature, spike]
milestone: v0.1.x
log:
  - stamp: 2026-08-13
    note: spec drawn — docs/future/semantic-differs.md
---

Feed the commit-message LLM domain-aware descriptions instead of raw diffs. Per-file-type external differ commands, selected by glob and invoked as `<command> <old> <new>` with `/dev/null` standing in for the missing side of an add or delete. Non-zero exit, timeout, or empty output falls back to that file's raw diff — a differ is informational, never load-bearing for the sync, which is the line that separates it from a lifecycle hook. Semantic descriptions claim the token budget ahead of raw diffs when both compete for it.

The spec is `docs/future/semantic-differs.md`. Its design half still holds; its implementation plan describes the March-era codebase and needs re-grounding against current internals before it becomes a work order.

## why

Raw diffs of structured data are semantically opaque — a wall of changed JSON keys tells the LLM almost nothing, so the message ends up describing the file rather than the change. sexton is aimed at knowledge repos and datasets, which is exactly where that bites.
