---
title: rewrite AGENTS.md as orientation
state: researching
created: 2026-08-13
tags: [documentation]
milestone: v0.1.x
---

Cut `AGENTS.md` down to the shape ranger uses — a short orientation file, not a manual. Four sections: **how to arrive** (numbered pointers at `docs/journal/`, `docs/current/`, `docs/future/roadmap/`, in that order), **posture** (what kind of project this is and what defensiveness has already been vetoed), **load-bearing rules** (the invariants whose violation is an automatic review finding), **process** (terminus gate, `## Unreleased` changelog entry as behavior lands, `unfurl -i` unconditionally on authored markdown).

Two rules to promote into load-bearing while doing it, both currently reachable only from `docs/journal/2026-06-19.md`: `ssh_key` is injected as `GIT_SSH_COMMAND` on the git child process and deliberately never written into a synced repo's git config (sexton stays non-invasive), and `runCtx` appends the env var last so it wins over the ambient value — both paths are pinned by tests in `git_test.go`.

Runs after `install-the-docs-split` — the architecture material can't leave this file until `docs/current/` exists to receive it.

## why

The file is 170 lines and most of the bulk belongs elsewhere: the package tree and the seven design decisions are `docs/current/` content, and the journal convention is inlined in full where ranger spends one line on it. An orientation file that long stops being read, which is how load-bearing invariants end up surviving only in a June journal entry.
