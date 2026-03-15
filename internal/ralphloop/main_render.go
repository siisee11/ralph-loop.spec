package ralphloop

import (
	"fmt"
	"io"
)

func newMainEmitter(format OutputFormat, stdout io.Writer, stderr io.Writer, summary *runSummary) func(eventRecord) {
	return func(event eventRecord) {
		summary.Events = append(summary.Events, event)
		switch format {
		case OutputNDJSON:
			_ = writeJSONLine(stdout, event)
		case OutputText:
			renderTextEvent(stdout, stderr, event)
		}
	}
}

func renderTextEvent(stdout io.Writer, stderr io.Writer, event eventRecord) {
	target := stdout
	if event.Status == "failed" && stderr != nil {
		target = stderr
	}
	switch event.Event {
	case "run.started":
		_, _ = fmt.Fprintf(target, "Starting Ralph Loop in %s on %s\n", event.WorktreePath, event.WorkBranch)
	case "phase.started":
		_, _ = fmt.Fprintf(target, "Phase: %s\n", event.Phase)
	case "iteration.completed":
		if event.Commit != "" {
			_, _ = fmt.Fprintf(target, "Iteration %d complete (%s)\n", event.Iteration, event.Commit)
		} else {
			_, _ = fmt.Fprintf(target, "Iteration %d complete\n", event.Iteration)
		}
	case "run.completed":
		if event.PRURL != "" {
			_, _ = fmt.Fprintf(target, "Completed: %s\n", event.PRURL)
		} else {
			_, _ = fmt.Fprintln(target, "Completed")
		}
	case "agent.message":
		if event.Message != "" {
			_, _ = fmt.Fprintln(target, event.Message)
		}
	}
}

func finalizeMain(format OutputFormat, stdout io.Writer, _ io.Writer, summary *runSummary) error {
	if summary.Status == "" {
		summary.Status = "completed"
	}
	if format == OutputJSON {
		return writeJSON(stdout, summary)
	}
	return nil
}

func finishMainError(format OutputFormat, stdout io.Writer, stderr io.Writer, summary *runSummary, err error) error {
	summary.Status = "failed"
	summary.Error = normalizeCommandError(err, "run_failed")
	if format == OutputJSON {
		if writeErr := writeJSON(stdout, summary); writeErr != nil {
			return writeErr
		}
		return markRendered(err)
	}
	if format == OutputNDJSON {
		terminal := eventRecord{
			Command:      string(CommandMain),
			Event:        "run.failed",
			Status:       "failed",
			Phase:        summary.Phase,
			WorktreePath: summary.WorktreePath,
			WorkBranch:   summary.WorkBranch,
			PlanPath:     summary.PlanPath,
			Message:      summary.Error.Message,
			TS:           nowRFC3339(),
		}
		_ = writeJSONLine(stdout, terminal)
		return markRendered(err)
	}
	_, _ = fmt.Fprintln(stderr, err.Error())
	return markRendered(err)
}
