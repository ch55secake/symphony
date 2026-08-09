import type { Theme } from "../theme/tokens"
import { WorkingIndicator } from "./WorkingIndicator"

export function StatusBar({ status = "READY", theme, compact = false }: { status?: string; theme: Theme; compact?: boolean }) {
  const working = status === "WORKING" || status === "CANCELING"
  const error = status.startsWith("Error:")
  return <box flexDirection="row" justifyContent="space-between" paddingLeft={1} paddingRight={1} style={{ width: "100%", flexShrink: 0 }}>
    {working
      ? <WorkingIndicator status={status as "WORKING" | "CANCELING"} theme={theme} />
      : <text fg={error ? theme.danger : theme.muted}>{status}</text>}
    {!compact && <text fg={theme.subtle}>CTRL+C CANCEL / QUIT   CTRL+Q QUIT   /HELP</text>}
  </box>
}
