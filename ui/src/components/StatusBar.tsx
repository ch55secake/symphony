import type { Theme } from "../theme/tokens"

export function StatusBar({ status = "READY", spinner, theme }: { status?: string; spinner: number; theme: Theme }) {
  const working = status === "WORKING" || status === "CANCELING"
  const error = status.startsWith("Error:")
  const color = error ? theme.danger : working ? theme.accent : theme.muted
  const label = working ? `${["|", "/", "-", "\\"][spinner]}  ${status}` : status
  return <box flexDirection="row" justifyContent="space-between" paddingLeft={1} paddingRight={1} style={{ width: "100%", flexShrink: 0 }}>
    <text fg={color}>{label}</text>
    <text fg={theme.subtle}>CTRL+C CANCEL / QUIT   CTRL+Q QUIT   /HELP</text>
  </box>
}
