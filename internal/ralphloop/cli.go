package ralphloop

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const (
	MainUsage = "Usage: ralph-loop init [--base-branch <branch>] [--work-branch <name>] [--output <format>]\n       ralph-loop \"<user prompt>\" [options]\n       ralph-loop tail [selector] [--lines N] [--follow] [--raw] [--output <format>]\n       ralph-loop ls [selector] [--output <format>]"
	InitUsage = "Usage: ralph-loop init [--base-branch <branch>] [--work-branch <name>] [--output <format>]"
	TailUsage = "Usage: ralph-loop tail [selector] [--lines N] [--follow] [--raw] [--output <format>]"
	ListUsage = "Usage: ralph-loop ls [selector] [--output <format>]"
)

type usageError struct {
	message string
}

func (err *usageError) Error() string {
	return err.message
}

func IsUsageError(err error) bool {
	var target *usageError
	return errors.As(err, &target)
}

var (
	runInitFn = runInitCommand
	runMainFn = runMain
	runTailFn = runTailCommand
	runListFn = runListCommand
)

func Run(args []string, cwd string, stdout io.Writer, stderr io.Writer) int {
	outputMode := detectOutputMode(args, stdout)

	repoRoot, err := ResolveRepoRoot(cwd)
	if err != nil {
		renderCommandError(outputMode, stdout, stderr, "main", normalizeCommandError(err, "resolve_repo_root_failed"))
		return 1
	}

	command, err := ParseCommand(args, repoRoot, outputMode)
	if err != nil {
		commandName := string(CommandMain)
		if len(args) > 0 && (args[0] == "init" || args[0] == "tail" || args[0] == "ls") {
			commandName = args[0]
		}
		renderCommandError(outputMode, stdout, stderr, commandName, normalizeCommandError(err, "invalid_arguments"))
		return 1
	}

	switch command.Kind {
	case CommandInit:
		if err := runInitFn(context.Background(), repoRoot, command.InitOptions, stdout, stderr); err != nil {
			if isRenderedError(err) {
				return 1
			}
			renderCommandError(command.InitOptions.Output, stdout, stderr, string(CommandInit), normalizeCommandError(err, "init_failed"))
			return 1
		}
		return 0
	case CommandTail:
		if err := runTailFn(context.Background(), repoRoot, command.TailOptions, stdout, stderr); err != nil {
			renderCommandError(command.TailOptions.Output, stdout, stderr, string(CommandTail), normalizeCommandError(err, "tail_failed"))
			return 1
		}
		return 0
	case CommandList:
		if err := runListFn(repoRoot, command.ListOptions, stdout, stderr); err != nil {
			renderCommandError(command.ListOptions.Output, stdout, stderr, string(CommandList), normalizeCommandError(err, "list_failed"))
			return 1
		}
		return 0
	default:
		if err := runMainFn(context.Background(), repoRoot, command.MainOptions, stdout, stderr); err != nil {
			if isRenderedError(err) {
				return 1
			}
			renderCommandError(command.MainOptions.Output, stdout, stderr, string(CommandMain), normalizeCommandError(err, "run_failed"))
			return 1
		}
		return 0
	}
}

func ResolveRepoRoot(cwd string) (string, error) {
	if _, err := os.Stat(filepath.Join(cwd, ".git")); err == nil {
		return cwd, nil
	}

	command := exec.Command("git", "rev-parse", "--show-toplevel")
	command.Dir = cwd
	output, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("failed to resolve repository root from %s: %w", cwd, err)
	}
	root := strings.TrimSpace(string(output))
	if root == "" {
		return "", fmt.Errorf("failed to resolve repository root from %s: empty git output", cwd)
	}
	return root, nil
}

func ParseCommand(args []string, repoRoot string, defaultOutput OutputFormat) (ParsedCommand, error) {
	if len(args) > 0 && args[0] == "init" {
		options, err := ParseInitArgs(args[1:], defaultOutput)
		if err != nil {
			return ParsedCommand{}, err
		}
		return ParsedCommand{Kind: CommandInit, InitOptions: options}, nil
	}
	if len(args) > 0 && args[0] == "tail" {
		options, err := ParseTailArgs(args[1:], defaultOutput)
		if err != nil {
			return ParsedCommand{}, err
		}
		return ParsedCommand{Kind: CommandTail, TailOptions: options}, nil
	}
	if len(args) > 0 && args[0] == "ls" {
		options, err := ParseListArgs(args[1:], defaultOutput)
		if err != nil {
			return ParsedCommand{}, err
		}
		return ParsedCommand{Kind: CommandList, ListOptions: options}, nil
	}

	options, err := ParseMainArgs(args, repoRoot, defaultOutput)
	if err != nil {
		return ParsedCommand{}, err
	}
	return ParsedCommand{Kind: CommandMain, MainOptions: options}, nil
}

func ParseInitArgs(args []string, defaultOutput OutputFormat) (InitOptions, error) {
	options := InitOptions{
		BaseBranch: "main",
		Output:     defaultOutput,
	}

	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch arg {
		case "--help", "-h":
			return InitOptions{}, &usageError{message: InitUsage}
		case "--base-branch":
			value, err := requireValue(args, &index, "--base-branch")
			if err != nil {
				return InitOptions{}, err
			}
			options.BaseBranch = strings.TrimSpace(value)
		case "--work-branch":
			value, err := requireValue(args, &index, "--work-branch")
			if err != nil {
				return InitOptions{}, err
			}
			options.WorkBranch = sanitizeBranchName(value)
			if options.WorkBranch == "" {
				return InitOptions{}, fmt.Errorf("invalid value for --work-branch: %s", value)
			}
		case "--output":
			value, err := requireValue(args, &index, "--output")
			if err != nil {
				return InitOptions{}, err
			}
			format, err := parseOutputFormat(value)
			if err != nil {
				return InitOptions{}, err
			}
			options.Output = format
		default:
			return InitOptions{}, &usageError{message: InitUsage}
		}
	}

	if options.BaseBranch == "" {
		options.BaseBranch = "main"
	}
	return options, nil
}

func ParseMainArgs(args []string, _ string, defaultOutput OutputFormat) (MainOptions, error) {
	promptParts := make([]string, 0, len(args))
	options := MainOptions{
		Model:          "gpt-5.3-codex",
		BaseBranch:     "main",
		MaxIterations:  20,
		TimeoutSeconds: 21600,
		ApprovalPolicy: "never",
		Sandbox:        "workspace-write",
		Output:         defaultOutput,
	}

	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch arg {
		case "--help", "-h":
			return MainOptions{}, &usageError{message: MainUsage}
		case "--model":
			value, err := requireValue(args, &index, "--model")
			if err != nil {
				return MainOptions{}, err
			}
			options.Model = value
		case "--base-branch":
			value, err := requireValue(args, &index, "--base-branch")
			if err != nil {
				return MainOptions{}, err
			}
			options.BaseBranch = value
		case "--max-iterations":
			value, err := requireValue(args, &index, "--max-iterations")
			if err != nil {
				return MainOptions{}, err
			}
			parsed, err := strconv.Atoi(value)
			if err != nil || parsed <= 0 {
				return MainOptions{}, fmt.Errorf("invalid value for --max-iterations: %s", value)
			}
			options.MaxIterations = parsed
		case "--work-branch":
			value, err := requireValue(args, &index, "--work-branch")
			if err != nil {
				return MainOptions{}, err
			}
			options.WorkBranch = sanitizeBranchName(value)
			if options.WorkBranch == "" {
				return MainOptions{}, fmt.Errorf("invalid value for --work-branch: %s", value)
			}
		case "--timeout":
			value, err := requireValue(args, &index, "--timeout")
			if err != nil {
				return MainOptions{}, err
			}
			parsed, err := strconv.Atoi(value)
			if err != nil || parsed <= 0 {
				return MainOptions{}, fmt.Errorf("invalid value for --timeout: %s", value)
			}
			options.TimeoutSeconds = parsed
		case "--approval-policy":
			value, err := requireValue(args, &index, "--approval-policy")
			if err != nil {
				return MainOptions{}, err
			}
			options.ApprovalPolicy = value
		case "--sandbox":
			value, err := requireValue(args, &index, "--sandbox")
			if err != nil {
				return MainOptions{}, err
			}
			options.Sandbox = value
		case "--output":
			value, err := requireValue(args, &index, "--output")
			if err != nil {
				return MainOptions{}, err
			}
			format, err := parseOutputFormat(value)
			if err != nil {
				return MainOptions{}, err
			}
			options.Output = format
		case "--preserve-worktree":
			options.PreserveWorktree = true
		default:
			promptParts = append(promptParts, arg)
		}
	}

	prompt := strings.TrimSpace(strings.Join(promptParts, " "))
	if prompt == "" {
		return MainOptions{}, &usageError{message: MainUsage}
	}

	options.Prompt = prompt
	if options.WorkBranch == "" {
		slug := slugifyPrompt(prompt)
		if len(slug) > 58 {
			slug = slug[:58]
			slug = strings.Trim(slug, "-")
		}
		if slug == "" {
			slug = "task"
		}
		options.WorkBranch = "ralph-" + slug
	}
	return options, nil
}

func ParseTailArgs(args []string, defaultOutput OutputFormat) (TailOptions, error) {
	options := TailOptions{
		Lines:  40,
		Output: defaultOutput,
	}
	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch arg {
		case "--help", "-h":
			return TailOptions{}, &usageError{message: TailUsage}
		case "--lines":
			value, err := requireValue(args, &index, "--lines")
			if err != nil {
				return TailOptions{}, err
			}
			parsed, err := strconv.Atoi(value)
			if err != nil || parsed <= 0 {
				return TailOptions{}, fmt.Errorf("invalid value for --lines: %s", value)
			}
			options.Lines = parsed
		case "--follow":
			options.Follow = true
		case "--raw":
			options.Raw = true
		case "--output":
			value, err := requireValue(args, &index, "--output")
			if err != nil {
				return TailOptions{}, err
			}
			format, err := parseOutputFormat(value)
			if err != nil {
				return TailOptions{}, err
			}
			options.Output = format
		default:
			if strings.HasPrefix(arg, "--") {
				return TailOptions{}, fmt.Errorf("unknown flag: %s", arg)
			}
			if options.Selector != "" {
				return TailOptions{}, &usageError{message: TailUsage}
			}
			options.Selector = arg
		}
	}
	return options, nil
}

func ParseListArgs(args []string, defaultOutput OutputFormat) (ListOptions, error) {
	options := ListOptions{Output: defaultOutput}
	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch arg {
		case "--help", "-h":
			return ListOptions{}, &usageError{message: ListUsage}
		case "--output":
			value, err := requireValue(args, &index, "--output")
			if err != nil {
				return ListOptions{}, err
			}
			format, err := parseOutputFormat(value)
			if err != nil {
				return ListOptions{}, err
			}
			options.Output = format
		default:
			if strings.HasPrefix(arg, "--") {
				return ListOptions{}, fmt.Errorf("unknown flag: %s", arg)
			}
			if options.Selector != "" {
				return ListOptions{}, &usageError{message: ListUsage}
			}
			options.Selector = arg
		}
	}
	return options, nil
}

func requireValue(args []string, index *int, flag string) (string, error) {
	next := *index + 1
	if next >= len(args) {
		return "", fmt.Errorf("missing value for %s", flag)
	}
	*index = next
	return args[next], nil
}

func slugifyPrompt(prompt string) string {
	normalized := strings.ToLower(strings.TrimSpace(prompt))
	normalized = regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(normalized, "-")
	normalized = regexp.MustCompile(`-+`).ReplaceAllString(normalized, "-")
	return strings.Trim(normalized, "-")
}

func sanitizeBranchName(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = regexp.MustCompile(`[^a-z0-9/-]+`).ReplaceAllString(normalized, "-")
	normalized = regexp.MustCompile(`/+`).ReplaceAllString(normalized, "-")
	normalized = regexp.MustCompile(`-+`).ReplaceAllString(normalized, "-")
	normalized = regexp.MustCompile(`^[/.-]+|[/.-]+$`).ReplaceAllString(normalized, "")
	if normalized == "" {
		return ""
	}
	if strings.HasPrefix(normalized, "ralph-") {
		return sanitizeToken(normalized, 64)
	}
	return "ralph-" + sanitizeToken(normalized, 58)
}

func sanitizeToken(value string, maxLength int) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = regexp.MustCompile(`[^a-z0-9-]+`).ReplaceAllString(normalized, "-")
	normalized = regexp.MustCompile(`-+`).ReplaceAllString(normalized, "-")
	normalized = strings.Trim(normalized, "-")
	if maxLength > 0 && len(normalized) > maxLength {
		normalized = strings.TrimRight(normalized[:maxLength], "-")
	}
	return normalized
}
