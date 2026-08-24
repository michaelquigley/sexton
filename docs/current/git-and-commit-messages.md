---
title: git and commit messages
created: 2026-08-14
---

# git and commit messages

`internal/git` shells out to the `git` CLI rather than linking a git library. Every invocation runs with the repo root as its working directory, capturing stdout and stderr separately; on failure the returned string is stderr when there is any, stdout otherwise, because the error classifiers below read it. A canceled context is reported as the context error, never as a git failure.

The surface is small: NUL-delimited porcelain status, whole-tree or literal-region `add`, whole-tree or path-limited `commit`, by-object `show`, `commit-tree` plus compare-and-swap `update-ref`, `pull --rebase`, `push <remote> HEAD:<branch>`, `rebase --abort`, `rev-parse`, and `log -1 --format=%aI`. `git.New` returns nil for a path that isn't a repository, which is how the container skips it.

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

`Pull` refuses to run against tracked dirt, returning `ErrDirtyWorkingTree` before invoking git. Untracked-only dirt is allowed because it does not inherently prevent a rebase; an incoming collision still fails through git. Whether a pull actually changed anything is decided by comparing HEAD before and after, not by reading git's output; the output check for "already up to date" is only the fallback for when the pre-read failed.

## status parsing

`status --porcelain=v1 -z -b -uall` is parsed into modified, added, deleted, and untracked display lists plus branch, ahead, behind, and raw entries carrying both index/worktree codes and rename/copy paths. Raw paths are the policy-partition input; display paths escape control-bearing and invalid UTF-8 names before they leave the git layer. Rename endpoints both count as changed paths, while a copy's old path is an unchanged source. Unrecognized status codes fall into modified rather than being dropped, so a file never disappears from the change summary.

## commit messages

The message for selected changes is generated from the created commit, not from the index or working tree:

1. Sexton stages the selected scope and commits it immediately with the count-free placeholder `sexton: pending summary`. The commit helper parses git's own success line, resolves the created object id without consulting mutable HEAD, and verifies that object's message.
2. `ShowNameStatus(sha)` supplies exact file metadata and the mechanical fallback. `Show(sha)` supplies the patch; if it exceeds 32KB, `ShowStat(sha)` is substituted — the summary, not a truncation.
3. The diff goes to the LLM as the user message, with the repo's `commit_message_prompt` as the system prompt. The trimmed first choice is the final message.
4. `RewordCommit` rebuilds the commit with the same tree, parents, author identity, and author time, then swaps the configured branch through `update-ref <new> <old>`. If another writer moved the branch, the swap refuses rather than rewording someone else's commit.

**Every message-generation failure degrades to the mechanical fallback** rather than failing the sync: no LLM configured, a commit diff that can't be read, an HTTP or API failure, or an empty completion. The fallback is `git.GenerateCommitMessage`, which counts the exact commit's status lists into a `sexton: add 2 files, update 1 file` line. Only context cancellation propagates from generation, because that means shutdown rather than a bad response. Commit creation, exact-object metadata reads, and the compare-and-swap reword are integrity boundaries and do fail the sync; a crash between creation and reword leaves the deliberately spartan placeholder.

The LLM client speaks OpenAI-compatible chat completions over plain `net/http` — a system message, a user message, `max_tokens`, and a bearer token when an API key is configured, from the env var named by `api_key_env` or directly in `api_key`. Any non-200 is an error carrying the response body.
