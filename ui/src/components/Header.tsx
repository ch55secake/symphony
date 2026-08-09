import type { Theme } from "../theme/tokens"
import type { Mode } from "../protocol/types"

function basename(path: string) {
  const parts = path.split("/").filter(Boolean)
  return parts[parts.length - 1] ?? path
}

export function Header({ provider, model, workspace, mode, theme, compact }: { provider: string; model: string; workspace: string; mode: Mode; theme: Theme; compact: boolean }) {
  if (compact) return <box flexDirection="row" justifyContent="space-between" style={{ width: "100%" }}>
    <text fg={mode === "plan" ? theme.model : theme.accent}>SYMPHONY / {mode.toUpperCase()}</text>
    <text fg={theme.muted}>{provider}</text>
  </box>
  return <box flexDirection="row" justifyContent="space-between" style={{ width: "100%" }}>
    <text fg={mode === "plan" ? theme.model : theme.accent}>SYMPHONY  <span fg={theme.subtle}>/ {mode.toUpperCase()} / AGENT CONSOLE</span></text>
    <text fg={theme.muted}>{provider} / {model}{compact ? "" : `  @ ${basename(workspace)}`}</text>
  </box>
}
