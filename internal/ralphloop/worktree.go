package ralphloop

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

type initWorktreeOptions struct {
	RepoRoot     string
	BaseBranch   string
	WorkBranch   string
	WorktreeName string
	StatusWriter io.Writer
}

type worktreeInitMetadata struct {
	WorktreeID    string `json:"worktree_id"`
	WorktreePath  string `json:"worktree_path"`
	WorkBranch    string `json:"work_branch"`
	BaseBranch    string `json:"base_branch"`
	RuntimeRoot   string `json:"runtime_root"`
	DepsInstalled bool   `json:"deps_installed"`
	BuildVerified bool   `json:"build_verified"`
	AppPort       int    `json:"app_port,omitempty"`
	WSPort        int    `json:"ws_port,omitempty"`
}

type gitContext struct {
	CommonRoot   string
	CurrentRoot  string
	InsideLinked bool
}

var (
	installDependenciesFn = installDependencies
	verifyBuildFn         = verifyBuild
)

func runInitCommand(ctx context.Context, repoRoot string, options InitOptions, stdout io.Writer, stderr io.Writer) error {
	if options.DryRun {
		planned, err := previewInitWorktree(repoRoot, options)
		if err != nil {
			return err
		}
		return writeJSON(stdout, planned)
	}

	metadata, err := initWorktreeFn(ctx, initWorktreeOptions{
		RepoRoot:     repoRoot,
		BaseBranch:   options.BaseBranch,
		WorkBranch:   options.WorkBranch,
		WorktreeName: strings.TrimPrefix(options.WorkBranch, "ralph-"),
		StatusWriter: stderr,
	})
	if err != nil {
		return err
	}

	result := initResult{
		Command:       string(CommandInit),
		Status:        "ok",
		WorktreeID:    metadata.WorktreeID,
		WorktreePath:  metadata.WorktreePath,
		WorkBranch:    metadata.WorkBranch,
		BaseBranch:    metadata.BaseBranch,
		DepsInstalled: metadata.DepsInstalled,
		BuildVerified: metadata.BuildVerified,
		RuntimeRoot:   metadata.RuntimeRoot,
	}

	switch options.Output {
	case OutputNDJSON:
		return writeJSONLine(stdout, result)
	default:
		return writeJSON(stdout, result)
	}
}

func previewInitWorktree(repoRoot string, options InitOptions) (dryRunResult, error) {
	gitCtx, err := resolveGitContext(repoRoot)
	if err != nil {
		return dryRunResult{}, err
	}

	baseBranch := strings.TrimSpace(options.BaseBranch)
	if baseBranch == "" {
		baseBranch = "main"
	}
	workBranch := strings.TrimSpace(options.WorkBranch)
	if workBranch == "" {
		workBranch = deriveDefaultWorkBranch(gitCtx.CurrentRoot)
	}
	worktreeName := sanitizeToken(strings.TrimPrefix(workBranch, "ralph-"), 48)
	if worktreeName == "" {
		worktreeName = "task"
	}

	worktreePath := predictedWorktreePath(gitCtx, workBranch, worktreeName)
	worktreeID, err := deriveWorktreeID(worktreePath)
	if err != nil {
		return dryRunResult{}, err
	}
	runtimeRoot := filepath.ToSlash(filepath.Join(".worktree", worktreeID)) + "/"

	return dryRunResult{
		Command:      string(CommandInit),
		Status:       "ok",
		DryRun:       true,
		Request: map[string]any{
			"base_branch": baseBranch,
			"work_branch": workBranch,
			"output":      options.Output,
		},
		PlannedSteps: []dryRunStep{
			{Name: "resolve-worktree", Description: "Resolve or create the target git worktree"},
			{Name: "clean-git-state", Description: "Inspect status, stash changes if necessary, and ensure the work branch is checked out"},
			{Name: "install-dependencies", Description: "Detect and install project dependencies"},
			{Name: "verify-build", Description: "Run the repository build or smoke command"},
			{Name: "prepare-runtime", Description: "Populate .env, derive runtime metadata, and ensure runtime directories exist"},
		},
		WorktreePath: worktreePath,
		WorkBranch:   workBranch,
		BaseBranch:   baseBranch,
		RuntimeRoot:  runtimeRoot,
	}, nil
}

func predictedWorktreePath(gitCtx gitContext, workBranch string, worktreeName string) string {
	if gitCtx.InsideLinked {
		return gitCtx.CurrentRoot
	}
	if existingPath := findWorktreePathForBranch(context.Background(), gitCtx.CommonRoot, workBranch); existingPath != "" {
		return existingPath
	}
	return filepath.Join(gitCtx.CommonRoot, ".worktrees", encodeTransportSegment(worktreeName))
}

func initWorktree(ctx context.Context, options initWorktreeOptions) (worktreeInitMetadata, error) {
	gitCtx, err := resolveGitContext(options.RepoRoot)
	if err != nil {
		return worktreeInitMetadata{}, err
	}

	baseBranch := strings.TrimSpace(options.BaseBranch)
	if baseBranch == "" {
		baseBranch = "main"
	}
	workBranch := strings.TrimSpace(options.WorkBranch)
	if workBranch == "" {
		workBranch = deriveDefaultWorkBranch(gitCtx.CurrentRoot)
	}
	worktreeName := sanitizeToken(firstNonEmpty(options.WorktreeName, strings.TrimPrefix(workBranch, "ralph-")), 48)
	if worktreeName == "" {
		worktreeName = "task"
	}

	worktreePath, workBranch, err := ensureWorktree(ctx, gitCtx, baseBranch, workBranch, worktreeName, options.StatusWriter)
	if err != nil {
		return worktreeInitMetadata{}, err
	}
	if err := stashIfDirty(ctx, worktreePath, options.StatusWriter); err != nil {
		return worktreeInitMetadata{}, err
	}
	if err := ensureBranchCheckedOut(ctx, worktreePath, workBranch, baseBranch, options.StatusWriter); err != nil {
		return worktreeInitMetadata{}, err
	}

	worktreeID, err := deriveWorktreeID(worktreePath)
	if err != nil {
		return worktreeInitMetadata{}, err
	}
	runtimeRoot := filepath.ToSlash(filepath.Join(".worktree", worktreeID)) + "/"
	appPort, wsPort, err := resolveWorktreePorts(worktreeID)
	if err != nil {
		return worktreeInitMetadata{}, err
	}

	if err := installDependenciesFn(ctx, worktreePath, options.StatusWriter); err != nil {
		return worktreeInitMetadata{}, err
	}
	if err := verifyBuildFn(ctx, worktreePath, options.StatusWriter); err != nil {
		return worktreeInitMetadata{}, err
	}
	if err := setupEnvConfig(worktreePath, worktreeID, runtimeRoot, appPort, wsPort, options.StatusWriter); err != nil {
		return worktreeInitMetadata{}, err
	}
	if err := ensureRuntimeDirs(worktreePath, runtimeRoot); err != nil {
		return worktreeInitMetadata{}, err
	}

	metadata := worktreeInitMetadata{
		WorktreeID:    worktreeID,
		WorktreePath:  worktreePath,
		WorkBranch:    workBranch,
		BaseBranch:    baseBranch,
		RuntimeRoot:   runtimeRoot,
		DepsInstalled: true,
		BuildVerified: true,
		AppPort:       appPort,
		WSPort:        wsPort,
	}
	if err := writeInitMetadata(worktreePath, runtimeRoot, metadata); err != nil {
		return worktreeInitMetadata{}, err
	}
	return metadata, nil
}

func resolveGitContext(cwd string) (gitContext, error) {
	currentRoot, err := ResolveRepoRoot(cwd)
	if err != nil {
		return gitContext{}, err
	}
	result, err := runCommand(context.Background(), currentRoot, "git", "rev-parse", "--git-common-dir")
	if err != nil {
		return gitContext{}, fmt.Errorf("failed to resolve git common dir: %s", commandFailureMessage(result, err, "git rev-parse --git-common-dir"))
	}
	commonDir := strings.TrimSpace(result.Stdout)
	if commonDir == "" {
		return gitContext{}, fmt.Errorf("failed to resolve git common dir: empty output")
	}
	if !filepath.IsAbs(commonDir) {
		commonDir = filepath.Join(currentRoot, commonDir)
	}
	commonDir, err = filepath.Abs(commonDir)
	if err != nil {
		return gitContext{}, err
	}
	commonRoot := filepath.Dir(commonDir)
	return gitContext{
		CommonRoot:   commonRoot,
		CurrentRoot:  currentRoot,
		InsideLinked: filepath.Clean(commonRoot) != filepath.Clean(currentRoot),
	}, nil
}

func ensureWorktree(ctx context.Context, gitCtx gitContext, baseBranch string, workBranch string, worktreeName string, stderr io.Writer) (string, string, error) {
	if gitCtx.InsideLinked {
		return gitCtx.CurrentRoot, firstNonEmpty(workBranch, currentGitBranch(gitCtx.CurrentRoot)), nil
	}

	if existingPath := findWorktreePathForBranch(ctx, gitCtx.CommonRoot, workBranch); existingPath != "" {
		logInit(stderr, "reusing existing worktree %s for branch %s", existingPath, workBranch)
		return existingPath, workBranch, nil
	}

	targetPath := filepath.Join(gitCtx.CommonRoot, ".worktrees", worktreeName)
	if isGitWorktree(targetPath) {
		logInit(stderr, "reusing worktree %s", targetPath)
		return targetPath, firstNonEmpty(workBranch, currentGitBranch(targetPath)), nil
	}

	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return "", "", err
	}

	if branchExists(ctx, gitCtx.CommonRoot, workBranch) {
		logInit(stderr, "creating worktree %s from existing branch %s", targetPath, workBranch)
		if result, err := runCommand(ctx, gitCtx.CommonRoot, "git", "-C", gitCtx.CommonRoot, "worktree", "add", targetPath, workBranch); err != nil {
			return "", "", fmt.Errorf("git worktree add failed: %s", commandFailureMessage(result, err, "git worktree add"))
		}
		return targetPath, workBranch, nil
	}

	logInit(stderr, "creating worktree %s from %s as %s", targetPath, baseBranch, workBranch)
	result, err := runCommand(ctx, gitCtx.CommonRoot, "git", "-C", gitCtx.CommonRoot, "worktree", "add", "-b", workBranch, targetPath, baseBranch)
	if err != nil {
		return "", "", fmt.Errorf("git worktree add failed: %s", commandFailureMessage(result, err, "git worktree add"))
	}
	return targetPath, workBranch, nil
}

func stashIfDirty(ctx context.Context, cwd string, stderr io.Writer) error {
	result, err := runCommand(ctx, cwd, "git", "status", "--porcelain", "--untracked-files=all")
	if err != nil {
		return fmt.Errorf("failed to inspect git status: %s", commandFailureMessage(result, err, "git status"))
	}
	if strings.TrimSpace(result.Stdout) == "" {
		return nil
	}
	logInit(stderr, "stashing uncommitted changes in %s", cwd)
	result, err = runCommand(ctx, cwd, "git", "stash", "push", "--include-untracked", "-m", "ralph-loop init auto-stash")
	if err != nil {
		return fmt.Errorf("failed to stash changes: %s", commandFailureMessage(result, err, "git stash"))
	}
	return nil
}

func ensureBranchCheckedOut(ctx context.Context, cwd string, workBranch string, baseBranch string, stderr io.Writer) error {
	if strings.TrimSpace(workBranch) == "" {
		return nil
	}
	current := currentGitBranch(cwd)
	if current == workBranch {
		return nil
	}
	if branchExists(ctx, cwd, workBranch) {
		logInit(stderr, "checking out branch %s", workBranch)
		result, err := runCommand(ctx, cwd, "git", "checkout", workBranch)
		if err != nil {
			return fmt.Errorf("failed to checkout branch %s: %s", workBranch, commandFailureMessage(result, err, "git checkout"))
		}
		return nil
	}
	logInit(stderr, "creating branch %s from %s", workBranch, baseBranch)
	result, err := runCommand(ctx, cwd, "git", "checkout", "-b", workBranch, baseBranch)
	if err != nil {
		return fmt.Errorf("failed to create branch %s: %s", workBranch, commandFailureMessage(result, err, "git checkout -b"))
	}
	return nil
}

func installDependencies(ctx context.Context, cwd string, stderr io.Writer) error {
	if fileExists(filepath.Join(cwd, "package.json")) {
		command := "npm"
		args := []string{"install"}
		if fileExists(filepath.Join(cwd, "bun.lockb")) || fileExists(filepath.Join(cwd, "bun.lock")) {
			command = "bun"
			args = []string{"install"}
		}
		logInit(stderr, "installing JavaScript dependencies with %s %s", command, strings.Join(args, " "))
		result, err := runCommand(ctx, cwd, command, args...)
		if err != nil {
			return fmt.Errorf("dependency install failed: %s", commandFailureMessage(result, err, command))
		}
	}
	if fileExists(filepath.Join(cwd, "Cargo.toml")) {
		logInit(stderr, "fetching Rust dependencies with cargo fetch")
		result, err := runCommand(ctx, cwd, "cargo", "fetch")
		if err != nil {
			return fmt.Errorf("dependency install failed: %s", commandFailureMessage(result, err, "cargo fetch"))
		}
	}
	return nil
}

func verifyBuild(ctx context.Context, cwd string, stderr io.Writer) error {
	switch {
	case fileExists(filepath.Join(cwd, "Cargo.toml")):
		logInit(stderr, "verifying build with cargo build")
		result, err := runCommand(ctx, cwd, "cargo", "build")
		if err != nil {
			return fmt.Errorf("build verification failed: %s", commandFailureMessage(result, err, "cargo build"))
		}
		return nil
	case fileExists(filepath.Join(cwd, "go.mod")):
		logInit(stderr, "verifying build with go test ./...")
		result, err := runCommand(ctx, cwd, "go", "test", "./...")
		if err != nil {
			return fmt.Errorf("build verification failed: %s", commandFailureMessage(result, err, "go test ./..."))
		}
		return nil
	case fileExists(filepath.Join(cwd, "package.json")):
		logInit(stderr, "verifying build with npm run build")
		result, err := runCommand(ctx, cwd, "npm", "run", "build")
		if err != nil {
			return fmt.Errorf("build verification failed: %s", commandFailureMessage(result, err, "npm run build"))
		}
		return nil
	default:
		return fmt.Errorf("build verification failed: no supported build command found")
	}
}

func setupEnvConfig(worktreePath string, worktreeID string, runtimeRoot string, appPort int, wsPort int, stderr io.Writer) error {
	envExamplePath := filepath.Join(worktreePath, ".env.example")
	envPath := filepath.Join(worktreePath, ".env")
	if fileExists(envExamplePath) && !fileExists(envPath) {
		logInit(stderr, "copying .env.example to .env")
		content, err := os.ReadFile(envExamplePath)
		if err != nil {
			return err
		}
		if err := os.WriteFile(envPath, content, 0o644); err != nil {
			return err
		}
	}
	if !fileExists(envPath) {
		if err := os.WriteFile(envPath, []byte(""), 0o644); err != nil {
			return err
		}
	}

	vars := map[string]string{
		"DISCODE_WORKTREE_ID":  worktreeID,
		"DISCODE_RUNTIME_ROOT": runtimeRoot,
		"DISCODE_APP_PORT":     strconv.Itoa(appPort),
		"DISCODE_WS_PORT":      strconv.Itoa(wsPort),
	}
	for key, value := range vars {
		_ = os.Setenv(key, value)
	}
	return upsertEnvFile(envPath, vars)
}

func ensureRuntimeDirs(worktreePath string, runtimeRoot string) error {
	dirs := []string{
		filepath.Join(worktreePath, runtimeRoot, "logs"),
		filepath.Join(worktreePath, runtimeRoot, "tmp"),
		filepath.Join(worktreePath, runtimeRoot, "run"),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return nil
}

func deriveWorktreeID(worktreePath string) (string, error) {
	absolute, err := filepath.EvalSymlinks(worktreePath)
	if err != nil {
		absolute = worktreePath
	}
	absolute, err = filepath.Abs(absolute)
	if err != nil {
		return "", err
	}
	hash := sha1.Sum([]byte(filepath.Clean(absolute)))
	prefix := sanitizeToken(filepath.Base(absolute), 24)
	if prefix == "" {
		prefix = "worktree"
	}
	return fmt.Sprintf("%s-%x", prefix, hash[:4]), nil
}

func resolveWorktreePorts(worktreeID string) (int, int, error) {
	appOverride, appHasOverride, err := resolvePortOverride("DISCODE_APP_PORT", "APP_PORT")
	if err != nil {
		return 0, 0, err
	}
	wsOverride, wsHasOverride, err := resolvePortOverride("DISCODE_WS_PORT", "WS_PORT")
	if err != nil {
		return 0, 0, err
	}

	if appHasOverride {
		if !portAvailable(appOverride) {
			return 0, 0, fmt.Errorf("configured app port %d is already occupied", appOverride)
		}
	}
	if wsHasOverride {
		if !portAvailable(wsOverride) {
			return 0, 0, fmt.Errorf("configured websocket port %d is already occupied", wsOverride)
		}
	}
	if appHasOverride && wsHasOverride {
		return appOverride, wsOverride, nil
	}

	offset := int(shortHash(worktreeID) % 1000)
	appBase := envIntDefault([]string{"DISCODE_APP_PORT_BASE", "APP_PORT_BASE"}, 3000)
	wsBase := envIntDefault([]string{"DISCODE_WS_PORT_BASE", "WS_PORT_BASE"}, 4000)
	appCandidate := appBase + offset*2
	wsCandidate := wsBase + offset*2 + 1

	appPort := appOverride
	if !appHasOverride {
		port, err := findAvailablePort(appCandidate, 2, 32)
		if err != nil {
			return 0, 0, fmt.Errorf("failed to assign app port near %d: %w", appCandidate, err)
		}
		appPort = port
	}
	wsPort := wsOverride
	if !wsHasOverride {
		port, err := findAvailablePort(wsCandidate, 2, 32)
		if err != nil {
			return 0, 0, fmt.Errorf("failed to assign websocket port near %d: %w", wsCandidate, err)
		}
		wsPort = port
	}
	return appPort, wsPort, nil
}

func writeInitMetadata(worktreePath string, runtimeRoot string, metadata worktreeInitMetadata) error {
	path := filepath.Join(worktreePath, runtimeRoot, "run", "init.json")
	encoded, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	return os.WriteFile(path, encoded, 0o644)
}

func ensureRalphLogPath(worktree worktreeInitMetadata) (string, error) {
	logPath := filepath.Join(worktree.WorktreePath, worktree.RuntimeRoot, "logs", "ralph-loop.log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return "", err
	}
	return logPath, nil
}

func cleanupWorktree(ctx context.Context, repoRoot string, worktreePath string) error {
	resolvedRepoRoot, err := filepath.Abs(repoRoot)
	if err != nil {
		return err
	}
	resolvedWorktree, err := filepath.Abs(worktreePath)
	if err != nil {
		return err
	}
	if filepath.Clean(resolvedRepoRoot) == filepath.Clean(resolvedWorktree) {
		return nil
	}
	result, err := runCommand(ctx, repoRoot, "git", "-C", repoRoot, "worktree", "remove", "--force", worktreePath)
	if err != nil {
		return fmt.Errorf("failed to remove worktree %s: %s", worktreePath, commandFailureMessage(result, err, "git worktree remove"))
	}
	return nil
}

type commandResult struct {
	Stdout string
	Stderr string
}

func runCommand(ctx context.Context, dir string, command string, args ...string) (commandResult, error) {
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Dir = dir

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	return commandResult{Stdout: stdout.String(), Stderr: stderr.String()}, err
}

func commandFailureMessage(result commandResult, err error, fallback string) string {
	if message := strings.TrimSpace(result.Stderr); message != "" {
		return message
	}
	if message := strings.TrimSpace(result.Stdout); message != "" {
		return message
	}
	if err != nil {
		return err.Error()
	}
	return fallback
}

func deriveDefaultWorkBranch(repoRoot string) string {
	current := currentGitBranch(repoRoot)
	if current != "" && current != "HEAD" && current != "main" {
		return sanitizeBranchName(current)
	}
	return "ralph-" + sanitizeToken(filepath.Base(repoRoot), 48)
}

func currentGitBranch(cwd string) string {
	result, err := runCommand(context.Background(), cwd, "git", "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(result.Stdout)
}

func branchExists(ctx context.Context, cwd string, branch string) bool {
	result, err := runCommand(ctx, cwd, "git", "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	return err == nil && strings.TrimSpace(result.Stderr) == ""
}

func findWorktreePathForBranch(ctx context.Context, cwd string, branch string) string {
	result, err := runCommand(ctx, cwd, "git", "-C", cwd, "worktree", "list", "--porcelain")
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimSpace(result.Stdout), "\n")
	currentPath := ""
	currentBranch := ""
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			if currentBranch == "refs/heads/"+branch && currentPath != "" {
				return currentPath
			}
			currentPath, currentBranch = "", ""
			continue
		}
		switch {
		case strings.HasPrefix(line, "worktree "):
			currentPath = strings.TrimSpace(strings.TrimPrefix(line, "worktree "))
		case strings.HasPrefix(line, "branch "):
			currentBranch = strings.TrimSpace(strings.TrimPrefix(line, "branch "))
		}
	}
	if currentBranch == "refs/heads/"+branch && currentPath != "" {
		return currentPath
	}
	return ""
}

func isGitWorktree(path string) bool {
	info, err := os.Stat(filepath.Join(path, ".git"))
	if err == nil {
		return info != nil
	}
	return false
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func upsertEnvFile(path string, vars map[string]string) error {
	content := ""
	if fileExists(path) {
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		content = string(raw)
	}
	lines := []string{}
	if content != "" {
		lines = strings.Split(strings.TrimRight(content, "\n"), "\n")
	}
	remaining := map[string]string{}
	for key, value := range vars {
		remaining[key] = value
	}
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || !strings.Contains(line, "=") {
			continue
		}
		key := strings.TrimSpace(strings.SplitN(line, "=", 2)[0])
		if value, ok := remaining[key]; ok {
			lines[index] = key + "=" + value
			delete(remaining, key)
		}
	}
	for key, value := range remaining {
		lines = append(lines, key+"="+value)
	}
	output := strings.Join(lines, "\n")
	if output != "" {
		output += "\n"
	}
	return os.WriteFile(path, []byte(output), 0o644)
}

func resolvePortOverride(keys ...string) (int, bool, error) {
	for _, key := range keys {
		value := strings.TrimSpace(os.Getenv(key))
		if value == "" {
			continue
		}
		port, err := strconv.Atoi(value)
		if err != nil || port <= 0 {
			return 0, false, fmt.Errorf("invalid port override %s=%s", key, value)
		}
		return port, true, nil
	}
	return 0, false, nil
}

func envIntDefault(keys []string, fallback int) int {
	for _, key := range keys {
		value := strings.TrimSpace(os.Getenv(key))
		if value == "" {
			continue
		}
		parsed, err := strconv.Atoi(value)
		if err == nil && parsed > 0 {
			return parsed
		}
	}
	return fallback
}

func findAvailablePort(start int, step int, attempts int) (int, error) {
	tried := make([]string, 0, attempts)
	if step <= 0 {
		step = 1
	}
	for index := 0; index < attempts; index++ {
		port := start + index*step
		tried = append(tried, strconv.Itoa(port))
		if portAvailable(port) {
			return port, nil
		}
	}
	return 0, fmt.Errorf("attempted ports: %s", strings.Join(tried, ", "))
}

func portAvailable(port int) bool {
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return false
	}
	_ = listener.Close()
	return true
}

func shortHash(value string) uint32 {
	sum := sha1.Sum([]byte(value))
	return uint32(sum[0])<<24 | uint32(sum[1])<<16 | uint32(sum[2])<<8 | uint32(sum[3])
}

func logInit(writer io.Writer, format string, args ...any) {
	if writer == nil {
		return
	}
	_, _ = fmt.Fprintf(writer, format+"\n", args...)
}
