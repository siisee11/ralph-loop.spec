# Ralph Loop Spec

Build a Go CLI that realizes the Ralph Wigum policy from Loop State Machine for a repository-oriented coding workflow. The CLI uses Codex app-server (via stdio JSON-RPC) to spawn and control agents, but the workflow semantics come from LSM rather than from a Ralph Loop-specific state machine.

## Prerequisite: Loop State Machine

`ralph-loop.spec` is downstream of `loop-state-machine.spec` and must be implemented on top of LSM's engine-policy contract.

- Canonical upstream spec: `https://github.com/siisee11/loop-state-machine.spec`
- Local checkout used while authoring this spec: `~/git/loop-state-machine.spec/`
- Normative policy reference: `lsm_policy_ralph_wigum.qnt`
- Normative engine contract reference: `lsm_engine.qnt`

This repository specifies Ralph-specific host behavior on top of LSM:

- Git worktree preparation
- Repository plan-file conventions
- Codex prompt construction
- Delivery and review markers
- Pull-request automation
- Operational commands such as `init`, `ls`, `tail`, and `schema`

## Overview

Ralph Loop must follow the Ralph Wigum phase order defined by LSM:

1. **Init** — Prepare or reuse the worktree by running `ralph-loop init`. Planning must not begin before this phase succeeds.
2. **Planning** — Explore the codebase and create the execution plan. Implementing must not begin before a plan exists.
3. **Implementing** — Iterate the implementation agent with one milestone per turn. This phase may `continue`, `retry`, `compact`, or `advance_phase` to review when delivery is complete.
4. **Review** — Validate the completed delivery, move the plan, open or update the PR, and only then signal final completion.

```
┌─────────────────────────────────────────────────────────────────┐
│                         Ralph Loop                              │
│                                                                 │
│  idle → init → planning → implementing → review → completed     │
│                                                                 │
│  init: run `ralph-loop init`                                    │
│  planning: create committed plan                                │
│  implementing: one milestone + one commit per iteration         │
│  review: validate delivery, create/update PR, then complete     │
└─────────────────────────────────────────────────────────────────┘
```

LSM directive mapping in this host:

- `advance_phase`: init → planning, planning → implementing, implementing → review
- `continue`: keep iterating inside implementing on the current thread
- `retry`: run a recovery turn inside implementing after a failed turn
- `compact`: compact the implementation thread when context is exhausted
- `complete`: review accepted the delivery and the host may terminate successfully
- `wait`: reserved for external blocking cases such as login or explicit operator input

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
  --max-iterations <n>     Safety cap on implementation iterations (default: 20)
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
- `ralph-loop "<prompt>" --output json` returns a single object describing the run result, including final status, current or final phase, worktree metadata, iteration count, plan path, PR URL if created, and any structured error.
- `ralph-loop "<prompt>" --output ndjson` streams lifecycle events as they happen and ends with a terminal event.
- `ralph-loop ls --output json` returns a JSON array of running sessions.
- `ralph-loop ls --output ndjson` emits one session object per line.
- `ralph-loop tail --output json` returns the selected log metadata plus an array of lines or parsed records.
- `ralph-loop tail --follow --output ndjson` emits one structured log or event record per line.

Example NDJSON events:

```json
{"command":"main","event":"run.started","status":"running","ts":"2026-03-15T12:00:00Z","worktree_path":"/repo/.worktrees/ralph-foo","work_branch":"ralph-foo"}
{"command":"main","event":"phase.started","phase":"planning","status":"running","ts":"2026-03-15T12:00:01Z"}
{"command":"main","event":"iteration.completed","phase":"implementing","iteration":1,"status":"ok","commit":"abc1234","ts":"2026-03-15T12:03:10Z"}
{"command":"main","event":"phase.started","phase":"review","status":"running","ts":"2026-03-15T12:12:00Z"}
{"command":"main","event":"run.completed","phase":"review","status":"completed","iterations":3,"plan_path":"/repo/docs/exec-plans/completed/foo.md","pr_url":"https://github.com/org/repo/pull/123","ts":"2026-03-15T12:14:22Z"}
```

Example structured error:

```json
{
  "command": "main",
  "status": "failed",
  "error": {
    "code": "planning_failed",
    "message": "planning phase completed without producing a committed execution plan"
  }
}
```

---

## Worktree-Aware Boot Architecture

The repository must expose a worktree-aware app boot flow that lets the active implementation thread launch one isolated app instance per worktree or change.

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

Ralph Loop depends on a stable boot contract so the implementation and review phases can launch exactly one app instance per worktree when needed.

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

## Phase 1: Init

The init phase is Ralph Loop's concrete realization of the LSM `init` phase.
It prepares the worktree and runtime contract before any planning turn starts.

### Init phase behavior

- Run `./ralph-loop init --base-branch {base_branch} --work-branch {work_branch}` directly from the host.
- Parse the success JSON to obtain the worktree path, worktree ID, work branch, base branch, and runtime root.
- If init fails, diagnose and retry once only if the failure is clearly recoverable.
- Planning must not begin until init succeeds.
- A successful init issues the LSM-equivalent `advance_phase` into planning.

---

## Phase 2: Planning

Spawn a fresh Codex thread for planning. This thread is distinct from the implementation thread.

### Planning prompt

```
You are the planning agent for a Ralph Loop run that follows the LSM Ralph Wigum policy.

## Task
{user_prompt}

## Context
- The worktree is already initialized at `{worktree_path}`.
- Your job in this phase is to understand the repository, create the execution plan, and stop.
- Do not start implementing milestones in this phase.

## Instructions

1. Read `AGENTS.md`, `ARCHITECTURE.md`, and any relevant repository docs.
2. Read `docs/PLANS.md` if it exists and follow the local plan template.
3. Create `docs/exec-plans/active/{slugified_task_name}.md` with:
   - Goal / scope
   - Background
   - Milestones
   - Current progress
   - Key decisions
   - Remaining issues / open questions
   - Links to related documents
4. Break the work into 3-7 milestones. Each milestone must be completable in a single implementation iteration.
5. Mark every milestone as `not started`.
6. Stage and commit the new plan with message `plan: {short_task_description}`.
7. Print the absolute plan path.

Output <promise>PLAN_READY</promise> when the committed plan is ready for implementation.
```

### Planning behavior

- Start a Codex app-server thread and send the planning prompt as one turn.
- Parse the plan file path from the agent output.
- Require a committed plan before advancing.
- If the turn fails or ends without `<promise>PLAN_READY</promise>`, abort with an error.
- A successful planning turn issues the LSM-equivalent `advance_phase` into implementing.

---

## Phase 3: Implementing

Spawn a new Codex thread for the implementation loop. This thread persists across iterations; each iteration is a new turn on the same thread until the policy advances to review.

### Implementation prompt

The same prompt is sent on every implementation turn. The plan file on disk is the source of truth for progress.

```
You are the implementation agent for a Ralph Loop run that follows the LSM Ralph Wigum policy.

## Task
{user_prompt}

## Execution plan
Read the plan at `{plan_file_path}` to determine the next milestone and current progress.

## Rules
- Complete exactly one milestone per iteration.
- Work through milestones sequentially unless the plan explicitly says otherwise.
- After finishing the milestone, update the plan:
  - mark the milestone as `done`
  - update current progress
  - record key decisions
  - record remaining issues or follow-ups
- Stage and commit all changes from the iteration, including plan updates.
- If you hit a blocker you cannot fully resolve, document it in the plan and commit the best safe progress you can.

## Delivery gating
- Do not declare the run complete from this phase.
- When all planned implementation work is done and the branch is ready for review, output <promise>DELIVERY_COMPLETE</promise>.
- If more work remains after this iteration, do not output the delivery marker.
```

### Implementation loop driver

```
thread_id = start_thread()
iteration = 0
prompt = implementation_prompt

while iteration < max_iterations:
    iteration += 1

    turn_id = start_turn(thread_id, prompt)
    result = wait_for_turn_completion(thread_id, turn_id)

    if turn_failed(result):
        if iteration < max_iterations:
            start_turn(thread_id, "The previous implementation iteration failed. Check git status, inspect errors, repair the workspace, and continue from the plan.")
            continue
        abort("Max iterations reached during implementing")

    agent_output = extract_agent_messages(result)

    if "<promise>DELIVERY_COMPLETE</promise>" in agent_output:
        advance_phase("review")
        break

if iteration >= max_iterations:
    warn("Reached max iterations before delivery was ready for review")
```

### Implementation behaviors

- **Same thread, multiple turns**: The implementing phase reuses a single Codex thread across iterations.
- **Commit per iteration**: Each iteration must leave a new commit or an explicit warning record if the agent failed to create one.
- **Delivery detection**: Scan `agentMessage` items for the literal string `<promise>DELIVERY_COMPLETE</promise>`.
- **Failure recovery**: A failed implementation turn maps to LSM `retry` and queues a recovery turn rather than aborting immediately.
- **Context compaction**: If the thread hits `ContextWindowExceeded`, call `thread/compact/start` and continue inside the same phase.
- **Review boundary**: Implementation may only advance the run to review; it must never emit final completion.

---

## Phase 4: Review

After implementation signals delivery readiness, spawn a separate review thread. Review is the only phase allowed to complete the run.

### Review prompt

```
You are the review agent for a Ralph Loop run that follows the LSM Ralph Wigum policy.

## Instructions

1. Read the completed plan at `{plan_file_path}`.
2. Review the commits with `git log {base_branch}..HEAD --oneline`.
3. Review the diff with `git diff {base_branch}...HEAD --stat`.
4. Verify the implementation is actually review-ready. If it is not, explain the gap and do not emit the completion marker.
5. Move the plan from `docs/exec-plans/active/` to `docs/exec-plans/completed/`. Commit that move.
6. Create or update the pull request with:
   - Title: concise and under 70 characters
   - Body:
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
7. Enable auto-merge with `gh pr merge --auto --squash`.
8. Wait for CI and report whether the PR merged cleanly or is pending because of failing checks.
9. Print the PR URL and final review status.

Output <promise>COMPLETE</promise> only when review is satisfied and the phase is ready to terminate the run successfully.
```

### Review behavior

- Start a fresh Codex thread for review rather than reusing the implementation thread.
- Send the review prompt as a single turn.
- Extract and return the PR URL from the agent output.
- Final completion is valid only when review emits `<promise>COMPLETE</promise>`.
- If review finds unresolved delivery gaps, the host may fail the run or route back into implementing only if the LSM policy state is explicitly updated to do so.

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
8. Repeat step 4-7 as needed for planning, implementation turns, and review
```

Important wire-format note:

- `thread/start` uses the simple `sandbox` enum string, not an object.
- The accepted current values are `read-only`, `workspace-write`, and `danger-full-access`.
- Do not send camelCase names like `workspaceWrite` on the wire.
- The richer object form with `writableRoots` and `networkAccess` belongs to `turn/start` as `sandboxPolicy`, not to `thread/start`.

### Reading control markers

Collect text from `agentMessage` items and let the host map markers to LSM directives:

- On `item/completed` where `item.type == "agentMessage"`, read `item.text`.
- Concatenate all agent message texts from the turn.
- During planning, search for `<promise>PLAN_READY</promise>`.
- During implementing, search for `<promise>DELIVERY_COMPLETE</promise>`.
- During review, search for `<promise>COMPLETE</promise>`.

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

### Core modules

Keep the Go implementation split into focused modules:

- CLI parsing and option normalization
- Codex app-server JSON-RPC client
- LSM policy state and directive mapping
- planning orchestration
- implementation-loop orchestration
- review orchestration
- control-marker detection
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
- Clean up the worktree after review reaches its terminal state, unless `--preserve-worktree` is set for debugging.

### Logging

- Log all Codex events to `.worktree/<worktree_id>/logs/ralph-loop.log`.
- Print high-level status to stdout in `text` mode: phase transitions, iteration counts, commit hashes, delivery marker, completion marker, and PR URL.
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
- [ ] LSM Ralph policy state machine integration
- [ ] Planning orchestration
- [ ] Implementation-loop orchestration with delivery detection
- [ ] Review orchestration with terminal completion detection
- [ ] Worktree/init integration that parses the `ralph-loop init` JSON contract
- [ ] Structured log file under `.worktree/<id>/logs/ralph-loop.log`
- [ ] Shared `--output text|json|ndjson` contract across `main`, `ls`, and `tail`
- [ ] Structured JSON errors in machine-readable modes
- [ ] Tests for CLI parsing, app-server client behavior, control-marker detection, and prompt construction
- [ ] Tests covering `json` and `ndjson` output contracts, including non-TTY defaulting

---

## Formal Verification with LSM

The normative workflow semantics for Ralph Loop come from LSM's Quint models, not from a Ralph Loop-specific phase machine.

### Normative upstream models

| Module | Source | What it verifies |
|--------|--------|------------------|
| `lsm_engine.qnt` | `loop-state-machine.spec` | Directive contract, turn lifecycle, waiting / compaction / retry mechanics, thread reuse semantics |
| `lsm_policy_ralph_wigum.qnt` | `loop-state-machine.spec` | Ralph phase ordering (`idle → init → planning → implementing → review → completed|failed`), delivery-before-review, explicit completion in review only |

### Supplementary local models

The TLA+ models under `spec/references/tla/` remain useful as local reference material for implementation details such as resource isolation and worktree management, but they are informative only. If a local model disagrees with LSM's Ralph policy, the LSM model wins.

### Required properties

Safety:

- **Phase ordering**: planning requires successful init, implementing requires a committed plan, review requires delivery completion.
- **Explicit review completion**: final success is only valid when review emits `<promise>COMPLETE</promise>`.
- **Implementation cannot terminate the run**: `<promise>DELIVERY_COMPLETE</promise>` may advance to review but must not end the run directly.
- **Iteration bound**: implementation iteration count never exceeds `MaxIterations`.
- **Thread policy**: implementation reuses its thread across iterations until the policy changes phase or compacts the thread.
- **Worktree exclusion**: concurrent sessions must not share worktree slots or ports.

Liveness:

- **Implementation eventually leaves the phase**: via delivery readiness, policy failure, or iteration exhaustion.
- **Compaction resolves**: context overflow triggers compaction and the run returns to a runnable or terminal state.
- **Review is terminally decisive**: review either completes the run or reports a non-completable outcome.
- **Resources freed**: terminated runs eventually release claimed worktrees and ports.

### Model checking

When validating the policy semantics, run the Quint models from the upstream repository checkout first. Use the local TLA+ models only as supplementary checks for Ralph-specific host behavior.

### Verification against implementation

When implementing Ralph Loop, verify that:

1. The orchestrator's phase transitions match `lsm_policy_ralph_wigum.qnt`.
2. The host honors LSM directives rather than inferring workflow meaning from raw turn status alone.
3. The implementation loop matches LSM retry and compaction behavior.
4. Review is the only phase that can convert a successful delivery into terminal completion.
5. Local worktree and session management still satisfy the supplementary isolation guarantees in `spec/references/tla/`.

---

## Verification

1. Run `ralph-loop init --base-branch main --work-branch ralph-init-smoke` from outside the repo root and confirm it succeeds.
2. Confirm `ralph-loop init` returns valid JSON on stdout and sends progress logs to stderr only.
3. Confirm running `ralph-loop init` twice on the same worktree is safe and returns the same `worktree_id` and `runtime_root`.
4. Confirm the boot command for the current worktree returns app metadata, blocks until healthy, and isolates ports and runtime files per worktree.
5. Run `ralph-loop "Add a health check endpoint that returns { status: 'ok', uptime: <seconds> }"` on the repository.
6. Confirm init completes before planning begins, and that planning produces and commits a plan before implementing starts.
7. Confirm the implementation agent iterates, commits per iteration, and only advances by emitting `<promise>DELIVERY_COMPLETE</promise>`.
8. Confirm the review agent opens or updates a well-formed PR with the plan summary and is the only phase that emits `<promise>COMPLETE</promise>`.
9. Confirm the worktree is cleaned up after the review phase reaches its terminal state.
10. Confirm `ralph-loop ls --output json` returns valid JSON.
11. Confirm `ralph-loop tail --follow --output ndjson` emits one JSON object per line.
12. Confirm piping `ralph-loop "<prompt>"` without `--output` defaults to structured JSON rather than text.

---

## Key Principles

- **One milestone per iteration.** The agent completes exactly one milestone per loop turn. This keeps commits atomic, diffs reviewable, and makes it trivial to revert a single unit of work.
- **LSM owns workflow meaning.** Ralph Loop must not invent a competing local phase machine when LSM already defines the Ralph policy.
- **Implementation is not completion.** The implementation loop stops only by advancing to review, not by declaring terminal success.
- **Every iteration leaves a commit.** Progress is never lost, and the review phase can reconstruct the full story from the commit history.
- **The plan is the shared state.** The plan file coordinates planning, implementing, and review, and remains the durable artifact in the repository.
- **Same thread for implementation.** Using one thread with multiple turns preserves context inside the implementing phase.
- **Separate threads for separate phases.** Planning, implementing, and review each get their own thread boundaries when the policy changes phase.
