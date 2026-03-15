package ralphloop

import (
	"strings"
	"testing"
)

func TestBuildCodingPromptIncludesPlanPath(t *testing.T) {
	t.Parallel()
	prompt := buildCodingPrompt(codingPromptOptions{
		UserPrompt: "Do the work",
		PlanPath:   "/tmp/plan.md",
	})
	if !strings.Contains(prompt, "/tmp/plan.md") {
		t.Fatalf("expected plan path in prompt: %s", prompt)
	}
}

func TestDefaultPlanFilenameUsesIssueMetadata(t *testing.T) {
	t.Parallel()
	name := defaultPlanFilename("Issue: ABC-123\nTitle: Add health check")
	if name != "abc-123-add-health-check.md" {
		t.Fatalf("unexpected plan filename %q", name)
	}
}
