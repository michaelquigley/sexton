---
title: the control plane
created: 2026-08-14
---

# the control plane

A running agent is driven two ways: locally over gRPC on a Unix domain socket, and remotely over Mattermost chat-ops (`mattermost.md`). Both reach the same agents through small adapters that live in `cmd/sexton`, and both expose the same four operations — status, sync, snooze, resume.

## the socket

`internal/rpc` serves gRPC on `~/.config/sexton/sexton.sock`, in a directory created `0700`. The filesystem is the whole access-control story: there is no authentication, because reaching the socket already means being the user who owns it.

Startup checks for a stale socket before binding. If the path exists, the server dials it — a successful dial means another agent is live and startup fails; a failed dial means the file is a leftover from an unclean exit and is removed. Shutdown stops the gRPC server gracefully and unlinks the socket.

## the commands

`sexton status [repo]`, `sexton sync <repo>`, `sexton snooze <repo> <duration>`, `sexton resume <repo>`. Each is a subcommand that dials the socket, makes one call, prints, and exits; durations use Go's `time.ParseDuration` grammar. The agent-side vocabulary is deliberately narrow — these are the only mutations a running agent accepts.

Errors carry gRPC status codes chosen by meaning rather than by convenience: `NotFound` for an unknown repo, `InvalidArgument` for an ambiguous repo or an unparseable duration, and `FailedPrecondition` for everything else — which is what a refusal looks like when the agent is snoozed or inside a holdout window. Timestamps cross the wire as RFC 3339 strings and remaining durations as rounded-to-second Go duration strings; absent values are empty strings rather than zero values.

Repo status carries retained error and attention details separately. The CLI renders them in one `detail` column with error first, then attention, including while `snoozed` or `holdout` masks the underlying state. This keeps a paused repo from appearing clean without confusing an attention condition with an error.

## dependency inversion

`internal/rpc` declares what it needs — an `AgentController` interface and a `RepoInfo` value type — and `internal/agent` knows nothing about it. The adapter satisfying the interface (`containerAdapter`) lives in `cmd/sexton`, which is the only package that imports both. This is what keeps `internal/agent` free of any transport import and avoids the circular dependency that would otherwise form; the Mattermost side reuses the same shape, with `mattermostAdapter` wrapping `containerAdapter` and translating `rpc.RepoInfo` into the Mattermost package's own status type so that package never imports `rpc` either.

The agent-side lookup is `Container.ResolveAgent`.

## resolving a repo

Three passes, each requiring exactly one match before it is accepted:

1. exact path
2. explicitly configured name
3. path basename

Explicit names and full paths are the stable identifiers; the basename is a convenience that works only while it is unambiguous. Multiple matches at any pass produce an ambiguity error listing the candidates and naming the fix ("use configured name or full path") — sexton never guesses which of two repos named `notes` was meant. An empty repo argument is legal only for `status`, where it means all repos.

## startup order

`runAgent` loads the config, builds the container, starts the RPC server, builds the alerter (which starts any Mattermost clients), assigns it to the container, and only then calls `da.Run`. The alerter must be on the container before `da.Run`, because `da.Run` wires it into every agent before starting them — an alerter assigned later would reach no agent. Both the RPC server's `Stop` and any Mattermost cleanup are deferred, so a signal unwinds them in reverse.
