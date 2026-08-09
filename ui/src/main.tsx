import { createCliRenderer } from "@opentui/core"
import { createRoot, type Root } from "@opentui/react"
import { App } from "./app/App"
import { Transport } from "./protocol/transport"

const renderer = await createCliRenderer({ exitOnCtrlC: false })
let root: Root | undefined
let shuttingDown = false
let transport: Transport

function shutdown() {
  if (shuttingDown) return
  shuttingDown = true
  transport?.stop()
  root?.unmount()
  renderer.destroy()
}

transport = new Transport(shutdown)
root = createRoot(renderer)
root.render(<App transport={transport} />)
transport.start()
void transport.send({ type: "app.ready" })
