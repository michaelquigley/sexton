---
title: broken remote reports sync success
state: researching
created: 2026-08-14
tags: [defect]
milestone: v0.1.x
---

Decide "this repo has no remote" from configuration, not from git's error text. `git remote get-url <remote>` exits 2 when the remote is not configured and 0 when it is — so: exit 2 means commit-only mode is legitimate and the sync completes; exit 0 means the remote exists, and any pull or push failure is a real error that must be alerted, never a completed sync.

While in there, detect a rebase conflict by state rather than by message — `.git/rebase-merge` present, or unmerged entries in porcelain status — so a reworded git message still reaches `RebaseAbort` instead of leaving the rebase in place.

## why

`isNoRemoteOutput` in `internal/git/git.go` matches the phrase `does not appear to be a git repository`, and git emits that phrase identically for three different situations. Verified against git in a scratch repo:

- no remote configured at all — `fatal: 'origin' does not appear to be a git repository`
- a remote name that doesn't exist — `fatal: 'nosuch' does not appear to be a git repository`
- **a remote configured but whose path is gone, moved, or wrong** — `fatal: '/path/to/repo.git' does not appear to be a git repository`

The third is a serious failure and sexton reads it as the first. `ErrNoRemote` from either pull or push calls `completeSync`, which sets state to `watching`, stamps `lastSync`, and **clears the error detail** — so the repo reports a healthy sync, no alert fires, and any prior error is wiped. A fleet whose server-side repo path breaks would go on committing locally and reporting success indefinitely, with the operator believing the content is pushed.

## background

Data is not at risk in the conflict case, and the reason is worth knowing before touching this. If a conflict is ever missed by the message match, the rebase is left in place; on the next poll `validateBranch` runs `rev-parse --abbrev-ref HEAD`, which returns `HEAD` during a rebase, fails the configured-branch check, and errors the repo before anything is staged. Verified. That guard is the reason a missed conflict cannot commit conflict markers — do not weaken `validateBranch` without replacing it.

Adjacent, currently inert: the porcelain parser in `internal/git/status.go` has no case for unmerged codes, so `UU` falls through to `Modified` and `AA` matches the `A` case as `Added`. Nothing reaches that parser mid-conflict today because of the guard above, but a change to either would make it live.
