package ralphloop

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var (
	spawnCodexClient  = newAppServerClient
	initWorktreeFn    = initWorktree
	cleanupWorktreeFn = cleanupWorktree
)

func runMain(ctx context.Context, repoRoot string, options MainOptions, stdout io.Writer, stderr io.Writer) (err error) {
	worktreeName := strings.TrimPrefix(options.WorkBranch, "ralph-")
	if worktreeName == "" {
		worktreeName = "task"
	}

	worktree, err := initWorktreeFn(ctx, initWorktreeOptions{
		RepoRoot:     repoRoot,
		BaseBranch:   options.BaseBranch,
		WorkBranch:   options.WorkBranch,
		WorktreeName: worktreeName,
	})
	if err != nil {
		return err
	}

	logPath, err := ensureRalphLogPath(worktree)
	if err != nil {
		return err
	}

	cleanupSession, err := registerRalphSession(worktree, logPath, time.Now().UTC())
	if err != nil {
		return err
	}
	defer cleanupSession()
	defer func() {
		if !options.PreserveWorktree {
			_ = cleanupWorktreeFn(context.Background(), repoRoot, worktree.WorktreePath)
		}
	}()

	planPath := filepath.Join(worktree.WorktreePath, "docs", "exec-plans", "active", defaultPlanFilename(options.Prompt))
	if err := ensurePlanParent(planPath); err != nil {
		return err
	}

	summary := runSummary{
		Command:      string(CommandMain),
		Status:       "running",
		WorktreePath: worktree.WorktreePath,
		WorkBranch:   worktree.WorkBranch,
		BaseBranch:   worktree.BaseBranch,
		PlanPath:     planPath,
		LogPath:      logPath,
	}
	emit := newMainEmitter(options.Output, stdout, stderr, &summary)

	emit(eventRecord{
		Command:      string(CommandMain),
		Event:        "run.started",
		Status:       "running",
		WorktreePath: worktree.WorktreePath,
		WorkBranch:   worktree.WorkBranch,
		PlanPath:     planPath,
		TS:           nowRFC3339(),
	})

	sandbox := resolveSandbox(options.Sandbox, worktree.WorktreePath)
	prSandbox := resolvePrSandbox(options.Sandbox, worktree.WorktreePath)
	turnTimeout := time.Duration(options.TimeoutSeconds) * time.Second
	if turnTimeout <= 0 || turnTimeout > 30*time.Minute {
		turnTimeout = 30 * time.Minute
	}

	var setupClient codexClient
	var codingClient codexClient
	var prClient codexClient
	defer func() {
		if setupClient != nil {
			_ = setupClient.Close()
		}
		if codingClient != nil {
			_ = codingClient.Close()
		}
		if prClient != nil {
			_ = prClient.Close()
		}
	}()

	emit(eventRecord{
		Command:      string(CommandMain),
		Event:        "phase.started",
		Status:       "running",
		Phase:        "setup",
		WorktreePath: worktree.WorktreePath,
		WorkBranch:   worktree.WorkBranch,
		PlanPath:     planPath,
		TS:           nowRFC3339(),
	})

	setupClient, err = spawnCodexClient(logPath)
	if err != nil {
		return finishMainError(options.Output, stdout, stderr, &summary, err)
	}
	setupClient.SetNotificationHandler(func(notification jsonRPCNotification) {
		message := notificationToAgentMessage(notification)
		if strings.TrimSpace(message) == "" {
			return
		}
		emit(eventRecord{
			Command: string(CommandMain),
			Event:   "agent.message",
			Status:  "running",
			Phase:   "setup",
			Message: message,
			TS:      nowRFC3339(),
		})
	})
	if err := setupClient.Initialize(ctx); err != nil {
		return finishMainError(options.Output, stdout, stderr, &summary, err)
	}
	if err := runSetupAgent(ctx, setupAgentOptions{
		Client:         setupClient,
		Model:          options.Model,
		Cwd:            worktree.WorktreePath,
		ApprovalPolicy: options.ApprovalPolicy,
		Sandbox:        sandbox,
		Timeout:        turnTimeout,
		UserPrompt:     options.Prompt,
		PlanPath:       planPath,
		WorktreePath:   worktree.WorktreePath,
		WorktreeID:     worktree.WorktreeID,
		WorkBranch:     worktree.WorkBranch,
		BaseBranch:     worktree.BaseBranch,
	}); err != nil {
		return finishMainError(options.Output, stdout, stderr, &summary, err)
	}
	emit(eventRecord{
		Command:      string(CommandMain),
		Event:        "phase.completed",
		Status:       "ok",
		Phase:        "setup",
		WorktreePath: worktree.WorktreePath,
		WorkBranch:   worktree.WorkBranch,
		PlanPath:     planPath,
		TS:           nowRFC3339(),
	})
	_ = setupClient.Close()
	setupClient = nil

	emit(eventRecord{
		Command:      string(CommandMain),
		Event:        "phase.started",
		Status:       "running",
		Phase:        "coding",
		WorktreePath: worktree.WorktreePath,
		WorkBranch:   worktree.WorkBranch,
		PlanPath:     planPath,
		TS:           nowRFC3339(),
	})
	codingClient, err = spawnCodexClient(logPath)
	if err != nil {
		return finishMainError(options.Output, stdout, stderr, &summary, err)
	}
	codingClient.SetNotificationHandler(func(notification jsonRPCNotification) {
		message := notificationToAgentMessage(notification)
		if strings.TrimSpace(message) == "" {
			return
		}
		emit(eventRecord{
			Command: string(CommandMain),
			Event:   "agent.message",
			Status:  "running",
			Phase:   "coding",
			Message: message,
			TS:      nowRFC3339(),
		})
	})
	if err := codingClient.Initialize(ctx); err != nil {
		return finishMainError(options.Output, stdout, stderr, &summary, err)
	}
	threadID, err := codingClient.StartThread(ctx, startThreadOptions{
		Model:          options.Model,
		Cwd:            worktree.WorktreePath,
		ApprovalPolicy: options.ApprovalPolicy,
		Sandbox:        sandbox,
	})
	if err != nil {
		return finishMainError(options.Output, stdout, stderr, &summary, err)
	}
	codingResult, err := runCodingLoop(ctx, codingLoopOptions{
		Client:        codingClient,
		ThreadID:      threadID,
		WorktreePath:  worktree.WorktreePath,
		UserPrompt:    options.Prompt,
		PlanPath:      planPath,
		MaxIterations: options.MaxIterations,
		Timeout:       turnTimeout,
		OnEvent:       emit,
	})
	if err != nil {
		return finishMainError(options.Output, stdout, stderr, &summary, err)
	}
	if !codingResult.Completed {
		return finishMainError(options.Output, stdout, stderr, &summary, fmt.Errorf("Ralph Loop reached %d iterations without completion", options.MaxIterations))
	}
	summary.Iterations = codingResult.Iterations
	emit(eventRecord{
		Command:      string(CommandMain),
		Event:        "phase.completed",
		Status:       "ok",
		Phase:        "coding",
		Iteration:    codingResult.Iterations,
		WorktreePath: worktree.WorktreePath,
		WorkBranch:   worktree.WorkBranch,
		PlanPath:     planPath,
		Commit:       shortCommit(codingResult.FinalHead),
		TS:           nowRFC3339(),
	})
	_ = codingClient.Close()
	codingClient = nil

	emit(eventRecord{
		Command:      string(CommandMain),
		Event:        "phase.started",
		Status:       "running",
		Phase:        "pr",
		WorktreePath: worktree.WorktreePath,
		WorkBranch:   worktree.WorkBranch,
		PlanPath:     planPath,
		TS:           nowRFC3339(),
	})
	prClient, err = spawnCodexClient(logPath)
	if err != nil {
		return finishMainError(options.Output, stdout, stderr, &summary, err)
	}
	prClient.SetNotificationHandler(func(notification jsonRPCNotification) {
		message := notificationToAgentMessage(notification)
		if strings.TrimSpace(message) == "" {
			return
		}
		emit(eventRecord{
			Command: string(CommandMain),
			Event:   "agent.message",
			Status:  "running",
			Phase:   "pr",
			Message: message,
			TS:      nowRFC3339(),
		})
	})
	if err := prClient.Initialize(ctx); err != nil {
		return finishMainError(options.Output, stdout, stderr, &summary, err)
	}
	prOutput, err := runPrAgent(ctx, prAgentOptions{
		Client:         prClient,
		Model:          options.Model,
		Cwd:            worktree.WorktreePath,
		ApprovalPolicy: options.ApprovalPolicy,
		ThreadSandbox:  sandbox,
		SandboxPolicy:  prSandbox,
		Timeout:        turnTimeout,
		PlanPath:       planPath,
		BaseBranch:     options.BaseBranch,
	})
	if err != nil {
		return finishMainError(options.Output, stdout, stderr, &summary, err)
	}
	summary.PRURL = extractPRURL(prOutput)
	emit(eventRecord{
		Command:      string(CommandMain),
		Event:        "phase.completed",
		Status:       "ok",
		Phase:        "pr",
		WorktreePath: worktree.WorktreePath,
		WorkBranch:   worktree.WorkBranch,
		PlanPath:     planPath,
		PRURL:        summary.PRURL,
		TS:           nowRFC3339(),
	})

	summary.Status = "completed"
	finalEvent := eventRecord{
		Command:      string(CommandMain),
		Event:        "run.completed",
		Status:       "completed",
		Iteration:    summary.Iterations,
		WorktreePath: worktree.WorktreePath,
		WorkBranch:   worktree.WorkBranch,
		PlanPath:     planPath,
		PRURL:        summary.PRURL,
		TS:           nowRFC3339(),
	}
	emit(finalEvent)
	return finalizeMain(options.Output, stdout, stderr, &summary)
}

func ensurePlanParent(planPath string) error {
	return os.MkdirAll(filepath.Dir(planPath), 0o755)
}
