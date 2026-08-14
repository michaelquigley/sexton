---
title: make clean wipes gobin
state: evaluating
created: 2026-08-13
tags: [defect]
milestone: v0.1.x
---

`make clean` runs `rm -f ${GOBIN}/*`, which deletes every binary in the user's `GOPATH/bin` — not just sexton's. Scope it to what this repo installs, the way ranger's does: `rm -f ${GOBIN}/sexton sexton`.

## why

There is no warning and no confirmation; a `make clean` in this repo silently removes every tool the user has `go install`ed. The blast radius is the whole toolchain, and the fix is one line.
