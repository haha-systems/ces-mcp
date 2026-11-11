# CES MCP Server

**A structured, auditable way to investigate problems with AI agents.**

CES stands for **Cumulative Epistemic State**. It is a way to organise an investigation so that observations, hypotheses, decisions, and results remain distinct and traceable.

This repository implements CES v0.2.0 as a standalone [Model Context Protocol (MCP)](https://modelcontextprotocol.io/) server. An MCP-compatible client can use it to guide an investigation through five phases, retain accepted evidence across phases, and check proposed updates before committing them.

CES is useful for tasks such as debugging a difficult production incident, investigating an unexpected test result, or assessing competing technical explanations.

**What it does:** manages a structured record of what an investigation knows, what remains uncertain, and why decisions were made.

**What it does not do:** supply its own reasoning model, automatically prove that conclusions are correct, or guarantee that agents run in isolated contexts.

## The idea, in one example

Suppose an AI assistant is investigating why a background job sometimes fails.

| Phase | Question | Example output |
| --- | --- | --- |
| **TRIAGE** | What do we know? | Failures began after a deployment. The logs show timeouts, but the cause is unknown. |
| **ABDUCE** | What could explain it? | Database contention and network latency are competing hypotheses. |
| **FRAME** | What would test them? | Compare query timings and network traces against explicit success and failure criteria. |
| **EXECUTE** | What happened when we tested? | Queries slowed under load; network latency remained stable. |
| **CLOSE** | What can we conclude? | The evidence supports database contention. Record any remaining uncertainty. |

Without a structured record, an assistant can gradually treat an early guess as an established fact. CES makes those transitions explicit. A phase agent *proposes* structured records; the engine checks the proposal, and an overseer decides whether to accept it.

The accepted state is **append-only**. New evidence can revise or contradict an earlier claim without erasing its history.

## Quick start

**Requirements:** Go 1.26 or newer, and an MCP client that can launch a local stdio server.

Clone and build:

~~~sh
git clone https://github.com/haha-systems/ces-mcp.git
cd ces-mcp
go build -o ./bin/ces-mcp ./cmd/ces-mcp
~~~

Configure your MCP client to launch the binary. For example, a client using the `mcpServers` JSON format might use:

~~~json
{
  "mcpServers": {
    "ces": {
      "command": "/absolute/path/to/ces-mcp/bin/ces-mcp",
      "args": [],
      "env": {
        "CES_DATA_DIR": "/absolute/path/to/ces-mcp/data"
      }
    }
  }
}
~~~

Replace the paths with paths on your machine. Client configuration formats differ; use the format your client expects. Ready-to-adapt examples are available in [`config/stdio-client.json`](config/stdio-client.json) and [`config/stdio-client.toml`](config/stdio-client.toml).

The default transport is **MCP over stdio**. The client starts the process and communicates with it through standard input and output. Running the binary directly in a terminal will not show an interactive application.

The default data directory is `./data`. Set `CES_DATA_DIR` to a stable, writable directory if you want run records to survive restarts independently of the current working directory.

### Try an investigation

Once the server is connected, you can ask an MCP-capable agent:

> Use CES to investigate why our background jobs intermittently time out. Start a run, keep observations separate from hypotheses, and do not close it as complete without supporting evidence.

The client must actually orchestrate the phases and submit the structured artifacts. **Connecting the MCP server alone does not automatically make an agent follow the CES workflow.**

## How a run works

CES enforces this phase order:

`TRIAGE → ABDUCE → FRAME → EXECUTE → CLOSE`

A typical **host-managed** run follows this sequence for each phase:

1. Call `ces_create` to begin a run in TRIAGE.
2. Call `ces_project` to get the current phase's authorised state, instructions, output schema, and hashes.
3. Call `ces_dispatch` to register a dispatch. The host runs a phase agent and gives it the projection.
4. Submit the agent's structured artifact with `ces_submit`.
5. Call `ces_validate` to check the proposal, then `ces_accept` to commit it, or `ces_reject` to record why it was rejected.
6. Call `ces_advance` to move to the next phase. Repeat until CLOSE.

The server validates state versions, dispatch references, schemas, allowed record changes, and phase transitions. It does not accept a conversational summary in place of the required structured artifact.

The overseer remains responsible for deciding whether an artifact should be accepted. **Validation means the proposal satisfies the protocol's rules; it is not independent verification of its factual claims.**

CLOSE can finish as **complete** only when the engine's evidence checks support that outcome. Otherwise, the run is recorded as incomplete.

For exact tool arguments and response formats, see [MCP API](docs/MCP_API.md).

## Why projections and hashes?

An agent should not need the entire conversation to continue an investigation. CES provides a **phase projection** containing the accepted state relevant to the current phase.

Every dispatch records identifiers and hashes for its state, projection, and instructions. On submission, the server checks that the returned artifact matches the registered dispatch and current state. This helps detect stale or mismatched work.

There is an important boundary: **a hash does not prove that an agent saw only the authorised projection.** The MCP host or agent runtime must enforce context and tool isolation. CES records the dispatch and checks its references; it cannot inspect an agent's hidden context.

## Running locally or remotely

| Mode | When to use it |
| --- | --- |
| **stdio (default)** | A local MCP client launches the server process. Easiest way to get started. |
| **Streamable HTTP** | An MCP client connects to a running server over HTTP. Useful for remote deployments. |

To run a local Streamable HTTP endpoint:

~~~sh
CES_TRANSPORT=http CES_HTTP_ADDR=127.0.0.1:8080 ./bin/ces-mcp
~~~

The default endpoint is `http://127.0.0.1:8080/mcp`. Use `CES_HTTP_PATH` to change the route.

For non-loopback listening addresses, the server requires `CES_HTTP_BEARER_TOKEN`. For remote access, use HTTPS through the server's TLS certificate/key settings or a trusted TLS-terminating reverse proxy. The proxy must forward the `Authorization` header. The bearer token is a shared secret; the server does not provide OAuth login or token issuance.

See [`config/remote-http-client.json`](config/remote-http-client.json) and [Development](docs/DEVELOPMENT.md) for configuration details.

## Host-managed and server-managed execution

In **host-managed mode**, your MCP client starts the phase agents, controls their contexts and tools, and submits their work back to CES.

In **server-managed mode**, CES can launch a configured command adapter for a phase. The included adapter starts a process per dispatch and expects a JSON artifact in return. It does not automatically accept or advance the run. This process boundary is not an operating-system sandbox.

Both modes use the same proposal validation, acceptance, and state-transition rules.

## Inspecting and exporting runs

The server also exposes tools for reviewing the investigation:

- `ces_status`: current phase and state information.
- `ces_history`: accepted events and operational history, including submissions and decisions.
- `ces_export`: complete run record and replayed state.

CES stores each run as a JSON file under `CES_DATA_DIR`. Treat this directory as private if investigations contain sensitive information. The accepted state is reconstructed by replaying the stored events.

A complete **illustrative** five-phase run is provided in [`examples/complete-run.json`](examples/complete-run.json). You can regenerate it with:

~~~sh
go run ./cmd/ces-demo > examples/complete-run.json
~~~

The example demonstrates the protocol. Its scenario is illustrative, not a verified investigation of a real system.

## Documentation

- [MCP API](docs/MCP_API.md): tools, schemas, resources, and dispatch artifacts.
- [Architecture](docs/ARCHITECTURE.md): state model, persistence, validation, and isolation boundaries.
- [Development](docs/DEVELOPMENT.md): build, tests, command adapter, and deployment settings.
- [Contributing](CONTRIBUTING.md): contribution guidelines.

To run the test suite:

~~~sh
go test ./... -count=1
~~~

## License

Apache-2.0. See [LICENSE](LICENSE).
