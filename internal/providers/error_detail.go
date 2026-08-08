// Package providers contains shared provider transport helpers.
package providers

import (
	"encoding/json"
	"io"
	"regexp"
	"strings"
)

const maxErrorBytes = 8 << 10

var (
	bearerPattern     = regexp.MustCompile(`(?i)bearer\s+\S+`)
	credentialPattern = regexp.MustCompile(`(?i)(api[_ -]?key|token|secret|password)\s*[:=]\s*\S+`)
)

// ErrorDetail extracts a bounded, credential-redacted message from a provider response.
func ErrorDetail(body io.Reader, apiKey string) string {
	data, err := io.ReadAll(io.LimitReader(body, maxErrorBytes))
	if err != nil || len(data) == 0 {
		return ""
	}
	var payload struct {
		Error   json.RawMessage `json:"error"`
		Message string          `json:"message"`
	}
	if json.Unmarshal(data, &payload) != nil {
		return ""
	}
	detail := payload.Message
	if len(payload.Error) > 0 {
		var errorPayload struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(payload.Error, &errorPayload) == nil && errorPayload.Message != "" {
			detail = errorPayload.Message
		} else {
			var message string
			if json.Unmarshal(payload.Error, &message) == nil {
				detail = message
			}
		}
	}
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return ""
	}
	if apiKey != "" {
		detail = strings.ReplaceAll(detail, apiKey, "[REDACTED]")
	}
	detail = bearerPattern.ReplaceAllString(detail, "Bearer [REDACTED]")
	detail = credentialPattern.ReplaceAllString(detail, "$1=[REDACTED]")
	return detail
}
