---
title: the sync loop
created: 2026-08-14
---

# the sync loop

One `agent.Agent` per configured repo, each with its own goroutine and its own `time.Ticker` at that repo's `poll_interval`. `agent.Container` is the `da.Run` target: it holds the agents plus the shared LLM client and alerter, injects them into each agent through `Wire`, and `da.Run` handles wire → start → signal wait → stop. There is no scheduler and no work queue — agents never coordinate, and a fleet's agents drift out of phase with each other by construction, which is load-bearing where holdout windows are concerned.

A sync runs immediately at `Start()`, before the ticker's first tick. After that the loop selects over four channels: stop, the ticker, a one-deep `syncCh` for requested syncs, and the snooze timer.

## states

`watching`, `syncing`, `error`, `snoozed`, `holdout`, `attention` (`internal/agent/state.go`). The state is guarded by the agent's mutex and read by both control planes. `attention` means local changes need an operator decision, not that sexton failed: polling and permitted sync work continue.

Every terminal path uses one resting-state chooser with complete precedence: active holdout, active request-time snooze, retained error, retained attention, then watching. Holdout and snooze can therefore mask an underlying error or attention state without clearing its detail; both status surfaces still show the highest-precedence retained detail. A snooze timer starts when the request arrives and is never restarted at a later phase boundary. If its duration elapses while a phase is blocked, the cycle falls through instead of manufacturing a fresh snooze.

## phase order

Every `sync()` cycle runs in exactly this order, and every step is followed by an abort check:

1. **validate branch** — the checked-out branch must equal the configured `branch`. A mismatch is an error, not a silent skip; sexton never switches branches.
2. **read and partition status** — raw status endpoints are selected by `commit_policy`: all endpoints under `all`, none under `none`, and configured directory prefixes under `regions`. Rename endpoints classify independently; a copy's old path is an unchanged source and is ignored. Any dirt at all still runs the unmerged-path guard before staging.
3. **commit selected changes** — `pre_commit` hook → policy-scoped stage → create a commit immediately with a minimal placeholder → capture its SHA → derive the LLM or mechanical message and file list from that exact object → compare-and-swap reword → `post_commit` hook. `all` stages and commits the whole tree; `regions` uses literal region pathspecs for both operations; `none` skips the block. The path-limited commit is the boundary, so unrelated index entries cannot enter it.
4. **evaluate attention** — unselected paths set or update the attention detail. Paths are raw-byte sorted, quoted, capped at ten with a `+N more` suffix, and say `(pulls paused)` when any are tracked. An unchanged detail alerts once; a changed set re-alerts; the first clean partition clears it and announces resolution.
5. **pull decision** — tracked unselected changes skip `git pull --rebase`, because rebasing a dirty tracked tree cannot succeed without forbidden autostash behavior. Untracked-only changes do not pause the pull. Dirt appearing after the partition can still produce `ErrDirtyWorkingTree`; that race ends the cycle without error and the next poll re-partitions it.
6. `git pull --rebase` → `post_pull` hook, **only when the pull actually moved HEAD**.
7. `pre_push` hook → `git push` → `post_sync` hook. Pushes are attempted even when attention paused the pull, so selected and operator-created commits continue moving when the remote accepts them.
8. read short HEAD and the author timestamp, then complete the sync. The status value is terminal HEAD; the file-bearing completion alert deliberately carries no SHA because a rebase or operator commit can make terminal HEAD differ from the commit its metadata describes.

`post_pull` firing only on real change is deliberate: the hook exists to react to incoming content, and on a quiet repo the pull is a no-op every poll interval. `git.Pull` reports movement by comparing HEAD before and after rather than parsing git's output, falling back to output matching only when the pre-read failed.

## interruption

Every phase boundary calls `shouldAbortSync`, which is two questions: has the run context been canceled (shutdown), and has a pause been requested. A `Snooze` arriving mid-sync cannot preempt the phase in flight — it sets `snoozePending`, and the next boundary applies it and drains any queued sync requests. This is why the agent can be stopped or snoozed in the middle of a long LLM call without leaving a half-run cycle behind: the cycle either completes or stops cleanly at a boundary, never mid-git-operation.

Context cancellation is distinguished from real failure everywhere. A canceled sync returns without setting an error state, and `clearCanceledSyncState` uses the resting-state chooser if it was left `syncing`.

## errors and recovery

`setError` records a detail string and resolves the displayed state through the shared precedence chooser, but **alerts only when the detail differs from the last one** — an agent failing the same way every 30 seconds produces one alert, not a stream. The repo keeps being retried on every subsequent poll; nothing has to be reset by hand.

Recovery is self-service and announced. `sync()` captures whether an error detail stood at cycle entry; a successful completion clears it and emits one `recovered from error` alert. Standing attention remains beneath the error and becomes the displayed state after recovery. `resume` can force an immediate retry but is not required for recovery.

Two failure shapes are not errors:

- **conflict** — `git.ErrConflict` aborts the rebase first, then errors. The working tree is never silently discarded; the operator resolves it.
- **no remote** — `git.ErrNoRemote` from either pull or push completes the sync normally with an empty SHA. A repo with no remote is a valid commit-only configuration, not a broken one.

## attention

Attention is deduplicated by its rendered detail just like errors are deduplicated by error detail, but `setAttention` never changes the displayed state mid-cycle. The agent stays `syncing` until it reaches a terminal boundary; this is what lets a snooze arriving in response to the alert stop the cycle before it touches the remote. At rest, error outranks attention, while holdout and snooze can mask either without clearing them.

Attention alerts use their own severity. The log sink renders them as warnings; Mattermost can mention configured users. Alert delivery is best-effort, but a sink failure is itself logged with the repo and undelivered message, so a Mattermost-only deployment never loses the last local evidence of a failed post.

## holdout windows

Daily local-time windows during which the agent refuses to touch the remote — the motivating case is a git host with a nightly maintenance restart. When any window is configured, a second goroutine (`runHoldoutScheduler`) sleeps to the next boundary and flips the state there, so entry and exit are punctual rather than waiting for a poll.

**Window exit deliberately does not trigger a sync.** Recovery is left to the next regular poll. This grants up to one `poll_interval` of grace for the remote to finish coming back, and across a fleet it staggers the retries by each agent's independent ticker phase instead of stampeding every agent at the same second. The immediate-sync-on-exit behavior is kept for `snooze` and `resume`, which are user-initiated and want responsiveness. The rationale is also recorded on `handleHoldoutTransition` itself, because the absence of a call is invisible to a reader.

## hooks

Five phases — `pre_commit`, `post_commit`, `post_pull`, `pre_push`, `post_sync` — each a list of shell commands run in order through `sh -c`. Hooks are control flow: a non-zero exit fails the phase, which errors the repo and stops the cycle.

Each hook runs in its own process group (`Setpgid`) and is killed by signalling the whole group, so a hook that spawns children can't leave orphans behind; `WaitDelay` bounds the wait after a kill. The environment carries `SEXTON_REPO_PATH`, `SEXTON_REPO_NAME`, and `SEXTON_HOOK`, plus any configured `env`. Working directory is the repo root unless the hook sets `dir`. A timeout produces a distinct error naming the command and the timeout; the default is 30s.
