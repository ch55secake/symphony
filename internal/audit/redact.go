// Package audit applies persistence-time safety controls to event payloads.
package audit

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/ch55secake/symphony/internal/events"
)

const RedactedValue = "[REDACTED]"

// Policy removes values whose object keys or string values match configured secret patterns.
type Policy struct {
	KeyPatterns   []*regexp.Regexp
	ValuePatterns []*regexp.Regexp
}

func DefaultPolicy() Policy {
	return Policy{
		KeyPatterns: []*regexp.Regexp{
			regexp.MustCompile(`(?i)(api[_-]?key|authorization|password|secret|token|credential)`),
		},
		ValuePatterns: []*regexp.Regexp{
			regexp.MustCompile(`(?i)bearer\s+[a-z0-9._-]+`),
			regexp.MustCompile(`(?i)(?:^|--?)(?:api[_-]?key|authorization|password|secret|token|credential)=\S+`),
		},
	}
}

// Redact marshals payload, removes matching values, and returns JSON safe for persistence.
func (p Policy) Redact(payload any) (json.RawMessage, []events.Redaction, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal payload: %w", err)
	}

	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, nil, fmt.Errorf("unmarshal payload: %w", err)
	}

	redactions := make([]events.Redaction, 0)
	p.redact(&value, "$", &redactions)
	safe, err := json.Marshal(value)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal redacted payload: %w", err)
	}
	return safe, redactions, nil
}

func (p Policy) redact(value *any, path string, redactions *[]events.Redaction) {
	switch current := (*value).(type) {
	case map[string]any:
		for key, child := range current {
			childPath := path + "." + key
			if p.matchesKey(key) {
				current[key] = RedactedValue
				*redactions = append(*redactions, events.Redaction{Path: childPath, Reason: "sensitive key"})
				continue
			}
			p.redact(&child, childPath, redactions)
			current[key] = child
		}
	case []any:
		for index, child := range current {
			if option, ok := child.(string); ok && isSensitiveOption(option) {
				current[index] = RedactedValue
				*redactions = append(*redactions, events.Redaction{Path: fmt.Sprintf("%s[%d]", path, index), Reason: "sensitive option"})
				if !strings.Contains(option, "=") && index+1 < len(current) {
					current[index+1] = RedactedValue
					*redactions = append(*redactions, events.Redaction{Path: fmt.Sprintf("%s[%d]", path, index+1), Reason: "sensitive option value"})
				}
				continue
			}
			p.redact(&child, fmt.Sprintf("%s[%d]", path, index), redactions)
			current[index] = child
		}
	case string:
		if p.matchesValue(current) {
			*value = RedactedValue
			*redactions = append(*redactions, events.Redaction{Path: path, Reason: "sensitive value"})
		}
	}
}

func isSensitiveOption(value string) bool {
	return regexp.MustCompile(`(?i)^--?(?:api[_-]?key|authorization|password|secret|token|credential)(?:=\S+)?$`).MatchString(strings.TrimSpace(value))
}

func (p Policy) matchesKey(key string) bool {
	return matches(p.KeyPatterns, key)
}

func (p Policy) matchesValue(value string) bool {
	return matches(p.ValuePatterns, value)
}

func matches(patterns []*regexp.Regexp, value string) bool {
	for _, pattern := range patterns {
		if pattern.MatchString(strings.TrimSpace(value)) {
			return true
		}
	}
	return false
}
