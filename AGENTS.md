# Symphony Contributor Guide

## Project Goal

Symphony is an interactive coding-agent harness. KurrentDB is the durable system of
record for every session event, enabling auditing and recorded replay.

## Development

- Format Go changes with `gofmt`.
- Run `go test ./...` and `go vet ./...` before submitting a change.
- Validate local infrastructure with `docker compose config --quiet`.
- Run the database integration test with `KURRENTDB_URL='kurrentdb://localhost:2113?tls=false' go test ./internal/store/kurrentdb` after starting KurrentDB.

## Event Store Rules

- Treat persisted events as immutable and versioned contracts.
- Persist intent before performing an external side effect, then persist its outcome.
- Use expected stream revisions for ordered session writes; do not use unconstrained appends for session events.
- Redact secrets before event construction or persistence. Never store credentials, raw authorization headers, or unredacted environment values.
- Preserve correlation and causation IDs when adding event types.

## Scope

- Keep pull requests small and independently reviewable.
- Do not add model providers, workspace tools, or TUI behavior to the event-store foundation unless the change explicitly requires it.
