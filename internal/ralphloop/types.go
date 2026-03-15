package ralphloop

import "time"

type CommandKind string

const (
	CommandInit CommandKind = "init"
	CommandMain CommandKind = "main"
	CommandTail CommandKind = "tail"
	CommandList CommandKind = "ls"
)

type OutputFormat string

const (
	OutputText   OutputFormat = "text"
	OutputJSON   OutputFormat = "json"
	OutputNDJSON OutputFormat = "ndjson"
)

type MainOptions struct {
	Prompt           string
	Model            string
	BaseBranch       string
	MaxIterations    int
	WorkBranch       string
	TimeoutSeconds   int
	ApprovalPolicy   string
	Sandbox          string
	PreserveWorktree bool
	Output           OutputFormat
}

type InitOptions struct {
	BaseBranch string
	WorkBranch string
	Output     OutputFormat
}

type TailOptions struct {
	Lines    int
	Follow   bool
	Raw      bool
	Selector string
	Output   OutputFormat
}

type ListOptions struct {
	Selector string
	Output   OutputFormat
}

type ParsedCommand struct {
	Kind        CommandKind
	InitOptions InitOptions
	MainOptions MainOptions
	TailOptions TailOptions
	ListOptions ListOptions
}

type initResult struct {
	Command       string        `json:"command"`
	Status        string        `json:"status"`
	WorktreeID    string        `json:"worktree_id,omitempty"`
	WorktreePath  string        `json:"worktree_path,omitempty"`
	WorkBranch    string        `json:"work_branch,omitempty"`
	BaseBranch    string        `json:"base_branch,omitempty"`
	DepsInstalled bool          `json:"deps_installed,omitempty"`
	BuildVerified bool          `json:"build_verified,omitempty"`
	RuntimeRoot   string        `json:"runtime_root,omitempty"`
	Events        []eventRecord `json:"events,omitempty"`
	Error         *commandError `json:"error,omitempty"`
}

type commandError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (err *commandError) Error() string {
	if err == nil {
		return ""
	}
	return err.Message
}

func newCommandError(code string, message string) error {
	return &commandError{Code: code, Message: message}
}

type runSummary struct {
	Command      string        `json:"command"`
	Status       string        `json:"status"`
	Phase        string        `json:"phase,omitempty"`
	Iterations   int           `json:"iterations,omitempty"`
	WorktreePath string        `json:"worktree_path,omitempty"`
	WorkBranch   string        `json:"work_branch,omitempty"`
	BaseBranch   string        `json:"base_branch,omitempty"`
	PlanPath     string        `json:"plan_path,omitempty"`
	PRURL        string        `json:"pr_url,omitempty"`
	LogPath      string        `json:"log_path,omitempty"`
	Events       []eventRecord `json:"events,omitempty"`
	Error        *commandError `json:"error,omitempty"`
}

type eventRecord struct {
	Command      string `json:"command"`
	Event        string `json:"event"`
	Status       string `json:"status,omitempty"`
	Phase        string `json:"phase,omitempty"`
	Iteration    int    `json:"iteration,omitempty"`
	Message      string `json:"message,omitempty"`
	WorktreePath string `json:"worktree_path,omitempty"`
	WorkBranch   string `json:"work_branch,omitempty"`
	PlanPath     string `json:"plan_path,omitempty"`
	PRURL        string `json:"pr_url,omitempty"`
	Commit       string `json:"commit,omitempty"`
	TS           string `json:"ts"`
}

type listSessionRecord struct {
	Command      string `json:"command"`
	PID          int    `json:"pid"`
	WorktreeID   string `json:"worktree_id,omitempty"`
	WorktreePath string `json:"worktree_path,omitempty"`
	WorkBranch   string `json:"work_branch,omitempty"`
	LogPath      string `json:"log_path,omitempty"`
	StartedAt    string `json:"started_at,omitempty"`
	Status       string `json:"status,omitempty"`
}

type tailResult struct {
	Command string           `json:"command"`
	Status  string           `json:"status"`
	LogPath string           `json:"log_path,omitempty"`
	Lines   []tailLineRecord `json:"lines,omitempty"`
	Error   *commandError    `json:"error,omitempty"`
}

type tailLineRecord struct {
	Command   string `json:"command"`
	LogPath   string `json:"log_path,omitempty"`
	Line      string `json:"line,omitempty"`
	Timestamp string `json:"ts,omitempty"`
	Event     string `json:"event,omitempty"`
	Status    string `json:"status,omitempty"`
}

func nowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339)
}
