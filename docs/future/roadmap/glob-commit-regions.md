---
title: glob commit regions
state: inbox
created: 2026-08-24
tags: [enhancement]
source: commit-policy spec, deferred section (retired at arc close)
---

`commit_regions` accepts directory prefixes only. add glob patterns (likely `doublestar`, the precedent the semantic-differs design establishes) for repos whose commit-worthy paths don't fall under clean prefixes. the partition, staging, and partial-commit machinery all key off the resolved region list, so the change should be contained to region normalization and matching — but the `:(literal)` pathspec discipline on staging and commits needs rethinking for patterns, since it exists precisely to keep region strings from being interpreted.
