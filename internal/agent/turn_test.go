package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/ch55secake/symphony/internal/audit"
	"github.com/ch55secake/symphony/internal/events"
	"github.com/ch55secake/symphony/internal/session"
)

var errProvider = errors.New("provider failed")

type recordingStore struct {
	events       []events.Event
	failOnAppend int
}

func (s *recordingStore) Append(_ context.Context, event events.Event, expectedRevision *uint64) (uint64, error) {
	if s.failOnAppend > 0 && len(s.events)+1 == s.failOnAppend {
		return 0, errors.New("append failed")
	}
	s.events = append(s.events, event)
	if expectedRevision == nil {
		return 0, nil
	}
	return *expectedRevision + 1, nil
}

type fakeProvider struct {
	completion Completion
	err        error
	calls      int
}

func (p *fakeProvider) Name() string {
	return "fake"
}

func (p *fakeProvider) Complete(_ context.Context, _ CompletionRequest) (Completion, error) {
	p.calls++
	return p.completion, p.err
}

func TestRunPersistsOrderedRedactedTurn(t *testing.T) {
	t.Parallel()
	store := &recordingStore{}
	sessions := session.New(store, audit.DefaultPolicy())
	handle, err := sessions.Start(context.Background(), "user", "/workspace")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	service, err := New(sessions)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	provider := &fakeProvider{completion: Completion{
		Content:    "Bearer model-secret",
		StopReason: "tool_use",
		ToolCalls: []events.ModelToolCall{{
			ID:        "call-1",
			Name:      "write_file",
			Arguments: json.RawMessage(`{"path":"note.txt","content":"ordinary-write-content"}`),
		}},
		InputTokens:  10,
		OutputTokens: 20,
	}}
	request := CompletionRequest{
		Model:    "test-model",
		Messages: []Message{{Role: RoleUser, Content: "Bearer user-secret"}},
		Tools:    []ToolDefinition{{Name: "write_file", InputSchema: json.RawMessage(`{"type":"object"}`)}},
	}
	completion, err := service.Run(context.Background(), handle, "user", provider, request)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if completion.StopReason != "tool_use" || provider.calls != 1 {
		t.Fatalf("completion = %#v, provider calls = %d", completion, provider.calls)
	}
	if len(store.events) != 4 || store.events[1].Type != events.UserMessage || store.events[2].Type != events.ModelRequested || store.events[3].Type != events.ModelCompletedV2 {
		t.Fatalf("events = %#v, want ordered model turn", store.events)
	}
	if store.events[2].CausationID == nil || *store.events[2].CausationID != store.events[1].ID {
		t.Fatal("model request does not cite user message as its cause")
	}
	if store.events[3].CausationID == nil || *store.events[3].CausationID != store.events[2].ID {
		t.Fatal("model completion does not cite model request as its cause")
	}
	var payload events.ModelCompletedV2Payload
	if err := json.Unmarshal(store.events[3].Payload, &payload); err != nil {
		t.Fatalf("unmarshal completion payload: %v", err)
	}
	if payload.Content != audit.RedactedValue || payload.ToolCalls[0].Name != "write_file" || payload.ToolCalls[0].ArgumentsHash != events.Hash([]byte(`{"path":"note.txt","content":"ordinary-write-content"}`)) || strings.Contains(string(store.events[3].Payload), "ordinary-write-content") {
		t.Fatalf("persisted completion = %#v, want safe tool-call summary", payload)
	}
}

func TestRunRecordsProviderFailure(t *testing.T) {
	t.Parallel()
	store := &recordingStore{}
	sessions := session.New(store, audit.DefaultPolicy())
	handle, err := sessions.Start(context.Background(), "user", "/workspace")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	service, err := New(sessions)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	provider := &fakeProvider{err: errProvider}
	_, err = service.Run(context.Background(), handle, "user", provider, CompletionRequest{
		Model:    "test-model",
		Messages: []Message{{Role: RoleUser, Content: "hello"}},
	})
	if !errors.Is(err, errProvider) {
		t.Fatalf("Run() error = %v, want provider error", err)
	}
	if len(store.events) != 4 || store.events[3].Type != events.ModelFailed {
		t.Fatalf("events = %#v, want failed model outcome", store.events)
	}
	var payload events.ModelFailedPayload
	if err := json.Unmarshal(store.events[3].Payload, &payload); err != nil {
		t.Fatalf("unmarshal failure payload: %v", err)
	}
	if payload.Code != "provider_error" {
		t.Fatalf("failure code = %q, want provider_error", payload.Code)
	}
}

func TestRunDoesNotCallProviderWhenIntentPersistenceFails(t *testing.T) {
	t.Parallel()
	store := &recordingStore{failOnAppend: 3}
	sessions := session.New(store, audit.DefaultPolicy())
	handle, err := sessions.Start(context.Background(), "user", "/workspace")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	service, err := New(sessions)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	provider := &fakeProvider{}
	_, err = service.Run(context.Background(), handle, "user", provider, CompletionRequest{
		Model:    "test-model",
		Messages: []Message{{Role: RoleUser, Content: "hello"}},
	})
	if err == nil {
		t.Fatal("Run() error = nil, want model intent persistence failure")
	}
	if provider.calls != 0 {
		t.Fatalf("provider calls = %d, want 0", provider.calls)
	}
	if len(store.events) != 2 || store.events[1].Type != events.UserMessage {
		t.Fatalf("events = %#v, want start and user message only", store.events)
	}
}
