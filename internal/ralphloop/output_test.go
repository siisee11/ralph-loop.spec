package ralphloop

import (
	"bytes"
	"strings"
	"testing"
)

func TestRenderCommandErrorJSON(t *testing.T) {
	t.Parallel()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	renderCommandError(OutputJSON, &stdout, &stderr, "main", &commandError{Code: "boom", Message: "failed"})
	if stderr.Len() != 0 {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}
	if !strings.Contains(stdout.String(), `"command": "main"`) {
		t.Fatalf("expected structured output, got %s", stdout.String())
	}
}

func TestMainEmitterNDJSON(t *testing.T) {
	t.Parallel()
	var stdout bytes.Buffer
	summary := &runSummary{Command: "main"}
	emit := newMainEmitter(OutputNDJSON, &stdout, &stdout, summary)
	emit(eventRecord{Command: "main", Event: "run.started", TS: nowRFC3339()})
	if !strings.Contains(stdout.String(), `"event":"run.started"`) {
		t.Fatalf("expected ndjson event, got %s", stdout.String())
	}
}
