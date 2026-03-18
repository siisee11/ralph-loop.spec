# Ralph Loop Spec

Build a Go CLI that drives a coding agent through a complete task lifecycle in an automated loop. The CLI uses Codex app-server (via stdio JSON-RPC) to spawn and control agents. The name comes from the "Ralph Wiggum Loop" pattern: the agent keeps working until it declares the task complete.

## Overview

The Ralph Loop has three phases:

1. **Setup Agent** — Runs `ralph-loop init` to prepare a clean worktree, explores the codebase, and creates an execution plan.
2. **Coding Agent Loop** — Iterates the coding agent with the user's prompt until the agent signals completion. Each iteration commits changes.
3. **PR Agent** — Reads the commits and completed plan, then opens a pull request.

```
┌─────────────────────────────────────────────────────────────────┐
│                         Ralph Loop                              │
│                                                                 │
│  ┌──────────┐    ┌────────────────────┐    ┌────────────┐       │
│  │  Setup   │───▶│  Coding Agent Loop │───▶│  PR Agent  │       │
│  │  Agent   │    │  (repeat until     │    │  (commits  │       │
│  │          │    │   COMPLETE)        │    │   → PR)    │       │
│  └──────────┘    └────────────────────┘    └────────────┘       │
│                                                                 │
│  runs init      commit per iteration       commits → PR         │
│  explores repo   one milestone per turn                         │
│  creates plan    <promise>COMPLETE</promise> = done             │
│                  plan progress updated                          │
└─────────────────────────────────────────────────────────────────┘
```

---

## Interface

Prefer a repo-root executable such as `./ralph-loop` as the user-facing command, even if the active implementation lives elsewhere.

```
ralph-loop init [options]
ralph-loop "<user prompt>" [options]
ralph-loop tail [selector] [options]
ralph-loop ls [selector] [options]
ralph-loop schema [command] [options]

Global agent-first options:
  --json <payload|->       Raw JSON payload for the command. Use - to read JSON from stdin.
  --output <format>        Output format: text, json, ndjson (default: text on TTY, json otherwise)
  --output-file <path>     Write the machine-readable result to a file under the current working directory
  --fields <mask>          Comma-separated field mask to reduce response size
  --page <n>               Page number for read commands (default: 1)
  --page-size <n>          Items per page for read commands
  --page-all               Stream every page in ndjson mode or aggregate every page in json mode

Main options:
  --model <model>          Codex model to use (default: gpt-5.3-codex)
  --base-branch <branch>   Branch to create the worktree from (default: main)
  --max-iterations <n>     Safety cap on coding loop iterations (default: 20)
  --work-branch <name>     Name for the working branch (default: ralph-<slugified-prompt>)
  --timeout <seconds>      Max wall-clock time for entire run (default: 43200 = 12h)
  --approval-policy <p>    Codex approval policy (default: never)
  --sandbox <policy>       Codex sandbox policy (default: workspace-write)
  --preserve-worktree      Keep the generated worktree on exit for debugging
  --dry-run                Validate and describe the request without side effects

Init options:
  --base-branch <branch>   Branch to create the worktree from (default: main)
  --work-branch <name>     Name for the working branch
  --dry-run                Validate and describe the request without side effects

Tail options:
  --lines N                Number of log lines to read (default: 40)
  --follow                 Stream appended log lines
  --raw                    Return raw log payloads instead of summaries

Schema options:
  --command <name>         Command name to describe when not provided positionally
```

### Raw payload input

Raw payload input is a required CLI contract for agents.

- Every command must accept `--json <payload>` and `--json -`.
- The JSON payload must map directly to the command schema with no flag-specific translation layer.
- Convenience flags remain supported, but raw JSON is first-class and documented alongside flags.
- For mutating commands such as `init` and the main run command, agents must be able to construct the request directly from the machine-readable command schema.

Example:

```json
{
  "command": "init",
  "base_branch": "main",
  "work_branch": "ralph-agent-first",
  "dry_run": true,
  "output": "json"
}
```

### Schema introspection

Schema introspection is a required runtime surface.

- `ralph-loop schema --output json` returns a machine-readable description of every command.
- `ralph-loop schema <command> --output json` returns the exact live schema for that command.
- The schema must include:
  - command name and description,
  - positional arguments,
  - options and aliases,
  - types,
  - required fields,
  - default values,
  - enum values,
  - nested raw payload schema,
  - whether the command mutates state,
  - whether `--dry-run` is supported.
- The schema must be generated at runtime from the same descriptors the CLI uses for parsing so that it always reflects the current command surface.

### Context window discipline

The CLI must actively help agents conserve tokens.

- Every read-oriented command (`ls`, `tail`, and `schema`) must support `--fields`.
- Every read-oriented command must support `--page`, `--page-size`, and `--page-all`.
- In `json` mode, paginated read commands return a single object that contains page metadata plus the filtered items for the requested page or all pages.
- In `ndjson` mode with `--page-all`, read commands emit one page envelope per line so an agent can process each page incrementally.
- Repository agent docs and skills must instruct agents to prefer `--fields` and narrow selectors before requesting full results.

### Machine-readable output

Machine-readable output is a required CLI contract, not a debug-only feature.

- Every command must support `--output text`, `--output json`, and `--output ndjson`.
- When stdout is a TTY, default to `text`.
- When stdout is not a TTY, default to `json`.
- `text` is for human operators.
- `json` emits exactly one JSON object for the full command result.
- `ndjson` emits one JSON object per event or record and is the preferred format for streaming commands such as the main loop and `tail --follow`.
- Errors must remain structured in `json` and `ndjson` modes. Do not fall back to prose-only stderr for machine-readable modes.
- Shared fields such as `command`, `status`, `error`, `phase`, `iteration`, `event`, `ts`, `worktree_path`, and `work_branch` should keep stable names and meanings across commands.
- If `--output-file` is used, stdout must remain parseable by printing either nothing or a minimal machine-readable receipt; the file content must match the requested `--output` format.
- `--output-file` paths must be sandboxed to the caller's current working directory and rejected if they escape it.

Recommended behavior:

- `ralph-loop init --output json` returns a single object with worktree metadata, install/build status, runtime root, and any structured error.
- `ralph-loop init` in default `text` mode still reserves stdout for the final success JSON object; progress and diagnostics go to stderr.
- `ralph-loop init --output ndjson` may emit structured step events and must end with a terminal record containing the same metadata as the JSON mode success object.
- `ralph-loop "<prompt>" --output json` returns a single object describing the run result, including final status, worktree metadata, iteration count, plan path, PR URL if created, and any structured error.
- `ralph-loop "<prompt>" --output ndjson` streams lifecycle events as they happen and ends with a terminal event.
- `ralph-loop ls --output json` returns a JSON array of running sessions.
- `ralph-loop ls --output ndjson` emits one session object per line.
- `ralph-loop tail --output json` returns the selected log metadata plus an array of lines or parsed records.
- `ralph-loop tail --follow --output ndjson` emits one structured log or event record per line.

Example NDJSON events:

```json
{"command":"main","event":"run.started","status":"running","ts":"2026-03-15T12:00:00Z","worktree_path":"/repo/.worktrees/ralph-foo","work_branch":"ralph-foo"}
{"command":"main","event":"phase.started","phase":"setup","status":"running","ts":"2026-03-15T12:00:01Z"}
{"command":"main","event":"iteration.completed","phase":"coding","iteration":1,"status":"ok","commit":"abc1234","ts":"2026-03-15T12:03:10Z"}
{"command":"main","event":"run.completed","status":"completed","iterations":3,"plan_path":"/repo/docs/exec-plans/completed/foo.md","pr_url":"https://github.com/org/repo/pull/123","ts":"2026-03-15T12:14:22Z"}
```

Example structured error:

```json
{
  "command": "main",
  "status": "failed",
  "error": {
    "code": "setup_failed",
    "message": "setup agent completed without the required completion token"
  }
}
```

---

## Worktree-Aware Boot Architecture

The repository must expose a worktree-aware app boot flow that lets one coding agent launch one isolated app instance per worktree or change.

### Boot strategy

- Provide one automation-safe boot entrypoint for the current worktree. This may be a dedicated script such as `scripts/harness/boot.sh` or a Ralph Loop subcommand such as `ralph-loop boot`, but the contract must be stable and documented.
- The boot entrypoint must resolve the current repo root and current worktree internally so it can be run from any subdirectory.
- Boot must block until the app is actually ready, or fail non-zero with a clear diagnostic. Blind sleeps are not acceptable.
- Boot must prefer convention over configuration, but allow env var overrides for every derived runtime resource.

### Worktree ID computation

- Compute the worktree ID from the canonical absolute path of the current Git worktree.
- Recommended algorithm:
  1. Resolve the current worktree path with `git rev-parse --show-toplevel` plus `realpath`.
  2. Take the basename of the worktree path as a human-readable prefix.
  3. Append a short stable hash of the canonical path, for example `foo-a1b2c3d4`.
- The worktree ID must remain stable across repeated boots from the same worktree path.
- Allow an explicit override through `DISCODE_WORKTREE_ID`, but default to the derived value.

### Resource assignment

- Use `.worktree/<worktree_id>/` as the runtime root for per-worktree state.
- Isolate at least these resources under that root:
  - Logs
  - Temp files
  - Runtime metadata / lock files
  - Local databases or SQLite files if the app uses them
  - Browser profile or user data directories if the app launches a browser
- Derive ports deterministically from the worktree ID so repeated boots for the same worktree choose the same defaults.
- Recommended strategy:
  - `offset = hash(worktree_id) % 1000`
  - `app_port = APP_PORT_BASE + offset * 2`
  - `ws_port = WS_PORT_BASE + offset * 2 + 1`
- Allow explicit overrides through env vars such as `PORT`, `APP_PORT`, `WS_PORT`, `DISCODE_APP_PORT`, and `DISCODE_WS_PORT`.

### Cleanup and reuse

- Persist boot metadata under `.worktree/<worktree_id>/run/`, including the chosen ports, PID, URLs, and healthcheck information.
- If the boot command detects a healthy app process already running for the same worktree and same runtime manifest, it may reuse that instance and return the existing metadata.
- On clean shutdown, remove stale lock files and transient temp files but preserve logs.
- On startup, opportunistically clean up stale runtime metadata whose PID is no longer alive.

### Failure handling

- If a derived port is already occupied by a different process, probe the next deterministic candidate in a bounded range.
- If no candidate is available in the allowed range, fail non-zero with a diagnostic that names the attempted ports.
- If a required runtime resource cannot be created, fail fast and report the exact path or resource that failed.
- If boot times out waiting for readiness, report the last readiness check status and the runtime root / log path.

## Coding Agent Launch Contract

Ralph Loop depends on a stable boot contract so the coding agent can launch exactly one app instance per worktree.

- Provide a command or script intended for automation use.
- Startup must block until the app is actually ready.
- Readiness must use a real healthcheck or equivalent probe rather than a fixed sleep.
- The command must return enough metadata for downstream tooling. At minimum:
  - `app_url`
  - `selected_port`
  - `healthcheck_url`
  - `healthcheck_status`
  - `worktree_id`
  - `runtime_root`
  - `observability_url` or `observability_base` when observability is started alongside boot
- The response should be machine-readable JSON on success.
- All non-JSON progress logs should go to stderr so downstream tooling can parse stdout.

## Environment Initialization Entrypoint (`ralph-loop init`)

Create a first-class environment preparation command:

```sh
ralph-loop init [--base-branch <branch>] [--work-branch <name>]
```

The command must perform the following steps in order:

1. **Create or reuse a git worktree**: If already inside a worktree, reuse it. Otherwise, create a new worktree using `git worktree add` from the specified base branch (default: `main`). Derive the worktree path using the same convention as `scripts/lib/worktree.sh`.
2. **Clean git state**: Inside the worktree, ensure a clean working tree. Stash any uncommitted changes. Create and checkout the work branch if specified.
3. **Install dependencies**: Run the project's package install or fetch commands. Detect the project type from files such as `package.json`, `bun.lockb`, `Cargo.toml`, or equivalent. Fail clearly if install fails.
4. **Verify build**: Run the project's default build or smoke command based on repository type, such as `go test ./...`, `cargo build`, or `npm run build`. If verification fails, exit non-zero with a diagnostic message.
5. **Set up environment config**: If `.env.example` exists and `.env` does not, copy it. Set `DISCODE_WORKTREE_ID` and any other worktree-derived env vars required by the project.
6. **Create runtime directories**: Ensure `.worktree/<worktree_id>/logs/`, `.worktree/<worktree_id>/tmp/`, and any other runtime dirs needed by boot or observability exist.

Success output must be a JSON object printed to stdout:

```json
{
  "worktree_id": "<derived_id>",
  "worktree_path": "<absolute_path_to_worktree>",
  "work_branch": "<branch_name>",
  "base_branch": "<base_branch>",
  "deps_installed": true,
  "build_verified": true,
  "runtime_root": ".worktree/<worktree_id>/"
}
```

Additional requirements:

- The command must be idempotent. Running it twice on the same worktree is safe.
- It must work from any directory and resolve the repo root internally.
- It must not require interactive input.
- Exit code `0` on success and non-zero on any failure.
- All output except the final success JSON goes to stderr so stdout remains parseable.
- `--dry-run` must validate inputs, resolve the repo root, derive the target worktree and runtime paths, and return a machine-readable execution plan without performing filesystem writes, git mutations, dependency installs, or builds.

### Input hardening and security posture

The CLI must defend against agent mistakes as an explicit design goal.

- Treat the agent as untrusted input. Document this security posture in repository agent docs.
- Reject control characters in resource identifiers, selectors, branch names, and output-file paths.
- Reject path traversal patterns such as `../`, `..\`, and their percent-encoded forms.
- Reject embedded query fragments such as `?`, `#`, and percent-encoded `?` or `#` in selectors and file-like identifiers.
- Reject double-encoded and percent-encoded path separators in selectors and file-like identifiers.
- Percent-encode identifiers when constructing any downstream path- or URL-like transport representation.
- Constrain any user-provided output file path to the current working directory after symlink resolution.

### Safety rails

Mutating commands must provide preflight validation and sanitize untrusted data.

- Every mutating command must support `--dry-run`.
- `--dry-run` must return the exact request shape the command would execute plus a list of planned side effects.
- Any untrusted data returned from downstream systems, logs, or agent messages must be sanitized before being rendered in text mode or copied into structured summaries.
- Sanitization must at minimum remove control characters, neutralize obvious prompt-injection markers, and record whether sanitization changed the payload.

### Agent knowledge packaging

The repository must ship agent-consumable knowledge alongside the CLI.

- Add an `AGENTS.md` at the repository root that explains the supported agent-first surfaces and guardrails.
- Add a versioned, discoverable skill library in-repo using YAML frontmatter plus Markdown, with at least:
  - one skill for safe execution of the main loop,
  - one skill for `init`,
  - one skill for log inspection with `tail`,
  - one skill for session discovery with `ls`,
  - one skill for schema introspection.
- The docs must explicitly instruct agents to:
  - start with `ralph-loop schema`,
  - prefer `--dry-run` before mutating commands,
  - prefer `--fields` and narrow selectors on read commands,
  - use `--output json` or `--output ndjson` instead of text.

---

## Phase 1: Setup Agent

Spawn a Codex agent via app-server with the following task prompt. This agent runs once, not in a loop. It initializes the environment using `ralph-loop init` and then creates an execution plan.

### Setup agent prompt

```
You are the setup agent for an automated coding loop. Prepare the environment and create an execution plan for the following task:

Task: {user_prompt}

Do the following steps in order:

1. **Initialize the environment**: Run `./ralph-loop init --base-branch {base_branch} --work-branch {work_branch}`. This command creates or reuses the worktree, installs dependencies, verifies the build, prepares runtime directories, and outputs JSON to stdout. Capture and verify it succeeds. If it fails, diagnose the error and retry once. If it fails again, stop and report the error.

2. **Explore the codebase**: Read `AGENTS.md`, `ARCHITECTURE.md`, and any relevant docs to understand the project structure and conventions.

3. **Create execution plan**: Read `docs/PLANS.md` for the plan template. Create a new plan file at `docs/exec-plans/active/{slugified_task_name}.md` with the following structure:
   - **Goal / scope**: What the task accomplishes
   - **Background**: Why this work is needed
   - **Milestones**: Break the task into 3-7 concrete milestones. Each milestone should be completable in a single agent iteration.
   - **Current progress**: Mark all milestones as "not started"
   - **Key decisions**: Leave empty for now (the coding agent will fill this in)
   - **Remaining issues / open questions**: Any blockers or unknowns you identified during exploration
   - **Links to related documents**: Link to relevant docs discovered during exploration

4. **Commit the plan**: Stage and commit the plan file with message `plan: {short_task_description}`.

5. **Output the plan file path**: Print the absolute path to the plan file.

Output <promise>COMPLETE</promise> when done.
```

### Setup agent behavior

- Start a Codex app-server process via stdio.
- Send `initialize` → `initialized` → `thread/start` → `turn/start` with the setup prompt.
- Stream events and wait for `turn/completed`.
- Parse the plan file path from the agent's output.
- Parse the `ralph-loop init` success JSON to obtain the worktree path, worktree ID, work branch, base branch, and runtime root for later phases.
- If the agent fails or the turn completes without `<promise>COMPLETE</promise>`, abort with an error.

---

## Phase 2: Coding Agent Loop

Spawn a new Codex thread for the coding agent. This thread persists across iterations — each iteration is a new turn on the same thread.

### Coding agent prompt (same prompt every iteration)

The same prompt is sent on every turn. The agent reads the plan file each time to determine current progress and what to do next.

```
You are a coding agent working in an automated loop. You will iterate until the task is fully complete.

## Task
{user_prompt}

## Execution plan
Read the plan at `{plan_file_path}` to understand the milestones and current progress. Pick up where the last iteration left off.

## Rules
- **One milestone per iteration.** Complete exactly one milestone, then stop. Do not work on multiple milestones in a single iteration. This keeps commits focused, progress trackable, and makes it easy to revert a single unit of work.
- Work through the milestones in the plan sequentially.
- After completing the milestone, update the plan file:
  - Mark the completed milestone as "done"
  - Update "current progress" with what you accomplished
  - Add any key decisions you made
  - Note any remaining issues
- Stage and commit ALL your changes (code + updated plan) with a descriptive commit message.
- If you encounter a blocker you cannot resolve, document it in the plan under "remaining issues" and commit what you have.

## Completion signal
When ALL milestones in the plan are complete and the task is fully done:
- Do a final update of the plan marking everything complete
- Commit all remaining changes
- Output <promise>COMPLETE</promise>

If there is still work to do after this iteration, do NOT output <promise>COMPLETE</promise>. You will get another iteration.
```

### Loop driver logic

The loop sends the exact same prompt on every iteration. The plan file on disk is the source of truth for progress — the agent reads it at the start of each turn to know where to pick up.

```
thread_id = start_thread()
iteration = 0
prompt = coding_agent_prompt  # same prompt every time

while iteration < max_iterations:
    iteration += 1

    turn_id = start_turn(thread_id, prompt)
    result = wait_for_turn_completion(thread_id, turn_id)

    if turn_failed(result):
        if iteration < max_iterations:
            # Send a recovery turn
            start_turn(thread_id, "The previous iteration failed. Check git status, review any errors, and try again. If the build is broken, fix it first.")
            continue
        else:
            abort("Max iterations reached with failures")

    agent_output = extract_agent_messages(result)

    if "<promise>COMPLETE</promise>" in agent_output:
        break

if iteration >= max_iterations:
    warn("Reached max iterations without completion signal")
```

### Key behaviors

- **Same thread, multiple turns**: Use a single Codex thread for the entire coding loop. Each iteration is a `turn/start` on the same `threadId`. This preserves conversation context across iterations.
- **Commit per iteration**: The agent is instructed to commit at the end of each iteration. The loop driver should verify a commit was made (check `git log`) and warn if the agent forgot.
- **Completion detection**: Scan the agent's text output (from `agentMessage` items in `item/completed` events) for the literal string `<promise>COMPLETE</promise>`.
- **Failure recovery**: If a turn completes with `status: "failed"`, send a recovery prompt on the next iteration rather than immediately aborting.
- **Context compaction**: If the thread approaches context limits (watch for `ContextWindowExceeded` errors), call `thread/compact/start` before the next turn.

---

## Phase 3: PR Agent

After the coding loop completes, spawn a separate Codex agent to create the pull request.

### PR agent prompt

```
You are a PR agent. Your job is to create a well-structured pull request from the work done on this branch.

## Instructions

1. **Read the completed plan**: Read `{plan_file_path}` to understand what was accomplished.

2. **Review the commits**: Run `git log {base_branch}..HEAD --oneline` to see all commits made during the coding loop.

3. **Review the diff**: Run `git diff {base_branch}...HEAD --stat` to see the scope of changes.

4. **Move the plan**: Move the plan from `docs/exec-plans/active/` to `docs/exec-plans/completed/`. Commit this move.

5. **Create the pull request**: Use `gh pr create` with:
   - **Title**: A concise title (under 70 characters) summarizing the change
   - **Body**: Use this format:
     ```
     ## Summary
     <2-4 bullet points summarizing what was done>

     ## Milestones completed
     <list from the plan>

     ## Key decisions
     <from the plan>

     ## Test plan
     <how to verify the changes>

     🤖 Generated with Ralph Loop
     ```

6. **Enable auto-merge**: Run `gh pr merge --auto --squash` to enable auto-merge. This tells GitHub to merge the PR automatically once all required status checks pass.

7. **Wait for CI and merge**: Poll the PR status until it is merged or fails:
   - Run `gh pr checks <pr_number> --watch` to wait for all CI checks to complete.
   - If all checks pass, auto-merge will complete automatically. Verify with `gh pr view <pr_number> --json state -q '.state'` — it should be `MERGED`.
   - If any check fails, report the failing check names and their output. Do NOT force-merge. Instead, output the failure details so a human or follow-up loop can address them.

8. **Output the PR URL and final status** (merged, or pending with failure details).

Output <promise>COMPLETE</promise> when done.
```

### PR agent behavior

- Start a new Codex app-server thread (separate from the coding thread).
- Send the PR prompt as a single turn.
- Wait for completion.
- Extract and return the PR URL from the agent's output.

---

## Codex App-Server Integration

The script communicates with Codex via the app-server stdio protocol. Reference `docs/references/codex-app-server-llm.txt` for the full API.

### Connection lifecycle

```
1. Spawn: codex app-server (stdio transport)
2. Send:  { "method": "initialize", "id": 0, "params": { "clientInfo": { "name": "ralph_loop", "title": "Ralph Loop", "version": "1.0.0" } } }
3. Send:  { "method": "initialized", "params": {} }
4. Send:  { "method": "thread/start", "id": 1, "params": { "model": "<model>", "cwd": "<worktree_path>", "approvalPolicy": "<policy>", "sandbox": "<sandbox>" } }
5. Read:  thread/started notification → extract threadId
6. Send:  { "method": "turn/start", "id": 2, "params": { "threadId": "<id>", "input": [{ "type": "text", "text": "<prompt>" }] } }
7. Read:  Stream item/* and turn/* notifications until turn/completed
8. Repeat step 6-7 for each iteration
```

Important wire-format note:

- `thread/start` uses the simple `sandbox` enum string, not an object.
- The accepted current values are `read-only`, `workspace-write`, and `danger-full-access`.
- Do not send camelCase names like `workspaceWrite` on the wire.
- The richer object form with `writableRoots` and `networkAccess` belongs to `turn/start` as `sandboxPolicy`, not to `thread/start`.

### Reading agent output

To detect `<promise>COMPLETE</promise>`, collect text from `agentMessage` items:

- On `item/completed` where `item.type == "agentMessage"`, read `item.text`.
- Concatenate all agent message texts from the turn.
- Search for the literal string `<promise>COMPLETE</promise>`.

### Sandbox policy

Use `workspace-write` with the worktree path as the writable root:

```json
{
  "type": "workspace-write",
  "writableRoots": ["<worktree_path>"],
  "networkAccess": true
}
```

Compatibility note:

- If the CLI accepts legacy aliases such as `readOnly`, `workspaceWrite`, or `dangerFullAccess`, normalize them before sending JSON-RPC requests.
- On the wire, always emit the kebab-case variants accepted by app-server.

### Error handling

- If `turn/completed` has `status: "failed"` with `codexErrorInfo: "ContextWindowExceeded"`, call `thread/compact/start` and retry.
- If app-server process exits unexpectedly, restart it and resume the thread with `thread/resume`.
- If a turn takes longer than a per-turn timeout (configurable, default 2 hours), send `turn/interrupt` and retry.

---

## Implementation Notes

### Implementation structure

Implement Ralph Loop in Go. The built harness in this repository landed on a Go Ralph Loop with a repo-root shim, and that is the only version this spec now considers.

Prefer this split:

```text
./ralph-loop                 # repo-root shim; stable operator entrypoint
cmd/ralph-loop/              # active CLI entrypoint
internal/ralphloop/          # reusable orchestration, client, logging, tail/ls helpers
```

Use the provided Go reference implementation under:

- `references/cmd/ralph-loop/`
- `references/internal/ralphloop/`
- `references/ralph-loop`

Copy those into the matching repository paths, then adapt them to the target repository's Go module path, prompts, verification flow, and CI wiring.

### Core modules

Keep the Go implementation split into focused modules:

- CLI parsing and option normalization
- Codex app-server JSON-RPC client
- setup-agent orchestration
- coding-loop orchestration
- PR-agent orchestration
- completion detection
- worktree/init integration
- log tailing and active-session listing if you expose operational subcommands

### The app-server client

This is the core integration. It should:

- Spawn `codex app-server` as a child process with stdio transport.
- Send JSON-RPC messages as newline-delimited JSON to stdin.
- Read JSON-RPC messages line-by-line from stdout.
- Track request IDs and resolve promises on matching responses.
- Emit events for notifications (`turn/started`, `item/completed`, etc.).
- Handle the full lifecycle: initialize → thread/start → turn/start → stream → turn/completed.

The built harness also benefited from:

- sandbox alias normalization before sending JSON-RPC requests,
- explicit handling for `ContextWindowExceeded` via `thread/compact/start`,
- per-turn timeout handling with `turn/interrupt`,
- idempotent client shutdown so late stdout/stderr does not crash the runner.

### Worktree management

- `ralph-loop init` is the canonical environment-preparation entrypoint.
- The loop driver parses the JSON output from `ralph-loop init` and sets `cwd` for subsequent Codex threads.
- Worktree-derived runtime paths must live under `.worktree/<worktree_id>/`.
- Clean up the worktree after the PR is created, unless `--preserve-worktree` is set for debugging.

### Logging

- Log all Codex events to `.worktree/<worktree_id>/logs/ralph-loop.log`.
- Print high-level status to stdout in `text` mode: phase transitions, iteration counts, commit hashes, completion signal, PR URL.
- In `json` and `ndjson` modes, write structured stdout instead of human log lines.
- On failure, print the last N lines of the log for debugging.
- Treat log writes as best-effort only; logging must never crash the loop runner.

### Output architecture

- Implement a shared renderer for `text`, `json`, and `ndjson` so every command uses the same output contract.
- Do not require environment variables for structured stdout. Environment-gated debug event streams are acceptable as internal diagnostics, but the user-facing interface must be `--output`.
- In `ndjson` mode, emit each record on a single line with no prefixes or wrappers.
- In `json` mode, buffer command progress internally and emit exactly one final object.
- Keep stderr human-oriented only in `text` mode. In machine-readable modes, encode errors in stdout JSON and keep stderr empty unless the process itself cannot initialize.

### Integration

- Keep `./ralph-loop` as the documented command even if the underlying implementation is `go run` during early bring-up.

---

## Deliverables

- [ ] `cmd/ralph-loop/main.go` active CLI entrypoint
- [ ] `internal/ralphloop/` package with reusable orchestration modules
- [ ] Stable repo-root `./ralph-loop` shim
- [ ] `ralph-loop init` subcommand with the documented JSON stdout contract
- [ ] Worktree-aware boot architecture and automation-safe boot command contract
- [ ] App-server stdio JSON-RPC client
- [ ] Setup-agent orchestration
- [ ] Coding-loop orchestration with completion detection
- [ ] PR-agent orchestration
- [ ] Worktree/init integration that parses the `ralph-loop init` JSON contract
- [ ] Structured log file under `.worktree/<id>/logs/ralph-loop.log`
- [ ] Shared `--output text|json|ndjson` contract across `main`, `ls`, and `tail`
- [ ] Structured JSON errors in machine-readable modes
- [ ] Tests for CLI parsing, app-server client behavior, completion detection, and prompt construction
- [ ] Tests covering `json` and `ndjson` output contracts, including non-TTY defaulting

---

## Formal Verification with TLA+

The spec ships with TLA+ models under `spec/references/tla/` that formally verify critical safety and liveness properties of the Ralph Loop workflow. Implementations must satisfy the properties defined in these models.

### Models

| Module | Scope | What it verifies |
|--------|-------|------------------|
| `RalphLoopMain` | Three-phase workflow | Phase ordering (idle → init → setup → coding → pr → completed\|failed), termination guarantee, iteration bounds, PR requires completion signal |
| `CodingLoop` | Inner coding loop | Turn lifecycle, context overflow triggers compaction, recovery prompt after failure, same thread across all iterations, loop always terminates |
| `ConcurrentSessions` | Multi-session isolation | No two sessions share a worktree or port, running sessions always hold resources, terminated sessions leak no resources |

### Key properties

Safety (nothing bad happens):

- **Phase ordering**: phases only transition forward; preconditions (worktree ready, plan exists) are satisfied before entering each phase.
- **Iteration bound**: iteration count never exceeds `MaxIterations`.
- **PR requires completion**: a pull request is only created after the coding loop signals `<promise>COMPLETE</promise>`.
- **Worktree exclusion**: no two concurrent sessions share the same worktree slot or port.
- **Same thread**: the coding loop uses exactly one Codex thread across all iterations.
- **No resource leak**: terminated or failed sessions release all claimed worktrees and ports.

Liveness (good things eventually happen):

- **Termination**: every workflow run eventually reaches `completed` or `failed`.
- **Coding loop ends**: the coding phase always terminates — via completion signal, max iterations, or consecutive failure cap.
- **Compaction resolves**: context window overflow triggers compaction, which always completes.
- **Turn always ends**: every started Codex turn reaches a terminal state (completed, failed, or timed out).
- **Resources freed**: all worktree slots and ports are eventually released.

### Running the model checker

```bash
brew install tlaplus
cd spec/references/tla
tlc RalphLoopMain.tla
tlc CodingLoop.tla
tlc ConcurrentSessions.tla
```

Each `.cfg` file uses small constants to keep model checking fast. Increase constants to explore deeper state spaces when needed.

### Verification against implementation

When implementing Ralph Loop, verify that:

1. The state machine in the orchestrator matches the phase transitions in `RalphLoopMain`.
2. The coding loop driver matches the turn lifecycle, failure recovery, and compaction behavior in `CodingLoop`.
3. Session registration and worktree/port allocation match the resource exclusion guarantees in `ConcurrentSessions`.
4. All safety invariants hold as runtime assertions or are structurally guaranteed by the code.
5. All liveness properties are ensured by bounded loops, timeouts, and resource cleanup in defer/finally blocks.

---

## Verification

1. Run `ralph-loop init --base-branch main --work-branch ralph-init-smoke` from outside the repo root and confirm it succeeds.
2. Confirm `ralph-loop init` returns valid JSON on stdout and sends progress logs to stderr only.
3. Confirm running `ralph-loop init` twice on the same worktree is safe and returns the same `worktree_id` and `runtime_root`.
4. Confirm the boot command for the current worktree returns app metadata, blocks until healthy, and isolates ports and runtime files per worktree.
5. Run `ralph-loop "Add a health check endpoint that returns { status: 'ok', uptime: <seconds> }"` on the repository.
6. Confirm the setup agent creates or reuses a clean worktree, installs deps, verifies the build, and produces a plan.
7. Confirm the coding agent iterates, commits per iteration, and eventually outputs `<promise>COMPLETE</promise>`.
8. Confirm the PR agent opens a well-formed PR with the plan summary.
9. Confirm the worktree is cleaned up after completion.
10. Confirm `ralph-loop ls --output json` returns valid JSON.
11. Confirm `ralph-loop tail --follow --output ndjson` emits one JSON object per line.
12. Confirm piping `ralph-loop "<prompt>"` without `--output` defaults to structured JSON rather than text.

---

## Key Principles

- **One milestone per iteration.** The agent completes exactly one milestone per loop turn. This keeps commits atomic, diffs reviewable, and makes it trivial to revert a single unit of work.
- **The agent works until it's done.** The loop only breaks on `<promise>COMPLETE</promise>` or the safety cap.
- **Every iteration leaves a commit.** Progress is never lost, and the PR agent can reconstruct the full story from the commit history.
- **The plan is the shared state.** The plan file is the coordination artifact between the setup agent, coding agent, and PR agent. It's also a durable record in the repository.
- **Same thread for the coding loop.** Using one thread with multiple turns preserves context and lets the agent build on its own prior work.
- **Separate threads for separate roles.** Setup, coding, and PR agents each get their own thread to keep concerns isolated.
