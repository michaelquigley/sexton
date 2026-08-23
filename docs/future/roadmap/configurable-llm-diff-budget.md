---
title: configurable llm diff budget
state: inbox
created: 2026-08-21
tags: [enhancement]
source: mercurius round 1 (CP-R1-003) discussion on the commit-policy arc
---

the 32KB `maxDiffBytes` cap on the diff sent for commit-message generation is hardcoded, sized for the floor of the fleet rather than the model actually configured. add a `max_diff_bytes` knob to the `llm` config block, defaulting to the current 32KB, so an endpoint with a 200k+ context model can be given the full diff instead of falling back to `--stat` summaries.
