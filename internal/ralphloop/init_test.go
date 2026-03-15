package ralphloop

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestInitWorktreeCreatesRuntimeMetadata(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module example.com/test\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "config", "user.email", "test@example.com")
	runGit(t, repo, "config", "user.name", "Test User")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "init")

	oldInstall := installDependenciesFn
	oldVerify := verifyBuildFn
	t.Cleanup(func() {
		installDependenciesFn = oldInstall
		verifyBuildFn = oldVerify
	})
	installDependenciesFn = func(context.Context, string, io.Writer) error { return nil }
	verifyBuildFn = func(context.Context, string, io.Writer) error { return nil }

	var stderr bytes.Buffer
	metadata, err := initWorktree(context.Background(), initWorktreeOptions{
		RepoRoot:     repo,
		BaseBranch:   "main",
		WorkBranch:   "ralph-init-test",
		WorktreeName: "init-test",
		StatusWriter: &stderr,
	})
	if err != nil {
		t.Fatalf("initWorktree() error = %v stderr=%s", err, stderr.String())
	}
	if metadata.WorktreeID == "" {
		t.Fatal("expected worktree id")
	}
	if !fileExists(filepath.Join(metadata.WorktreePath, metadata.RuntimeRoot, "run", "init.json")) {
		t.Fatal("expected init metadata file")
	}
	content, err := os.ReadFile(filepath.Join(metadata.WorktreePath, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(content, []byte("DISCODE_WORKTREE_ID="+metadata.WorktreeID)) {
		t.Fatalf("expected .env to contain DISCODE_WORKTREE_ID, got %s", string(content))
	}
}

func TestRunInitCommandWritesJSON(t *testing.T) {
	oldInit := initWorktreeFn
	t.Cleanup(func() { initWorktreeFn = oldInit })
	initWorktreeFn = func(context.Context, initWorktreeOptions) (worktreeInitMetadata, error) {
		return worktreeInitMetadata{
			WorktreeID:    "foo-1234",
			WorktreePath:  "/tmp/foo",
			WorkBranch:    "ralph-foo",
			BaseBranch:    "main",
			RuntimeRoot:   ".worktree/foo-1234/",
			DepsInstalled: true,
			BuildVerified: true,
		}, nil
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := runInitCommand(context.Background(), "/repo", InitOptions{BaseBranch: "main", Output: OutputJSON}, &stdout, &stderr); err != nil {
		t.Fatalf("runInitCommand() error = %v", err)
	}
	result := initResult{}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("expected JSON output: %v", err)
	}
	if result.Command != "init" || result.Status != "ok" {
		t.Fatalf("unexpected result %+v", result)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	result, err := runCommand(context.Background(), dir, "git", args...)
	if err != nil {
		t.Fatalf("git %v failed: %v stdout=%s stderr=%s", args, err, result.Stdout, result.Stderr)
	}
}
