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

Redacted values are deliberately not recoverable. Their event retains the field path,
redaction reason, and hash of the resulting safe payload so replay can show that data
was omitted without leaking it.

## Local Development

Start KurrentDB:

```sh
docker compose up -d
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
