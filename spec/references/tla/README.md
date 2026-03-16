# Ralph Loop TLA+ Specifications

Formal models of the Ralph Loop workflow for verification with the TLC model checker.

## Models

| Module | What it models | Key properties verified |
|--------|---------------|------------------------|
| `RalphLoopMain` | Three-phase workflow (init → setup → coding → pr → done) | Phase ordering, termination, iteration bounds, PR-requires-completion |
| `CodingLoop` | Inner coding loop with turn lifecycle, context overflow, recovery | Turn always ends, compaction resolves overflow, same-thread invariant |
| `ConcurrentSessions` | Multiple ralph-loop sessions sharing worktrees and ports | Worktree exclusion, port exclusion, no resource leaks |

## Running

```bash
# Install TLA+ tools
brew install tlaplus

# Run model checker
cd spec/references/tla
tlc RalphLoopMain.tla
tlc CodingLoop.tla
tlc ConcurrentSessions.tla
```

Or with the TLA+ VS Code extension: open any `.tla` file and run "TLA+: Check model".

## Tuning state space

Each `.cfg` file uses small constants (e.g., `MaxIterations = 5`) to keep model checking fast. To explore deeper:

```
# In RalphLoopMain.cfg
MaxIterations = 10        # more iterations (slower)
MaxFailuresPerPhase = 3   # more failure paths
```

Larger constants exponentially increase state space. Start small, increase only when needed.
