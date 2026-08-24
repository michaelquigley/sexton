---
title: mention users on error alerts
state: inbox
created: 2026-08-24
tags: [enhancement]
source: commit-policy spec, deferred section (retired at arc close)
---

`mention_users` mentions the configured operators on attention alerts only. extend the mention treatment to error-class alerts — one config knob deciding which severities mention, defaulting to attention-only for compatibility. deliberately left out of commit-policy v1; add when wanted.
