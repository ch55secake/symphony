import type { ToolActivity } from "../protocol/types"
import type { Theme } from "../theme/tokens"

function verb(tool: ToolActivity) {
  const byTool: Record<string, Record<ToolActivity["phase"], string>> = {
    read_file: { requested: "Looking for", running: "Reading", awaiting_approval: "Ready to read", completed: "Read", failed: "Could not read", denied: "Read denied" },
    write_file: { requested: "Wants to write", running: "Writing", awaiting_approval: "Wants to write", completed: "Wrote", failed: "Could not write", denied: "Write denied" },
    run_command: { requested: "Wants to run", running: "Running", awaiting_approval: "Wants to run", completed: "Ran", failed: "Command failed", denied: "Command denied" },
  }
  return byTool[tool.name]?.[tool.phase] ?? `${tool.phase.replaceAll("_", " ")} ${tool.name}`
}

function formatBytes(bytes?: number) {
  if (bytes === undefined) return ""
  if (bytes < 1024) return `${bytes} B`
  return `${(bytes / 1024).toFixed(bytes < 10240 ? 1 : 0)} KB`
}

export function ToolActivityRow({ tool, theme }: { tool: ToolActivity; theme: Theme }) {
  const failed = tool.phase === "failed" || tool.phase === "denied"
  const active = tool.phase === "running" || tool.phase === "awaiting_approval"
  const subject = tool.command || tool.target || tool.name
  const metadata = [
    tool.working_directory ? `in ${tool.working_directory}` : "",
    tool.exit_code !== undefined ? `exit ${tool.exit_code}` : "",
    formatBytes(tool.bytes),
    tool.truncated ? "truncated" : "",
    tool.output_hidden ? (tool.name === "run_command" ? "output hidden" : "content hidden") : "",
  ].filter(Boolean).join("  ·  ")
  return <box flexDirection="column" paddingLeft={2} marginBottom={1}>
    <text fg={failed ? theme.danger : active ? theme.accent : theme.subtle}>{active ? ">" : "+"} {verb(tool)}  <span fg={theme.text}>{subject}</span></text>
    {metadata && <text fg={theme.subtle}>  {metadata}</text>}
  </box>
}
