---
title: broken remote reports sync success
state: researching
created: 2026-08-14
tags: [defect]
milestone: v0.1.x
---

Decide "this repo has no remote" from configuration, not from git's error text. `git remote get-url <remote>` exits 2 when the remote is not configured and 0 when it is — so: exit 2 means commit-only mode is legitimate and the sync completes; exit 0 means the remote exists, and any pull or push failure is a real error that must be alerted, never a completed sync.

The conflict half of this card is done (2026-08-14): staging now refuses a tree with unmerged paths, and the conflict branch aborts the rebase ahead of the cancellation check on its own context. What remains is the no-remote classifier described above.

## why

`isNoRemoteOutput` in `internal/git/git.go` matches the phrase `does not appear to be a git repository`, and git emits that phrase identically for three different situations. Verified against git in a scratch repo:

- no remote configured at all — `fatal: 'origin' does not appear to be a git repository`
- a remote name that doesn't exist — `fatal: 'nosuch' does not appear to be a git repository`
- **a remote configured but whose path is gone, moved, or wrong** — `fatal: '/path/to/repo.git' does not appear to be a git repository`

The third is a serious failure and sexton reads it as the first. `ErrNoRemote` from either pull or push calls `completeSync`, which sets state to `watching`, stamps `lastSync`, and **clears the error detail** — so the repo reports a healthy sync, no alert fires, and any prior error is wiped. A fleet whose server-side repo path breaks would go on committing locally and reporting success indefinitely, with the operator believing the content is pushed.

## background

This section originally claimed `validateBranch` was reason enough that a missed conflict could never commit conflict markers. That was wrong, and terminus found the hole on 2026-08-14: `validateBranch` only catches an interrupted *rebase*, where git detaches HEAD. A merge or cherry-pick conflict leaves the repo on its branch, passes the check, and `git add -A` was staging and committing the markers. Reproduced, then fixed by the unmerged-path check before staging. Two guards now, each covering a case the other misses — do not weaken either.

The porcelain parser in `internal/git/status.go` still has no case for unmerged codes: `UU` falls through to `Modified` and `AA` matches the `A` case as `Added`. This was previously recorded here as inert, which was only true under the mistaken belief above. It is inert now for a real reason — nothing reaches the parser mid-conflict because staging refuses first — but the parser remains wrong on its own terms and is worth its own card.
