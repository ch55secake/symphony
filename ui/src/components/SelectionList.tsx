import type { Theme } from "../theme/tokens"

export function SelectionList({ options, selected, current, height, theme }: { options: string[]; selected: number; current?: string; height: number; theme: Theme }) {
  if (options.length === 0) return <box alignItems="center" justifyContent="center" style={{ height, width: "100%" }}>
    <text fg={theme.subtle}>No options available</text>
  </box>
  const visibleRows = Math.max(1, height)
  const start = Math.max(0, Math.min(selected - Math.floor(visibleRows / 2), options.length - visibleRows))
  return <box flexDirection="column" style={{ height, width: "100%", overflow: "hidden" }} paddingLeft={1} paddingRight={1}>
    {options.slice(start, start + visibleRows).map((option, index) => {
      const optionIndex = start + index
      const focused = optionIndex === selected
      return <box key={option} flexDirection="row" justifyContent="space-between" paddingLeft={1} style={{ width: "100%" }}>
        <box style={{ flexGrow: 1, minWidth: 1, overflow: "hidden" }}>
          <text fg={focused ? theme.model : theme.muted} wrapMode="none" truncate>{focused ? "> " : "  "}{option}</text>
        </box>
        {option === current && <text fg={focused ? theme.accent : theme.subtle}> CURRENT</text>}
      </box>
    })}
  </box>
}
