package ralphloop

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
)

type ralphSessionView struct {
	PID          int
	WorktreeID   string
	WorktreePath string
	WorkBranch   string
	LogPath      string
	StartedAt    string
}

func runListCommand(repoRoot string, options ListOptions, stdout io.Writer, _ io.Writer) error {
	sessions, err := listRunningRalphSessions(repoRoot, options.Selector)
	if err != nil {
		return err
	}

	switch options.Output {
	case OutputJSON:
		records := make([]listSessionRecord, 0, len(sessions))
		for _, session := range sessions {
			records = append(records, sessionRecord(session))
		}
		return writeJSON(stdout, records)
	case OutputNDJSON:
		for _, session := range sessions {
			if err := writeJSONLine(stdout, sessionRecord(session)); err != nil {
				return err
			}
		}
		return nil
	default:
		if len(sessions) == 0 {
			_, _ = fmt.Fprintf(stdout, "No running Ralph Loop sessions found under %s\n", repoRoot)
			return nil
		}
		_, _ = fmt.Fprintln(stdout, "Running Ralph Loop sessions:")
		writer := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
		_, _ = fmt.Fprintln(writer, "PID\tWORKTREE\tBRANCH\tSTARTED\tLOG")
		for _, session := range sessions {
			_, _ = fmt.Fprintf(writer, "%d\t%s\t%s\t%s\t%s\n", session.PID, filepath.ToSlash(session.WorktreePath), firstNonEmpty(session.WorkBranch, "-"), firstNonEmpty(session.StartedAt, "-"), filepath.ToSlash(session.LogPath))
		}
		return writer.Flush()
	}
}

func sessionRecord(session ralphSessionView) listSessionRecord {
	return listSessionRecord{
		Command:      string(CommandList),
		PID:          session.PID,
		WorktreeID:   session.WorktreeID,
		WorktreePath: session.WorktreePath,
		WorkBranch:   session.WorkBranch,
		LogPath:      session.LogPath,
		StartedAt:    session.StartedAt,
		Status:       "running",
	}
}

func listRunningRalphSessions(repoRoot string, selector string) ([]ralphSessionView, error) {
	pidFiles, err := listRalphSessionPIDFiles(repoRoot)
	if err != nil {
		return nil, err
	}

	sessions := make([]ralphSessionView, 0, len(pidFiles))
	for _, pidPath := range pidFiles {
		session, ok := loadRunningRalphSession(pidPath)
		if !ok {
			continue
		}
		if strings.TrimSpace(selector) != "" && !matchesSessionSelector(session, selector) {
			continue
		}
		sessions = append(sessions, session)
	}

	sort.Slice(sessions, func(i, j int) bool {
		if sessions[i].StartedAt != sessions[j].StartedAt {
			return sessions[i].StartedAt > sessions[j].StartedAt
		}
		return sessions[i].WorktreePath < sessions[j].WorktreePath
	})
	return sessions, nil
}

func listRalphSessionPIDFiles(repoRoot string) ([]string, error) {
	roots := []string{filepath.Join(repoRoot, ".worktree"), filepath.Join(repoRoot, ".worktrees")}
	files := make([]string, 0, 8)
	seen := map[string]struct{}{}

	for _, root := range roots {
		if _, err := os.Stat(root); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				return nil
			}
			if !strings.HasSuffix(filepath.ToSlash(path), "/run/ralph-loop.pid") {
				return nil
			}
			if _, ok := seen[path]; ok {
				return nil
			}
			seen[path] = struct{}{}
			files = append(files, path)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	sort.Strings(files)
	return files, nil
}

func loadRunningRalphSession(pidPath string) (ralphSessionView, bool) {
	content, err := os.ReadFile(pidPath)
	if err != nil {
		return ralphSessionView{}, false
	}
	pidText := strings.TrimSpace(string(content))
	if pidText == "" {
		return ralphSessionView{}, false
	}
	pid := 0
	if _, err := fmt.Sscanf(pidText, "%d", &pid); err != nil || !pidIsRunning(pid) {
		return ralphSessionView{}, false
	}

	state := ralphSessionState{}
	metadataPath := strings.TrimSuffix(pidPath, ".pid") + ".json"
	if metadataBytes, err := os.ReadFile(metadataPath); err == nil {
		_ = json.Unmarshal(metadataBytes, &state)
	}
	state.PID = pid

	return ralphSessionView{
		PID:          state.PID,
		WorktreeID:   state.WorktreeID,
		WorktreePath: state.WorktreePath,
		WorkBranch:   state.WorkBranch,
		LogPath:      state.LogPath,
		StartedAt:    state.StartedAt,
	}, true
}

func matchesSessionSelector(session ralphSessionView, selector string) bool {
	target := strings.TrimSpace(selector)
	if target == "" {
		return true
	}
	fields := []string{
		session.WorktreeID,
		session.WorktreePath,
		session.WorkBranch,
		session.LogPath,
	}
	for _, field := range fields {
		if strings.Contains(field, target) {
			return true
		}
	}
	return false
}
