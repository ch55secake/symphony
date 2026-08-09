import { useEffect, useState } from "react"
import { useKeyboard, useTerminalDimensions } from "@opentui/react"
import { commandSuggestions } from "../commands/registry"
import type { UIState } from "../protocol/types"
import type { Transport } from "../protocol/transport"
import { getTheme } from "../theme/tokens"
import { ChatScreen } from "../screens/ChatScreen"
import { SelectionScreen } from "../screens/SelectionScreen"
import { ConfirmationScreen, SettingsScreen, StartupScreen } from "../screens/UtilityScreens"
import { WelcomeScreen } from "../screens/WelcomeScreen"

export function App({ transport }: { transport: Transport }) {
  const { width, height } = useTerminalDimensions()
  const [state, setState] = useState<UIState>({ phase: "starting", status: "Starting Symphony..." })
  const [value, setValue] = useState("")
  const [commandSelected, setCommandSelected] = useState(0)
  const [optionSelected, setOptionSelected] = useState(0)
  const [settingSelected, setSettingSelected] = useState(0)
  const minimumSuggestionHeight = state.phase === "welcome" ? 18 : 15
  const suggestions = height < minimumSuggestionHeight ? [] : commandSuggestions(value)
  const mode = ("mode" in state ? state.mode : undefined) ?? "build"
  const previewTheme = state.phase === "select" && state.selection === "theme" ? state.options?.[optionSelected] : undefined
  const theme = getTheme(previewTheme ?? ("theme" in state ? state.theme : undefined))

  useEffect(() => transport.subscribe(setState), [transport])
  useEffect(() => setCommandSelected(0), [value, state.phase])
  useEffect(() => {
    if (state.phase !== "select") return
    const current = state.selection === "model" ? state.model : state.theme ?? "default"
    setOptionSelected(Math.max(0, (state.options ?? []).indexOf(current)))
  }, [state])
  useEffect(() => setSettingSelected(0), [state.phase])

  const submitPrompt = async (prompt: string) => {
    if (state.phase === "chat" && (state.status === "WORKING" || state.status === "CANCELING")) return
    if (state.phase === "welcome") await transport.send({ type: "chat.start" })
    if (prompt) await transport.send({ type: "prompt.submit", payload: { prompt } })
    setValue("")
  }

  useKeyboard((key) => {
    if (key.ctrl && key.name === "q") {
      void transport.send({ type: "app.quit" })
      return
    }
    if (key.ctrl && key.name === "c") {
      void transport.send({ type: "app.cancel" })
      return
    }
    if ((state.phase === "welcome" || state.phase === "chat") && key.name === "tab" && value.trim() === "" && !(state.phase === "chat" && (state.approval || state.status === "WORKING" || state.status === "CANCELING"))) {
      key.preventDefault()
      void transport.send({ type: "mode.set", payload: { mode: mode === "plan" ? "build" : "plan", phase: state.phase } })
      return
    }
    if (state.phase === "chat" && state.approval) {
      if (key.name === "y") void transport.send({ type: "approval.resolve", payload: { approved: true } })
      if (key.name === "n" || key.name === "escape") void transport.send({ type: "approval.resolve", payload: { approved: false } })
      return
    }
    if (state.phase === "confirm") {
      if (key.name === "y") void transport.send({ type: "allow-all.confirm", payload: { approved: true } })
      if (key.name === "n" || key.name === "escape") void transport.send({ type: "allow-all.confirm", payload: { approved: false } })
      return
    }
    if (state.phase === "settings") {
      if (key.name === "up" || key.name === "down" || key.name === "return") key.preventDefault()
      if (key.name === "up") setSettingSelected((index) => Math.max(0, index - 1))
      if (key.name === "down") setSettingSelected((index) => Math.min(2, index + 1))
      if (key.name === "return") {
        const prompt = settingSelected === 0 ? "/model" : settingSelected === 1 ? "/theme" : state.allow_all ? "/allow-all off" : "/allow-all"
        void transport.send({ type: "prompt.submit", payload: { prompt } })
      }
      if (key.name === "escape") void transport.send({ type: "chat.start" })
      return
    }
    if (state.phase === "select") {
      const options = state.options ?? []
      if (key.name === "up" || key.name === "down" || key.name === "return") key.preventDefault()
      if (key.name === "up") setOptionSelected((index) => Math.max(0, index - 1))
      if (key.name === "down") setOptionSelected((index) => Math.max(0, Math.min(options.length - 1, index + 1)))
      if (key.name === "return" && options[optionSelected]) void transport.send({ type: "selection.submit", payload: { selection: state.selection ?? "", value: options[optionSelected] } })
      if (key.name === "escape") void transport.send({ type: "chat.start" })
      return
    }
    if (suggestions.length === 0) return
    if (key.name === "up" || key.name === "down" || key.name === "tab" || key.name === "return") key.preventDefault()
    if (key.name === "up") setCommandSelected((index) => Math.max(0, index - 1))
    if (key.name === "down") setCommandSelected((index) => Math.min(suggestions.length - 1, index + 1))
    if (key.name === "tab" && suggestions[commandSelected]) setValue(suggestions[commandSelected].name)
    if (key.name === "return" && suggestions[commandSelected]) void submitPrompt(suggestions[commandSelected].name)
  })

  const submit = async () => {
    await submitPrompt(value.trim())
  }

  if (state.phase === "starting" || state.phase === "error") return <StartupScreen status={state.status} error={state.phase === "error"} theme={theme} />
  if (state.phase === "welcome") return <WelcomeScreen {...state} mode={mode} value={value} onChange={setValue} onSubmit={() => void submit()} suggestions={suggestions} selected={commandSelected} theme={theme} width={width} height={height} />
  if (state.phase === "select") return <SelectionScreen name={state.selection ?? "option"} options={state.options ?? []} selected={optionSelected} current={state.selection === "model" ? state.model : state.theme ?? "default"} width={width} height={height} theme={theme} />
  if (state.phase === "confirm") return <ConfirmationScreen status={state.status} theme={theme} width={width} height={height} />
  if (state.phase === "settings") return <SettingsScreen provider={state.provider} model={state.model} themeName={state.theme} workspace={state.workspace} allowAll={state.allow_all ?? false} selected={settingSelected} width={width} height={height} theme={theme} />
  return <ChatScreen provider={state.provider} model={state.model} workspace={state.workspace} status={state.status} transcript={state.transcript ?? []} approval={state.approval} value={value} onChange={setValue} onSubmit={() => void submit()} suggestions={suggestions} selected={commandSelected} mode={mode} theme={theme} width={width} height={height} />
}
