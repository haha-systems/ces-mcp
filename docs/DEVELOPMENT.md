# Local development

## Requirements

- Go 1.26 or newer.
- Network access on the first build to download Go modules.

## Run

```sh
go run ./cmd/ces-mcp
```

The server uses stdio, so run it through an MCP client or inspector that can launch stdio servers. Do not expect application logs on stdout; startup errors go to stderr. Persistent runs default to `./data`.

## Build and verify

```sh
gofmt -w cmd internal
go test ./... -count=1
go test -race ./...
go vet ./...
go build ./...
```

The tests cover a complete five-phase run, fresh projections after each accepted phase, stale and mismatched dispatches, unauthorized mutations, persistence recovery, replay, contradictory evidence, independent runs, host/server state equivalence, concurrent version conflicts, idempotent submissions, and MCP tool/resource schemas.

## Example export

```sh
go run ./cmd/ces-demo > examples/complete-run.json
```

The demo creates isolated temporary storage, performs all five phases through host-managed dispatch and the normal validation/acceptance path, exports the run, and removes its temporary storage.

## Optional command runtime

The included command adapter supports server-managed execution. Configure:

| Variable | Purpose |
| --- | --- |
| `CES_RUNTIME_EXECUTABLE` | Executable path or command name for the adapter. Empty disables server-managed mode. |
| `CES_RUNTIME_ARGS` | Space-separated arguments. |
| `CES_RUNTIME_ENV_ALLOWLIST` | Comma-separated environment variable names passed to the adapter. Other environment variables are not inherited. |

For each dispatch the command receives one JSON `DispatchManifest` on stdin. It must write one JSON phase artifact on stdout. Diagnostics may go to stderr. A fresh process starts for each dispatch. This adapter is not an OS sandbox and does not attest that a model provider used a fresh context.

## Data and recovery

Use a stable, private directory for `CES_DATA_DIR`. The engine serializes per-run updates with a file lock and atomically replaces each run file on commit. Restart the server with the same directory to recover runs. Do not edit run files while the server is active.
