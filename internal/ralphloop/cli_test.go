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

	options, err := ParseMainArgs([]string{"ship", "feature"}, t.TempDir(), OutputJSON)
	if err != nil {
		t.Fatalf("ParseMainArgs() error = %v", err)
	}
	if options.Output != OutputJSON {
		t.Fatalf("expected output json, got %s", options.Output)
	}
	if options.WorkBranch != "ralph-ship-feature" {
		t.Fatalf("expected default branch, got %s", options.WorkBranch)
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
	code := Run([]string{"task"}, repo, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run() code = %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"status": "completed"`) {
		t.Fatalf("expected JSON output, got %s", stdout.String())
	}
}
