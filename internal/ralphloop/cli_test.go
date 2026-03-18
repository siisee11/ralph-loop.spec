package ralphloop

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseMainArgsDefaults(t *testing.T) {
	t.Parallel()

	options, err := ParseMainArgs([]string{"ship", "feature"}, OutputJSON)
	if err != nil {
		t.Fatalf("ParseMainArgs() error = %v", err)
	}
	if options.Output != OutputJSON {
		t.Fatalf("expected output json, got %s", options.Output)
	}
	if options.WorkBranch != "ralph-ship-feature" {
		t.Fatalf("expected default branch, got %s", options.WorkBranch)
	}
	if options.TimeoutSeconds != 43200 {
		t.Fatalf("expected default timeout 43200, got %d", options.TimeoutSeconds)
	}
}

func TestParseInitArgsDefaults(t *testing.T) {
	t.Parallel()

	options, err := ParseInitArgs(nil, OutputJSON)
	if err != nil {
		t.Fatalf("ParseInitArgs() error = %v", err)
	}
	if options.BaseBranch != "main" {
		t.Fatalf("expected default base branch main, got %s", options.BaseBranch)
	}
	if options.Output != OutputJSON {
		t.Fatalf("expected output json, got %s", options.Output)
	}
}

func TestRunDefaultsToJSONWhenStdoutIsNotTTY(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	original := runMainFn
	t.Cleanup(func() { runMainFn = original })
	runMainFn = func(_ context.Context, _ string, options MainOptions, stdout io.Writer, _ io.Writer) error {
		if options.Output != OutputJSON {
			t.Fatalf("expected OutputJSON, got %s", options.Output)
		}
		return writeJSON(stdout, runSummary{Command: "main", Status: "completed"})
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run([]string{"task"}, repo, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run() code = %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"status": "completed"`) {
		t.Fatalf("expected JSON output, got %s", stdout.String())
	}
}

func TestRunInitDispatches(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	original := runInitFn
	t.Cleanup(func() { runInitFn = original })
	runInitFn = func(_ context.Context, _ string, options InitOptions, stdout io.Writer, _ io.Writer) error {
		if options.BaseBranch != "dev" {
			t.Fatalf("expected base branch dev, got %s", options.BaseBranch)
		}
		return writeJSON(stdout, initResult{Command: "init", Status: "ok"})
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run([]string{"init", "--base-branch", "dev"}, repo, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run() code = %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"command": "init"`) {
		t.Fatalf("expected init output, got %s", stdout.String())
	}
}
