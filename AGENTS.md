# Sexton

Git-based repository synchronization agent. Keeps local git repos in sync with their remotes by polling for changes, committing with LLM-generated summaries, and pushing — marking a repo errored and retrying when it hits a conflict or failure it cannot immediately resolve.

Designed for knowledge repositories and datasets (markdown collections, config stores, structured data), not code repos.

## Architecture

### Package Structure

```
sexton/
├── api/v1/                  # gRPC service definition and generated code
│   ├── sexton.proto         # protobuf service definition
│   ├── sexton.pb.go         # generated protobuf types
│   └── sexton_grpc.pb.go    # generated gRPC client/server stubs
├── cmd/sexton/              # CLI entrypoint
│   ├── main.go              # root command; registers `agent` + `version`; control subcommands self-register
│   ├── agent.go             # `sexton agent --config <path>` — starts the agent; holds the control-plane adapters
│   ├── client.go            # shared gRPC dial helper (Unix socket)
│   ├── status.go            # `sexton status [repo]` — query agent status
│   ├── sync.go              # `sexton sync <repo>` — trigger immediate sync
│   ├── snooze.go            # `sexton snooze <repo> <duration>` — pause sync
│   ├── resume.go            # `sexton resume <repo>` — clear snooze/error, force a retry
│   └── version.go           # sets push/build DevVersion (the `version` subcommand comes from push/build)
├── internal/
│   ├── agent/               # core agent infrastructure
│   │   ├── container.go     # Container: top-level da.Run target, holds agents + shared LLM/alerter
│   │   ├── agent.go         # Agent: per-repo sync loop (poll, hooks, commit, pull, push); holdout, snooze/resume, error+retry
│   │   ├── hooks.go         # lifecycle hook runner (shell commands at sync phase boundaries)
│   │   ├── controller.go    # Container.ResolveAgent + lookup errors (ErrRepoNotFound, ErrAmbiguousRepo)
│   │   ├── state.go         # State enum (watching, syncing, error, snoozed, holdout)
│   │   └── alerter.go       # Alerter interface, AlertEvent, LogAlerter, MultiAlerter
│   ├── config/              # configuration loading and resolution
│   │   ├── model.go         # config + resolved types (repos, llm, alerts/mattermost, hooks, holdout)
│   │   ├── load.go          # YAML loading (df/dd), path expansion, cascade, SocketPath/GlobalConfigDir
│   │   └── holdout.go       # daily holdout-window resolution (midnight-crossing split, overlap merge)
│   ├── format/              # display helpers
│   │   └── duration.go      # human-friendly relative time ("5m ago")
│   ├── git/                 # git CLI wrapper
│   │   ├── git.go           # Git struct: status, commit, pull --rebase, push, rebase abort
│   │   ├── errors.go        # sentinel errors (conflict, no remote, etc.)
│   │   ├── status.go        # porcelain status parsing
│   │   └── message.go       # fallback commit message generation
│   ├── llm/                 # LLM client for commit summarization
│   │   └── client.go        # OpenAI-compatible chat completions client
│   ├── mattermost/          # Mattermost alerter + chat-ops control surface
│   │   ├── client.go        # REST + websocket client (posts messages, listens for commands)
│   │   ├── commands.go      # CommandHandler interface + command dispatch (status/sync/snooze/resume)
│   │   ├── alerter.go       # posts alert events to a Mattermost channel (implements agent.Alerter)
│   │   └── formatter.go     # formats alerts and command responses
│   └── rpc/                 # gRPC control plane server
│       ├── server.go        # Server: lifecycle, Unix socket listener, stale-socket detection
│       ├── handler.go       # gRPC method implementations
│       └── controller.go    # AgentController interface + RepoInfo (satisfied by the cmd/ adapter)
└── go.mod
```

### Key Design Decisions

1. **Polls, not watches** — fixed-interval `git status` polling. simpler and more portable than filesystem events.

2. **Shells out to git** — uses `git` CLI directly rather than go-git. simpler, more predictable for the operations sexton needs.

3. **`da.Run` lifecycle** — `agent.Container` is a concrete container for `github.com/michaelquigley/df/da`. agents implement `da.Wireable[Container]`/`da.Startable`/`da.Stoppable`. `da.Run` handles wire → start → signal wait → stop. shared resources (LLM client, alerter) live on Container and are injected into agents via `Wire`.

4. **Mark errored, keep retrying, never lose data** — on a conflict or unrecoverable error the affected repo enters the `error` state, is surfaced in `status`, and is retried automatically on the next poll until the underlying issue clears (it recovers on its own and alerts "recovered from error"). conflicts abort the rebase first; the working tree is never silently discarded. user-facing `resume` can force an immediate retry but is no longer required for recovery.

5. **Control plane dependency inversion** — the `internal/rpc` package defines an `AgentController` interface for the operations it needs (repo status, trigger sync, snooze, resume) plus a `RepoInfo` value type. the adapter that satisfies it (`containerAdapter`) lives in `cmd/sexton`, not in `internal/agent` — this keeps `internal/agent` free of any `rpc` import and avoids a circular dependency. the agent-side lookup is `Container.ResolveAgent`.

6. **Two control planes, one agent** — sexton can be driven locally over gRPC on a Unix socket (`internal/rpc`, used by the `status`/`sync`/`snooze`/`resume` CLI) and remotely over Mattermost chat-ops via websocket (`internal/mattermost`). both reach the agent through small adapters in `cmd/sexton` (`containerAdapter`, `mattermostAdapter`). alerting fans out the same way: `agent.MultiAlerter` composes the `LogAlerter` with any Mattermost alerters, and Mattermost clients are de-duplicated by identity so duplicate alert entries don't double-process inbound commands.

7. **Holdout windows pause sync, but don't stampede on exit** — daily local-time maintenance windows (`holdout_windows`) move a repo into the `holdout` state, typically to avoid touching a remote during a known-bad period such as a nightly restart. when a window *ends*, sexton deliberately does not fire an immediate sync — recovery is left to the next regular poll, which grants up to one poll interval of grace for the remote to come back and naturally staggers retries across a fleet by each agent's ticker phase. the immediate sync on exit is kept only for the user-initiated `snooze`/`resume`.

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

- **Global**: `~/.config/sexton/config.yaml` (or `$XDG_CONFIG_HOME/sexton/config.yaml`)
- **Repo-local**: `.sexton.yaml` in repo root (overrides global)
- **Cascade**: repo-local > global > built-in defaults
- **Control socket**: `~/.config/sexton/sexton.sock` (gRPC control plane)
- **Per repo**: poll interval, branch/remote, commit-message prompt, lifecycle hooks, and daily holdout windows are configurable; the top-level `alerts` list selects `log` and/or `mattermost`. config binds via `df/dd`, so types use `dd:` struct tags (e.g. `dd:",+required"`), never `yaml:` tags. see `README.md` for the full field reference.

## Spec

The full design specification lives at `~/Repos/q/writing/grimoire/software/sexton/sexton-spec.md`.

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

## Project Rules

- in Go code, all comments should start with a lowercase letter, unless the first word of the sentence is referring to a Go type that starts with an uppercase letter.

- all outputs logged or otherwise emitted to a user should prefer lowercase unless it is referring to a type that requires uppercase letters to express accurately. dynamic data in outputs should appear between single quotes, like "the user selected the 'value' setting", where `value` represents a variable.

- Go files should be named like `dashManager.go` not `dash_manager.go`. unit tests should be named `dashManager_test.go`.

- never use emoji.

- clean up any build artifacts (binaries, test executables) created during development or testing. do not leave compiled binaries in the repository.

- always use mermaid diagrams in markdown documents instead of ASCII art.
