import type { Command } from "../commands/registry"
import { Brand } from "../components/Brand"
import { Composer } from "../components/Composer"
import type { Theme } from "../theme/tokens"

type Props = {
  provider: string
  model: string
  workspace: string
  value: string
  onChange: (value: string) => void
  onSubmit: () => void
  suggestions: Command[]
  selected: number
  theme: Theme
  width: number
  height: number
}

export function WelcomeScreen(props: Props) {
  const compact = props.width < 80 || props.height < 22
  return <box flexDirection="column" alignItems="center" justifyContent="center" style={{ flexGrow: 1 }} padding={1} gap={1}>
    <Brand theme={props.theme} expanded={!compact && props.width >= 92} />
    <text fg={props.theme.model}>AUDITED CODING AGENT</text>
    <text fg={props.theme.muted}>{props.provider} / {props.model}{compact ? "" : `  |  ${props.workspace}`}</text>
    <box style={{ width: compact ? "100%" : "82%" }}>
      <Composer value={props.value} onChange={props.onChange} onSubmit={props.onSubmit} suggestions={props.suggestions} selected={props.selected} theme={props.theme} compact={compact} />
    </box>
    <text fg={props.theme.subtle}>ENTER STARTS   CTRL+C OR CTRL+Q QUITS</text>
  </box>
}
