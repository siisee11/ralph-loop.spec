package ralphloop

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestAppServerClientLifecycle(t *testing.T) {
	command := fmt.Sprintf("%s -test.run=TestHelperProcessAppServer --", os.Args[0])
	t.Setenv("RALPH_LOOP_CODEX_COMMAND", command)
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")

	client, err := newAppServerClient("")
	if err != nil {
		t.Fatalf("newAppServerClient() error = %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Initialize(ctx); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	threadID, err := client.StartThread(ctx, startThreadOptions{
		Model:          "gpt-5.3-codex",
		Cwd:            "/tmp/project",
		ApprovalPolicy: "never",
		Sandbox:        "workspace-write",
	})
	if err != nil {
		t.Fatalf("StartThread() error = %v", err)
	}
	if threadID != "thread-1" {
		t.Fatalf("expected thread-1, got %q", threadID)
	}

	result, err := client.RunTurn(ctx, runTurnOptions{
		ThreadID: "thread-1",
		Prompt:   "hello",
		Timeout:  time.Second,
	})
	if err != nil {
		t.Fatalf("RunTurn() error = %v", err)
	}
	if result.Status != "completed" {
		t.Fatalf("expected completed, got %s", result.Status)
	}
	if !containsCompletionSignal(result.AgentText) {
		t.Fatalf("expected agent text to contain completion token, got %q", result.AgentText)
	}
}

func TestHelperProcessAppServer(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	defer os.Exit(0)

	decoder := json.NewDecoder(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)

	for {
		var envelope jsonRPCEnvelope
		if err := decoder.Decode(&envelope); err != nil {
			return
		}
		method := strings.TrimSpace(envelope.Method)
		switch method {
		case "initialize":
			_ = encoder.Encode(jsonRPCEnvelope{ID: envelope.ID, Result: mustMarshalRaw(map[string]any{})})
		case "initialized":
		case "thread/start":
			_ = encoder.Encode(jsonRPCEnvelope{ID: envelope.ID, Result: mustMarshalRaw(map[string]any{"thread": map[string]any{"id": "thread-1"}})})
		case "turn/start":
			_ = encoder.Encode(jsonRPCEnvelope{ID: envelope.ID, Result: mustMarshalRaw(map[string]any{"turn": map[string]any{"id": "turn-1"}})})
			_ = encoder.Encode(jsonRPCEnvelope{Method: "turn/started", Params: mustMarshalRaw(map[string]any{"turn": map[string]any{"id": "turn-1"}})})
			_ = encoder.Encode(jsonRPCEnvelope{Method: "item/completed", Params: mustMarshalRaw(map[string]any{
				"item": map[string]any{"type": "agentMessage", "text": "done " + completeToken},
			})})
			_ = encoder.Encode(jsonRPCEnvelope{Method: "turn/completed", Params: mustMarshalRaw(map[string]any{"turn": map[string]any{"id": "turn-1", "status": "completed"}})})
		case "thread/compact/start", "turn/interrupt":
			_ = encoder.Encode(jsonRPCEnvelope{ID: envelope.ID, Result: mustMarshalRaw(map[string]any{})})
		default:
			_ = encoder.Encode(jsonRPCEnvelope{ID: envelope.ID, Error: &jsonRPCError{Code: -32601, Message: method}})
		}
	}
}
