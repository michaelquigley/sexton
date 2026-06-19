# CHANGELOG

## Unreleased

FEATURE: New per-repo `ssh_key` config points git at a specific private key via `GIT_SSH_COMMAND` (with `IdentitiesOnly=yes`), so sexton can sync SSH remotes without a running `ssh-agent` — enabling headless operation under a `systemctl --user` service. The key must be passphrase-less.

## v0.1.0

FEATURE: New `sexton version` subcommand reports build metadata — version, commit, build date, and branch — using the `github.com/michaelquigley/push` build package, with release binaries stamped via goreleaser. The running build is also surfaced in the agent startup log and in the Mattermost `status` output, so it is easy to confirm which build each agent across a fleet is running.

FIX: When a holdout window ends, the agent no longer fires an immediate sync. A holdout window typically guards a known-bad period such as a remote's nightly maintenance restart, so syncing the instant the window lifts tended to reach a remote that was still recovering — and across a fleet of agents it produced a synchronized burst of `git` failures at the exact boundary second. Recovery is now left to the next regular poll, which grants up to one `poll_interval` of grace for the remote to come back and naturally staggers retries across agents by their independent poll phases. The immediate sync on exit is intentionally retained for `snooze` and `resume`, which are user-initiated.