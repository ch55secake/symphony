import { SelectionList } from "../components/SelectionList"
import { getTheme, type Theme } from "../theme/tokens"

const themeDescriptions: Record<string, string> = {
  default: "Blue + lilac",
  contrast: "Cyan + fuchsia",
  mono: "Monochrome",
}

function ThemeList({ options, selected, current, compact, theme }: { options: string[]; selected: number; current?: string; compact: boolean; theme: Theme }) {
  return <box flexDirection="column" style={{ width: "100%" }}>
    {options.map((option, index) => {
      const focused = index === selected
      const palette = getTheme(option)
      return <box key={option} flexDirection="column" paddingLeft={1} marginBottom={compact ? 0 : 1}>
        <box flexDirection="row" justifyContent="space-between" style={{ width: "100%" }}>
          <text fg={focused ? theme.model : theme.muted}>{focused ? "> " : "  "}{option.toUpperCase()}</text>
          <text><span fg={palette.accent}>●</span> <span fg={palette.model}>●</span> <span fg={palette.success}>●</span>{option === current ? <span fg={theme.accent}>  CURRENT</span> : null}</text>
        </box>
        {!compact && <text fg={theme.subtle}>  {themeDescriptions[option] ?? "Custom palette"}</text>}
      </box>
    })}
  </box>
}

export function SelectionScreen({ name, options, selected, current, width, height, theme }: { name: string; options: string[]; selected: number; current?: string; width: number; height: number; theme: Theme }) {
  if (height < 12 || width < 30) return <box alignItems="center" justifyContent="center" style={{ width, height }}>
    <text fg={theme.warning}>Resize terminal to continue</text>
  </box>
  const compact = width < 70 || height < 18
  const panelWidth = width < 80 ? "100%" : "72%"
  const modelRows = Math.max(3, Math.min(options.length, height - (compact ? 9 : 11)))
  const position = options.length === 0 ? "0 / 0" : `${Math.min(selected + 1, options.length)} / ${options.length}`
  return <box flexDirection="column" alignItems="center" justifyContent="center" style={{ flexGrow: 1 }} padding={1} gap={1}>
    <text fg={theme.accent}>SYMPHONY  <span fg={theme.subtle}>/ {name.toUpperCase()}</span></text>
    <text fg={theme.model}>{name === "theme" ? "PREVIEW A PALETTE" : "CHOOSE THE ACTIVE MODEL"}</text>
    {!compact && <text fg={theme.muted}>{name === "theme" ? "Changes are saved for future sessions" : "The next turn will use this model"}</text>}
    <box flexDirection="column" style={{ width: panelWidth }}>
      {name === "theme"
        ? <ThemeList options={options} selected={selected} current={current} compact={compact} theme={theme} />
        : <SelectionList options={options} selected={selected} current={current} height={modelRows} theme={theme} />}
      {name !== "theme" && <box flexDirection="row" justifyContent="space-between" paddingLeft={2} paddingRight={1} style={{ width: "100%" }}>
        <text fg={theme.subtle}>{selected > 0 ? "↑ MORE" : ""}</text>
        <text fg={theme.subtle}>{position}</text>
        <text fg={theme.subtle}>{selected < options.length - 1 ? "MORE ↓" : ""}</text>
      </box>}
    </box>
    <text fg={theme.accent}>UP/DOWN <span fg={theme.subtle}>MOVE</span>   ENTER <span fg={theme.subtle}>APPLY</span>   ESC <span fg={theme.subtle}>BACK</span></text>
  </box>
}
