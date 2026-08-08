package providers

import (
	"strings"
	"testing"
)

func TestErrorDetailExtractsAndRedactsProviderMessage(t *testing.T) {
	detail := ErrorDetail(strings.NewReader(`{"error":{"message":"Invalid token test-key; Authorization: Bearer other-secret"}}`), "test-key")
	if detail != "Invalid token [REDACTED]; Authorization: Bearer [REDACTED]" {
		t.Fatalf("ErrorDetail() = %q", detail)
	}
}
