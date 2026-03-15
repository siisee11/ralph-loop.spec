---
name: tail
description: Inspect Ralph Loop logs without wasting context.
version: 1
---

# Tail

- Start with `./ralph-loop schema tail --output json`.
- Prefer `--fields page,items.line,items.ts,items.status`.
- Narrow with a selector before using `--page-all`.
- Use `--output ndjson --page-all` for streaming page envelopes.
