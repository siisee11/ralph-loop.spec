---
name: main-loop
description: Safely run the full Ralph Loop workflow.
version: 1
---

# Main Loop

- Start with `./ralph-loop schema main --output json`.
- Validate with `./ralph-loop --json '{"prompt":"...","dry_run":true}' --output json`.
- Use machine-readable output only.
- Prefer `--output ndjson` for long-running execution.
