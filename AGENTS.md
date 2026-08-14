# AGENTS.md — sexton

sexton keeps local git repos in sync with their remotes: poll for changes, commit with an LLM-generated summary, push — marking the repo errored and retrying when it hits something it cannot resolve. Aimed at knowledge repositories and datasets, not code repos. It **polls** rather than watching the filesystem and **shells out to the `git` CLI** rather than linking a git library; both are assumed everywhere.

## How to arrive

1. **`docs/journal/`** — agent memory, dated entries, newest first. Read the recent ones before changing anything; write freely as you work. **Never** put project memory in your harness's own memory directory or tool, whatever it offers — the journal is the only durable home. Entries are prior-session context, not truth: where one conflicts with the code or `docs/current/`, the code wins. Spec: `software/conventions/agent-memory-convention.md` in the grimoire.
2. **`docs/current/`** — built behavior, and the authority on what exists: `sync-loop.md` (states, phase order, hooks, error and retry, holdout windows), `configuration.md` (the three-layer cascade, identity rules, holdout resolution), `git-and-commit-messages.md` (the git wrapper, ssh authentication, the LLM chain and its fallbacks), `control-plane.md` (the socket, the CLI, repo resolution), `mattermost.md` (chat-ops and alerting).
3. **`docs/future/roadmap/`** — the roadmap, per **Roadmap** below. `docs/future/semantic-differs.md` is the one live design document.
4. **`README.md`** — the user-facing configuration and usage reference.

## The code

- **`cmd/sexton/`** — cobra entrypoint. `agent.go` starts the daemon and holds both control-plane adapters; `status.go`, `sync.go`, `snooze.go`, `resume.go` are the client subcommands; `client.go` is the shared dial helper.
- **`internal/agent/`** — the heart. `agent.go` is the per-repo sync loop, `container.go` the `da.Run` target holding the agents and shared LLM/alerter, `hooks.go` the lifecycle hook runner, `state.go` the state enum, `alerter.go` the alert interface and fan-out, `controller.go` repo resolution.
- **`internal/config/`** — `model.go` for the config and resolved types, `load.go` for the cascade and path expansion, `holdout.go` for window resolution.
- **`internal/git/`** — the git CLI wrapper: `git.go` commands and ssh, `errors.go` sentinels, `status.go` porcelain parsing, `message.go` the fallback commit message.
- **`internal/llm/`** — the OpenAI-compatible chat completions client.
- **`internal/mattermost/`** — REST and websocket client, command dispatch, alerter, response formatting.
- **`internal/rpc/`** — the gRPC server over the Unix socket; `controller.go` declares the interface `cmd/sexton` satisfies, which is what keeps `internal/agent` free of any transport import.
- **`internal/format/`** — relative-time display helper.
- **`api/v1/`** — `sexton.proto` and its generated stubs; regenerate rather than edit.

## Posture

Pre-1.0 personal infrastructure, single supervised implementer; breaking changes are fine, and simple beats defensive where both are correct. sexton runs unattended — headless under `systemctl --user` across a small fleet — which makes silent success the worst failure mode: error a repo rather than assume a benign explanation. The synced repos are the user's data, so losing a working tree is unacceptable where sitting in `error` for a week is not.

## Load-bearing rules

Violating one of these is a review finding, not a style note.

- **Never mutate a synced repo's configuration.** A per-repo `ssh_key` becomes `GIT_SSH_COMMAND` on the git child process, deliberately not `git config core.sshCommand` — sexton commits to these repositories, it never reconfigures them.
- **`GIT_SSH_COMMAND` is appended last** so it beats any ambient value; with no `ssh_key`, `cmd.Env` stays nil so git inherits the user's `ssh-agent`. Both paths are pinned in `git_test.go`.
- **Never discard a working tree.** A conflict aborts the rebase and errors the repo; resolution is the operator's. No `reset --hard`, no force push, no stash-and-drop.
- **`validateBranch` is a guard.** `rev-parse --abbrev-ref HEAD` returns `HEAD` mid-rebase, which is what stops a left-behind rebase being staged and committed on the next poll (verified 2026-08-14). Don't weaken it without replacing what it catches.
- **Config binds through `df/dd`** — `dd:` struct tags, never `yaml:`. A `yaml:` tag and a misspelled key are both silently ignored, so the feature just never turns on.

## Process

- **Scope** — ad-hoc improvement is the default. Anything architectural earns a spec in `docs/future/` and a mercurius review arc before implementation; `mercurius.yaml` is in the repo root but is still the unedited stock template, so calibrate it before opening a session rather than trusting its `review_context`. The roadmap is where that judgment gets made.
- **Terminus** gates substantive changes before Michael reviews them. sexton has no rubric in `terminus-canon/projects/` yet, so `terminus review --quality <ref>` against ad-hoc canon refs is the only path and it is advisory; onboarding is on the roadmap.
- **Changelog** — an entry under `## Unreleased` as behavior lands: `FEATURE`/`CHANGE`/`FIX` prose, in-house format, not Keep a Changelog. Working rule: what ships in the binary gets an entry, build tooling and documentation work does not.
- **Docs** — synthesize built behavior into `docs/current/` as it lands. Run `unfurl -i <file>` on any markdown you author or edit, unconditionally.
- **Go** — `df/dl` logging, `df/dd` binding, `df/da` lifecycle. Files are `dashManager.go` and `dashManager_test.go`, never snake_case. Comments start lowercase unless the first word is a capitalized Go type. Output prefers lowercase with dynamic values in single quotes: `selected the 'value' setting`.
- **House** — no emoji; mermaid rather than ASCII art; leave no build artifacts in the tree.
- **Build** — `make build` installs, `make test` runs tests and `go vet`, `make push` stages the binary into the depot. After editing `api/v1/sexton.proto`:

```bash
protoc --go_out=. --go_opt=paths=source_relative \
       --go-grpc_out=. --go-grpc_opt=paths=source_relative \
       api/v1/sexton.proto
```

## Roadmap

This repo's roadmap lives in `docs/future/roadmap/` — one frontmatter-markdown item per file, per the roadmap convention in the grimoire (software/conventions/roadmap-convention.md). You may add items freely: write the file directly with required `title`, `state: inbox`, and `created:` (today, YYYY-MM-DD), optional `tags`/`source`/`log`, and a body that is a small, clear prompt -- the problem or solution to execute, not documentation of it; trust the code and the day's journal entry for what's discoverable, and point a `log:` stamp at the specific journal entry when a card leans on hard-won context. Everything above the first `##` heading is the prompt; supporting material that isn't the prompt goes in named sections below it (`## why` for justification, `## background` for a longer description), which are conventional, never required, and never validated. The filename is the slug of the title (lowercase ASCII, hyphens; discard every other character); never overwrite an existing file. Read sibling items for the shape.

Hard rules: never touch `order.yaml` (priority is the operator's judgment, set at triage); never commit roadmap changes unless directed — the uncommitted diff is the review queue; never delete items; edits change only the lines that express them. Label the kind from the house set when one fits: defect, documentation, enhancement, epic, feature, story; add `spike` alongside it when the work carries unknowns that need discovery.
