------------------------ MODULE ConcurrentSessions -------------------------
(***************************************************************************)
(* TLA+ specification for concurrent Ralph Loop sessions.                  *)
(*                                                                         *)
(* Multiple ralph-loop invocations may run in parallel on the same repo.   *)
(* Each session must get an isolated worktree with no resource conflicts.  *)
(***************************************************************************)
EXTENDS Naturals, FiniteSets

CONSTANTS
    Sessions,          \* Set of session identifiers, e.g., {"s1", "s2", "s3"}
    WorktreeSlots,     \* Set of available worktree slots (finite resource pool)
    PortRange          \* Set of available ports for deterministic assignment

VARIABLES
    sessionState,      \* Function: session → {"idle", "initializing", "running", "done", "failed"}
    worktreeOwner,     \* Function: worktree slot → session | "free"
    portOwner,         \* Function: port → session | "free"
    sessionWorktree,   \* Function: session → worktree slot | "none"
    sessionPort,       \* Function: session → port | "none"
    runtimeDirs        \* Function: session → BOOLEAN (runtime dirs created)

vars == <<sessionState, worktreeOwner, portOwner, sessionWorktree, sessionPort, runtimeDirs>>

(***************************************************************************)
(* Type invariant                                                          *)
(***************************************************************************)
TypeInvariant ==
    /\ \A s \in Sessions:
        sessionState[s] \in {"idle", "initializing", "running", "done", "failed"}
    /\ \A w \in WorktreeSlots:
        worktreeOwner[w] \in Sessions \cup {"free"}
    /\ \A p \in PortRange:
        portOwner[p] \in Sessions \cup {"free"}
    /\ \A s \in Sessions:
        sessionWorktree[s] \in WorktreeSlots \cup {"none"}
    /\ \A s \in Sessions:
        sessionPort[s] \in PortRange \cup {"none"}
    /\ \A s \in Sessions:
        runtimeDirs[s] \in BOOLEAN

(***************************************************************************)
(* Initial state: all sessions idle, all resources free.                   *)
(***************************************************************************)
Init ==
    /\ sessionState    = [s \in Sessions |-> "idle"]
    /\ worktreeOwner   = [w \in WorktreeSlots |-> "free"]
    /\ portOwner       = [p \in PortRange |-> "free"]
    /\ sessionWorktree = [s \in Sessions |-> "none"]
    /\ sessionPort     = [s \in Sessions |-> "none"]
    /\ runtimeDirs     = [s \in Sessions |-> FALSE]

(***************************************************************************)
(* SessionStart: A session begins initialization.                          *)
(* Claims a worktree slot and a port atomically.                           *)
(***************************************************************************)
SessionStart(s) ==
    /\ sessionState[s] = "idle"
    /\ \E w \in WorktreeSlots, p \in PortRange:
        /\ worktreeOwner[w] = "free"
        /\ portOwner[p] = "free"
        /\ sessionState'    = [sessionState    EXCEPT ![s] = "initializing"]
        /\ worktreeOwner'   = [worktreeOwner   EXCEPT ![w] = s]
        /\ portOwner'       = [portOwner       EXCEPT ![p] = s]
        /\ sessionWorktree' = [sessionWorktree EXCEPT ![s] = w]
        /\ sessionPort'     = [sessionPort     EXCEPT ![s] = p]
        /\ UNCHANGED runtimeDirs

(***************************************************************************)
(* SessionInitSucceeds: Worktree created, deps installed, runtime dirs up. *)
(***************************************************************************)
SessionInitSucceeds(s) ==
    /\ sessionState[s] = "initializing"
    /\ sessionState' = [sessionState EXCEPT ![s] = "running"]
    /\ runtimeDirs'  = [runtimeDirs  EXCEPT ![s] = TRUE]
    /\ UNCHANGED <<worktreeOwner, portOwner, sessionWorktree, sessionPort>>

(***************************************************************************)
(* SessionInitFails: Init failed — release claimed resources.              *)
(***************************************************************************)
SessionInitFails(s) ==
    /\ sessionState[s] = "initializing"
    /\ LET w == sessionWorktree[s]
           p == sessionPort[s]
       IN
        /\ sessionState'    = [sessionState    EXCEPT ![s] = "failed"]
        /\ worktreeOwner'   = [worktreeOwner   EXCEPT ![w] = "free"]
        /\ portOwner'       = [portOwner       EXCEPT ![p] = "free"]
        /\ sessionWorktree' = [sessionWorktree EXCEPT ![s] = "none"]
        /\ sessionPort'     = [sessionPort     EXCEPT ![s] = "none"]
        /\ UNCHANGED runtimeDirs

(***************************************************************************)
(* SessionCompletes: Workflow finishes — release resources.                 *)
(***************************************************************************)
SessionCompletes(s) ==
    /\ sessionState[s] = "running"
    /\ LET w == sessionWorktree[s]
           p == sessionPort[s]
       IN
        /\ sessionState'    = [sessionState    EXCEPT ![s] = "done"]
        /\ worktreeOwner'   = [worktreeOwner   EXCEPT ![w] = "free"]
        /\ portOwner'       = [portOwner       EXCEPT ![p] = "free"]
        /\ sessionWorktree' = [sessionWorktree EXCEPT ![s] = "none"]
        /\ sessionPort'     = [sessionPort     EXCEPT ![s] = "none"]
        /\ runtimeDirs'     = [runtimeDirs     EXCEPT ![s] = FALSE]

(***************************************************************************)
(* SessionFails: Workflow fails mid-run — release resources.               *)
(***************************************************************************)
SessionFails(s) ==
    /\ sessionState[s] = "running"
    /\ LET w == sessionWorktree[s]
           p == sessionPort[s]
       IN
        /\ sessionState'    = [sessionState    EXCEPT ![s] = "failed"]
        /\ worktreeOwner'   = [worktreeOwner   EXCEPT ![w] = "free"]
        /\ portOwner'       = [portOwner       EXCEPT ![p] = "free"]
        /\ sessionWorktree' = [sessionWorktree EXCEPT ![s] = "none"]
        /\ sessionPort'     = [sessionPort     EXCEPT ![s] = "none"]
        /\ runtimeDirs'     = [runtimeDirs     EXCEPT ![s] = FALSE]

(***************************************************************************)
(* Next-state relation                                                     *)
(***************************************************************************)
Next ==
    \E s \in Sessions:
        \/ SessionStart(s)
        \/ SessionInitSucceeds(s)
        \/ SessionInitFails(s)
        \/ SessionCompletes(s)
        \/ SessionFails(s)

Fairness ==
    \A s \in Sessions:
        /\ WF_vars(SessionStart(s))
        /\ WF_vars(SessionInitSucceeds(s))
        /\ WF_vars(SessionCompletes(s))

Spec == Init /\ [][Next]_vars /\ Fairness

---------------------------------------------------------------------------
(* SAFETY PROPERTIES                                                       *)
---------------------------------------------------------------------------

(***************************************************************************)
(* S1: No two active sessions share the same worktree.                     *)
(*     Core isolation guarantee.                                           *)
(***************************************************************************)
WorktreeExclusion ==
    \A w \in WorktreeSlots:
        Cardinality({s \in Sessions: sessionWorktree[s] = w}) <= 1

(***************************************************************************)
(* S2: No two active sessions share the same port.                         *)
(*     Port derivation must be collision-free for concurrent sessions.     *)
(***************************************************************************)
PortExclusion ==
    \A p \in PortRange:
        Cardinality({s \in Sessions: sessionPort[s] = p}) <= 1

(***************************************************************************)
(* S3: A running session always has a worktree and port assigned.          *)
(***************************************************************************)
RunningHasResources ==
    \A s \in Sessions:
        sessionState[s] = "running" =>
            /\ sessionWorktree[s] \in WorktreeSlots
            /\ sessionPort[s] \in PortRange
            /\ runtimeDirs[s]

(***************************************************************************)
(* S4: Resources owned by a session match the session's assignment.        *)
(***************************************************************************)
OwnershipConsistency ==
    \A s \in Sessions:
        /\ (sessionWorktree[s] \in WorktreeSlots =>
                worktreeOwner[sessionWorktree[s]] = s)
        /\ (sessionPort[s] \in PortRange =>
                portOwner[sessionPort[s]] = s)

(***************************************************************************)
(* S5: Idle/done/failed sessions hold no resources.                        *)
(***************************************************************************)
NoResourceLeak ==
    \A s \in Sessions:
        sessionState[s] \in {"idle", "done", "failed"} =>
            /\ sessionWorktree[s] = "none"
            /\ sessionPort[s] = "none"

(***************************************************************************)
(* S6: Terminal states are absorbing.                                      *)
(***************************************************************************)
TerminalAbsorbing ==
    \A s \in Sessions:
        /\ (sessionState[s] = "done")   => [](sessionState[s] = "done")
        /\ (sessionState[s] = "failed") => [](sessionState[s] = "failed")

---------------------------------------------------------------------------
(* LIVENESS PROPERTIES                                                     *)
---------------------------------------------------------------------------

(***************************************************************************)
(* L1: Every session eventually terminates.                                *)
(***************************************************************************)
AllSessionsTerminate ==
    \A s \in Sessions: <>(sessionState[s] \in {"done", "failed"})

(***************************************************************************)
(* L2: All worktree slots are eventually freed.                            *)
(***************************************************************************)
AllWorktreesFreed ==
    <>(\A w \in WorktreeSlots: worktreeOwner[w] = "free")

(***************************************************************************)
(* L3: All ports are eventually freed.                                     *)
(***************************************************************************)
AllPortsFreed ==
    <>(\A p \in PortRange: portOwner[p] = "free")

=============================================================================
