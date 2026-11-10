# Local development

## Requirements

- Go 1.26 or newer.
- Network access on the first build to download Go modules.

## Run

```sh
go run ./cmd/ces-mcp
```

The server uses stdio, so run it through an MCP client or inspector that can launch stdio servers. Do not expect application logs on stdout; startup errors go to stderr. Persistent runs default to `./data`.

## Remote Streamable HTTP

```sh
CES_TRANSPORT=http CES_HTTP_ADDR=127.0.0.1:8080 go run ./cmd/ces-mcp
```

The endpoint is `http://127.0.0.1:8080/mcp` unless you set `CES_HTTP_PATH`. To bind beyond loopback, also set a strong `CES_HTTP_BEARER_TOKEN`. For remote deployment, configure HTTPS with `CES_HTTP_TLS_CERT_FILE` and `CES_HTTP_TLS_KEY_FILE`, or terminate TLS at a trusted reverse proxy. The proxy must forward `Authorization` to the server. The service rejects non-loopback binds without a bearer token.

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
| `CES_TRANSPORT` | `stdio` (default) or `http` / `streamable-http`. |
| `CES_HTTP_ADDR` | HTTP listen address; defaults to `127.0.0.1:8080`. |
| `CES_HTTP_PATH` | MCP route; defaults to `/mcp`. |
| `CES_HTTP_BEARER_TOKEN` | Optional loopback auth token; required for non-loopback binds. |
| `CES_HTTP_TLS_CERT_FILE` | TLS certificate PEM path. Set together with the key path. |
| `CES_HTTP_TLS_KEY_FILE` | TLS private key PEM path. Set together with the certificate path. |
| `CES_RUNTIME_EXECUTABLE` | Executable path or command name for the adapter. Empty disables server-managed mode. |
| `CES_RUNTIME_ARGS` | Space-separated arguments. |
| `CES_RUNTIME_ENV_ALLOWLIST` | Comma-separated environment variable names passed to the adapter. Other environment variables are not inherited. |

For each dispatch the command receives one JSON `DispatchManifest` on stdin. It must write one JSON phase artifact on stdout. Diagnostics may go to stderr. A fresh process starts for each dispatch. This adapter is not an OS sandbox and does not attest that a model provider used a fresh context.

## Data and recovery

Use a stable, private directory for `CES_DATA_DIR`. The engine serializes per-run updates with a file lock and atomically replaces each run file on commit. Restart the server with the same directory to recover runs. Do not edit run files while the server is active.
