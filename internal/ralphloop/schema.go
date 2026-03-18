package ralphloop

import (
	"fmt"
	"io"
	"strings"
)

type schemaProperty struct {
	Type        string         `json:"type"`
	Description string         `json:"description,omitempty"`
	Required    bool           `json:"required,omitempty"`
	Default     any            `json:"default,omitempty"`
	Enum        []string       `json:"enum,omitempty"`
	Properties  map[string]any `json:"properties,omitempty"`
}

type commandOptionSchema struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	Description string   `json:"description"`
	Required    bool     `json:"required,omitempty"`
	Default     any      `json:"default,omitempty"`
	Enum        []string `json:"enum,omitempty"`
}

type commandSchemaRecord struct {
	Command          string                  `json:"command"`
	Description      string                  `json:"description"`
	Mutating         bool                    `json:"mutating"`
	SupportsDryRun   bool                    `json:"supports_dry_run"`
	Positionals      []commandOptionSchema   `json:"positionals,omitempty"`
	Options          []commandOptionSchema   `json:"options"`
	RawPayloadSchema map[string]schemaProperty `json:"raw_payload_schema"`
}

func runtimeCommandSchemas() []commandSchemaRecord {
	return []commandSchemaRecord{
		{
			Command:        string(CommandInit),
			Description:    "Create or reuse a prepared worktree",
			Mutating:       true,
			SupportsDryRun: true,
			Options: []commandOptionSchema{
				{Name: "--base-branch", Type: "string", Description: "Base branch name", Default: "main"},
				{Name: "--work-branch", Type: "string", Description: "Work branch name"},
				{Name: "--dry-run", Type: "boolean", Description: "Validate and describe the request without side effects", Default: false},
				{Name: "--json", Type: "object", Description: "Raw command payload"},
				{Name: "--output", Type: "string", Description: "Output format", Default: "json", Enum: []string{"text", "json", "ndjson"}},
				{Name: "--output-file", Type: "string", Description: "Write output to a file under the current working directory"},
			},
			RawPayloadSchema: map[string]schemaProperty{
				"base_branch": {Type: "string", Description: "Base branch name", Default: "main"},
				"work_branch": {Type: "string", Description: "Work branch name"},
				"dry_run":     {Type: "boolean", Description: "Validate without side effects", Default: false},
				"output":      {Type: "string", Description: "Output format", Default: "json", Enum: []string{"text", "json", "ndjson"}},
				"output_file": {Type: "string", Description: "Sandboxed output file path"},
			},
		},
		{
			Command:        string(CommandMain),
			Description:    "Run the full Ralph Loop workflow",
			Mutating:       true,
			SupportsDryRun: true,
			Positionals: []commandOptionSchema{
				{Name: "prompt", Type: "string", Description: "User prompt when not using --json", Required: true},
			},
			Options: []commandOptionSchema{
				{Name: "--model", Type: "string", Description: "Codex model", Default: "gpt-5.3-codex"},
				{Name: "--base-branch", Type: "string", Description: "Base branch", Default: "main"},
				{Name: "--max-iterations", Type: "integer", Description: "Maximum coding iterations", Default: 20},
				{Name: "--work-branch", Type: "string", Description: "Work branch name"},
				{Name: "--timeout", Type: "integer", Description: "Run timeout in seconds", Default: 43200},
				{Name: "--approval-policy", Type: "string", Description: "Approval policy", Default: "never"},
				{Name: "--sandbox", Type: "string", Description: "Sandbox policy", Default: "workspace-write", Enum: []string{"read-only", "workspace-write", "danger-full-access", "readOnly", "workspaceWrite", "dangerFullAccess"}},
				{Name: "--preserve-worktree", Type: "boolean", Description: "Retain the worktree after completion", Default: false},
				{Name: "--dry-run", Type: "boolean", Description: "Validate and describe the request without side effects", Default: false},
				{Name: "--json", Type: "object", Description: "Raw command payload"},
				{Name: "--output", Type: "string", Description: "Output format", Default: "json", Enum: []string{"text", "json", "ndjson"}},
				{Name: "--output-file", Type: "string", Description: "Write output to a file under the current working directory"},
			},
			RawPayloadSchema: map[string]schemaProperty{
				"prompt":            {Type: "string", Description: "User prompt", Required: true},
				"model":             {Type: "string", Description: "Codex model", Default: "gpt-5.3-codex"},
				"base_branch":       {Type: "string", Description: "Base branch", Default: "main"},
				"max_iterations":    {Type: "integer", Description: "Maximum coding iterations", Default: 20},
				"work_branch":       {Type: "string", Description: "Work branch name"},
				"timeout_seconds":   {Type: "integer", Description: "Run timeout in seconds", Default: 43200},
				"approval_policy":   {Type: "string", Description: "Approval policy", Default: "never"},
				"sandbox":           {Type: "string", Description: "Sandbox policy", Default: "workspace-write", Enum: []string{"read-only", "workspace-write", "danger-full-access", "readOnly", "workspaceWrite", "dangerFullAccess"}},
				"preserve_worktree": {Type: "boolean", Description: "Retain the worktree after completion", Default: false},
				"dry_run":           {Type: "boolean", Description: "Validate without side effects", Default: false},
				"output":            {Type: "string", Description: "Output format", Default: "json", Enum: []string{"text", "json", "ndjson"}},
				"output_file":       {Type: "string", Description: "Sandboxed output file path"},
			},
		},
		{
			Command:        string(CommandTail),
			Description:    "Read or follow Ralph Loop logs",
			SupportsDryRun: false,
			Options: []commandOptionSchema{
				{Name: "--lines", Type: "integer", Description: "Number of lines to read", Default: 40},
				{Name: "--follow", Type: "boolean", Description: "Follow the log stream", Default: false},
				{Name: "--raw", Type: "boolean", Description: "Emit raw log lines", Default: false},
				{Name: "--fields", Type: "string", Description: "Comma-separated field mask"},
				{Name: "--page", Type: "integer", Description: "Page number", Default: 1},
				{Name: "--page-size", Type: "integer", Description: "Page size", Default: 50},
				{Name: "--page-all", Type: "boolean", Description: "Return all pages", Default: false},
				{Name: "--json", Type: "object", Description: "Raw command payload"},
				{Name: "--output", Type: "string", Description: "Output format", Default: "json", Enum: []string{"text", "json", "ndjson"}},
				{Name: "--output-file", Type: "string", Description: "Write output to a file under the current working directory"},
			},
			RawPayloadSchema: map[string]schemaProperty{
				"selector":    {Type: "string", Description: "Log selector"},
				"lines":       {Type: "integer", Description: "Number of lines to read", Default: 40},
				"follow":      {Type: "boolean", Description: "Follow the log stream", Default: false},
				"raw":         {Type: "boolean", Description: "Emit raw log lines", Default: false},
				"fields":      {Type: "array", Description: "Field mask"},
				"page":        {Type: "integer", Description: "Page number", Default: 1},
				"page_size":   {Type: "integer", Description: "Page size", Default: 50},
				"page_all":    {Type: "boolean", Description: "Return all pages", Default: false},
				"output":      {Type: "string", Description: "Output format", Default: "json", Enum: []string{"text", "json", "ndjson"}},
				"output_file": {Type: "string", Description: "Sandboxed output file path"},
			},
		},
		{
			Command:        string(CommandList),
			Description:    "List running Ralph Loop sessions",
			SupportsDryRun: false,
			Options: []commandOptionSchema{
				{Name: "--fields", Type: "string", Description: "Comma-separated field mask"},
				{Name: "--page", Type: "integer", Description: "Page number", Default: 1},
				{Name: "--page-size", Type: "integer", Description: "Page size", Default: 50},
				{Name: "--page-all", Type: "boolean", Description: "Return all pages", Default: false},
				{Name: "--json", Type: "object", Description: "Raw command payload"},
				{Name: "--output", Type: "string", Description: "Output format", Default: "json", Enum: []string{"text", "json", "ndjson"}},
				{Name: "--output-file", Type: "string", Description: "Write output to a file under the current working directory"},
			},
			RawPayloadSchema: map[string]schemaProperty{
				"selector":    {Type: "string", Description: "Session selector"},
				"fields":      {Type: "array", Description: "Field mask"},
				"page":        {Type: "integer", Description: "Page number", Default: 1},
				"page_size":   {Type: "integer", Description: "Page size", Default: 50},
				"page_all":    {Type: "boolean", Description: "Return all pages", Default: false},
				"output":      {Type: "string", Description: "Output format", Default: "json", Enum: []string{"text", "json", "ndjson"}},
				"output_file": {Type: "string", Description: "Sandboxed output file path"},
			},
		},
		{
			Command:        string(CommandSchema),
			Description:    "Describe the live command schemas",
			SupportsDryRun: false,
			Options: []commandOptionSchema{
				{Name: "--command", Type: "string", Description: "Command to describe"},
				{Name: "--fields", Type: "string", Description: "Comma-separated field mask"},
				{Name: "--page", Type: "integer", Description: "Page number", Default: 1},
				{Name: "--page-size", Type: "integer", Description: "Page size", Default: 50},
				{Name: "--page-all", Type: "boolean", Description: "Return all pages", Default: false},
				{Name: "--json", Type: "object", Description: "Raw command payload"},
				{Name: "--output", Type: "string", Description: "Output format", Default: "json", Enum: []string{"text", "json", "ndjson"}},
				{Name: "--output-file", Type: "string", Description: "Write output to a file under the current working directory"},
			},
			RawPayloadSchema: map[string]schemaProperty{
				"command":     {Type: "string", Description: "Command to describe"},
				"fields":      {Type: "array", Description: "Field mask"},
				"page":        {Type: "integer", Description: "Page number", Default: 1},
				"page_size":   {Type: "integer", Description: "Page size", Default: 50},
				"page_all":    {Type: "boolean", Description: "Return all pages", Default: false},
				"output":      {Type: "string", Description: "Output format", Default: "json", Enum: []string{"text", "json", "ndjson"}},
				"output_file": {Type: "string", Description: "Sandboxed output file path"},
			},
		},
	}
}

func runSchemaCommand(options SchemaOptions, stdout io.Writer, _ io.Writer) error {
	schemas := runtimeCommandSchemas()
	if strings.TrimSpace(options.Command) != "" {
		selected := make([]commandSchemaRecord, 0, 1)
		for _, schema := range schemas {
			if schema.Command == options.Command {
				selected = append(selected, schema)
				break
			}
		}
		if len(selected) == 0 {
			return fmt.Errorf("unknown schema command: %s", options.Command)
		}
		schemas = selected
	}

	if options.Output == OutputText {
		for _, schema := range schemas {
			_, _ = fmt.Fprintf(stdout, "%s: %s\n", schema.Command, schema.Description)
		}
		return nil
	}
	return renderPagedResult(writerAdapter{target: stdout}, options.Output, string(CommandSchema), "ok", schemasToMaps(schemas), options.Fields, options.Page, options.PageSize, options.PageAll)
}

func schemasToMaps(schemas []commandSchemaRecord) []map[string]any {
	items := make([]map[string]any, 0, len(schemas))
	for _, schema := range schemas {
		items = append(items, map[string]any{
			"command":            schema.Command,
			"description":        schema.Description,
			"mutating":           schema.Mutating,
			"supports_dry_run":   schema.SupportsDryRun,
			"positionals":        schema.Positionals,
			"options":            schema.Options,
			"raw_payload_schema": schema.RawPayloadSchema,
		})
	}
	return items
}
