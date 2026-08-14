---
title: pull-only repos
state: researching
created: 2026-08-13
tags: [feature, spike]
milestone: v0.1.x
---

there are repos we want to keep synchronized across environments, like `terminus-canon`... where we don't really want to have sexton auto-committing. in the cases where sexton notices local changes, it should probably squawk loudly about local changes, so that the operator can properly triage the commit.

maybe we can do something even smarter here? not sure.
