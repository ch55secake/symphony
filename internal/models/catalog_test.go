package models

import (
	"context"
	"net/http"
	"net/http/httptest"
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
