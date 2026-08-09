import type { Command } from "../commands/registry"
import type { Theme } from "../theme/tokens"

type Props = {
  value: string
  onChange: (value: string) => void
  onSubmit: () => void
  suggestions: Command[]
  selected: number
  theme: Theme
  compact?: boolean
}

export function Composer({ value, onChange, onSubmit, suggestions, selected, theme, compact }: Props) {
  return <box flexDirection="column" style={{ width: "100%", flexShrink: 0 }}>
    {suggestions.length > 0 && <box flexDirection="column" paddingLeft={2} paddingRight={1} style={{ width: "100%", flexShrink: 0, backgroundColor: theme.surface }}>
      {suggestions.map((command, index) => <box key={command.name} paddingLeft={1} style={{ backgroundColor: index === selected ? theme.surfaceRaised : theme.surface }}>
        <text fg={index === selected ? theme.model : theme.muted}>
          {index === selected ? "> " : "  "}{command.name}{compact ? "" : `  ${command.description}`}
        </text>
      </box>)}
      <text fg={theme.subtle}>  UP/DOWN SELECT   TAB COMPLETE   ENTER RUN</text>
    </box>}
    <box paddingLeft={1} paddingRight={1} style={{ width: "100%", flexShrink: 0, backgroundColor: theme.surfaceRaised }}>
      <text fg={theme.accent}>ASK </text>
      <input focused placeholder="Describe the next change..." onInput={onChange} onSubmit={onSubmit} value={value} style={{ flexGrow: 1, minWidth: 8 }} />
    </box>
  </box>
}
