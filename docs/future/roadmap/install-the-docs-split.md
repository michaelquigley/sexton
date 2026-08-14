---
title: install the docs split
state: researching
created: 2026-08-13
tags: [documentation]
milestone: v0.1.x
---

Stand up `docs/current/` and retire the realized design material into it. `docs/MATTERMOST.md` is a 700-line implementation spec for a feature that shipped — synthesize the built behavior into `docs/current/` and delete the spec; git history keeps it. The "Key Design Decisions" list in `AGENTS.md` is doing `docs/current/`'s job for the sync loop, holdout windows, and the control-plane inversion; move the descriptions of built behavior across and leave `AGENTS.md` an orientation file.

## why

sexton predates the docs split and never had it installed. With no `docs/current/`, an arriving agent has nowhere to read what the system actually does short of the code, and nowhere to write it as behavior lands — which is how a shipped spec ends up still sitting in `docs/` written in the future tense.
