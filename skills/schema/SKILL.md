---
name: schema
description: Discover the live Ralph Loop command surface.
version: 1
---

# Schema

- Start every session with `./ralph-loop schema --output json`.
- Use `--fields items.command,items.options,items.raw_payload_schema` when you only need invocation details.
- Build `--json` payloads directly from `raw_payload_schema`.
