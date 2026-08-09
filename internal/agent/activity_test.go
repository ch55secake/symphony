package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ch55secake/symphony/internal/events"
)

func TestProjectToolActivityHidesWriteContent(t *testing.T) {
	activity := ProjectToolActivity(events.ModelToolCall{ID: "call-1", Name: "write_file", Arguments: json.RawMessage(`{"path":"note.txt","content":"private sentinel"}`)}, ActivityRequested)
	encoded, err := json.Marshal(activity)
	if err != nil {
		t.Fatalf("marshal activity: %v", err)
	}
	if activity.Target != "note.txt" || activity.Bytes != len("private sentinel") || strings.Contains(string(encoded), "private sentinel") {
		t.Fatalf("activity = %#v, encoded = %s", activity, encoded)
	}
}

func TestProjectToolActivityRedactsCommandPreview(t *testing.T) {
	activity := ProjectToolActivity(events.ModelToolCall{ID: "call-1", Name: "run_command", Arguments: json.RawMessage(`{"executable":"deploy","arguments":["--token","secret-value","--auth=auth-secret","--auth","space-secret","--access-key=access-secret","OPENAI_API_KEY=another-secret","safe"],"working_directory":"src\nignored"}`)}, ActivityRequested)
	if strings.Contains(activity.Command, "secret-value") || strings.Contains(activity.Command, "auth-secret") || strings.Contains(activity.Command, "space-secret") || strings.Contains(activity.Command, "access-secret") || strings.Contains(activity.Command, "another-secret") || !strings.Contains(activity.Command, "[REDACTED]") || strings.Contains(activity.WorkingDirectory, "\n") {
		t.Fatalf("activity = %#v", activity)
	}
}
