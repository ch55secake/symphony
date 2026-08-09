import type { Approval } from "../protocol/types"
import type { Theme } from "../theme/tokens"

export function ApprovalPanel({ approval, theme, compact }: { approval: Approval; theme: Theme; compact: boolean }) {
  const summary = compact && approval.summary.length > 24 ? `${approval.summary.slice(0, 21)}...` : approval.summary
  return <box border borderColor={theme.warning} flexDirection="column" paddingLeft={1} paddingRight={1}>
    <text fg={theme.warning}>{compact ? "APPROVE" : "APPROVAL REQUIRED"}  {approval.action.toUpperCase()}</text>
    <text fg={theme.text}>{summary}</text>
    {!compact && <text fg={theme.subtle}>Operation {approval.hash}</text>}
    <text fg={theme.warning}>[Y] APPROVE   [N] DENY</text>
  </box>
}
