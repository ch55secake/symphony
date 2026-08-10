import { Brand } from "../components/Brand"
import type { Theme } from "../theme/tokens"

export function StartupScreen({ status, error, theme }: { status?: string; error: boolean; theme: Theme }) {
  return <box flexDirection="column" alignItems="center" justifyContent="center" style={{ flexGrow: 1 }} gap={1}>
    <Brand theme={theme} expanded={false} />
    <text fg={error ? theme.danger : theme.muted}>{status}</text>
  </box>
}

export function ConfirmationScreen({ status, theme, width, height }: { status?: string; theme: Theme; width: number; height: number }) {
  if (height < 13 || width < 36) return <box alignItems="center" justifyContent="center" style={{ width, height }}>
    <text fg={theme.warning}>Resize terminal to continue</text>
  </box>
  const compact = width < 70 || height < 16
  return <box flexDirection="column" alignItems="center" justifyContent="center" style={{ flexGrow: 1 }} padding={1} gap={1}>
    <text fg={theme.accent}>SYMPHONY  <span fg={theme.subtle}>/ APPROVALS</span></text>
    <text fg={theme.warning}>ENABLE ALLOW ALL?</text>
    <box border borderColor={theme.warning} flexDirection="column" paddingLeft={2} paddingRight={2} style={{ width: compact ? "100%" : "68%", flexShrink: 0 }}>
      <text fg={theme.text}>{status}</text>
      {!compact && <text fg={theme.muted}>Commands and file writes will run without individual prompts.</text>}
      <text fg={theme.subtle}>This policy lasts for the current session only.</text>
    </box>
    <text fg={theme.warning}>Y <span fg={theme.subtle}>ENABLE</span>   N/ESC <span fg={theme.subtle}>KEEP ASKING</span></text>
  </box>
}

const editableSettings = ["MODEL", "THEME", "APPROVALS", "THINKING"] as const

function SettingRow({ label, value, selected, color, theme }: { label: string; value: string; selected: boolean; color?: string; theme: Theme }) {
  return <box flexDirection="row" paddingLeft={1} style={{ width: "100%" }}>
    <box style={{ width: 16, flexShrink: 0 }}><text fg={selected ? theme.model : theme.muted}>{selected ? "> " : "  "}{label}</text></box>
    <box justifyContent="flex-end" style={{ flexGrow: 1, minWidth: 1, overflow: "hidden" }}>
      <text fg={color ?? (selected ? theme.text : theme.muted)} wrapMode="none" truncate>{value}</text>
    </box>
  </box>
}

export function SettingsScreen({ provider, model, themeName, workspace, allowAll, reasoningSummaries = false, selected, width, height, theme }: { provider: string; model: string; themeName?: string; workspace: string; allowAll: boolean; reasoningSummaries?: boolean; selected: number; width: number; height: number; theme: Theme }) {
  if (height < 14 || width < 40) return <box alignItems="center" justifyContent="center" style={{ width, height }}>
    <text fg={theme.warning}>Resize terminal to continue</text>
  </box>
  const compact = width < 70 || height < 18
  const panelWidth = width < 80 ? "100%" : "68%"
  return <box flexDirection="column" alignItems="center" justifyContent="center" style={{ flexGrow: 1 }} padding={1} gap={1}>
    <text fg={theme.accent}>SYMPHONY  <span fg={theme.subtle}>/ SETTINGS</span></text>
    <text fg={theme.model}>SESSION CONTROL</text>
    {!compact && <text fg={theme.muted}>Choose a setting to change</text>}
    <box flexDirection="column" style={{ width: panelWidth }} gap={1}>
      <box flexDirection="column" style={{ width: "100%" }}>
        <SettingRow label={editableSettings[0]} value={model} selected={selected === 0} theme={theme} />
        <SettingRow label={editableSettings[1]} value={(themeName ?? "default").toUpperCase()} selected={selected === 1} color={selected === 1 ? theme.accent : undefined} theme={theme} />
        <SettingRow label={editableSettings[2]} value={allowAll ? "ALLOW ALL" : "ASK EVERY TIME"} selected={selected === 2} color={allowAll ? theme.warning : theme.success} theme={theme} />
        <SettingRow label={editableSettings[3]} value={reasoningSummaries ? "SUMMARIES ON" : "OFF"} selected={selected === 3} color={reasoningSummaries ? theme.accent : theme.muted} theme={theme} />
      </box>
      {!compact && <box border={["top"]} borderColor={theme.border} flexDirection="column" paddingTop={1} style={{ width: "100%" }}>
        <SettingRow label="PROVIDER" value={provider} selected={false} theme={theme} />
        <SettingRow label="WORKSPACE" value={workspace} selected={false} theme={theme} />
      </box>}
    </box>
    <text fg={theme.accent}>UP/DOWN <span fg={theme.subtle}>MOVE</span>   ENTER <span fg={theme.subtle}>CHANGE</span>   ESC <span fg={theme.subtle}>BACK</span></text>
  </box>
}
