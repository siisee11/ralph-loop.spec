package ralphloop

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

type setupAgentOptions struct {
	Client         codexClient
	Model          string
	Cwd            string
	ApprovalPolicy string
	Sandbox        any
	Timeout        time.Duration
	UserPrompt     string
	PlanPath       string
	WorktreePath   string
	WorktreeID     string
	WorkBranch     string
	BaseBranch     string
}

type codingLoopOptions struct {
	Client        codexClient
	ThreadID      string
	WorktreePath  string
	UserPrompt    string
	PlanPath      string
	MaxIterations int
	Timeout       time.Duration
	OnEvent       func(eventRecord)
}

type codingLoopResult struct {
	Iterations int
	Completed  bool
	FinalHead  string
}

type prAgentOptions struct {
	Client         codexClient
	Model          string
	Cwd            string
	ApprovalPolicy string
	ThreadSandbox  any
	SandboxPolicy  any
	Timeout        time.Duration
	PlanPath       string
	BaseBranch     string
}

func runSetupAgent(ctx context.Context, options setupAgentOptions) error {
	threadID, err := options.Client.StartThread(ctx, startThreadOptions{
		Model:          options.Model,
		Cwd:            options.Cwd,
		ApprovalPolicy: options.ApprovalPolicy,
		Sandbox:        options.Sandbox,
	})
	if err != nil {
		return err
	}

	result, err := options.Client.RunTurn(ctx, runTurnOptions{
		ThreadID: threadID,
		Prompt: buildSetupPrompt(setupPromptOptions{
			UserPrompt:   options.UserPrompt,
			PlanPath:     options.PlanPath,
			WorktreePath: options.WorktreePath,
			WorktreeID:   options.WorktreeID,
			WorkBranch:   options.WorkBranch,
			BaseBranch:   options.BaseBranch,
		}),
		Timeout: options.Timeout,
	})
	if err != nil {
		return err
	}
	if strings.EqualFold(result.Status, "failed") {
		if strings.TrimSpace(result.CodexErrorInfo) == "" {
			return fmt.Errorf("setup agent failed")
		}
		return fmt.Errorf("setup agent failed: %s", result.CodexErrorInfo)
	}
	if !containsCompletionSignal(result.AgentText) {
		return fmt.Errorf("setup agent completed without the required completion token")
	}
	if _, err := os.Stat(options.PlanPath); err != nil {
		return fmt.Errorf("setup agent did not create the plan file: %s", options.PlanPath)
	}
	return nil
}

func runCodingLoop(ctx context.Context, options codingLoopOptions) (codingLoopResult, error) {
	iterations := 0
	completed := false
	nextPrompt := buildCodingPrompt(codingPromptOptions{UserPrompt: options.UserPrompt, PlanPath: options.PlanPath})

	for ; iterations < options.MaxIterations; iterations++ {
		iterationNumber := iterations + 1
		previousHead := currentHead(options.WorktreePath)
		result, err := options.Client.RunTurn(ctx, runTurnOptions{
			ThreadID: options.ThreadID,
			Prompt:   nextPrompt,
			Timeout:  options.Timeout,
		})
		if err != nil {
			return codingLoopResult{}, err
		}

		if strings.EqualFold(result.Status, "failed") {
			if result.CodexErrorInfo == "ContextWindowExceeded" {
				if err := options.Client.CompactThread(ctx, options.ThreadID); err != nil {
					return codingLoopResult{}, err
				}
			}
			if options.OnEvent != nil {
				options.OnEvent(eventRecord{
					Command:   string(CommandMain),
					Event:     "iteration.failed",
					Status:    "failed",
					Phase:     "coding",
					Iteration: iterationNumber,
					Message:   firstNonEmpty(result.CodexErrorInfo, "coding iteration failed"),
					TS:        nowRFC3339(),
				})
			}
			nextPrompt = buildRecoveryPrompt(options.PlanPath)
			continue
		}

		updatedHead := currentHead(options.WorktreePath)
		if options.OnEvent != nil {
			options.OnEvent(eventRecord{
				Command:   string(CommandMain),
				Event:     "iteration.completed",
				Status:    "ok",
				Phase:     "coding",
				Iteration: iterationNumber,
				Commit:    shortCommit(updatedHead),
				Message:   "iteration completed",
				TS:        nowRFC3339(),
			})
		}

		if containsCompletionSignal(result.AgentText) {
			completed = true
			if updatedHead == "" {
				updatedHead = previousHead
			}
			break
		}
		nextPrompt = buildCodingPrompt(codingPromptOptions{UserPrompt: options.UserPrompt, PlanPath: options.PlanPath})
	}

	count := iterations
	if completed {
		count++
	}
	return codingLoopResult{Iterations: count, Completed: completed, FinalHead: currentHead(options.WorktreePath)}, nil
}

func runPrAgent(ctx context.Context, options prAgentOptions) (string, error) {
	threadID, err := options.Client.StartThread(ctx, startThreadOptions{
		Model:          options.Model,
		Cwd:            options.Cwd,
		ApprovalPolicy: options.ApprovalPolicy,
		Sandbox:        options.ThreadSandbox,
	})
	if err != nil {
		return "", err
	}

	result, err := options.Client.RunTurn(ctx, runTurnOptions{
		ThreadID:       threadID,
		Prompt:         buildPrPrompt(prPromptOptions{PlanPath: options.PlanPath, BaseBranch: options.BaseBranch}),
		Timeout:        options.Timeout,
		Cwd:            options.Cwd,
		ApprovalPolicy: options.ApprovalPolicy,
		SandboxPolicy:  options.SandboxPolicy,
	})
	if err != nil {
		return "", err
	}
	if strings.EqualFold(result.Status, "failed") {
		if strings.TrimSpace(result.CodexErrorInfo) == "" {
			return "", fmt.Errorf("PR agent failed")
		}
		return "", fmt.Errorf("PR agent failed: %s", result.CodexErrorInfo)
	}
	if !containsCompletionSignal(result.AgentText) {
		return "", fmt.Errorf("PR agent completed without the required completion token")
	}
	return result.AgentText, nil
}

func currentHead(cwd string) string {
	command := exec.Command("git", "rev-parse", "HEAD")
	command.Dir = cwd
	output, err := command.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func shortCommit(commit string) string {
	trimmed := strings.TrimSpace(commit)
	if len(trimmed) > 7 {
		return trimmed[:7]
	}
	return trimmed
}
