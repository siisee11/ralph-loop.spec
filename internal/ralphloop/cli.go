package ralphloop

import (
	"context"
	"encoding/json"
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
	MainUsage   = "Usage: ralph-loop init [options]\n       ralph-loop \"<user prompt>\" [options]\n       ralph-loop tail [selector] [options]\n       ralph-loop ls [selector] [options]\n       ralph-loop schema [command] [options]"
	InitUsage   = "Usage: ralph-loop init [--base-branch <branch>] [--work-branch <name>] [--dry-run] [--json <payload|->] [--output <format>] [--output-file <path>]"
	TailUsage   = "Usage: ralph-loop tail [selector] [--lines N] [--follow] [--raw] [--fields <mask>] [--page N] [--page-size N] [--page-all] [--json <payload|->] [--output <format>] [--output-file <path>]"
	ListUsage   = "Usage: ralph-loop ls [selector] [--fields <mask>] [--page N] [--page-size N] [--page-all] [--json <payload|->] [--output <format>] [--output-file <path>]"
	SchemaUsage = "Usage: ralph-loop schema [command] [--command <name>] [--fields <mask>] [--page N] [--page-size N] [--page-all] [--json <payload|->] [--output <format>] [--output-file <path>]"
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
	runInitFn   = runInitCommand
	runMainFn   = runMain
	runTailFn   = runTailCommand
	runListFn   = runListCommand
	runSchemaFn = runSchemaCommand
)

func Run(args []string, cwd string, stdin io.Reader, stdout io.Writer, stderr io.Writer) int {
	outputMode := detectOutputMode(args, stdout)
	expandedArgs, err := expandJSONInput(args, stdin)
	if err != nil {
		renderCommandError(outputMode, stdout, stderr, commandNameFromArgs(args), normalizeCommandError(err, "invalid_arguments"))
		return 1
	}

	command, err := ParseCommand(expandedArgs, outputMode)
	if err != nil {
		renderCommandError(outputMode, stdout, stderr, commandNameFromArgs(expandedArgs), normalizeCommandError(err, "invalid_arguments"))
		return 1
	}

	outputWriter, closeOutput, err := prepareOutputWriter(cwd, commandOutputFile(command), stdout)
	if err != nil {
		renderCommandError(commandOutputFormat(command), stdout, stderr, commandName(command), normalizeCommandError(err, "invalid_output_file"))
		return 1
	}
	if closeOutput != nil {
		defer closeOutput()
	}

	switch command.Kind {
	case CommandSchema:
		if err := runSchemaFn(command.SchemaOptions, outputWriter, stderr); err != nil {
			renderCommandError(command.SchemaOptions.Output, outputWriter, stderr, string(CommandSchema), normalizeCommandError(err, "schema_failed"))
			return 1
		}
		return 0
	default:
		repoRoot, err := ResolveRepoRoot(cwd)
		if err != nil {
			renderCommandError(commandOutputFormat(command), outputWriter, stderr, commandName(command), normalizeCommandError(err, "resolve_repo_root_failed"))
			return 1
		}
		switch command.Kind {
		case CommandInit:
			if err := runInitFn(context.Background(), repoRoot, command.InitOptions, outputWriter, stderr); err != nil {
				if isRenderedError(err) {
					return 1
				}
				renderCommandError(command.InitOptions.Output, outputWriter, stderr, string(CommandInit), normalizeCommandError(err, "init_failed"))
				return 1
			}
			return 0
		case CommandTail:
			if err := runTailFn(context.Background(), repoRoot, command.TailOptions, outputWriter, stderr); err != nil {
				renderCommandError(command.TailOptions.Output, outputWriter, stderr, string(CommandTail), normalizeCommandError(err, "tail_failed"))
				return 1
			}
			return 0
		case CommandList:
			if err := runListFn(repoRoot, command.ListOptions, outputWriter, stderr); err != nil {
				renderCommandError(command.ListOptions.Output, outputWriter, stderr, string(CommandList), normalizeCommandError(err, "list_failed"))
				return 1
			}
			return 0
		default:
			if err := runMainFn(context.Background(), repoRoot, command.MainOptions, outputWriter, stderr); err != nil {
				if isRenderedError(err) {
					return 1
				}
				renderCommandError(command.MainOptions.Output, outputWriter, stderr, string(CommandMain), normalizeCommandError(err, "run_failed"))
				return 1
			}
			return 0
		}
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

func ParseCommand(args []string, defaultOutput OutputFormat) (ParsedCommand, error) {
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
	if len(args) > 0 && args[0] == "schema" {
		options, err := ParseSchemaArgs(args[1:], defaultOutput)
		if err != nil {
			return ParsedCommand{}, err
		}
		return ParsedCommand{Kind: CommandSchema, SchemaOptions: options}, nil
	}

	options, err := ParseMainArgs(args, defaultOutput)
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

	rawPayload, err := findRawPayload(args)
	if err != nil {
		return InitOptions{}, err
	}
	if rawPayload != "" {
		if err := applyInitPayload(&options, rawPayload); err != nil {
			return InitOptions{}, err
		}
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
			if err := validateSelector("work branch", value); err != nil {
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
		case "--output-file":
			value, err := requireValue(args, &index, "--output-file")
			if err != nil {
				return InitOptions{}, err
			}
			options.OutputFile = value
		case "--dry-run":
			options.DryRun = true
		case "--json":
			index++
		default:
			return InitOptions{}, &usageError{message: InitUsage}
		}
	}

	if strings.TrimSpace(options.BaseBranch) == "" {
		options.BaseBranch = "main"
	}
	return options, nil
}

func ParseMainArgs(args []string, defaultOutput OutputFormat) (MainOptions, error) {
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

	rawPayload, err := findRawPayload(args)
	if err != nil {
		return MainOptions{}, err
	}
	if rawPayload != "" {
		if err := applyMainPayload(&options, rawPayload); err != nil {
			return MainOptions{}, err
		}
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
			if err := validateSelector("work branch", value); err != nil {
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
		case "--output-file":
			value, err := requireValue(args, &index, "--output-file")
			if err != nil {
				return MainOptions{}, err
			}
			options.OutputFile = value
		case "--preserve-worktree":
			options.PreserveWorktree = true
		case "--dry-run":
			options.DryRun = true
		case "--json":
			index++
		default:
			promptParts = append(promptParts, arg)
		}
	}

	prompt := strings.TrimSpace(strings.Join(promptParts, " "))
	if prompt != "" {
		options.Prompt = prompt
	}
	if strings.TrimSpace(options.Prompt) == "" {
		return MainOptions{}, &usageError{message: MainUsage}
	}
	if options.WorkBranch == "" {
		slug := slugifyPrompt(options.Prompt)
		if len(slug) > 58 {
			slug = strings.Trim(slug[:58], "-")
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
		Lines:    40,
		Output:   defaultOutput,
		Page:     1,
		PageSize: 50,
	}
	rawPayload, err := findRawPayload(args)
	if err != nil {
		return TailOptions{}, err
	}
	if rawPayload != "" {
		if err := applyTailPayload(&options, rawPayload); err != nil {
			return TailOptions{}, err
		}
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
		case "--fields":
			value, err := requireValue(args, &index, "--fields")
			if err != nil {
				return TailOptions{}, err
			}
			options.Fields = parseFieldMask(value)
		case "--page":
			value, err := requireValue(args, &index, "--page")
			if err != nil {
				return TailOptions{}, err
			}
			options.Page, err = parsePositiveInt(value, "--page")
			if err != nil {
				return TailOptions{}, err
			}
		case "--page-size":
			value, err := requireValue(args, &index, "--page-size")
			if err != nil {
				return TailOptions{}, err
			}
			options.PageSize, err = parsePositiveInt(value, "--page-size")
			if err != nil {
				return TailOptions{}, err
			}
		case "--page-all":
			options.PageAll = true
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
		case "--output-file":
			value, err := requireValue(args, &index, "--output-file")
			if err != nil {
				return TailOptions{}, err
			}
			options.OutputFile = value
		case "--json":
			index++
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
	if err := validateSelector("selector", options.Selector); err != nil {
		return TailOptions{}, err
	}
	return options, nil
}

func ParseListArgs(args []string, defaultOutput OutputFormat) (ListOptions, error) {
	options := ListOptions{
		Output:   defaultOutput,
		Page:     1,
		PageSize: 50,
	}
	rawPayload, err := findRawPayload(args)
	if err != nil {
		return ListOptions{}, err
	}
	if rawPayload != "" {
		if err := applyListPayload(&options, rawPayload); err != nil {
			return ListOptions{}, err
		}
	}
	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch arg {
		case "--help", "-h":
			return ListOptions{}, &usageError{message: ListUsage}
		case "--fields":
			value, err := requireValue(args, &index, "--fields")
			if err != nil {
				return ListOptions{}, err
			}
			options.Fields = parseFieldMask(value)
		case "--page":
			value, err := requireValue(args, &index, "--page")
			if err != nil {
				return ListOptions{}, err
			}
			options.Page, err = parsePositiveInt(value, "--page")
			if err != nil {
				return ListOptions{}, err
			}
		case "--page-size":
			value, err := requireValue(args, &index, "--page-size")
			if err != nil {
				return ListOptions{}, err
			}
			options.PageSize, err = parsePositiveInt(value, "--page-size")
			if err != nil {
				return ListOptions{}, err
			}
		case "--page-all":
			options.PageAll = true
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
		case "--output-file":
			value, err := requireValue(args, &index, "--output-file")
			if err != nil {
				return ListOptions{}, err
			}
			options.OutputFile = value
		case "--json":
			index++
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
	if err := validateSelector("selector", options.Selector); err != nil {
		return ListOptions{}, err
	}
	return options, nil
}

func ParseSchemaArgs(args []string, defaultOutput OutputFormat) (SchemaOptions, error) {
	options := SchemaOptions{
		Output:   defaultOutput,
		Page:     1,
		PageSize: 50,
	}
	rawPayload, err := findRawPayload(args)
	if err != nil {
		return SchemaOptions{}, err
	}
	if rawPayload != "" {
		if err := applySchemaPayload(&options, rawPayload); err != nil {
			return SchemaOptions{}, err
		}
	}
	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch arg {
		case "--help", "-h":
			return SchemaOptions{}, &usageError{message: SchemaUsage}
		case "--command":
			value, err := requireValue(args, &index, "--command")
			if err != nil {
				return SchemaOptions{}, err
			}
			options.Command = strings.TrimSpace(value)
		case "--fields":
			value, err := requireValue(args, &index, "--fields")
			if err != nil {
				return SchemaOptions{}, err
			}
			options.Fields = parseFieldMask(value)
		case "--page":
			value, err := requireValue(args, &index, "--page")
			if err != nil {
				return SchemaOptions{}, err
			}
			options.Page, err = parsePositiveInt(value, "--page")
			if err != nil {
				return SchemaOptions{}, err
			}
		case "--page-size":
			value, err := requireValue(args, &index, "--page-size")
			if err != nil {
				return SchemaOptions{}, err
			}
			options.PageSize, err = parsePositiveInt(value, "--page-size")
			if err != nil {
				return SchemaOptions{}, err
			}
		case "--page-all":
			options.PageAll = true
		case "--output":
			value, err := requireValue(args, &index, "--output")
			if err != nil {
				return SchemaOptions{}, err
			}
			format, err := parseOutputFormat(value)
			if err != nil {
				return SchemaOptions{}, err
			}
			options.Output = format
		case "--output-file":
			value, err := requireValue(args, &index, "--output-file")
			if err != nil {
				return SchemaOptions{}, err
			}
			options.OutputFile = value
		case "--json":
			index++
		default:
			if strings.HasPrefix(arg, "--") {
				return SchemaOptions{}, fmt.Errorf("unknown flag: %s", arg)
			}
			if options.Command != "" {
				return SchemaOptions{}, &usageError{message: SchemaUsage}
			}
			options.Command = arg
		}
	}
	if err := validateSelector("command", options.Command); err != nil {
		return SchemaOptions{}, err
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

func parsePositiveInt(value string, flag string) (int, error) {
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("invalid value for %s: %s", flag, value)
	}
	return parsed, nil
}

func findRawPayload(args []string) (string, error) {
	rawPayload := ""
	for index := 0; index < len(args); index++ {
		if args[index] != "--json" {
			continue
		}
		value, err := requireValue(args, &index, "--json")
		if err != nil {
			return "", err
		}
		rawPayload = value
	}
	return rawPayload, nil
}

func parseRawPayload(raw string) (map[string]any, error) {
	payload := map[string]any{}
	if strings.TrimSpace(raw) == "" {
		return payload, nil
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil, fmt.Errorf("invalid --json payload: %w", err)
	}
	return payload, nil
}

func applyInitPayload(options *InitOptions, raw string) error {
	payload, err := parseRawPayload(raw)
	if err != nil {
		return err
	}
	options.BaseBranch = firstNonEmpty(payloadString(payload, "base_branch"), options.BaseBranch)
	if value := payloadString(payload, "work_branch"); value != "" {
		if err := validateSelector("work branch", value); err != nil {
			return err
		}
		options.WorkBranch = sanitizeBranchName(value)
	}
	if value, ok := payloadBool(payload, "dry_run"); ok {
		options.DryRun = value
	}
	if value := payloadString(payload, "output"); value != "" {
		format, err := parseOutputFormat(value)
		if err != nil {
			return err
		}
		options.Output = format
	}
	options.OutputFile = firstNonEmpty(payloadString(payload, "output_file"), options.OutputFile)
	return nil
}

func applyMainPayload(options *MainOptions, raw string) error {
	payload, err := parseRawPayload(raw)
	if err != nil {
		return err
	}
	options.Prompt = firstNonEmpty(payloadString(payload, "prompt"), options.Prompt)
	options.Model = firstNonEmpty(payloadString(payload, "model"), options.Model)
	options.BaseBranch = firstNonEmpty(payloadString(payload, "base_branch"), options.BaseBranch)
	if value, ok := payloadInt(payload, "max_iterations"); ok {
		options.MaxIterations = value
	}
	if value := payloadString(payload, "work_branch"); value != "" {
		if err := validateSelector("work branch", value); err != nil {
			return err
		}
		options.WorkBranch = sanitizeBranchName(value)
	}
	if value, ok := payloadInt(payload, "timeout_seconds"); ok {
		options.TimeoutSeconds = value
	}
	options.ApprovalPolicy = firstNonEmpty(payloadString(payload, "approval_policy"), options.ApprovalPolicy)
	options.Sandbox = firstNonEmpty(payloadString(payload, "sandbox"), options.Sandbox)
	if value, ok := payloadBool(payload, "preserve_worktree"); ok {
		options.PreserveWorktree = value
	}
	if value, ok := payloadBool(payload, "dry_run"); ok {
		options.DryRun = value
	}
	if value := payloadString(payload, "output"); value != "" {
		format, err := parseOutputFormat(value)
		if err != nil {
			return err
		}
		options.Output = format
	}
	options.OutputFile = firstNonEmpty(payloadString(payload, "output_file"), options.OutputFile)
	return nil
}

func applyTailPayload(options *TailOptions, raw string) error {
	payload, err := parseRawPayload(raw)
	if err != nil {
		return err
	}
	options.Selector = firstNonEmpty(payloadString(payload, "selector"), options.Selector)
	if value, ok := payloadInt(payload, "lines"); ok {
		options.Lines = value
	}
	if value, ok := payloadBool(payload, "follow"); ok {
		options.Follow = value
	}
	if value, ok := payloadBool(payload, "raw"); ok {
		options.Raw = value
	}
	if fields := payloadFields(payload, "fields"); len(fields) > 0 {
		options.Fields = fields
	}
	if value, ok := payloadInt(payload, "page"); ok {
		options.Page = value
	}
	if value, ok := payloadInt(payload, "page_size"); ok {
		options.PageSize = value
	}
	if value, ok := payloadBool(payload, "page_all"); ok {
		options.PageAll = value
	}
	if value := payloadString(payload, "output"); value != "" {
		format, err := parseOutputFormat(value)
		if err != nil {
			return err
		}
		options.Output = format
	}
	options.OutputFile = firstNonEmpty(payloadString(payload, "output_file"), options.OutputFile)
	return validateSelector("selector", options.Selector)
}

func applyListPayload(options *ListOptions, raw string) error {
	payload, err := parseRawPayload(raw)
	if err != nil {
		return err
	}
	options.Selector = firstNonEmpty(payloadString(payload, "selector"), options.Selector)
	if fields := payloadFields(payload, "fields"); len(fields) > 0 {
		options.Fields = fields
	}
	if value, ok := payloadInt(payload, "page"); ok {
		options.Page = value
	}
	if value, ok := payloadInt(payload, "page_size"); ok {
		options.PageSize = value
	}
	if value, ok := payloadBool(payload, "page_all"); ok {
		options.PageAll = value
	}
	if value := payloadString(payload, "output"); value != "" {
		format, err := parseOutputFormat(value)
		if err != nil {
			return err
		}
		options.Output = format
	}
	options.OutputFile = firstNonEmpty(payloadString(payload, "output_file"), options.OutputFile)
	return validateSelector("selector", options.Selector)
}

func applySchemaPayload(options *SchemaOptions, raw string) error {
	payload, err := parseRawPayload(raw)
	if err != nil {
		return err
	}
	options.Command = firstNonEmpty(payloadString(payload, "command"), options.Command)
	if fields := payloadFields(payload, "fields"); len(fields) > 0 {
		options.Fields = fields
	}
	if value, ok := payloadInt(payload, "page"); ok {
		options.Page = value
	}
	if value, ok := payloadInt(payload, "page_size"); ok {
		options.PageSize = value
	}
	if value, ok := payloadBool(payload, "page_all"); ok {
		options.PageAll = value
	}
	if value := payloadString(payload, "output"); value != "" {
		format, err := parseOutputFormat(value)
		if err != nil {
			return err
		}
		options.Output = format
	}
	options.OutputFile = firstNonEmpty(payloadString(payload, "output_file"), options.OutputFile)
	return nil
}

func payloadString(payload map[string]any, key string) string {
	return strings.TrimSpace(valueString(payload[key]))
}

func payloadBool(payload map[string]any, key string) (bool, bool) {
	value, ok := payload[key]
	if !ok {
		return false, false
	}
	typed, ok := value.(bool)
	return typed, ok
}

func payloadInt(payload map[string]any, key string) (int, bool) {
	value, ok := numberValue(payload[key])
	if !ok {
		return 0, false
	}
	return int(value), true
}

func payloadFields(payload map[string]any, key string) FieldMask {
	value, ok := payload[key]
	if !ok {
		return nil
	}
	switch typed := value.(type) {
	case string:
		return parseFieldMask(typed)
	case []any:
		parts := make([]string, 0, len(typed))
		for _, raw := range typed {
			if text := valueString(raw); text != "" {
				parts = append(parts, text)
			}
		}
		return FieldMask(parts)
	default:
		return nil
	}
}

func expandJSONInput(args []string, stdin io.Reader) ([]string, error) {
	expanded := make([]string, 0, len(args))
	for index := 0; index < len(args); index++ {
		arg := args[index]
		expanded = append(expanded, arg)
		if arg != "--json" {
			continue
		}
		if index+1 >= len(args) {
			return nil, fmt.Errorf("missing value for --json")
		}
		index++
		value := args[index]
		if value == "-" {
			if stdin == nil {
				return nil, fmt.Errorf("stdin is required when using --json -")
			}
			content, err := io.ReadAll(stdin)
			if err != nil {
				return nil, fmt.Errorf("failed to read stdin for --json: %w", err)
			}
			value = strings.TrimSpace(string(content))
		}
		expanded = append(expanded, value)
	}
	return expanded, nil
}

func prepareOutputWriter(cwd string, rawPath string, stdout io.Writer) (io.Writer, func(), error) {
	trimmed := strings.TrimSpace(rawPath)
	if trimmed == "" {
		return stdout, nil, nil
	}
	path, err := validateOutputFilePath(cwd, trimmed)
	if err != nil {
		return nil, nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, nil, err
	}
	file, err := os.Create(path)
	if err != nil {
		return nil, nil, err
	}
	return file, func() { _ = file.Close() }, nil
}

func commandName(parsed ParsedCommand) string {
	if parsed.Kind == "" {
		return string(CommandMain)
	}
	return string(parsed.Kind)
}

func commandNameFromArgs(args []string) string {
	if len(args) > 0 {
		switch args[0] {
		case "init", "tail", "ls", "schema":
			return args[0]
		}
	}
	return string(CommandMain)
}

func commandOutputFormat(parsed ParsedCommand) OutputFormat {
	switch parsed.Kind {
	case CommandInit:
		return parsed.InitOptions.Output
	case CommandTail:
		return parsed.TailOptions.Output
	case CommandList:
		return parsed.ListOptions.Output
	case CommandSchema:
		return parsed.SchemaOptions.Output
	default:
		return parsed.MainOptions.Output
	}
}

func commandOutputFile(parsed ParsedCommand) string {
	switch parsed.Kind {
	case CommandInit:
		return parsed.InitOptions.OutputFile
	case CommandTail:
		return parsed.TailOptions.OutputFile
	case CommandList:
		return parsed.ListOptions.OutputFile
	case CommandSchema:
		return parsed.SchemaOptions.OutputFile
	default:
		return parsed.MainOptions.OutputFile
	}
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
