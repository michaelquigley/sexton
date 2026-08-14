---
title: the sync loop
created: 2026-08-14
---

# the sync loop

One `agent.Agent` per configured repo, each with its own goroutine and its own `time.Ticker` at that repo's `poll_interval`. `agent.Container` is the `da.Run` target: it holds the agents plus the shared LLM client and alerter, injects them into each agent through `Wire`, and `da.Run` handles wire → start → signal wait → stop. There is no scheduler and no work queue — agents never coordinate, and a fleet's agents drift out of phase with each other by construction, which is load-bearing where holdout windows are concerned.

A sync runs immediately at `Start()`, before the ticker's first tick. After that the loop selects over four channels: stop, the ticker, a one-deep `syncCh` for requested syncs, and the snooze timer.

## states

`watching`, `syncing`, `error`, `snoozed`, `holdout` (`internal/agent/state.go`). The state is guarded by the agent's mutex and read by both control planes.

Precedence is not symmetric. Holdout outranks everything: entering a window moves the agent to `holdout` regardless of what it was, `TriggerSync` and `Resume` refuse while a window is active, and even `setError` during a window records the error detail but leaves the state `holdout`. Snooze outranks error — a snooze expiring on an agent that still has an error detail returns to `error`, not `watching`.

## phase order

The dirty branch of `sync()` runs in exactly this order, and every step is followed by an abort check:

1. **validate branch** — the checked-out branch must equal the configured `branch`. A mismatch is an error, not a silent skip; sexton never switches branches.
2. **is the tree dirty** — clean trees skip straight to the pull.
3. `pre_commit` hook → `git add -A` → read status → generate the commit message → `git commit` → `post_commit` hook.
4. `git pull --rebase` → `post_pull` hook, **only when the pull actually moved HEAD**.
5. `pre_push` hook → `git push` → `post_sync` hook.
6. read short HEAD and the author timestamp, then complete the sync.

`post_pull` firing only on real change is deliberate: the hook exists to react to incoming content, and on a quiet repo the pull is a no-op every poll interval. `git.Pull` reports movement by comparing HEAD before and after rather than parsing git's output, falling back to output matching only when the pre-read failed.

## interruption

Every phase boundary calls `shouldAbortSync`, which is two questions: has the run context been canceled (shutdown), and has a pause been requested. A `Snooze` arriving mid-sync cannot preempt the phase in flight — it sets `snoozePending`, and the next boundary applies it and drains any queued sync requests. This is why the agent can be stopped or snoozed in the middle of a long LLM call without leaving a half-run cycle behind: the cycle either completes or stops cleanly at a boundary, never mid-git-operation.

Context cancellation is distinguished from real failure everywhere. A canceled sync returns without setting an error state, and `clearCanceledSyncState` puts the agent back to `watching` if it was left `syncing`.

## errors and recovery

`setError` moves the agent to `error` and records a detail string, but **alerts only when the detail differs from the last one** — an agent failing the same way every 30 seconds produces one alert, not a stream. The repo keeps being retried on every subsequent poll; nothing has to be reset by hand.

Recovery is self-service and announced. `completeSync` clears the error detail, and `sync()` captures `wasError` before calling it so a successful cycle after a failure emits a `recovered from error` alert. `resume` can force an immediate retry but is not required for recovery.

Two failure shapes are not errors:

- **conflict** — `git.ErrConflict` aborts the rebase first, then errors. The working tree is never silently discarded; the operator resolves it.
- **no remote** — `git.ErrNoRemote` from either pull or push completes the sync normally with an empty SHA. A repo with no remote is a valid commit-only configuration, not a broken one.

## holdout windows

Daily local-time windows during which the agent refuses to touch the remote — the motivating case is a git host with a nightly maintenance restart. When any window is configured, a second goroutine (`runHoldoutScheduler`) sleeps to the next boundary and flips the state there, so entry and exit are punctual rather than waiting for a poll.

**Window exit deliberately does not trigger a sync.** Recovery is left to the next regular poll. This grants up to one `poll_interval` of grace for the remote to finish coming back, and across a fleet it staggers the retries by each agent's independent ticker phase instead of stampeding every agent at the same second. The immediate-sync-on-exit behavior is kept for `snooze` and `resume`, which are user-initiated and want responsiveness. The rationale is also recorded on `handleHoldoutTransition` itself, because the absence of a call is invisible to a reader.

## hooks

Five phases — `pre_commit`, `post_commit`, `post_pull`, `pre_push`, `post_sync` — each a list of shell commands run in order through `sh -c`. Hooks are control flow: a non-zero exit fails the phase, which errors the repo and stops the cycle.

Each hook runs in its own process group (`Setpgid`) and is killed by signalling the whole group, so a hook that spawns children can't leave orphans behind; `WaitDelay` bounds the wait after a kill. The environment carries `SEXTON_REPO_PATH`, `SEXTON_REPO_NAME`, and `SEXTON_HOOK`, plus any configured `env`. Working directory is the repo root unless the hook sets `dir`. A timeout produces a distinct error naming the command and the timeout; the default is 30s.
