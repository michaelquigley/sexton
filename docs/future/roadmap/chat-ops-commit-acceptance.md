---
title: chat-ops commit acceptance
state: inbox
created: 2026-08-24
tags: [feature]
source: commit-policy spec, deferred section (retired at arc close)
---

let the operator accept an attention condition from mattermost: the attention alert carries a proposed commit message (and, smarter, an LLM summary of the unselected dirt), and a `commit <repo>` command accepts it — sexton executes the commit, but the acceptance is explicitly the operator's, so the commit gate holds. deferred from commit-policy v1 to keep that release a policy change rather than a new command surface. pairs with the semantic-differs design for the summary half.
