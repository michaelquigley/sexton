---
title: adopt repo-local config changes
state: researching
created: 2026-08-21
tags: [enhancement]
milestone: v0.1.x
source: mercurius round 2 (CP-R2-A01) on the commit-policy arc
---

configuration resolves once at startup, so a `.sexton.yaml` change that arrives in a synced repo — pulled by sexton itself — is not adopted until the daemon restarts. with the commit-policy migration keeping policy in-repo, that gap is the normal path for policy changes reaching the fleet: the repo sits fail-safe (defaulting to `none`, warning) until each host's daemon restarts. teach sexton to notice a changed `.sexton.yaml`, most naturally after a pull moves HEAD, and re-resolve that repo's configuration live.
