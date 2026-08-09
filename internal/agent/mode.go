package agent

// Mode controls the intent and tool surface of an interactive session.
type Mode string

const (
	ModeBuild Mode = "build"
	ModePlan  Mode = "plan"
)

// Instructions returns the provider-neutral guidance for the active mode.
func (m Mode) Instructions() string {
	if m == ModePlan {
		return "You are in Plan mode. Investigate the workspace and produce a concrete implementation plan. Do not modify files. You may inspect files and run approved workspace commands for investigation only. Clearly separate findings, proposed changes, and validation steps."
	}
	return "You are in Build mode. Implement the user's requested changes in the workspace. Inspect existing code before editing, use the available tools deliberately, and verify the result."
}

// ToolsForMode limits the exposed tool definitions to the active mode.
func ToolsForMode(tools []ToolDefinition, mode Mode) []ToolDefinition {
	if mode != ModePlan {
		return append([]ToolDefinition(nil), tools...)
	}
	filtered := make([]ToolDefinition, 0, len(tools))
	for _, tool := range tools {
		if tool.Name != "write_file" {
			filtered = append(filtered, tool)
		}
	}
	return filtered
}
