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
