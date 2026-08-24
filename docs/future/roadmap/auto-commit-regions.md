---
title: auto-commit regions
state: evaluating
created: 2026-08-20
tags: [enhancement]
milestone: v0.1.x
---

let's teach sexton a new set of policies for managing commits against repos. in our `grimoire` repo, we want sexton to auto-commit changes against `journal/`, but any other region... sexton should complain loudly until the user/operator manually commits the changes.

this is a similar-but-different posture to [[pull-only-repos]].
