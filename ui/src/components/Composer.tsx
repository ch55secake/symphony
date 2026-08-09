import type { Command } from "../commands/registry"
import type { Mode } from "../protocol/types"
import type { Theme } from "../theme/tokens"

type Props = {
  value: string
  onChange: (value: string) => void
  onSubmit: () => void
  suggestions: Command[]
  selected: number
  mode: Mode
  theme: Theme
  compact?: boolean
}

export function Composer({ value, onChange, onSubmit, suggestions, selected, mode, theme, compact }: Props) {
  return <box flexDirection="column" style={{ width: "100%", flexShrink: 0 }}>
    {suggestions.length > 0 && <box flexDirection="column" paddingLeft={2} paddingRight={1} style={{ width: "100%", flexShrink: 0 }}>
      {suggestions.map((command, index) => <box key={command.name} paddingLeft={1}>
        <text fg={index === selected ? theme.model : theme.muted}>
          {index === selected ? "> " : "  "}{command.name}{compact ? "" : `  ${command.description}`}
        </text>
      </box>)}
      <text fg={theme.subtle}>  UP/DOWN SELECT   TAB COMPLETE   ENTER RUN</text>
    </box>}
    <box border={["bottom"]} borderColor={theme.accent} paddingLeft={1} paddingRight={1} style={{ width: "100%", flexShrink: 0 }}>
      <text fg={mode === "plan" ? theme.model : theme.accent}>{mode.toUpperCase()} &gt; </text>
      <input
        focused
        placeholder="Describe the next change..."
        placeholderColor={theme.subtle}
        textColor={theme.text}
        focusedTextColor={theme.text}
        selectionBg={theme.borderStrong}
        selectionFg={theme.text}
        cursorColor={theme.accent}
        cursorStyle={{ style: "line", blinking: true }}
        onInput={onChange}
        onSubmit={onSubmit}
        value={value}
        style={{ flexGrow: 1, minWidth: 8 }}
      />
    </box>
    <text fg={theme.subtle}>  TAB SWITCH MODE   {mode === "plan" ? "PLAN: INSPECT + PROPOSE" : "BUILD: IMPLEMENT + VERIFY"}</text>
  </box>
}
