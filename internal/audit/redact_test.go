package audit

import (
	"encoding/json"
	"testing"
)

func TestDefaultPolicyRedactsNestedSecrets(t *testing.T) {
	t.Parallel()
	payload, redactions, err := DefaultPolicy().Redact(map[string]any{
		"request": map[string]any{
			"authorization": "Bearer top-secret",
			"message":       "safe",
		},
	})
	if err != nil {
		t.Fatalf("Redact() error = %v", err)
	}
	if len(redactions) != 1 || redactions[0].Path != "$.request.authorization" {
		t.Fatalf("redactions = %#v, want authorization redaction", redactions)
	}
	var decoded map[string]map[string]string
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if got := decoded["request"]["authorization"]; got != RedactedValue {
		t.Fatalf("authorization = %q, want %q", got, RedactedValue)
	}
	if got := decoded["request"]["message"]; got != "safe" {
		t.Fatalf("message = %q, want safe value", got)
	}
}

func TestDefaultPolicyRedactsSensitiveValuesInArrays(t *testing.T) {
	t.Parallel()
	payload, redactions, err := DefaultPolicy().Redact(map[string]any{
		"messages": []any{
			map[string]any{"note": "Bearer abc.def"},
		},
	})
	if err != nil {
		t.Fatalf("Redact() error = %v", err)
	}
	if len(redactions) != 1 || redactions[0].Path != "$.messages[0].note" {
		t.Fatalf("redactions = %#v, want array value redaction", redactions)
	}
	var decoded struct {
		Messages []struct {
			Note string `json:"note"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if got := decoded.Messages[0].Note; got != RedactedValue {
		t.Fatalf("note = %q, want %q", got, RedactedValue)
	}
}

func TestDefaultPolicyRedactsSecretCommandArguments(t *testing.T) {
	t.Parallel()
	payload, redactions, err := DefaultPolicy().Redact(map[string]any{
		"arguments": []any{"--token=top-secret", "--token", "top-secret", "-secret", "also-secret"},
	})
	if err != nil {
		t.Fatalf("Redact() error = %v", err)
	}
	if len(redactions) != 5 || redactions[0].Path != "$.arguments[0]" || redactions[1].Path != "$.arguments[1]" || redactions[2].Path != "$.arguments[2]" || redactions[3].Path != "$.arguments[3]" || redactions[4].Path != "$.arguments[4]" {
		t.Fatalf("redactions = %#v, want command argument redaction", redactions)
	}
	var decoded struct {
		Arguments []string `json:"arguments"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	for _, argument := range decoded.Arguments {
		if argument != RedactedValue {
			t.Fatalf("argument = %q, want %q", argument, RedactedValue)
		}
	}
}

func TestDefaultPolicyPreservesTokenUsageMetadata(t *testing.T) {
	t.Parallel()
	payload, redactions, err := DefaultPolicy().Redact(map[string]any{"input_tokens": 42})
	if err != nil {
		t.Fatalf("Redact() error = %v", err)
	}
	if len(redactions) != 0 {
		t.Fatalf("redactions = %#v, want none", redactions)
	}
	var decoded struct {
		InputTokens int `json:"input_tokens"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if decoded.InputTokens != 42 {
		t.Fatalf("input tokens = %d, want 42", decoded.InputTokens)
	}
}
