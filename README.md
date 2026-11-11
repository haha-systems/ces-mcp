# CES MCP Server

**A structured, auditable reasoning process for AI agents.**

CES stands for **Cumulative Epistemic State**. It separates what an agent has observed from what it believes, proposes, does, and verifies. Instead of jumping from a request to a solution, the agent works through bounded phases and preserves evidence and uncertainty between them.

CES is designed as a **general reasoning framework**, applicable to software engineering, research, planning, analysis, and other tasks where assumptions may change as new evidence arrives. This repository implements **[CES v0.2.0](docs/SPECIFICATION.md)** as a standalone [Model Context Protocol (MCP)](https://modelcontextprotocol.io/) server.

### Why non-monotonic reasoning?

In [non-monotonic reasoning](https://en.wikipedia.org/wiki/Non-monotonic_logic), a tentative conclusion can be revised or withdrawn when new information contradicts it. For example, an agent might initially favour one explanation or approach, encounter conflicting evidence, then reject that hypothesis and reconsider its plan.

CES supports this *style* of reasoning by recording competing hypotheses, explicit evidence relations, and revisions without deleting earlier accepted records. **CES is a reasoning protocol, not itself a formally defined non-monotonic logic or proof system.** Its validators check consistency with the protocol's schemas and phase rules, not whether a conclusion is objectively true.

This structure is intended to reduce premature commitment, confirmation bias, and goal-seeking behaviour: an agent must establish the problem, consider alternatives, state what would disprove its approach, and check the outcome before recommending completion. It does not guarantee freedom from those failures.

**What it does:** maintains an append-only, evidence-linked record of observations, claims, hypotheses, constraints, actions, and outcomes, with explicit control over how the record changes.

**What it does not do:** supply its own reasoning model, prove that claims are correct, enforce isolation inside an external model, or prevent every form of goal-hacking.

## The five phases

CES has five phases. Their general purpose is independent of any particular application:

| Phase | Purpose |
| --- | --- |
| **TRIAGE** | Bound the task; establish observations, constraints, and unknowns without committing to a solution. |
| **ABDUCE** | Generate and distinguish competing hypotheses. Identify evidence and conditions that could disprove them. |
| **FRAME** | Define one bounded course of action, with completion and disconfirmation conditions. |
| **EXECUTE** | Carry out the agreed frame, record actual outcomes, and surface contradictions instead of silently changing the plan. |
| **CLOSE** | Independently check the evidence against completion conditions, seek disconfirmation, and retain unresolved uncertainty. |

For example, an agent evaluating a proposed software architecture could first establish the requirements and known failures, compare hypotheses about their causes, define a limited prototype, implement it, and test whether the evidence supports adopting the approach. The same discipline can guide other tasks that need revisable conclusions.

**Current implementation scope:** CES v0.2.0 uses these five phases, but its phase contracts and artifact fields still contain engineering and investigation-oriented terms, such as `root cause`, `leading hypothesis`, and `fix`. The general definitions above describe the broader CES approach; they are not a claim that this release automatically supports every reasoning task without adapting its contracts.

An agent can sometimes treat its first plausible answer as established fact. CES makes the transition from hypothesis to accepted decision explicit. A phase agent *proposes* structured records; the engine validates the proposal against its permitted state changes; an overseer chooses whether to accept it.

The accepted event history is **append-only**. New evidence can contradict or supersede prior conclusions while preserving how the earlier conclusion was reached.

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

### Try CES

Once the server is connected, you can ask an MCP-capable agent:

> Use CES to evaluate whether we should replace our current queue architecture. Establish the requirements and evidence, compare plausible explanations for its limitations, define a bounded test, and verify the result before recommending a change.

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

## Why projections and context isolation?

A long-running agent can carry forward an early assumption as if it were proven. It can also become attached to an intended result and reinterpret later evidence to fit that result. CES addresses these risks by limiting each phase to its specific authority and an **authorised projection** of the accepted state.

The projection contains the structured evidence and decisions needed for the phase, rather than an unrestricted conversation transcript. A fresh phase-agent context, when the host actually enforces one, reduces the influence of earlier speculative messages and premature plans. In ABDUCE, for example, the agent must consider competing explanations; in CLOSE, its job is to challenge the completion claim rather than defend the earlier work.

This supports revisable, **non-monotonic reasoning**: contradictions remain explicit, rejected or superseded hypotheses remain in history, and later phases must respond to the new evidence. It can reduce goal-seeking and goal-hacking pressures, but it cannot eliminate them or guarantee that an agent will reason correctly.

Each dispatch also records hashes of its accepted state, projection, and phase instructions. Submission checks detect artifacts referencing the wrong or stale dispatch.

**Isolation boundary:** a matching hash proves that an artifact refers to the stored dispatch; it does **not** prove that the agent saw only that projection. The host or runtime must enforce fresh contexts and restrict external tools. CES cannot inspect hidden model context, and its tool allow-list is advisory unless enforced by the host.

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

- [CES v0.2.0 specification](docs/SPECIFICATION.md): scope, state model, phase invariants, and conformance boundaries.
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
