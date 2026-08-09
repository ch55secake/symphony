import type { ClientMessage, ServerMessage, UIState } from "./types"

type StateListener = (state: UIState) => void

export class Transport {
  private readonly input: Bun.BunFile | null
  private readonly output: Bun.BunFile | null
  private readonly listeners = new Set<StateListener>()
  private reader?: ReadableStreamDefaultReader<string>
  private latestState?: UIState
  private stopped = false

  constructor(private readonly shutdown: () => void) {
    const inputFD = Number(process.env.SYMPHONY_RPC_IN_FD)
    const outputFD = Number(process.env.SYMPHONY_RPC_OUT_FD)
    this.input = Number.isInteger(inputFD) ? Bun.file(inputFD) : null
    this.output = Number.isInteger(outputFD) ? Bun.file(outputFD) : null
  }

  subscribe(listener: StateListener) {
    this.listeners.add(listener)
    if (this.latestState) listener(this.latestState)
    return () => {
      this.listeners.delete(listener)
    }
  }

  async send(message: ClientMessage) {
    if (!this.output || this.stopped) return
    const payload = "payload" in message ? message.payload : undefined
    try {
      await Bun.write(this.output, JSON.stringify({ version: 1, type: message.type, payload }) + "\n")
    } catch {
      this.shutdown()
    }
  }

  start() {
    if (!this.input) return
    this.reader = this.input.stream().pipeThrough(new TextDecoderStream()).getReader()
    void this.readMessages()
  }

  stop() {
    this.stopped = true
    void this.reader?.cancel()
  }

  private async readMessages() {
    let buffer = ""
    try {
      for (;;) {
        const { done, value } = await this.reader!.read()
        if (done) break
        buffer += value
        const lines = buffer.split("\n")
        buffer = lines.pop() ?? ""
        for (const line of lines) this.receive(line)
      }
    } catch {
      // Parent disconnect and explicit reader cancellation share the teardown path.
    } finally {
      this.shutdown()
    }
  }

  private receive(line: string) {
    try {
      const message = JSON.parse(line) as ServerMessage
      if (message.version !== 1) return
      if (message.type === "app.shutdown") {
        this.shutdown()
        return
      }
      if (message.type === "state") {
        this.latestState = message.payload
        for (const listener of this.listeners) listener(message.payload)
      }
    } catch {
      // A malformed frame is isolated from the current display state.
    }
  }
}
