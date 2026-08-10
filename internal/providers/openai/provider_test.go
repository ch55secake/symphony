package openai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ch55secake/symphony/internal/agent"
	"github.com/ch55secake/symphony/internal/events"
)

func TestCompleteMapsResponseAndDisablesOpenAIStorage(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/responses" {
			t.Fatalf("request = %s %s, want POST /responses", request.Method, request.URL.Path)
		}
		if got := request.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("authorization = %q, want API key", got)
		}
		var body struct {
			Model        string `json:"model"`
			Instructions string `json:"instructions"`
			Store        bool   `json:"store"`
			Input        []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"input"`
			Tools []struct {
				Type string `json:"type"`
				Name string `json:"name"`
			} `json:"tools"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body.Model != "gpt-test" || body.Instructions != "plan safely" || body.Store || len(body.Input) != 2 || body.Input[0].Role != "system" || len(body.Tools) != 1 || body.Tools[0].Type != "function" {
			t.Fatalf("request body = %#v, want mapped response request", body)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{
			"status":"completed",
			"output":[
				{"type":"message","content":[{"type":"output_text","text":"hello"}]},
				{"type":"function_call","id":"fc_1","call_id":"call_1","name":"read_file","arguments":"{\"path\":\"note.txt\"}"}
			],
			"usage":{"input_tokens":12,"output_tokens":8}
		}`))
	}))
	defer server.Close()

	provider, err := New(Config{APIKey: "test-key", BaseURL: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	completion, err := provider.Complete(context.Background(), agent.CompletionRequest{
		Model: "gpt-test", Instructions: "plan safely",
		Messages: []agent.Message{
			{Role: agent.RoleSystem, Content: "be helpful"},
			{Role: agent.RoleUser, Content: "read the file"},
		},
		Tools: []agent.ToolDefinition{{Name: "read_file", InputSchema: json.RawMessage(`{"type":"object"}`)}},
	})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if completion.Content != "hello" || completion.StopReason != "completed" || completion.InputTokens != 12 || completion.OutputTokens != 8 {
		t.Fatalf("completion = %#v, want mapped text and usage", completion)
	}
	if len(completion.ToolCalls) != 1 || completion.ToolCalls[0].ID != "call_1" || completion.ToolCalls[0].Name != "read_file" || string(completion.ToolCalls[0].Arguments) != `{"path":"note.txt"}` {
		t.Fatalf("tool calls = %#v, want mapped function call", completion.ToolCalls)
	}
}

func TestCompleteReturnsSafeHTTPError(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusUnauthorized)
		_, _ = writer.Write([]byte(`{"error":{"message":"Bearer server-secret"}}`))
	}))
	defer server.Close()
	provider, err := New(Config{APIKey: "test-key", BaseURL: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	_, err = provider.Complete(context.Background(), agent.CompletionRequest{Model: "gpt-test"})
	var providerError *Error
	if !errors.As(err, &providerError) || providerError.StatusCode != http.StatusUnauthorized {
		t.Fatalf("Complete() error = %v, want safe HTTP error", err)
	}
	if strings.Contains(err.Error(), "server-secret") {
		t.Fatalf("error = %q, must not include response body", err)
	}
}

func TestCompleteRejectsMalformedFunctionArguments(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"status":"completed","output":[{"type":"function_call","call_id":"call_1","name":"read_file","arguments":"not-json"}]}`))
	}))
	defer server.Close()
	provider, err := New(Config{APIKey: "test-key", BaseURL: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	_, err = provider.Complete(context.Background(), agent.CompletionRequest{Model: "gpt-test"})
	if err == nil || !strings.Contains(err.Error(), "arguments are not valid JSON") {
		t.Fatalf("Complete() error = %v, want malformed arguments error", err)
	}
}

func TestNewRequiresAPIKey(t *testing.T) {
	t.Parallel()
	if _, err := New(Config{}); err == nil {
		t.Fatal("New() error = nil, want missing API key error")
	}
}

func TestRequestMapsToolResults(t *testing.T) {
	t.Parallel()
	request := toRequest(agent.CompletionRequest{Messages: []agent.Message{
		{Role: agent.RoleAssistant, ToolCalls: []events.ModelToolCall{{ID: "call-1", Name: "read_file", Arguments: json.RawMessage(`{"path":"note.txt"}`)}}},
		{Role: agent.RoleUser, ToolResults: []agent.ToolResult{{CallID: "call-1", Name: "read_file", Content: "contents"}}},
	}})
	encoded, err := json.Marshal(request.Input)
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}
	if !strings.Contains(string(encoded), `"type":"function_call"`) || !strings.Contains(string(encoded), `"type":"function_call_output"`) || !strings.Contains(string(encoded), `"call_id":"call-1"`) {
		t.Fatalf("input = %s, want function call and result", encoded)
	}
}

func TestCompleteStreamMapsTextReasoningAndCompletion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body struct {
			Stream    bool `json:"stream"`
			Reasoning struct {
				Summary string `json:"summary"`
			} `json:"reasoning"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if !body.Stream || body.Reasoning.Summary != "auto" {
			t.Fatalf("stream request = %#v, want streaming reasoning request", body)
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte("event: response.reasoning_summary_text.delta\ndata: {\"type\":\"response.reasoning_summary_text.delta\",\"delta\":\"Inspecting...\"}\n\n"))
		_, _ = writer.Write([]byte("event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\"}\n\n"))
		_, _ = writer.Write([]byte("event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"content\":[{\"type\":\"output_text\",\"text\":\"hello\"}]}],\"usage\":{\"input_tokens\":4,\"output_tokens\":2}}}\n\n"))
	}))
	defer server.Close()
	provider, err := New(Config{APIKey: "test-key", BaseURL: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	var updates []agent.StreamEvent
	completion, err := provider.CompleteStream(context.Background(), agent.CompletionRequest{Model: "gpt-test", ReasoningSummaries: true}, func(event agent.StreamEvent) { updates = append(updates, event) })
	if err != nil {
		t.Fatalf("CompleteStream() error = %v", err)
	}
	if completion.Content != "hello" || completion.InputTokens != 4 || completion.OutputTokens != 2 {
		t.Fatalf("completion = %#v", completion)
	}
	if len(updates) != 2 || updates[0].Kind != agent.StreamReasoning || updates[1].Kind != agent.StreamText {
		t.Fatalf("updates = %#v", updates)
	}
}
