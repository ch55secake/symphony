import type { Command } from "../commands/registry"
import { ApprovalPanel } from "../components/ApprovalPanel"
import { Composer } from "../components/Composer"
import { Conversation } from "../components/Conversation"
import { Header } from "../components/Header"
import { StatusBar } from "../components/StatusBar"
import type { Approval, Mode, TranscriptEntry } from "../protocol/types"
import type { Theme } from "../theme/tokens"

type Props = {
  provider: string
  model: string
  workspace: string
  status?: string
  transcript: TranscriptEntry[]
  approval?: Approval
  value: string
  onChange: (value: string) => void
  onSubmit: () => void
  suggestions: Command[]
  selected: number
  mode: Mode
  theme: Theme
  width: number
  height: number
}

export function ChatScreen(props: Props) {
  if (props.height < 9 || props.width < 30) return <box alignItems="center" justifyContent="center" style={{ width: props.width, height: props.height }}>
    <text fg={props.theme.warning}>Resize terminal to continue</text>
  </box>
  const compact = props.width < 80 || props.height < 22
  const tiny = props.height < 15 || props.width < 60
  const busy = props.status === "WORKING" || props.status === "CANCELING"
  const visibleSuggestions = tiny ? [] : props.suggestions
  return <box flexDirection="column" padding={compact ? 0 : 1} gap={1} style={{ width: props.width, height: props.height }}>
    {!tiny && <Header provider={props.provider} model={props.model} workspace={props.workspace} mode={props.mode} theme={props.theme} compact={compact} />}
    <Conversation entries={props.transcript} theme={props.theme} />
    {props.approval
      ? <ApprovalPanel approval={props.approval} theme={props.theme} compact={compact} />
      : busy
        ? null
        : <Composer value={props.value} onChange={props.onChange} onSubmit={props.onSubmit} suggestions={visibleSuggestions} selected={props.selected} mode={props.mode} theme={props.theme} compact={compact} />}
    {!tiny && <StatusBar status={props.status} mode={props.mode} theme={props.theme} />}
    {tiny && busy && <StatusBar status={props.status} mode={props.mode} theme={props.theme} compact />}
  </box>
}
