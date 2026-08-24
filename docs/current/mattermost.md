---
title: mattermost
created: 2026-08-14
---

# mattermost

An optional second control plane, reached over a websocket instead of the local socket, and an alert sink in the same package. One `alerts` entry of `type: mattermost` gives both: outbound alerts posted to a channel, and inbound commands accepted from that channel or a DM.

The client is raw `net/http` plus `gorilla/websocket`, deliberately not the official Mattermost Go client — the API surface sexton needs is six endpoints (`users/me`, `users/{id}`, `users/username/{name}`, `channels/direct`, `posts`, `websocket`), and the official library's dependency tree costs more than that is worth.

## startup

The token resolves from `token_env` first, then a literal `token`. `Start` authenticates with `GET /api/v4/users/me` to learn the bot's own id and username, opens the websocket with the bearer token on the upgrade request, and launches the listener.

**Startup failure is fatal to the agent.** A missing token, a failed authentication, or an unreachable server stops sexton rather than degrading to log-only alerting — the same posture as the RPC server. Log-only is a configuration (drop the entry), never an accident.

Once running, delivery is best-effort in the other direction: a failed `PostMessage` is not retried, but the agent logs a warning naming the repo and undelivered message. This matters for Mattermost-only configurations, where a failed post would otherwise leave no evidence. A dropped websocket reconnects with a backoff of 1, 2, 4, 8, 15, then 30 seconds, holding at 30; every wait and every iteration checks the stop channel, so shutdown during a backoff is immediate rather than waiting out the delay.

## addressing

The listener acts only on `posted` events, and ignores the bot's own posts by user id. When `allowed_users` is non-empty, the poster's id is resolved to a username (cached in memory after the first lookup) and filtered against the list, case-insensitively; a non-matching user is ignored silently. Usernames rather than user ids are what the config carries, which trades a rename hazard for being able to write the file by hand.

Two ways to address an agent coexist, which is what makes a shared channel work for a fleet:

- **mention** — the bot's user id appears in the event's mention list. All `@token`s are stripped and the remainder is the command. No trigger word needed, and mentioning several bots in one message has each of them execute it independently.
- **trigger word** — the message begins with a configured trigger word (default `sexton`), matched case-insensitively at a word boundary. Every agent in the channel answers, which is the broadcast form.

Alerts are posted only to the configured `channel_id`. Command responses go back to the channel the command arrived in, so DMs work without extra configuration.

`mention_users` is an outbound list of usernames. Attention alerts prepend those configured `@mentions` and an `attention` marker; warnings, errors, and informational alerts ignore the list. Repo names, details, commit messages, and filenames are rendered so their markdown and `@tokens` are inert — configured usernames are the only live mentions an attention alert can carry.

`dm_users` routes attention alerts to direct messages instead. When the list is non-empty, an attention alert is posted to each named user's DM channel with the bot — no mention prefix, since a direct message notifies on its own — and the channel post for that alert is skipped. Every other severity keeps the channel. Usernames resolve lazily on the first attention alert and are cached, as is each DM channel. If any direct delivery fails, the alert falls back to the channel post with the configured mentions and the failure is logged; an attention condition is never lost to a bad username or an API hiccup.

## commands

`status [repo]`, `sync <repo>`, `snooze <repo> <duration>`, `resume <repo>`, `help`. A bare trigger word returns help; an unrecognized subcommand returns the unknown-command line followed by help; missing arguments return a command-specific message rather than generic help. Repo resolution and its ambiguity errors are the same as the socket path's.

`Dispatch` takes command text with the trigger word or mentions already removed, which keeps the whole grammar testable without a websocket.

Status renders as a markdown table — repo, state, branch, last sync, last change, detail — with relative times, and with the state cell replaced by `holdout (12m left)` or `snoozed (30m left)` when a pause is active. Detail carries a retained error first, otherwise retained attention; it remains visible when a pause masks the underlying state. Untrusted table values are markdown- and mention-neutral. The build string is appended, so asking a fleet for status in a shared channel shows which build each agent is running.

## fan-out and de-duplication

`agent.MultiAlerter` composes the sinks; a single configured sink is used directly rather than wrapped. Alert events carry severity, repo name, message, optional error, and — for a completed sync — the commit message and the per-category file lists, which the formatter renders as a quoted message with backticked file names.

Mattermost clients are **de-duplicated by identity**, where identity is the server URL plus the auth source (`env:VAR` or the literal token). Two alert entries pointing at the same bot share one client and therefore one websocket, so a command posted once is processed once instead of once per entry. The corollary is enforced at startup: two entries with the same identity but different `allowed_users` or `trigger_words` are a configuration error, because a shared client can only honor one ingress policy. `channel_id`, `mention_users`, and `dm_users` are egress policy and may differ freely between entries sharing a client — that is one bot alerting into two channels with channel-specific recipients.

## drift from the original spec

The retired implementation spec (`docs/MATTERMOST.md`, in git history) described duplicate Mattermost entries as each creating its own client; the de-duplication above replaced that. It also typed `ResumeRepo` as returning only an error, where the shipped controller returns a message string so `resume` can report that a holdout window is still in force.
