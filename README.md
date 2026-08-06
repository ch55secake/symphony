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
