# Symphony

Symphony is a terminal coding-agent harness designed around immutable event streams.
Every agent interaction will be recorded as an auditable session event before later
milestones add model orchestration, workspace tools, and the interactive TUI.

## Event Store Foundation

This first increment provides a Go library for durable session event streams:

- KurrentDB stream per session: `session-{uuid}`
- Versioned, hash-verified event envelopes
- Optimistic-concurrency appends
- Stream reads and live subscriptions
- Persistence-time JSON redaction for common secret fields and Bearer tokens
- Session lifecycle service that owns ordered start, finish, and failure events

Redacted values are deliberately not recoverable. Their event retains the field path,
redaction reason, and hash of the resulting safe payload so replay can show that data
was omitted without leaking it.

## Local Development

Start KurrentDB and wait for its health check:

```sh
docker compose up -d --wait
```

The insecure local database accepts this connection string:

```text
kurrentdb://localhost:2113?tls=false
```

Run unit tests:

```sh
go test ./...
```

Run the KurrentDB integration test after the container is healthy:

```sh
KURRENTDB_URL='kurrentdb://localhost:2113?tls=false' go test ./internal/store/kurrentdb
```

## Configuration

Symphony reads optional YAML configuration from the user configuration directory:

- macOS: `~/Library/Application Support/symphony/config.yaml`
- Linux: `~/.config/symphony/config.yaml`

For example:

```yaml
kurrentdb_url: kurrentdb://localhost:2113?tls=false
provider: opencode
model: gpt-5.6-terra
transport: responses
workspace: /path/to/workspace
openai_api_key: your-openai-key
anthropic_api_key: your-anthropic-key
opencode_api_key: your-opencode-key
```

Provider API keys are stored in plaintext in this file. Restrict its permissions and do not
commit it. Configuration precedence is command-line flags, environment variables, this file,
then built-in defaults. Environment variables use uppercase key names, including
`KURRENTDB_URL`, `PROVIDER`, `MODEL`, `TRANSPORT`, `WORKSPACE`, `OPENAI_API_KEY`,
`ANTHROPIC_API_KEY`, and `OPENCODE_API_KEY`.

## Session Lifecycle

`internal/session` creates a session stream and owns its revision for lifecycle
writes. It records `session.started` before subsequent runtime work and appends one
terminal `session.finished` or `session.failed` event with correlation and causation
metadata. Payloads pass through the audit policy before they are persisted.

## Workspace Reads

`internal/workspace` confines reads to a configured workspace root. Each read persists
`file.read.requested` before opening the file, followed by a completion or failure event.
Completion events contain only the path, byte count, duration, and content hash; raw file
content is returned to the caller but is never persisted.

## Workspace Writes

Writes use a durable request, approval, and execution flow. `file.write.requested`
records only the proposed path, byte count, and content hash. A separate
`file.write.approved` event is required before execution validates the supplied content
against that hash and atomically replaces the target. Raw write content is never persisted.

## Workspace Commands

Commands use the same durable request and approval flow. They accept an executable and
argument list, never an implicit shell string, and may only use a workspace-relative
working directory. Runtime stdout and stderr are bounded and returned to the caller;
events retain only output hashes, byte counts, truncation state, exit code, and duration.

## Agent Turns

`internal/agent` persists user messages and model-request intent before contacting a
provider, then persists the model completion or failure. It exposes a provider-neutral
completion contract so OpenAI and Anthropic adapters can share the same audited turn path.

## OpenAI Provider

`internal/providers/openai` implements the non-streaming OpenAI Responses API. Configure
its `Config.APIKey` from `OPENAI_API_KEY` at process composition time; the provider sends
`store: false` so OpenAI does not become an additional conversation store. API keys and
response error bodies are never persisted by Symphony.

## Anthropic Provider

`internal/providers/anthropic` implements the non-streaming Anthropic Messages API.
Configure its `Config.APIKey` from `ANTHROPIC_API_KEY` at process composition time. API
keys and Anthropic response error bodies are never persisted by Symphony.

## OpenCode Go Provider

`internal/providers/opencode` supports OpenCode Go with `OPENCODE_API_KEY`. It uses
the OpenCode Zen API without persisting API keys or response error bodies. Select the
endpoint explicitly: `responses` is the default for Responses-compatible models, and
`chat-completions` supports OpenAI-compatible chat models.


## Read-Only Tool Loop

`internal/agent.Loop` follows provider tool calls with the native `read_file` tool.
It persists metadata-only tool results before each follow-up provider request, forwards
bounded file content only in memory, and stops on unknown tools, tool failures, provider
errors, cancellation, or its configured tool-round limit.

## Write Approval Bridge

The `write_file` tool pauses `agent.Loop` after persisting a write request and generic
approval request. Callers must explicitly approve or deny the returned pending action;
approval executes the hash-bound write and resumes the provider loop, while denial resumes
with an error tool result and performs no filesystem mutation.

## Command Approval Bridge

The `run_command` tool likewise pauses `agent.Loop` after recording a structured command
request and generic approval request. Approval runs the hash-bound command and provides its
bounded output only to the resumed provider loop; command output is never persisted.

## CLI Runner

Run an audited agent session with one provider credential in the environment:

```sh
KURRENTDB_URL='kurrentdb://localhost:2113?tls=false' OPENAI_API_KEY='...' \
  go run ./cmd/symphony run --provider openai --model gpt-5.2 --workspace . "Read README.md"
```

Anthropic uses `ANTHROPIC_API_KEY` and `--provider anthropic`. The runner prints the final
completion. Write and command requests stop at a terminal prompt showing only safe action
metadata and a hash; enter `y` or `yes` to approve, or any other input to deny.

OpenCode Go uses `OPENCODE_API_KEY`. For a Responses-compatible model:

```sh
KURRENTDB_URL='kurrentdb://localhost:2113?tls=false' OPENCODE_API_KEY='...' \
  go run ./cmd/symphony run --provider opencode --model gpt-5.6-terra --workspace . "Read README.md"
```

For a Chat Completions-compatible model, select that transport explicitly:

```sh
KURRENTDB_URL='kurrentdb://localhost:2113?tls=false' OPENCODE_API_KEY='...' \
  go run ./cmd/symphony run --provider opencode --transport chat-completions --model kimi-k2.7-code --workspace . "Read README.md"
```

## Session Replay

Each run prints its session ID. Replay that session's recorded audit timeline as JSON Lines
without invoking a provider or repeating side effects:

```sh
KURRENTDB_URL='kurrentdb://localhost:2113?tls=false' \
  go run ./cmd/symphony replay SESSION_ID
```

## Interactive TUI

Start a multi-turn session from the workspace you want Symphony to use:

```sh
go run ./cmd/symphony
```

`go run ./cmd/symphony tui` is an equivalent explicit alias.

Symphony starts or reuses a local `symphony-kurrentdb` Docker container before opening
the TUI. When the configuration already contains a provider, model, and matching API
key, Symphony opens the welcome screen; pressing Enter starts chat. Otherwise, the
centered splash accepts `/connect`, which collects a provider API key, fetches available
models, and saves the selected provider, key, and model in the user configuration file.
The TUI always uses the current directory as the workspace.

While chat is open, these commands manage the active session:

- `/connect` changes provider, API key, and model using the masked connection picker.
- `/model` lists models for the active provider; `/model NAME` selects one directly.
- `/theme` lists built-in themes; `/theme default`, `/theme contrast`, and `/theme mono` apply and persist a theme.
- `/allow-all` enables automatic approval for workspace writes and commands for the current session after confirmation. `/allow-all off` restores prompts. Every action remains recorded and constrained to the workspace.
- `/settings` displays the current connection, theme, and approval mode.
- `/help` lists available commands.

Choose `opencode-go` for an OpenCode Go subscription. It uses the Go model catalog and
endpoint, which are separate from pay-as-you-go OpenCode Zen billing.

Use `Enter` to send a prompt. The TUI retains conversation and tool
context in memory for the current session. Write and command requests remain paused
until explicitly approved with `y` or denied with `n` or `Esc`; the interface shows
only the existing safe summary and hash. Use `Ctrl+Q` to finish the session or
`Ctrl+C` to cancel it.

## OpenTUI Migration

The OpenTUI React client lives in `ui/`. It is compiled with Bun and communicates with
the Go runtime over private file descriptors using a versioned JSON-lines protocol; it
does not receive provider credentials, KurrentDB access, workspace capabilities, or
event-store access. During migration, set `SYMPHONY_UI_EXECUTABLE=ui/dist/symphony-ui`
to use the OpenTUI path with an existing saved connection. Go remains responsible for
KurrentDB startup, session events, model calls, and approval resolution.
