import type { Theme } from "../theme/tokens"

function basename(path: string) {
  const parts = path.split("/").filter(Boolean)
  return parts[parts.length - 1] ?? path
}

export function Header({ provider, model, workspace, theme, compact }: { provider: string; model: string; workspace: string; theme: Theme; compact: boolean }) {
  if (compact) return <box flexDirection="row" justifyContent="space-between" style={{ width: "100%" }}>
    <text fg={theme.accent}>SYMPHONY</text>
    <text fg={theme.muted}>{provider}</text>
  </box>
  return <box flexDirection="row" justifyContent="space-between" style={{ width: "100%" }}>
    <text fg={theme.accent}>SYMPHONY  <span fg={theme.subtle}>AGENT CONSOLE</span></text>
    <text fg={theme.muted}>{provider} / {model}{compact ? "" : `  @ ${basename(workspace)}`}</text>
  </box>
}
