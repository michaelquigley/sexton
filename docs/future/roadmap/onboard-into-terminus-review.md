---
title: onboard into terminus review
state: evaluating
created: 2026-08-13
tags: [enhancement]
milestone: v0.1.x
---

sexton has no rubric in `terminus-canon/projects/`, so no code review gates it. Run the onboarding playbook at `terminus-canon/ONBOARDING.md`: classify (go project, daemon, files-and-subprocess), true-mirror the general and golang families against what sexton actually does, harvest the project-local invariants, compose `projects/sexton/rubric.yaml`, and validate with `terminus rubrics`.

The playbook is propose-don't-automate — an agent proposes, Michael authors and approves the blocking calibration.

## why

Most of the Go and House convention bullets in `AGENTS.md` are already canonized general-tier material — `no-emoji`, `mermaid-diagrams`, `camelcase-file-naming`, `lowercase-output` all exist as qualities. Today those rules hold only if an agent reads the orientation file and remembers them; under a rubric they are reviewed. The project-local harvest is the other half of the value: sexton's real invariants (the `GIT_SSH_COMMAND` injection that deliberately never touches a synced repo's git config, the never-silently-discard-the-working-tree guarantee) are the kind of rule whose violation is a bug, and nothing checks them.
