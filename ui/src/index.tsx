import { createCliRenderer } from "@opentui/core"
import { createRoot, useKeyboard, useTerminalDimensions } from "@opentui/react"
import { useEffect, useState } from "react"

type Entry = { role: "user" | "assistant" | "activity"; label: string; content?: string; activity?: string }
type State = { phase: "starting" | "welcome" | "chat" | "error" | "select" | "confirm" | "settings"; provider?: string; model?: string; theme?: string; workspace?: string; status?: string; transcript?: Entry[]; pending?: string; selection?: string; options?: string[] }
type ServerMessage = { version: 1; type: "state"; payload: State }
type ClientMessage = { type: string; prompt?: string; approved?: boolean; selection?: string; value?: string }

const commands = [
  { name: "/allow-all", description: "Approve actions for this session" },
  { name: "/help", description: "Show available commands" },
  { name: "/model", description: "List or select a model" },
  { name: "/settings", description: "Show current settings" },
  { name: "/theme", description: "Select the next-session theme" },
]

const mark = `███████╗██╗   ██╗███╗   ███╗██████╗ ██╗  ██╗ ██████╗ ███╗   ██╗██╗   ██╗
██╔════╝╚██╗ ██╔╝████╗ ████║██╔══██╗██║  ██║██╔═══██╗████╗  ██║╚██╗ ██╔╝
███████╗ ╚████╔╝ ██╔████╔██║██████╔╝███████║██║   ██║██╔██╗ ██║ ╚████╔╝
╚════██║  ╚██╔╝  ██║╚██╔╝██║██╔═══╝ ██╔══██║██║   ██║██║╚██╗██║  ╚██╔╝
███████║   ██║   ██║ ╚═╝ ██║██║     ██║  ██║╚██████╔╝██║ ╚████║   ██║
╚══════╝   ╚═╝   ╚═╝     ╚═╝╚═╝     ╚═╝  ╚═╝ ╚═════╝ ╚═══╝╚═══╝   ╚═╝`

const rpcIn = Number(process.env.SYMPHONY_RPC_IN_FD)
const rpcOut = Number(process.env.SYMPHONY_RPC_OUT_FD)
const input = Number.isInteger(rpcIn) ? Bun.file(rpcIn) : null
const output = Number.isInteger(rpcOut) ? Bun.file(rpcOut) : null

async function send(message: ClientMessage) {
  if (output) await Bun.write(output, JSON.stringify({ version: 1, type: message.type, payload: { prompt: message.prompt, approved: message.approved, selection: message.selection, value: message.value } }) + "\n")
}

function Composer({ value, setValue, submit, suggestions, selected }: { value: string; setValue: (value: string) => void; submit: () => void; suggestions: typeof commands; selected: number }) {
  return <>
    {suggestions.length > 0 && <box border borderColor="#475569" flexDirection="column" paddingLeft={1} paddingRight={1} style={{ width: "100%" }}>
      {suggestions.map((command, index) => <text key={command.name} fg={index === selected ? "#f9a8d4" : "#cbd5e1"}>{index === selected ? "> " : "  "}{command.name}</text>)}
      <text fg="#64748b">Up/Down selects  ·  Tab completes</text>
    </box>}
    <box border borderColor="#38bdf8" paddingLeft={1} paddingRight={1} style={{ width: "100%" }}><text fg="#38bdf8">ASK  </text><input focused placeholder="Describe what you need..." onInput={setValue} onSubmit={submit} value={value} /></box>
  </>
}

function Conversation({ entries, height }: { entries: Entry[]; height: number }) {
  return <scrollbox style={{ height, width: "100%" }}>
    {entries.map((entry, index) => {
      if (entry.role === "activity") return <text key={index} fg="#64748b">{entry.activity}</text>
      const user = entry.role === "user"
      return <box key={index} border borderColor={user ? "#334155" : "#3b82f6"} flexDirection="column" paddingLeft={1} paddingRight={1} marginBottom={1}>
        <text fg={user ? "#93c5fd" : "#f9a8d4"}>{entry.label}</text>
        <text fg="#e2e8f0">{entry.content}</text>
      </box>
    })}
  </scrollbox>
}

function App() {
  const { width, height } = useTerminalDimensions()
  const [state, setState] = useState<State>({ phase: "starting", status: "Starting Symphony..." })
  const [value, setValue] = useState("")
  const [selected, setSelected] = useState(0)
	const [spinnerFrame, setSpinnerFrame] = useState(0)
  const suggestions = commands.filter((command) => value.startsWith("/") && !value.includes(" ") && command.name.startsWith(value) && command.name !== value)
  const composerHeight = state.pending ? 2 : 3 + (suggestions.length > 0 ? Math.min(suggestions.length + 2, 7) : 0)
  const conversationHeight = Math.max(3, height - composerHeight - 7)

  const colors = state.theme === "contrast" ? { accent: "#22d3ee", model: "#f0abfc", muted: "#e2e8f0", border: "#94a3b8" } : state.theme === "mono" ? { accent: "#ffffff", model: "#ffffff", muted: "#d4d4d4", border: "#737373" } : { accent: "#38bdf8", model: "#f9a8d4", muted: "#94a3b8", border: "#475569" }

  useEffect(() => setSelected(0), [value])
  useEffect(() => setSelected(0), [state.phase, state.selection])
	useEffect(() => {
		if (state.status !== "WORKING") return
		const timer = setInterval(() => setSpinnerFrame((frame) => (frame + 1) % 4), 120)
		return () => clearInterval(timer)
	}, [state.status])
  useEffect(() => {
    if (!input) return
    const reader = input.stream().pipeThrough(new TextDecoderStream()).getReader()
    let buffer = ""
    void (async () => {
      for (;;) {
        const { done, value } = await reader.read()
        if (done) return
        buffer += value
        const lines = buffer.split("\n")
        buffer = lines.pop() ?? ""
        for (const line of lines) {
          try {
            const message = JSON.parse(line) as ServerMessage
            if (message.version === 1 && message.type === "state") setState(message.payload)
          } catch { /* Ignore malformed backend messages. */ }
        }
      }
    })()
  }, [])

  useKeyboard((key) => {
    if (key.ctrl && key.name === "q") void send({ type: "app.quit" })
    if (state.pending && key.name === "y") void send({ type: "approval.resolve", approved: true })
    if (state.pending && (key.name === "n" || key.name === "escape")) void send({ type: "approval.resolve", approved: false })
    if (state.phase === "confirm") {
      if (key.name === "y") void send({ type: "allow-all.confirm", approved: true })
      if (key.name === "n" || key.name === "escape") void send({ type: "allow-all.confirm", approved: false })
      return
    }
    if (state.phase === "settings" && (key.name === "escape" || key.name === "return")) {
      void send({ type: "chat.start" })
      return
    }
    if (state.phase === "select") {
      const options = state.options ?? []
      if (key.name === "up") setSelected((index) => Math.max(0, index - 1))
      if (key.name === "down") setSelected((index) => Math.min(options.length - 1, index + 1))
      if (key.name === "return" && options[selected]) void send({ type: "selection.submit", selection: state.selection, value: options[selected] })
      if (key.name === "escape") void send({ type: "chat.start" })
      return
    }
    if (suggestions.length === 0) return
    if (key.name === "up") setSelected((index) => Math.max(0, index - 1))
    if (key.name === "down") setSelected((index) => Math.min(suggestions.length - 1, index + 1))
    if (key.name === "tab") setValue(suggestions[selected].name)
  })

  const submit = async () => {
    if (state.phase === "welcome") {
      await send({ type: "chat.start" })
      if (value.trim()) await send({ type: "prompt.submit", prompt: value })
      setValue("")
      return
    }
    if (value.trim()) await send({ type: "prompt.submit", prompt: value })
    setValue("")
  }

  if (state.phase === "welcome") return <box flexDirection="column" alignItems="center" justifyContent="center" style={{ flexGrow: 1 }} padding={1} gap={1}>
    <text fg={colors.accent}>{mark}</text>
    <text fg={colors.muted}>{state.provider} / {state.model}  |  {state.workspace}</text>
    <box style={{ width: "80%" }}><Composer value={value} setValue={setValue} submit={() => { void submit() }} suggestions={suggestions} selected={selected} /></box>
    <text fg={colors.muted}>Enter starts chat  ·  Ctrl+Q quits</text>
  </box>

  if (state.phase === "select") return <box flexDirection="column" alignItems="center" justifyContent="center" style={{ flexGrow: 1 }} padding={1} gap={1}>
    <text fg={colors.accent}>SELECT {state.selection?.toUpperCase()}</text>
    <box border borderColor={colors.border} flexDirection="column" paddingLeft={1} paddingRight={1} style={{ width: "70%" }}>
      {(state.options ?? []).map((option, index) => <text key={option} fg={index === selected ? colors.model : colors.muted}>{index === selected ? "> " : "  "}{option}</text>)}
    </box>
    <text fg="#64748b">Up/Down selects  ·  Enter applies  ·  Esc cancels</text>
  </box>

  if (state.phase === "confirm") return <box flexDirection="column" alignItems="center" justifyContent="center" style={{ flexGrow: 1 }} padding={1} gap={1}>
    <box border borderColor="#fbbf24" paddingLeft={1} paddingRight={1}><text fg="#fbbf24">{state.status}</text></box>
    <text fg="#94a3b8">[y] enable  ·  [n/Esc] cancel</text>
  </box>

  if (state.phase === "settings") return <box flexDirection="column" alignItems="center" justifyContent="center" style={{ flexGrow: 1 }} padding={1} gap={1}>
    <text fg={colors.accent}>SETTINGS</text>
    <box border borderColor={colors.border} flexDirection="column" paddingLeft={1} paddingRight={1}>
      <text fg={colors.muted}>Provider  {state.provider}</text><text fg={colors.muted}>Model     {state.model}</text><text fg={colors.muted}>Theme     {state.theme}</text>
      <text fg="#64748b">/model changes model  ·  /theme changes theme  ·  /allow-all changes approval</text>
    </box>
    <text fg="#64748b">Enter or Esc returns to chat</text>
  </box>

  if (state.phase === "starting" || state.phase === "error") return <box flexDirection="column" alignItems="center" justifyContent="center" style={{ flexGrow: 1 }}><text fg={state.phase === "error" ? "#f87171" : "#38bdf8"}>SYMPHONY</text><text fg="#94a3b8">{state.status}</text></box>

  return <box flexDirection="column" padding={1} gap={1} style={{ width, height }}>
    <box flexDirection="row" justifyContent="space-between"><text fg={colors.accent}>SYMPHONY</text><text fg={colors.muted}>{state.provider} / {state.model}  {state.workspace}</text></box>
    <Conversation entries={state.transcript ?? []} height={conversationHeight} />
    <box border borderColor={state.pending ? "#fbbf24" : state.status === "WORKING" ? colors.accent : "#334155"} paddingLeft={1} paddingRight={1}><text fg={state.pending ? "#fbbf24" : state.status === "WORKING" ? colors.accent : "#94a3b8"}>{state.pending ?? (state.status === "WORKING" ? `${["⠋", "⠙", "⠹", "⠸"][spinnerFrame]}  WORKING` : state.status)}</text></box>
    {state.pending ? <text fg="#64748b">User input is paused until the action is approved or denied.</text> : <Composer value={value} setValue={setValue} submit={() => { void submit() }} suggestions={suggestions} selected={selected} />}
  </box>
}

const renderer = await createCliRenderer({ exitOnCtrlC: false })
createRoot(renderer).render(<App />)
void send({ type: "app.ready" })
