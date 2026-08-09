import type { Theme } from "../theme/tokens"

export function SelectionList({ options, selected, height, theme }: { options: string[]; selected: number; height: number; theme: Theme }) {
  if (options.length === 0) return <box alignItems="center" justifyContent="center" style={{ height, width: "100%" }}>
    <text fg={theme.subtle}>No options available</text>
  </box>
  const visibleRows = Math.max(1, height)
  const start = Math.max(0, Math.min(selected - Math.floor(visibleRows / 2), options.length - visibleRows))
  return <box flexDirection="column" style={{ height, width: "100%", overflow: "hidden" }} paddingLeft={1} paddingRight={1}>
    {options.slice(start, start + visibleRows).map((option, index) => {
      const optionIndex = start + index
      return <box key={option} paddingLeft={1} style={{ backgroundColor: optionIndex === selected ? theme.surfaceRaised : theme.background }}>
        <text fg={optionIndex === selected ? theme.model : theme.muted}>{optionIndex === selected ? "> " : "  "}{option}</text>
      </box>
    })}
  </box>
}
