# CHANGELOG

## Unreleased

FIX: A repository left with an unresolved merge or cherry-pick conflict is no longer staged, committed, and pushed with its conflict markers intact. The configured-branch check catches an interrupted *rebase*, because git detaches HEAD for one — but a merge or cherry-pick conflict leaves the repository on its branch, so it passed that check and `git add -A` swept up the conflict-marked files. Sexton now refuses to stage a tree with unmerged paths, entering the error state naming them and retrying every poll until the conflict is resolved by hand.

FIX: A shutdown arriving while a `git pull --rebase` is conflicting no longer leaves the repository mid-rebase. The conflict is now handled before the cancellation check, and the `rebase --abort` that cleans it up runs on its own bounded context rather than the sync's, which by then may already be canceled. An abort that itself fails is reported in the repo's error detail instead of being discarded, so the error no longer claims a rebase was aborted when it was not.

CHANGE: The `sexton status` table now prints its column headings in lowercase, matching the lowercase headings the Mattermost `status` response has always used. The same table rendered through the two control planes previously disagreed on case.

## v0.1.1

FEATURE: New per-repo `ssh_key` config points git at a specific private key via `GIT_SSH_COMMAND` (with `IdentitiesOnly=yes`), so sexton can sync SSH remotes without a running `ssh-agent` — enabling headless operation under a `systemctl --user` service. The key must be passphrase-less.

## v0.1.0

FEATURE: New `sexton version` subcommand reports build metadata — version, commit, build date, and branch — using the `github.com/michaelquigley/push` build package, with release binaries stamped via goreleaser. The running build is also surfaced in the agent startup log and in the Mattermost `status` output, so it is easy to confirm which build each agent across a fleet is running.

FIX: When a holdout window ends, the agent no longer fires an immediate sync. A holdout window typically guards a known-bad period such as a remote's nightly maintenance restart, so syncing the instant the window lifts tended to reach a remote that was still recovering — and across a fleet of agents it produced a synchronized burst of `git` failures at the exact boundary second. Recovery is now left to the next regular poll, which grants up to one `poll_interval` of grace for the remote to come back and naturally staggers retries across agents by their independent poll phases. The immediate sync on exit is intentionally retained for `snooze` and `resume`, which are user-initiated.