package ralphloop

import "testing"

func TestContainsCompletionSignal(t *testing.T) {
	t.Parallel()
	if !containsCompletionSignal("done <promise>COMPLETE</promise>") {
		t.Fatal("expected completion signal")
	}
}

func TestCollectAgentText(t *testing.T) {
	t.Parallel()
	got := collectAgentText([]string{" one ", "", "two"})
	if got != "one\ntwo" {
		t.Fatalf("collectAgentText() = %q", got)
	}
}
