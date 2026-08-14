# Sexton

Git-based repository synchronization agent. Keeps local git repos in sync with their remotes by polling for changes, committing with LLM-generated summaries, and pushing — marking a repo errored and retrying when it hits a conflict or failure it cannot immediately resolve.

Designed for knowledge repositories and datasets (markdown collections, config stores, structured data), not code repos.

## Architecture

Built behavior lives in `docs/current/`, and that is the authority on what exists:

- `sync-loop.md` — the per-repo agent: states, phase order, hooks, error and retry, holdout windows.
- `configuration.md` — the three-layer cascade, identity rules, holdout resolution.
- `git-and-commit-messages.md` — the git CLI wrapper, ssh authentication, the LLM summarization chain and its fallbacks.
- `control-plane.md` — the gRPC socket, the CLI, repo resolution, the dependency inversion the adapters exist for.
- `mattermost.md` — chat-ops control and alerting.

Two decisions worth knowing before reading any of it: sexton **polls** rather than watching the filesystem, and it **shells out to the `git` CLI** rather than linking a git library. Both are simplicity choices, and both are assumed everywhere.

## Tech Stack

- **Language**: Go
- **CLI**: github.com/spf13/cobra
- **Config**: github.com/michaelquigley/df/dd (YAML binding)
- **Lifecycle**: github.com/michaelquigley/df/da (container + start/stop)
- **Logging**: github.com/michaelquigley/df/dl
- **gRPC**: google.golang.org/grpc (control plane over Unix socket)
- **Protobuf**: google.golang.org/protobuf
- **Mattermost**: github.com/gorilla/websocket (chat-ops control + alerting over websocket)
- **Build metadata**: github.com/michaelquigley/push (`version` subcommand, release build stamping)
- **Git**: shells out to `git` CLI

## Development Commands

```bash
# build (or `make build`, which runs `go install ./...`)
go build ./cmd/sexton

# run
sexton agent --config path/to/config.yaml

# test (or `make test`, which also runs `go vet ./...`)
go test ./...

# tidy
go mod tidy

# regenerate gRPC/protobuf code
protoc --go_out=. --go_opt=paths=source_relative \
       --go-grpc_out=. --go-grpc_opt=paths=source_relative \
       api/v1/sexton.proto
```

## Configuration

`docs/current/configuration.md` describes the cascade and the resolution rules; `README.md` is the user-facing field reference. The one rule to carry into any config change: these types bind through `df/dd`, so they use `dd:` struct tags (e.g. `dd:",+required"`) and **never** `yaml:` tags — a `yaml:` tag is silently ignored, and so is a misspelled key.

## Design documents

Forward-looking design — specs, visions, work orders — lives in `docs/future/`. Descriptions of behavior that is actually built belong in `docs/current/`. The original sexton spec lived in the grimoire's `software/sexton/` tree; that tree was dissolved into the project repos on 2026-07-06, and what survived of it is `docs/future/semantic-differs.md`.

## Project memory

Durable knowledge about this project lives in `docs/journal/`, dated files `docs/journal/YYYY-MM-DD.md`. This is project memory; it does not go in harness-local storage (`.claude/` or equivalent), where it's invisible to every other harness and collaborator and dies with the host. Concretely: do not write to your harness's memory directory or memory tool for this project — even when the harness presents it as the default place for durable knowledge. That tool is the silo this convention exists to replace; the journal is the only durable home.

On arrival, read the most recent entries to pick up where the last session left off, before you start changing things. Treat them as prior-session context, not verified truth — if an entry conflicts with the code or a `docs/current/` doc, the code wins.

Write the smallest entry that carries the session's durable insight, and nothing more. The test for every line: *would a competent agent get this wrong, or waste time rediscovering it, working from the tree alone?* If it's recoverable by reading the code, the diff, `docs/current/`, or git history, leave it out.

That filter keeps four kinds of thing and discards the rest:

- **Decisions whose rationale isn't visible in the result** — why a value was chosen, what a line guards against, why something that looks like dead code or a no-op is load-bearing.
- **Deliberate non-actions** — a change you considered and chose not to make, so the next agent doesn't "fix" it. An unchanged file leaves no trace in a diff.
- **Couplings that span files** — two places that must move together, an ordering that matters, an assumption one file makes about another.
- **Live state** — what's unverified, unfinished, or waiting on something external.

Skip change inventories, restatements of the diff, and play-by-play of how you worked. There's no write-time approval gate; Michael reviews on commit. Append to the day's file if it exists, and write the few lines you'd want the next agent to read — honest and self-contained.

## Roadmap

This repo's roadmap lives in `docs/future/roadmap/` — one frontmatter-markdown item per file, per the roadmap convention in the grimoire (software/conventions/roadmap-convention.md). You may add items freely: write the file directly with required `title`, `state: inbox`, and `created:` (today, YYYY-MM-DD), optional `tags`/`source`/`log`, and a body that is a small, clear prompt -- the problem or solution to execute, not documentation of it; trust the code and the day's journal entry for what's discoverable, and point a `log:` stamp at the specific journal entry when a card leans on hard-won context. Everything above the first `##` heading is the prompt; supporting material that isn't the prompt goes in named sections below it (`## why` for justification, `## background` for a longer description), which are conventional, never required, and never validated. The filename is the slug of the title (lowercase ASCII, hyphens; discard every other character); never overwrite an existing file. Read sibling items for the shape.

Hard rules: never touch `order.yaml` (priority is the operator's judgment, set at triage); never commit roadmap changes unless directed — the uncommitted diff is the review queue; never delete items; edits change only the lines that express them. Label the kind from the house set when one fits: defect, documentation, enhancement, epic, feature, story; add `spike` alongside it when the work carries unknowns that need discovery.

## Project Rules

- in Go code, all comments should start with a lowercase letter, unless the first word of the sentence is referring to a Go type that starts with an uppercase letter.

- all outputs logged or otherwise emitted to a user should prefer lowercase unless it is referring to a type that requires uppercase letters to express accurately. dynamic data in outputs should appear between single quotes, like "the user selected the 'value' setting", where `value` represents a variable.

- Go files should be named like `dashManager.go` not `dash_manager.go`. unit tests should be named `dashManager_test.go`.

- never use emoji.

- clean up any build artifacts (binaries, test executables) created during development or testing. do not leave compiled binaries in the repository.

- always use mermaid diagrams in markdown documents instead of ASCII art.
