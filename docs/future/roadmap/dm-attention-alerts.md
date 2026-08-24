---
title: dm attention alerts
state: building
created: 2026-08-24
tags: [feature]
source: mattermost notification diagnosis, 2026-08-24 (see journal)
---

deliver attention alerts as a direct message from the sexton bot instead of a channel post with a mention. a DM notifies inherently — no mention parsing, no per-channel settings, and it cannot be defeated by a client parked on the alert channel, which is exactly the failure diagnosed on 2026-08-24. shape: `dm_users` on the mattermost alert entry (egress policy, like `channel_id` and `mention_users`); attention-severity alerts go to each user's DM channel, other severities keep the channel; on DM delivery failure, fall back to the channel post with mentions so the alert is never lost. two new API calls on the raw client (resolve username to id; open-or-get the direct channel), both cached.

## why

largely supersedes [[mention-users-on-error-alerts]]: severity-to-destination routing is the general form of both wishes, and the DM path is the one notification shape proven loud on this fleet with zero client-side tuning.
