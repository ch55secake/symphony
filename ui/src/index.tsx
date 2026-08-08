import { createCliRenderer } from "@opentui/core"
import { createRoot, useKeyboard } from "@opentui/react"
import { useEffect, useState } from "react"

type State = { phase: string; provider?: string; model?: string; workspace?: string; status?: string; transcript?: string[]; pending?: string }
type ServerMessage = { version: 1; type: "state"; payload: State }
type ClientMessage = { type: string; prompt?: string; approved?: boolean }

const rpcIn = Number(process.env.SYMPHONY_RPC_IN_FD)
const rpcOut = Number(process.env.SYMPHONY_RPC_OUT_FD)
const input = Number.isInteger(rpcIn) ? Bun.file(rpcIn) : null
const output = Number.isInteger(rpcOut) ? Bun.file(rpcOut) : null

function send(message: ClientMessage) {
  if (output) Bun.write(output, JSON.stringify({ version: 1, type: message.type, payload: { prompt: message.prompt, approved: message.approved } }) + "\n")
}

function App() {
  const [state, setState] = useState<State>({ phase: "starting", status: "Starting Symphony..." })
  const [value, setValue] = useState("")
	const suggestions = ["/allow-all", "/help", "/model", "/settings", "/theme"].filter((command) => value.startsWith("/") && !value.includes(" ") && command.startsWith(value) && command !== value)

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
          try { const message = JSON.parse(line) as ServerMessage; if (message.version === 1 && message.type === "state") setState(message.payload) } catch { /* Ignore malformed backend messages. */ }
        }
      }
    })()
  }, [])

  useKeyboard((key) => {
    if (key.ctrl && key.name === "q") send({ type: "app.quit" })
    if (state.pending && key.name === "y") send({ type: "approval.resolve", approved: true })
    if (state.pending && (key.name === "n" || key.name === "escape")) send({ type: "approval.resolve", approved: false })
  })

  return <box flexDirection="column" padding={1} gap={1}>
    <box flexDirection="row" justifyContent="space-between"><text fg="#7dd3fc">SYMPHONY</text><text fg="#94a3b8">{state.provider} / {state.model}  {state.workspace}</text></box>
    <scrollbox focused style={{ flexGrow: 1 }}>{(state.transcript ?? []).map((line, index) => <text key={index}>{line}</text>)}</scrollbox>
    <text fg={state.pending ? "#fbbf24" : "#94a3b8"}>{state.pending ?? state.status}</text>
    {suggestions.length > 0 && <box border borderColor="#64748b" flexDirection="column" paddingLeft={1} paddingRight={1}>{suggestions.map((command) => <text key={command} fg="#cbd5e1">{command}</text>)}<text fg="#64748b">Tab completes</text></box>}
    <box border borderColor="#7dd3fc" paddingLeft={1} paddingRight={1}><input focused placeholder="Describe what you need..." onInput={setValue} onSubmit={() => { send({ type: "prompt.submit", prompt: value }); setValue("") }} value={value} /></box>
  </box>
}

const renderer = await createCliRenderer({ exitOnCtrlC: false })
createRoot(renderer).render(<App />)
send({ type: "app.ready" })
