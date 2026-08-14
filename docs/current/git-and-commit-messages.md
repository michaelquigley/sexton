---
title: git and commit messages
created: 2026-08-14
---

# git and commit messages

`internal/git` shells out to the `git` CLI rather than linking a git library. Every invocation runs with the repo root as its working directory, capturing stdout and stderr separately; on failure the returned string is stderr when there is any, stdout otherwise, because the error classifiers below read it. A canceled context is reported as the context error, never as a git failure.

The surface is small: `status --porcelain`, `add -A`, `commit -m`, `pull --rebase`, `push <remote> HEAD:<branch>`, `rebase --abort`, `rev-parse`, `log -1 --format=%aI`, and the two diff forms. `git.New` returns nil for a path that isn't a repository, which is how the container skips it.

## authentication

A per-repo `ssh_key` becomes `GIT_SSH_COMMAND=ssh -i <key> -o IdentitiesOnly=yes` on the git child process. `IdentitiesOnly` stops ssh offering agent keys ahead of the configured one; the key path is shell-quoted because git parses this variable with sh-style word splitting.

Two properties here are deliberate and load-bearing:

- **It is never written into the repo's git config.** Not `core.sshCommand`, not anything else — sexton does not mutate the repositories it syncs, and the key path stays in sexton's own YAML where the fleet's configuration lives.
- **The variable is appended last** to a copy of the process environment, so it wins over any ambient `GIT_SSH_COMMAND`. With no `ssh_key` configured the child's environment is left nil entirely and git inherits the ambient one — which is how the interactive `ssh-agent` path keeps working. Both branches are pinned by tests.

Host keys come from the user's `~/.ssh/known_hosts`. A fresh `systemctl --user` environment that has never connected to the host interactively will fail host verification until the host is known; sexton exposes no `StrictHostKeyChecking` or port options.

## error classification

git's exit codes don't distinguish the cases sexton needs, so `Pull` and `Push` classify by **matching against the command's output text** — `conflict`/`CONFLICT` for a rebase conflict, and a short list of phrases (`no remote`, `no configured push destination`, `no such remote`, `does not appear to be a git repository`) for a missing remote. Anything unmatched becomes a generic pull/push failure carrying the trimmed output.

This is string matching against another program's messages, and it is the most fragile seam in the codebase. The conflict case degrades safely: an unmatched conflict becomes a generic error, which still errors the repo and still gets retried, and the left-behind rebase is caught on the next poll by the branch check, since `rev-parse --abbrev-ref HEAD` reports `HEAD` during a rebase.

The no-remote case does not degrade safely, and the current behavior is a known defect. Git emits `does not appear to be a git repository` identically whether no remote is configured, the named remote doesn't exist, or the remote is configured but its path is gone — and `ErrNoRemote` completes the sync, stamping `lastSync` and clearing any error detail. A repo whose remote has broken therefore reports healthy syncs and raises no alert. See the `broken-remote-reports-sync-success` roadmap item.

`Pull` refuses to run against a dirty tree at all, returning `ErrDirtyWorkingTree` before invoking git. Whether a pull actually changed anything is decided by comparing HEAD before and after, not by reading git's output; the output check for "already up to date" is only the fallback for when the pre-read failed.

## status parsing

`status --porcelain -b -uall` is parsed into modified, added, deleted, and untracked lists plus branch, ahead, and behind. Renames report the destination path. Unrecognized status codes fall into modified rather than being dropped, so a file never disappears from the change summary.

## commit messages

The message for a dirty tree is generated per cycle:

1. `git diff --staged HEAD`. If it exceeds 32KB, `git diff --stat HEAD` is substituted — the summary, not a truncation.
2. The diff goes to the LLM as the user message, with the repo's `commit_message_prompt` as the system prompt.
3. The trimmed first choice is the commit message.

**Every failure in that chain degrades to the mechanical fallback** rather than failing the sync: no LLM configured, a diff that can't be read, an HTTP or API failure, or an empty completion. The fallback is `git.GenerateCommitMessage`, which counts the status lists into a `sexton: add 2 files, update 1 file` line. Only context cancellation propagates, because that means shutdown rather than a bad response.

The LLM client speaks OpenAI-compatible chat completions over plain `net/http` — a system message, a user message, `max_tokens`, and a bearer token when an API key env var is configured. Any non-200 is an error carrying the response body.
