package anthropic

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

func TestCompleteMapsMessagesAndToolUse(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/v1/messages" {
			t.Fatalf("request = %s %s, want POST /v1/messages", request.Method, request.URL.Path)
		}
		if got := request.Header.Get("x-api-key"); got != "test-key" {
			t.Fatalf("x-api-key = %q, want API key", got)
		}
		if got := request.Header.Get("anthropic-version"); got != apiVersion {
			t.Fatalf("anthropic-version = %q, want %q", got, apiVersion)
		}
		var body struct {
			Model     string `json:"model"`
			MaxTokens int    `json:"max_tokens"`
			System    string `json:"system"`
			Messages  []struct {
				Role string `json:"role"`
			} `json:"messages"`
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body.Model != "claude-test" || body.MaxTokens != 512 || body.System != "plan safely\n\nfirst system\n\nsecond system" || len(body.Messages) != 2 || body.Messages[0].Role != "user" || len(body.Tools) != 1 || body.Tools[0].Name != "read_file" {
			t.Fatalf("request body = %#v, want mapped Messages request", body)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{
			"stop_reason":"tool_use",
			"content":[
				{"type":"text","text":"hello"},
				{"type":"tool_use","id":"toolu_1","name":"read_file","input":{"path":"note.txt"}}
			],
			"usage":{"input_tokens":12,"output_tokens":8}
		}`))
	}))
	defer server.Close()

	provider, err := New(Config{APIKey: "test-key", BaseURL: server.URL, MaxTokens: 512, HTTPClient: server.Client()})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	completion, err := provider.Complete(context.Background(), agent.CompletionRequest{
		Model: "claude-test", Instructions: "plan safely",
		Messages: []agent.Message{
			{Role: agent.RoleSystem, Content: "first system"},
			{Role: agent.RoleUser, Content: "read the file"},
			{Role: agent.RoleSystem, Content: "second system"},
			{Role: agent.RoleAssistant, Content: "working"},
		},
		Tools: []agent.ToolDefinition{{Name: "read_file", InputSchema: json.RawMessage(`{"type":"object"}`)}},
	})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if completion.Content != "hello" || completion.StopReason != "tool_use" || completion.InputTokens != 12 || completion.OutputTokens != 8 {
		t.Fatalf("completion = %#v, want mapped text and usage", completion)
	}
	if len(completion.ToolCalls) != 1 || completion.ToolCalls[0].ID != "toolu_1" || completion.ToolCalls[0].Name != "read_file" || string(completion.ToolCalls[0].Arguments) != `{"path":"note.txt"}` {
		t.Fatalf("tool calls = %#v, want mapped tool use", completion.ToolCalls)
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
	_, err = provider.Complete(context.Background(), agent.CompletionRequest{Model: "claude-test", Messages: []agent.Message{{Role: agent.RoleUser, Content: "hello"}}})
	var providerError *Error
	if !errors.As(err, &providerError) || providerError.StatusCode != http.StatusUnauthorized {
		t.Fatalf("Complete() error = %v, want safe HTTP error", err)
	}
	if strings.Contains(err.Error(), "server-secret") {
		t.Fatalf("error = %q, must not include response body", err)
	}
}

func TestCompleteRejectsNonObjectToolInput(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"stop_reason":"tool_use","content":[{"type":"tool_use","id":"toolu_1","name":"read_file","input":"not-an-object"}]}`))
	}))
	defer server.Close()
	provider, err := New(Config{APIKey: "test-key", BaseURL: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	_, err = provider.Complete(context.Background(), agent.CompletionRequest{Model: "claude-test", Messages: []agent.Message{{Role: agent.RoleUser, Content: "hello"}}})
	if err == nil || !strings.Contains(err.Error(), "not a JSON object") {
		t.Fatalf("Complete() error = %v, want invalid tool input error", err)
	}
}

func TestNewRequiresAPIKeyAndDefaultsMaxTokens(t *testing.T) {
	t.Parallel()
	if _, err := New(Config{}); err == nil {
		t.Fatal("New() error = nil, want missing API key error")
	}
	provider, err := New(Config{APIKey: "test-key"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if provider.maxTokens != defaultMaxTokens {
		t.Fatalf("max tokens = %d, want %d", provider.maxTokens, defaultMaxTokens)
	}
}

func TestProviderUsesBearerAuthenticationWhenConfigured(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if got := request.Header.Get("Authorization"); got != "Bearer test-key" || request.Header.Get("x-api-key") != "" || request.Header.Get("anthropic-version") != "" {
			t.Fatalf("headers = %#v", request.Header)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"stop_reason":"end_turn","content":[{"type":"text","text":"ok"}]}`))
	}))
	defer server.Close()
	provider, err := New(Config{APIKey: "test-key", BaseURL: server.URL, ProviderName: "OpenCode Go", BearerAuth: true, HTTPClient: server.Client()})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if provider.Name() != "opencode-go" {
		t.Fatalf("Name() = %q", provider.Name())
	}
	if _, err := provider.Complete(context.Background(), agent.CompletionRequest{Model: "test", Messages: []agent.Message{{Role: agent.RoleUser, Content: "hello"}}}); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
}

func TestRequestMapsToolResults(t *testing.T) {
	t.Parallel()
	request, err := toRequest(agent.CompletionRequest{Model: "claude-test", Messages: []agent.Message{
		{Role: agent.RoleAssistant, ToolCalls: []events.ModelToolCall{{ID: "toolu_1", Name: "read_file", Arguments: json.RawMessage(`{"path":"note.txt"}`)}}},
		{Role: agent.RoleUser, ToolResults: []agent.ToolResult{{CallID: "toolu_1", Name: "read_file", Content: "contents"}}},
	}}, defaultMaxTokens)
	if err != nil {
		t.Fatalf("toRequest() error = %v", err)
	}
	encoded, err := json.Marshal(request.Messages)
	if err != nil {
		t.Fatalf("marshal messages: %v", err)
	}
	if !strings.Contains(string(encoded), `"type":"tool_use"`) || !strings.Contains(string(encoded), `"type":"tool_result"`) || !strings.Contains(string(encoded), `"tool_use_id":"toolu_1"`) {
		t.Fatalf("messages = %s, want tool use and result", encoded)
	}
}
