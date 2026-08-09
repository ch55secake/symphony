import { SelectionList } from "../components/SelectionList"
import type { Theme } from "../theme/tokens"

export function SelectionScreen({ name, options, selected, width, height, theme }: { name: string; options: string[]; selected: number; width: number; height: number; theme: Theme }) {
  if (height < 10 || width < 30) return <box alignItems="center" justifyContent="center" style={{ width, height }}>
    <text fg={theme.warning}>Resize terminal to continue</text>
  </box>
  const panelWidth = width < 80 ? "100%" : "72%"
  return <box flexDirection="column" alignItems="center" justifyContent="center" style={{ flexGrow: 1 }} padding={1} gap={1}>
    <text fg={theme.accent}>SELECT {name.toUpperCase()}</text>
    <box style={{ width: panelWidth }}><SelectionList options={options} selected={selected} height={Math.max(4, Math.min(options.length + 2, height - 8))} theme={theme} /></box>
    <text fg={theme.subtle}>UP/DOWN SELECT   ENTER APPLY   ESC CANCEL</text>
  </box>
}
