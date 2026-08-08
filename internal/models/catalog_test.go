package models

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestListUsesProviderAuthenticationAndSortsModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/models" || request.Header.Get("Authorization") != "Bearer test-key" {
			writer.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = writer.Write([]byte(`{"data":[{"id":"z-model"},{"id":"a-model"}]}`))
	}))
	defer server.Close()
	listed, err := List(context.Background(), Config{Provider: "openai", APIKey: " test-key\n", BaseURL: server.URL})
	if err != nil || len(listed) != 2 || listed[0] != "a-model" || listed[1] != "z-model" {
		t.Fatalf("List() = %#v, %v", listed, err)
	}
}

func TestListRejectsInvalidInputsAndResponses(t *testing.T) {
	if _, err := List(context.Background(), Config{Provider: "openai"}); err == nil || !strings.Contains(err.Error(), "API key") {
		t.Fatalf("List() error = %v", err)
	}
	if _, err := List(context.Background(), Config{Provider: "unknown", APIKey: "test-key"}); err == nil || !strings.Contains(err.Error(), "unknown provider") {
		t.Fatalf("List() error = %v", err)
	}
	for _, test := range []struct {
		status int
		body   string
		want   string
	}{
		{status: http.StatusUnauthorized, want: "HTTP 401"},
		{status: http.StatusOK, body: `{"data":[]}`, want: "no models"},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			writer.WriteHeader(test.status)
			_, _ = writer.Write([]byte(test.body))
		}))
		_, err := List(context.Background(), Config{Provider: "openai", APIKey: "test-key", BaseURL: server.URL})
		server.Close()
		if err == nil || !strings.Contains(err.Error(), test.want) {
			t.Fatalf("List() error = %v, want %q", err, test.want)
		}
	}
}

func TestListUsesAnthropicHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("x-api-key") != "test-key" || request.Header.Get("anthropic-version") != "2023-06-01" {
			writer.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = writer.Write([]byte(`{"data":[{"id":"claude-test"}]}`))
	}))
	defer server.Close()
	listed, err := List(context.Background(), Config{Provider: "anthropic", APIKey: "test-key", BaseURL: server.URL})
	if err != nil || len(listed) != 1 || listed[0] != "claude-test" {
		t.Fatalf("List() = %#v, %v", listed, err)
	}
}
