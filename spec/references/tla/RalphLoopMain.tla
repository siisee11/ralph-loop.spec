--------------------------- MODULE RalphLoopMain ---------------------------
(***************************************************************************)
(* TLA+ specification for the Ralph Loop three-phase workflow.             *)
(*                                                                         *)
(* Models the high-level lifecycle:                                        *)
(*   idle → init → setup → coding (loop) → pr → completed | failed        *)
(***************************************************************************)
EXTENDS Naturals, FiniteSets, Sequences

CONSTANTS
    MaxIterations,      \* Safety cap on coding loop iterations (default: 20)
    MaxFailuresPerPhase \* Max consecutive failures before abort (models retry budget)

VARIABLES
    phase,          \* Current workflow phase
    iteration,      \* Coding loop iteration counter (0-based internally)
    complete,       \* Agent signaled <promise>COMPLETE</promise>
    worktreeReady,  \* Worktree has been successfully initialized
    planExists,     \* Execution plan file has been created
    commitCount,    \* Number of commits made during coding loop
    failCount,      \* Consecutive failure counter within current phase
    prCreated,      \* PR has been created
    codexAlive      \* Codex app-server process is alive

vars == <<phase, iteration, complete, worktreeReady, planExists,
          commitCount, failCount, prCreated, codexAlive>>

(***************************************************************************)
(* Type invariant — every reachable state must satisfy this.               *)
(***************************************************************************)
TypeInvariant ==
    /\ phase \in {"idle", "init", "setup", "coding", "pr", "completed", "failed"}
    /\ iteration \in 0..MaxIterations
    /\ complete \in BOOLEAN
    /\ worktreeReady \in BOOLEAN
    /\ planExists \in BOOLEAN
    /\ commitCount \in Nat
    /\ failCount \in 0..MaxFailuresPerPhase
    /\ prCreated \in BOOLEAN
    /\ codexAlive \in BOOLEAN

(***************************************************************************)
(* Initial state                                                           *)
(***************************************************************************)
Init ==
    /\ phase = "idle"
    /\ iteration = 0
    /\ complete = FALSE
    /\ worktreeReady = FALSE
    /\ planExists = FALSE
    /\ commitCount = 0
    /\ failCount = 0
    /\ prCreated = FALSE
    /\ codexAlive = FALSE

(***************************************************************************)
(* Phase: idle → init                                                      *)
(* ralph-loop init creates or reuses a worktree, installs deps, verifies   *)
(* the build.                                                              *)
(***************************************************************************)
StartInit ==
    /\ phase = "idle"
    /\ phase' = "init"
    /\ UNCHANGED <<iteration, complete, worktreeReady, planExists,
                    commitCount, failCount, prCreated, codexAlive>>

(***************************************************************************)
(* Phase: init succeeds                                                    *)
(* Worktree created, deps installed, build verified.                       *)
(***************************************************************************)
InitSucceeds ==
    /\ phase = "init"
    /\ worktreeReady' = TRUE
    /\ phase' = "setup"
    /\ codexAlive' = TRUE   \* Setup agent Codex client spawned
    /\ failCount' = 0
    /\ UNCHANGED <<iteration, complete, planExists, commitCount, prCreated>>

(***************************************************************************)
(* Phase: init fails                                                       *)
(* Worktree creation, dep install, or build verification failed.           *)
(***************************************************************************)
InitFails ==
    /\ phase = "init"
    /\ phase' = "failed"
    /\ UNCHANGED <<iteration, complete, worktreeReady, planExists,
                    commitCount, failCount, prCreated, codexAlive>>

(***************************************************************************)
(* Phase: setup agent succeeds                                             *)
(* Agent explored repo, created execution plan, committed it.              *)
(* Requires: agent output contains <promise>COMPLETE</promise> AND plan    *)
(* file exists on disk.                                                    *)
(***************************************************************************)
SetupSucceeds ==
    /\ phase = "setup"
    /\ codexAlive
    /\ planExists' = TRUE
    /\ phase' = "coding"
    /\ codexAlive' = TRUE   \* New Codex client for coding loop
    /\ failCount' = 0
    /\ UNCHANGED <<iteration, complete, worktreeReady, commitCount, prCreated>>

(***************************************************************************)
(* Phase: setup agent fails                                                *)
(* Turn failed, no COMPLETE signal, or plan file missing.                  *)
(***************************************************************************)
SetupFails ==
    /\ phase = "setup"
    /\ phase' = "failed"
    /\ codexAlive' = FALSE
    /\ UNCHANGED <<iteration, complete, worktreeReady, planExists,
                    commitCount, failCount, prCreated>>

(***************************************************************************)
(* Phase: coding iteration succeeds                                        *)
(* Agent completed one milestone, committed changes, did NOT signal done.  *)
(***************************************************************************)
CodingIterationSucceeds ==
    /\ phase = "coding"
    /\ codexAlive
    /\ ~complete
    /\ iteration < MaxIterations
    /\ iteration' = iteration + 1
    /\ commitCount' = commitCount + 1
    /\ complete' = FALSE
    /\ failCount' = 0
    /\ UNCHANGED <<phase, worktreeReady, planExists, prCreated, codexAlive>>

(***************************************************************************)
(* Phase: coding iteration succeeds AND agent signals COMPLETE             *)
(* Agent completed final milestone.                                        *)
(***************************************************************************)
CodingIterationCompletes ==
    /\ phase = "coding"
    /\ codexAlive
    /\ ~complete
    /\ iteration < MaxIterations
    /\ iteration' = iteration + 1
    /\ commitCount' = commitCount + 1
    /\ complete' = TRUE
    /\ failCount' = 0
    /\ UNCHANGED <<phase, worktreeReady, planExists, prCreated, codexAlive>>

(***************************************************************************)
(* Phase: coding iteration fails (turn failed)                             *)
(* Codex turn returned status=failed. May trigger context compaction.      *)
(* Next iteration sends recovery prompt.                                   *)
(***************************************************************************)
CodingIterationFails ==
    /\ phase = "coding"
    /\ codexAlive
    /\ ~complete
    /\ iteration < MaxIterations
    /\ failCount < MaxFailuresPerPhase
    /\ iteration' = iteration + 1
    /\ failCount' = failCount + 1
    /\ UNCHANGED <<phase, complete, worktreeReady, planExists,
                    commitCount, prCreated, codexAlive>>

(***************************************************************************)
(* Phase: coding → pr (transition)                                         *)
(* Either COMPLETE was signaled or max iterations reached.                 *)
(***************************************************************************)
CodingFinished ==
    /\ phase = "coding"
    /\ (complete \/ iteration >= MaxIterations)
    /\ phase' = "pr"
    /\ codexAlive' = TRUE   \* New Codex client for PR agent
    /\ failCount' = 0
    /\ UNCHANGED <<iteration, complete, worktreeReady, planExists,
                    commitCount, prCreated>>

(***************************************************************************)
(* Phase: coding loop exhausts max iterations without COMPLETE             *)
(* This is an error condition — the workflow aborts.                       *)
(***************************************************************************)
CodingExhausted ==
    /\ phase = "coding"
    /\ ~complete
    /\ iteration >= MaxIterations
    /\ phase' = "failed"
    /\ codexAlive' = FALSE
    /\ UNCHANGED <<iteration, complete, worktreeReady, planExists,
                    commitCount, failCount, prCreated>>

(***************************************************************************)
(* Phase: Codex process dies unexpectedly during coding                    *)
(***************************************************************************)
CodexCrashDuringCoding ==
    /\ phase = "coding"
    /\ codexAlive
    /\ codexAlive' = FALSE
    /\ phase' = "failed"
    /\ UNCHANGED <<iteration, complete, worktreeReady, planExists,
                    commitCount, failCount, prCreated>>

(***************************************************************************)
(* Phase: PR agent succeeds                                                *)
(* Agent created PR, enabled auto-merge, output COMPLETE.                  *)
(***************************************************************************)
PRSucceeds ==
    /\ phase = "pr"
    /\ codexAlive
    /\ complete                 \* Only create PR if coding completed
    /\ prCreated' = TRUE
    /\ phase' = "completed"
    /\ UNCHANGED <<iteration, complete, worktreeReady, planExists,
                    commitCount, failCount, codexAlive>>

(***************************************************************************)
(* Phase: PR agent fails                                                   *)
(* Turn failed or no COMPLETE signal from PR agent.                        *)
(***************************************************************************)
PRFails ==
    /\ phase = "pr"
    /\ phase' = "failed"
    /\ codexAlive' = FALSE
    /\ UNCHANGED <<iteration, complete, worktreeReady, planExists,
                    commitCount, failCount, prCreated>>

(***************************************************************************)
(* Next-state relation                                                     *)
(***************************************************************************)
Next ==
    \/ StartInit
    \/ InitSucceeds
    \/ InitFails
    \/ SetupSucceeds
    \/ SetupFails
    \/ CodingIterationSucceeds
    \/ CodingIterationCompletes
    \/ CodingIterationFails
    \/ CodingFinished
    \/ CodingExhausted
    \/ CodexCrashDuringCoding
    \/ PRSucceeds
    \/ PRFails

(***************************************************************************)
(* Fairness: if the system can make progress, it eventually does.          *)
(* WeakFairness ensures no infinite stuttering — Codex will eventually     *)
(* respond if alive, and phase transitions don't stall indefinitely.       *)
(***************************************************************************)
Fairness ==
    /\ WF_vars(StartInit)
    /\ WF_vars(InitSucceeds)
    /\ WF_vars(SetupSucceeds)
    /\ WF_vars(CodingIterationSucceeds \/ CodingIterationCompletes \/ CodingIterationFails)
    /\ WF_vars(CodingFinished \/ CodingExhausted)
    /\ WF_vars(PRSucceeds)

Spec == Init /\ [][Next]_vars /\ Fairness

---------------------------------------------------------------------------
(* SAFETY PROPERTIES                                                       *)
---------------------------------------------------------------------------

(***************************************************************************)
(* S1: Phase ordering — phases only transition forward, never backward.    *)
(*     idle → init → setup → coding → pr → completed|failed               *)
(***************************************************************************)
PhaseOrderInvariant ==
    /\ (phase = "idle"   => ~worktreeReady /\ ~planExists /\ ~prCreated)
    /\ (phase = "init"   => ~planExists /\ ~prCreated)
    /\ (phase = "setup"  => worktreeReady /\ ~prCreated)
    /\ (phase = "coding" => worktreeReady /\ planExists /\ ~prCreated)
    /\ (phase = "pr"     => worktreeReady /\ planExists /\ complete)
    /\ (phase = "completed" => worktreeReady /\ planExists /\ complete /\ prCreated)

(***************************************************************************)
(* S2: Iteration count never exceeds MaxIterations.                        *)
(***************************************************************************)
IterationBound == iteration <= MaxIterations

(***************************************************************************)
(* S3: Commits only happen during coding phase.                            *)
(***************************************************************************)
CommitOnlyInCoding ==
    (phase \notin {"coding", "pr", "completed", "failed"}) => commitCount = 0

(***************************************************************************)
(* S4: Terminal states are absorbing — once completed or failed, no more   *)
(*     state changes.                                                      *)
(***************************************************************************)
TerminalIsAbsorbing ==
    /\ (phase = "completed") => [](phase = "completed")
    /\ (phase = "failed")    => [](phase = "failed")

(***************************************************************************)
(* S5: PR is only created after coding completes with COMPLETE signal.     *)
(***************************************************************************)
PRRequiresCompletion == prCreated => complete

(***************************************************************************)
(* S6: No Codex interaction without a live Codex client.                   *)
(***************************************************************************)
NoZombieCodex ==
    /\ (phase \in {"setup", "coding", "pr"}) => codexAlive

---------------------------------------------------------------------------
(* LIVENESS PROPERTIES                                                     *)
---------------------------------------------------------------------------

(***************************************************************************)
(* L1: The workflow always terminates — reaches completed or failed.       *)
(***************************************************************************)
Termination == <>(phase \in {"completed", "failed"})

(***************************************************************************)
(* L2: If init succeeds, we eventually reach setup.                        *)
(***************************************************************************)
InitLeadsToSetup == [](worktreeReady => <>(phase \in {"setup", "coding", "pr", "completed", "failed"}))

(***************************************************************************)
(* L3: If plan exists and coding starts, we eventually leave coding.       *)
(***************************************************************************)
CodingEventuallyEnds == [](phase = "coding" => <>(phase \in {"pr", "completed", "failed"}))

(***************************************************************************)
(* L4: completed state is reachable (the happy path exists).               *)
(***************************************************************************)
CompletionReachable == <>(phase = "completed")

=============================================================================
