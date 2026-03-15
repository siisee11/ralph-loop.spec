# Ralph Loop Agent Guide

The CLI treats the agent as untrusted input.

Use these rules every time:

- Start with `./ralph-loop schema --output json`.
- Prefer `--output json` or `--output ndjson` over text.
- Prefer `--fields` and narrow selectors on `ls`, `tail`, and `schema`.
- Run `--dry-run` before `init` or the main loop.
- Use `--json <payload>` when building requests programmatically from the schema.
- Keep `--output-file` paths under the current working directory only.

Discoverable skills live under [`skills/index.yaml`](/Users/dev/git/ralph-loop.spec/skills/index.yaml).
