---
title: configuration
created: 2026-08-14
---

# configuration

Three layers, resolved once at startup into a `config.ResolvedRepo` per repo. Nothing is re-read while the agent runs; a config change requires a restart.

- **global** — `~/.config/sexton/config.yaml`, or `$XDG_CONFIG_HOME/sexton/config.yaml` when that variable is set. `--config` overrides the path.
- **repo-local** — `.sexton.yaml` in the repo root, read for every configured repo.
- **built-in defaults** — `30s` poll, branch `main`, remote `origin`, commit policy `none`, and the default commit-message prompt.

A missing config file at any layer is not an error: `Load` returns defaults, and a repo without `.sexton.yaml` resolves from the global entry alone. The socket path (`~/.config/sexton/sexton.sock`) is derived from the same directory, so `XDG_CONFIG_HOME` moves the control socket with the config.

## binding

Config types bind through `df/dd` (`dd.MergeYAMLFile`), which matches snake_case YAML keys to PascalCase Go fields by name. **These types carry `dd:` struct tags and never `yaml:` tags** — a `yaml:` tag is invisible to the binder and silently does nothing. `MattermostConfig.URL` and `.ChannelID` are tagged `dd:",+required"`; everything else is optional.

The binder ignores unknown keys. A misspelled field name is not an error — it simply leaves the feature unconfigured, which is the failure mode to suspect first when a configured behavior doesn't appear.

## the cascade

Scalars resolve by first-non-empty over repo-local, then the global repo entry, then defaults, then a hardcoded fallback. Lists — hooks, holdout windows, and commit regions — **replace rather than merge**: the first layer with a non-empty list for that phase or setting wins entirely, so a repo-local `pre_commit` list overrides the global one instead of appending to it.

Paths expand `~` and environment variables, applied to repo paths and to `ssh_key`.

## identity

A repo's name is its explicit `name` (repo-local first, then the global entry) or, absent one, the basename of its path. Whether the name was explicit is recorded on the resolved repo and matters twice: only explicitly-named repos participate in duplicate-name detection, and only explicit names are stable identifiers for lookup — a basename resolves a repo only when it is unambiguous.

Container construction is strict about identity and lenient about everything else. Duplicate repo paths, or duplicate explicit names, are fatal. A repo whose config won't resolve, or whose path isn't a git repository, is logged and skipped. A configured `ssh_key` that doesn't exist on disk is a warning, not a failure — the key may be provisioned later, and git will produce the real error. If nothing survives, startup fails with `no valid repos to watch`.

## commit policy

`commit_policy` is `all`, `regions`, or `none`. `all` selects every changed path, `regions` selects changes beneath the normalized repo-relative directory prefixes in `commit_regions`, and `none` never creates a commit. Policies affect commit creation, not pushing: clean repos and operator-created commits still pull and push under `none`, while `regions` continues pushing selected commits.

A region is cleaned, made repo-relative, and normalized with a trailing slash so `journal/` does not select `journal-drafts/`. Empty, absolute, escaping, or whole-repository regions are rejected. `regions` with no resolved regions is also rejected because it would silently select nothing. A region list under `all` or `none` is valid but inert, which lets a higher-precedence policy override a lower layer without also having to erase its list.

Missing policy defaults to `none`, deliberately breaking the pre-policy behavior that committed the whole tree. The agent warns once at startup with `no commit_policy configured; defaulting to 'none'`. If `.sexton.yaml` exists but cannot be parsed, its repo-local layer is replaced with an explicit `none` rather than falling through to broader global policy; the repo remains monitored and warns once that repo-local config is malformed and the policy was forced. Fixing either condition requires a restart because configuration is startup-only.

## holdout windows

Each window is a `start` and `end` in 24-hour `HH:MM` local time, resolved to minutes-from-midnight. A window whose start equals its end is rejected rather than treated as empty or as all-day. A window whose end is earlier than its start crosses midnight and is **split into two** — `00:00`–`end` and `start`–`24:00` — so the runtime check is a simple containment test with no wraparound case. After splitting, windows are sorted and overlapping ones merged, so overlapping entries in the config are harmless.

Windows are evaluated in `time.Local` against the day's midnight, which means they follow the wall clock across a DST transition rather than holding a fixed duration.

## the llm

`endpoint`, `model`, `api_key`, `api_key_env`, and `max_tokens` (default 512). The client is nil when no endpoint is configured, and a nil client is a supported configuration — commit messages fall back to the mechanical generator. Omitting the whole block is therefore legitimate, and none of these fields is individually required. The one combination that is rejected at load is a `model` with no `endpoint`: it builds no client, so the setting would silently do nothing.

The API key is resolved once at startup: a non-empty value from the environment variable named by `api_key_env` wins, otherwise the key set directly in `api_key` is used. Both are read at startup only, so a rotated key requires a restart. The pair mirrors the Mattermost `token`/`token_env` precedence, so the two credential-bearing integrations share one rule.

## alerts

The top-level `alerts` list selects the alert sinks: `log` (or an empty type) and `mattermost`. An empty list means log-only. A `mattermost` entry with no `mattermost:` block, or with an empty `channel_id`, fails startup rather than degrading. `mention_users` is an optional list of Mattermost usernames used only for attention alerts. See `mattermost.md` for the remaining Mattermost behavior.
