export type TranscriptEntry = {
  role: "user" | "assistant" | "activity"
  label: string
  content?: string
  tool?: ToolActivity
}

export type ToolActivity = {
  id: string
  name: string
  phase: "requested" | "running" | "awaiting_approval" | "completed" | "failed" | "denied"
  target?: string
  command?: string
  working_directory?: string
  bytes?: number
  truncated?: boolean
  exit_code?: number
  output_hidden?: boolean
}

export type Approval = {
  action: string
  summary: string
  hash: string
}

type SessionDetails = {
  provider: string
  model: string
  theme?: string
  workspace: string
  status?: string
}

export type UIState =
  | { phase: "starting"; status?: string }
  | { phase: "error"; status?: string }
  | ({ phase: "welcome" } & SessionDetails)
  | ({ phase: "chat"; transcript?: TranscriptEntry[]; approval?: Approval } & SessionDetails)
  | ({ phase: "select"; selection?: string; options?: string[] } & SessionDetails)
  | ({ phase: "confirm" } & SessionDetails)
  | ({ phase: "settings" } & SessionDetails)

export type ServerMessage =
  | { version: 1; type: "state"; payload: UIState }
  | { version: 1; type: "app.shutdown" }

export type ClientMessage =
  | { type: "app.ready" | "app.quit" | "app.cancel" | "chat.start" }
  | { type: "prompt.submit"; payload: { prompt: string } }
  | { type: "approval.resolve" | "allow-all.confirm"; payload: { approved: boolean } }
  | { type: "selection.submit"; payload: { selection: string; value: string } }
