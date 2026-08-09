package opencode

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

func TestCompleteResponsesUsesOpenCodeEndpoint(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/responses" {
			t.Fatalf("request = %s %s, want POST /responses", request.Method, request.URL.Path)
		}
		if got := request.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("authorization = %q, want API key", got)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"hello"}]}],"usage":{"input_tokens":12,"output_tokens":8}}`))
	}))
	defer server.Close()

	provider, err := New(Config{APIKey: "test-key", BaseURL: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	completion, err := provider.Complete(context.Background(), agent.CompletionRequest{Model: "gpt-test", Messages: []agent.Message{{Role: agent.RoleUser, Content: "hello"}}})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if provider.Name() != "opencode" || completion.Content != "hello" || completion.InputTokens != 12 || completion.OutputTokens != 8 {
		t.Fatalf("provider = %q, completion = %#v", provider.Name(), completion)
	}
}

func TestCompleteChatMapsToolsAndToolResults(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/chat/completions" {
			t.Fatalf("request = %s %s, want POST /chat/completions", request.Method, request.URL.Path)
		}
		if got := request.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("authorization = %q, want API key", got)
		}
		var body struct {
			Model    string `json:"model"`
			Messages []struct {
				Role       string `json:"role"`
				ToolCallID string `json:"tool_call_id"`
			} `json:"messages"`
			Tools []struct {
				Type     string `json:"type"`
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			} `json:"tools"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body.Model != "kimi-test" || len(body.Tools) != 1 || body.Tools[0].Type != "function" || body.Tools[0].Function.Name != "read_file" || len(body.Messages) != 4 || body.Messages[0].Role != "system" || body.Messages[3].Role != "tool" || body.Messages[3].ToolCallID != "call-1" {
			t.Fatalf("request body = %#v, want mapped chat request", body)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"choices":[{"finish_reason":"tool_calls","message":{"content":"","tool_calls":[{"id":"call-2","function":{"name":"read_file","arguments":"{\"path\":\"next.txt\"}"}}]}}],"usage":{"prompt_tokens":9,"completion_tokens":3}}`))
	}))
	defer server.Close()

	provider, err := New(Config{APIKey: "test-key", BaseURL: server.URL, Transport: TransportChat, HTTPClient: server.Client()})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	completion, err := provider.Complete(context.Background(), agent.CompletionRequest{
		Model: "kimi-test", Instructions: "plan safely",
		Messages: []agent.Message{
			{Role: agent.RoleUser, Content: "read a file"},
			{Role: agent.RoleAssistant, ToolCalls: []events.ModelToolCall{{ID: "call-1", Name: "read_file", Arguments: json.RawMessage(`{"path":"note.txt"}`)}}},
			{Role: agent.RoleUser, ToolResults: []agent.ToolResult{{CallID: "call-1", Content: "contents"}}},
		},
		Tools: []agent.ToolDefinition{{Name: "read_file", InputSchema: json.RawMessage(`{"type":"object"}`)}},
	})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if completion.StopReason != "tool_calls" || completion.InputTokens != 9 || completion.OutputTokens != 3 || len(completion.ToolCalls) != 1 || completion.ToolCalls[0].ID != "call-2" || string(completion.ToolCalls[0].Arguments) != `{"path":"next.txt"}` {
		t.Fatalf("completion = %#v, want mapped chat completion", completion)
	}
}

func TestCompleteChatReturnsSafeHTTPError(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusUnauthorized)
		_, _ = writer.Write([]byte(`{"error":"Bearer server-secret"}`))
	}))
	defer server.Close()
	provider, err := New(Config{APIKey: "test-key", BaseURL: server.URL, Transport: TransportChat, HTTPClient: server.Client()})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	_, err = provider.Complete(context.Background(), agent.CompletionRequest{Model: "kimi-test", Messages: []agent.Message{{Role: agent.RoleUser, Content: "hello"}}})
	var providerError *Error
	if !errors.As(err, &providerError) || providerError.StatusCode != http.StatusUnauthorized || strings.Contains(err.Error(), "server-secret") {
		t.Fatalf("Complete() error = %v, want safe HTTP error", err)
	}
}

func TestCompleteResponsesReturnsOpenCodeHTTPError(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusUnauthorized)
		_, _ = writer.Write([]byte(`{"error":"Bearer server-secret"}`))
	}))
	defer server.Close()
	provider, err := New(Config{APIKey: "test-key", BaseURL: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	_, err = provider.Complete(context.Background(), agent.CompletionRequest{Model: "gpt-test", Messages: []agent.Message{{Role: agent.RoleUser, Content: "hello"}}})
	var providerError *Error
	if !errors.As(err, &providerError) || providerError.StatusCode != http.StatusUnauthorized || strings.Contains(err.Error(), "server-secret") || strings.Contains(err.Error(), "OpenAI") {
		t.Fatalf("Complete() error = %v, want safe OpenCode HTTP error", err)
	}
}

func TestNewValidatesConfiguration(t *testing.T) {
	t.Parallel()
	if _, err := New(Config{}); err == nil {
		t.Fatal("New() error = nil, want missing API key error")
	}
	if _, err := New(Config{APIKey: "test-key", Transport: "other"}); err == nil {
		t.Fatal("New() error = nil, want transport validation error")
	}
}

func TestChatCompletionRejectsMalformedFunctionArguments(t *testing.T) {
	t.Parallel()
	_, err := toChatCompletion(chatResponse{Choices: []chatChoice{{Message: chatResponseMessage{ToolCalls: []chatResponseToolCall{{ID: "call-1", Function: chatResponseFunction{Name: "read_file", Arguments: "not-json"}}}}}}})
	if err == nil || !strings.Contains(err.Error(), "not a JSON object") {
		t.Fatalf("toChatCompletion() error = %v, want malformed arguments error", err)
	}
}
