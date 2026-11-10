# CES MCP Server

This repository provides a standalone Model Context Protocol server for Cumulative Epistemic State (CES) v0.2.0. It reuses the CES state store, event replay, phase projections, relation rules, permissions, and phase artifact validators from the existing `internal/epistemic` and `internal/cognition` implementations.

CES keeps accepted epistemic state as typed, append-only events. Agents return structured proposals. The engine validates each proposal against the current phase projection and commits it only after an explicit accept.

## Start

Requirements: Go 1.26 or newer.

```sh
go build -o ./bin/ces-mcp ./cmd/ces-mcp
CES_DATA_DIR="$PWD/data" ./bin/ces-mcp
```

The server uses MCP stdio. Configure it as a local MCP server in the client. Example launch declarations are in [`config/`](config/); the JSON and TOML examples use common server-block formats, so adapt the field names to the client. The default data directory is `./data`; set `CES_DATA_DIR` to choose another location.

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
