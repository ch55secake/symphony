import { Brand } from "../components/Brand"
import type { Theme } from "../theme/tokens"

export function StartupScreen({ status, error, theme }: { status?: string; error: boolean; theme: Theme }) {
  return <box flexDirection="column" alignItems="center" justifyContent="center" style={{ flexGrow: 1 }} gap={1}>
    <Brand theme={theme} expanded={false} />
    <text fg={error ? theme.danger : theme.muted}>{status}</text>
  </box>
}

export function ConfirmationScreen({ status, theme }: { status?: string; theme: Theme }) {
  return <box flexDirection="column" alignItems="center" justifyContent="center" style={{ flexGrow: 1 }} padding={1} gap={1}>
    <box flexDirection="column" paddingLeft={2} paddingRight={2} style={{ backgroundColor: theme.surface }}>
      <text fg={theme.warning}>CHANGE APPROVAL MODE</text>
      <text fg={theme.text}>{status}</text>
    </box>
    <text fg={theme.subtle}>[Y] ENABLE   [N/ESC] CANCEL</text>
  </box>
}

export function SettingsScreen({ provider, model, themeName, workspace, theme }: { provider: string; model: string; themeName?: string; workspace: string; theme: Theme }) {
  return <box flexDirection="column" alignItems="center" justifyContent="center" style={{ flexGrow: 1 }} padding={1} gap={1}>
    <text fg={theme.accent}>SESSION SETTINGS</text>
    <box flexDirection="column" paddingLeft={2} paddingRight={2}>
      <text fg={theme.muted}>PROVIDER   <span fg={theme.text}>{provider}</span></text>
      <text fg={theme.muted}>MODEL      <span fg={theme.text}>{model}</span></text>
      <text fg={theme.muted}>THEME      <span fg={theme.text}>{themeName ?? "default"}</span></text>
      <text fg={theme.muted}>WORKSPACE  <span fg={theme.text}>{workspace}</span></text>
    </box>
    <text fg={theme.subtle}>/MODEL   /THEME   /ALLOW-ALL</text>
    <text fg={theme.subtle}>ENTER OR ESC RETURNS</text>
  </box>
}
