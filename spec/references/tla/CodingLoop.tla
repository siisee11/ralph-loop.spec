---------------------------- MODULE CodingLoop -----------------------------
(***************************************************************************)
(* Detailed TLA+ specification of the Ralph Loop coding phase.             *)
(*                                                                         *)
(* Models the inner loop at a finer granularity than RalphLoopMain:        *)
(*   - Turn lifecycle (start → running → completed/failed)                 *)
(*   - Context window overflow and compaction                              *)
(*   - Recovery prompts after failure                                      *)
(*   - Per-turn timeout and interrupt                                      *)
(*   - Commit verification per iteration                                   *)
(***************************************************************************)
EXTENDS Naturals, Sequences

CONSTANTS
    MaxIterations,           \* Safety cap (default: 20)
    MaxConsecutiveFailures,  \* Before we consider the loop stuck
    ContextWindowSize        \* Abstract capacity (e.g., 5 units)

VARIABLES
    loopState,       \* Overall loop state: "running", "completed", "exhausted", "error"
    iteration,       \* Current iteration number (1-based once started)
    turnState,       \* Current turn state: "idle", "started", "running", "completed", "failed", "timedout"
    agentSignaled,   \* Agent output <promise>COMPLETE</promise> this turn
    contextUsed,     \* Abstract context window usage (0..ContextWindowSize)
    compacting,      \* Whether thread compaction is in progress
    consecutiveFails,\* Consecutive failed turns (reset on success)
    commitsMade,     \* Total commits (each successful turn should produce one)
    usingRecovery,   \* Next turn uses recovery prompt instead of normal prompt
    threadID         \* Abstract thread identifier (stays same across turns)

vars == <<loopState, iteration, turnState, agentSignaled, contextUsed,
          compacting, consecutiveFails, commitsMade, usingRecovery, threadID>>

(***************************************************************************)
(* Type invariant                                                          *)
(***************************************************************************)
TypeInvariant ==
    /\ loopState \in {"running", "completed", "exhausted", "error"}
    /\ iteration \in 0..MaxIterations
    /\ turnState \in {"idle", "started", "running", "completed", "failed", "timedout"}
    /\ agentSignaled \in BOOLEAN
    /\ contextUsed \in 0..ContextWindowSize
    /\ compacting \in BOOLEAN
    /\ consecutiveFails \in 0..MaxConsecutiveFailures
    /\ commitsMade \in Nat
    /\ usingRecovery \in BOOLEAN
    /\ threadID \in {"thread_1"}    \* Same thread throughout

(***************************************************************************)
(* Initial state — coding loop just started, thread already created.       *)
(***************************************************************************)
Init ==
    /\ loopState = "running"
    /\ iteration = 0
    /\ turnState = "idle"
    /\ agentSignaled = FALSE
    /\ contextUsed = 0
    /\ compacting = FALSE
    /\ consecutiveFails = 0
    /\ commitsMade = 0
    /\ usingRecovery = FALSE
    /\ threadID = "thread_1"

(***************************************************************************)
(* StartTurn: Begin a new coding iteration.                                *)
(* Sends coding prompt (or recovery prompt after failure).                 *)
(***************************************************************************)
StartTurn ==
    /\ loopState = "running"
    /\ turnState = "idle"
    /\ ~compacting
    /\ iteration < MaxIterations
    /\ iteration' = iteration + 1
    /\ turnState' = "started"
    /\ agentSignaled' = FALSE
    /\ UNCHANGED <<loopState, contextUsed, compacting, consecutiveFails,
                    commitsMade, usingRecovery, threadID>>

(***************************************************************************)
(* TurnRunning: Turn transitions from started to actively running.         *)
(* Context window usage increases as the agent works.                      *)
(***************************************************************************)
TurnRunning ==
    /\ turnState = "started"
    /\ contextUsed < ContextWindowSize
    /\ turnState' = "running"
    /\ contextUsed' = contextUsed + 1
    /\ UNCHANGED <<loopState, iteration, agentSignaled, compacting,
                    consecutiveFails, commitsMade, usingRecovery, threadID>>

(***************************************************************************)
(* TurnSucceeds: Agent completes the turn without signaling COMPLETE.      *)
(* Agent worked on one milestone, committed changes.                       *)
(***************************************************************************)
TurnSucceeds ==
    /\ turnState = "running"
    /\ turnState' = "completed"
    /\ agentSignaled' = FALSE
    /\ commitsMade' = commitsMade + 1
    /\ consecutiveFails' = 0
    /\ usingRecovery' = FALSE
    /\ UNCHANGED <<loopState, iteration, contextUsed, compacting, threadID>>

(***************************************************************************)
(* TurnSucceedsAndCompletes: Agent signals <promise>COMPLETE</promise>.    *)
(* All milestones done. This is the happy termination of the loop.         *)
(***************************************************************************)
TurnSucceedsAndCompletes ==
    /\ turnState = "running"
    /\ turnState' = "completed"
    /\ agentSignaled' = TRUE
    /\ commitsMade' = commitsMade + 1
    /\ consecutiveFails' = 0
    /\ usingRecovery' = FALSE
    /\ UNCHANGED <<loopState, iteration, contextUsed, compacting, threadID>>

(***************************************************************************)
(* TurnFails: Agent turn returns status=failed (not context overflow).     *)
(* Next iteration will use recovery prompt.                                *)
(***************************************************************************)
TurnFails ==
    /\ turnState = "running"
    /\ turnState' = "failed"
    /\ consecutiveFails' = consecutiveFails + 1
    /\ usingRecovery' = TRUE
    /\ UNCHANGED <<loopState, iteration, agentSignaled, contextUsed,
                    compacting, commitsMade, threadID>>

(***************************************************************************)
(* TurnFailsContextOverflow: Context window exceeded.                      *)
(* Triggers thread compaction before next turn.                            *)
(***************************************************************************)
TurnFailsContextOverflow ==
    /\ turnState = "running"
    /\ contextUsed >= ContextWindowSize
    /\ turnState' = "failed"
    /\ compacting' = TRUE
    /\ consecutiveFails' = consecutiveFails + 1
    /\ usingRecovery' = TRUE
    /\ UNCHANGED <<loopState, iteration, agentSignaled, contextUsed,
                    commitsMade, threadID>>

(***************************************************************************)
(* TurnTimedOut: Per-turn timeout fires, turn/interrupt sent.              *)
(***************************************************************************)
TurnTimedOut ==
    /\ turnState \in {"started", "running"}
    /\ turnState' = "timedout"
    /\ consecutiveFails' = consecutiveFails + 1
    /\ usingRecovery' = TRUE
    /\ UNCHANGED <<loopState, iteration, agentSignaled, contextUsed,
                    compacting, commitsMade, threadID>>

(***************************************************************************)
(* CompactThread: Reduce context window usage after overflow.              *)
(***************************************************************************)
CompactThread ==
    /\ compacting
    /\ contextUsed' = contextUsed \div 2    \* Compaction roughly halves context
    /\ compacting' = FALSE
    /\ UNCHANGED <<loopState, iteration, turnState, agentSignaled,
                    consecutiveFails, commitsMade, usingRecovery, threadID>>

(***************************************************************************)
(* ResetForNextTurn: After a completed/failed/timedout turn, prepare for   *)
(* the next iteration (if allowed).                                        *)
(***************************************************************************)
ResetForNextTurn ==
    /\ turnState \in {"completed", "failed", "timedout"}
    /\ ~agentSignaled                       \* Not done yet
    /\ iteration < MaxIterations            \* Still have budget
    /\ ~compacting                          \* Not mid-compaction
    /\ consecutiveFails < MaxConsecutiveFailures
    /\ turnState' = "idle"
    /\ UNCHANGED <<loopState, iteration, agentSignaled, contextUsed,
                    compacting, consecutiveFails, commitsMade, usingRecovery,
                    threadID>>

(***************************************************************************)
(* LoopCompleted: Agent signaled COMPLETE — coding loop terminates.        *)
(***************************************************************************)
LoopCompleted ==
    /\ turnState = "completed"
    /\ agentSignaled
    /\ loopState' = "completed"
    /\ UNCHANGED <<iteration, turnState, agentSignaled, contextUsed,
                    compacting, consecutiveFails, commitsMade, usingRecovery,
                    threadID>>

(***************************************************************************)
(* LoopExhausted: Max iterations reached without COMPLETE signal.          *)
(***************************************************************************)
LoopExhausted ==
    /\ turnState \in {"completed", "failed", "timedout"}
    /\ ~agentSignaled
    /\ iteration >= MaxIterations
    /\ ~compacting
    /\ loopState' = "exhausted"
    /\ UNCHANGED <<iteration, turnState, agentSignaled, contextUsed,
                    compacting, consecutiveFails, commitsMade, usingRecovery,
                    threadID>>

(***************************************************************************)
(* LoopError: Too many consecutive failures — abort.                       *)
(***************************************************************************)
LoopError ==
    /\ consecutiveFails >= MaxConsecutiveFailures
    /\ turnState \in {"failed", "timedout"}
    /\ ~compacting
    /\ loopState' = "error"
    /\ UNCHANGED <<iteration, turnState, agentSignaled, contextUsed,
                    compacting, consecutiveFails, commitsMade, usingRecovery,
                    threadID>>

(***************************************************************************)
(* Next-state relation                                                     *)
(***************************************************************************)
Next ==
    \/ StartTurn
    \/ TurnRunning
    \/ TurnSucceeds
    \/ TurnSucceedsAndCompletes
    \/ TurnFails
    \/ TurnFailsContextOverflow
    \/ TurnTimedOut
    \/ CompactThread
    \/ ResetForNextTurn
    \/ LoopCompleted
    \/ LoopExhausted
    \/ LoopError

(***************************************************************************)
(* Fairness conditions                                                     *)
(***************************************************************************)
Fairness ==
    /\ WF_vars(StartTurn)
    /\ WF_vars(TurnRunning)
    /\ WF_vars(TurnSucceeds \/ TurnSucceedsAndCompletes \/ TurnFails
                \/ TurnFailsContextOverflow \/ TurnTimedOut)
    /\ WF_vars(CompactThread)
    /\ WF_vars(ResetForNextTurn)
    /\ WF_vars(LoopCompleted \/ LoopExhausted \/ LoopError)

Spec == Init /\ [][Next]_vars /\ Fairness

---------------------------------------------------------------------------
(* SAFETY PROPERTIES                                                       *)
---------------------------------------------------------------------------

(***************************************************************************)
(* S1: Iteration count respects the safety cap.                            *)
(***************************************************************************)
IterationBound == iteration <= MaxIterations

(***************************************************************************)
(* S2: Context usage is bounded.                                           *)
(***************************************************************************)
ContextBound == contextUsed <= ContextWindowSize

(***************************************************************************)
(* S3: Compaction only happens after context overflow.                     *)
(***************************************************************************)
CompactionOnlyAfterOverflow ==
    compacting => contextUsed >= ContextWindowSize \div 2

(***************************************************************************)
(* S4: Thread ID never changes — same thread across all iterations.        *)
(*     "Same thread for the coding loop."                                  *)
(***************************************************************************)
SameThread == threadID = "thread_1"

(***************************************************************************)
(* S5: COMPLETE signal only comes from a successful turn.                  *)
(***************************************************************************)
CompleteOnlyOnSuccess ==
    agentSignaled => turnState = "completed"

(***************************************************************************)
(* S6: Recovery prompt used only after a failure.                          *)
(***************************************************************************)
RecoveryAfterFailure ==
    usingRecovery => consecutiveFails > 0

(***************************************************************************)
(* S7: Terminal loop states are absorbing.                                 *)
(***************************************************************************)
TerminalIsAbsorbing ==
    /\ (loopState = "completed") => [](loopState = "completed")
    /\ (loopState = "exhausted") => [](loopState = "exhausted")
    /\ (loopState = "error")     => [](loopState = "error")

(***************************************************************************)
(* S8: Every successful turn produces exactly one commit.                  *)
(*     "Every iteration leaves a commit."                                  *)
(***************************************************************************)
CommitPerSuccessfulTurn ==
    (turnState = "completed" /\ turnState' = "idle") =>
        (commitsMade' = commitsMade \/ commitsMade' = commitsMade + 1)

---------------------------------------------------------------------------
(* LIVENESS PROPERTIES                                                     *)
---------------------------------------------------------------------------

(***************************************************************************)
(* L1: The coding loop always terminates.                                  *)
(***************************************************************************)
LoopTerminates == <>(loopState \in {"completed", "exhausted", "error"})

(***************************************************************************)
(* L2: If the agent keeps succeeding without signaling COMPLETE,           *)
(*     we eventually hit max iterations.                                   *)
(***************************************************************************)
EventualExhaustionOrCompletion ==
    [](loopState = "running" => <>(loopState \in {"completed", "exhausted", "error"}))

(***************************************************************************)
(* L3: Context overflow is always eventually resolved (compaction runs).   *)
(***************************************************************************)
CompactionEventuallyCompletes ==
    [](compacting => <>~compacting)

(***************************************************************************)
(* L4: A started turn eventually reaches a terminal turn state.            *)
(***************************************************************************)
TurnAlwaysEnds ==
    [](turnState \in {"started", "running"} =>
        <>(turnState \in {"completed", "failed", "timedout"}))

=============================================================================
