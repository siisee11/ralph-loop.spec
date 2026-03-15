---
name: ls
description: List running Ralph Loop sessions efficiently.
version: 1
---

# LS

- Start with `./ralph-loop schema ls --output json`.
- Prefer `--fields page,items.pid,items.work_branch,items.worktree_path`.
- Use selectors to narrow results before requesting all pages.
