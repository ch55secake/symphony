// Package models retrieves models available to configured providers.
package models

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
)

const (
	openAIURL    = "https://api.openai.com/v1"
	opencodeURL  = "https://opencode.ai/zen/v1"
	anthropicURL = "https://api.anthropic.com/v1"
)

// Config supplies credentials and optional test endpoints for discovery.
type Config struct {
	Provider   string
	APIKey     string
	BaseURL    string
	HTTPClient *http.Client
}

// List returns model IDs available to the configured provider.
func List(ctx context.Context, config Config) ([]string, error) {
	apiKey := strings.TrimSpace(config.APIKey)
	if apiKey == "" {
		return nil, errors.New("provider API key is required")
	}
	baseURL := strings.TrimRight(config.BaseURL, "/")
	if baseURL == "" {
		switch config.Provider {
		case "openai":
			baseURL = openAIURL
		case "opencode":
			baseURL = opencodeURL
		case "anthropic":
			baseURL = anthropicURL
		default:
			return nil, fmt.Errorf("unknown provider %q", config.Provider)
		}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/models", nil)
	if err != nil {
		return nil, fmt.Errorf("create model request: %w", err)
	}
	if config.Provider == "anthropic" {
		request.Header.Set("x-api-key", apiKey)
		request.Header.Set("anthropic-version", "2023-06-01")
	} else {
		request.Header.Set("Authorization", "Bearer "+apiKey)
	}
	client := config.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("list models: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("list models: provider returned HTTP %d", response.StatusCode)
	}
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode model list: %w", err)
	}
	models := make([]string, 0, len(payload.Data))
	for _, model := range payload.Data {
		if model.ID != "" {
			models = append(models, model.ID)
		}
	}
	if len(models) == 0 {
		return nil, errors.New("provider returned no models")
	}
	sort.Strings(models)
	return models, nil
}
