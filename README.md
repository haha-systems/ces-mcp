# CES MCP Server

This repository provides a standalone Model Context Protocol server for Cumulative Epistemic State (CES) v0.2.0. It reuses the CES state store, event replay, phase projections, relation rules, permissions, and phase artifact validators from the existing `internal/epistemic` and `internal/cognition` implementations.

CES keeps accepted epistemic state as typed, append-only events. Agents return structured proposals. The engine validates each proposal against the current phase projection and commits it only after an explicit accept.

## Start

Requirements: Go 1.26 or newer.

```sh
go build -o ./bin/ces-mcp ./cmd/ces-mcp
CES_DATA_DIR="$PWD/data" ./bin/ces-mcp
```

The server uses MCP stdio by default. Configure it as a local MCP server in the client. Example launch declarations are in [`config/`](config/); the JSON and TOML examples use common server-block formats, so adapt the field names to the client. The default data directory is `./data`; set `CES_DATA_DIR` to choose another location.

## Remote Streamable HTTP

Select the Streamable HTTP transport and bind address:

```sh
CES_TRANSPORT=http CES_HTTP_ADDR=127.0.0.1:8080 ./bin/ces-mcp
```

The MCP endpoint is `http://127.0.0.1:8080/mcp` by default. Set `CES_HTTP_PATH` to change the path. A non-loopback bind such as `0.0.0.0:8080` requires `CES_HTTP_BEARER_TOKEN`. Use a strong token and TLS, either through `CES_HTTP_TLS_CERT_FILE` and `CES_HTTP_TLS_KEY_FILE` or a TLS-terminating proxy. The remote client declaration example is [`config/remote-http-client.json`](config/remote-http-client.json).

## Phase workflow

`ces_create` starts in TRIAGE. For each phase, the client reads `ces_project`, creates a fresh agent context when it can, calls `ces_dispatch`, submits the returned structured artifact, calls `ces_validate`, then calls `ces_accept` and `ces_advance`. The engine permits only:

`TRIAGE → ABDUCE → FRAME → EXECUTE → CLOSE`

Each dispatch has a unique session identifier and includes the run, phase, accepted state version and hash, projection hash, prompt hash, and optional parent run. The hashes bind the result to the original dispatch. They do not prove that an agent received no additional context. The host or runtime must enforce context and tool isolation.

See [`docs/MCP_API.md`](docs/MCP_API.md) for tool and resource schemas, [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) for state ownership and isolation, and [`docs/DEVELOPMENT.md`](docs/DEVELOPMENT.md) for local development and tests.

## Example run

`examples/complete-run.json` is a complete five-phase export. Regenerate it with:

```sh
go run ./cmd/ces-demo > examples/complete-run.json
```

This is a protocol example with illustrative phase artifacts. Its observations describe the example scenario and are not claims about a real system.

## Contributing

See [`CONTRIBUTING.md`](CONTRIBUTING.md) for contribution guidance and [`docs/DEVELOPMENT.md`](docs/DEVELOPMENT.md) for local checks and release steps.

## License

Licensed under Apache-2.0. See [`LICENSE`](LICENSE).
