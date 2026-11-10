# Architecture

## Responsibilities

### CES state engine

`internal/epistemic` owns the typed state model, event store, phase permissions, relation rules, deterministic replay, and projection logic. `internal/cognition` owns the structured phase artifacts, phase instructions, output schemas, and validation against the authorised projection.

`internal/engine` is the persistence and orchestration boundary. It stores one JSON run file per run and derives accepted state by replaying that run's accepted events. It does not build state from conversational history. Records include observations, claims, hypotheses, unknowns, constraints, frames, actions, outcomes, and typed relations with producer provenance and revision identifiers.

Accepted facts are append-only. Status changes and relation retractions add events that refer to prior records. Rejected proposals are retained separately and do not enter the accepted event stream. Acceptance and phase changes increment the state version. A per-run file lock serializes changes; commits write a temporary file, sync it, and atomically rename it.

The state hash covers accepted events and accepted decision records. The projection hash covers the phase-authorised projection. The prompt hash covers the phase instructions and output schema. Replay uses the accepted event stream and produces the same state for the same events.

### CES orchestrator

The orchestrator derives the active phase from accepted state. It allows only TRIAGE → ABDUCE → FRAME → EXECUTE → CLOSE. Before each dispatch, it creates a new projection from the current event store and attaches that projection, the phase instructions, output schema, and verification hashes to a dispatch manifest.

A result is checked against the stored dispatch, latest state version, latest dispatch, current phase, hashes, JSON schema, phase contract, and state store's mutation permissions. The overseer must call `ces_accept` for a validated proposal. `ces_advance` requires an accepted artifact. CLOSE can finish as complete only when the engine's evidence gate confirms the leading hypothesis and frame completion conditions; otherwise the run ends incomplete.

### MCP interface

`internal/mcpserver` exposes the engine through MCP tools and resources. The engine does not import the MCP package. The server supports stdio and Streamable HTTP transports through the official Go MCP SDK. Set `CES_TRANSPORT=stdio` or `CES_TRANSPORT=http`; stdio remains the default. HTTP serves the same MCP server and engine state at `/mcp` by default.

Every tool result includes both MCP structured content and a JSON response envelope:

```json
{"ok":true,"data":{"run_id":"ces_..."}}
```

Failures use the same envelope with `ok: false` and an `error` object containing a stable code and a message. Input arguments are checked against the advertised JSON Schema and decoded with unknown fields rejected.

## Dispatch and isolation

Every dispatch records:

- run identifier and phase;
- state version and state hash;
- projection hash and prompt hash;
- unique session identifier;
- parent run identifier when present.

Submissions must include the exact original dispatch reference. Reused session identifiers, stale state, unknown or superseded dispatches, mismatched hashes, duplicate JSON keys, invalid records, and unauthorized phase mutations are rejected.

CES can verify that an artifact refers to a stored dispatch and projection. It cannot verify the actual prompt, memory, hidden context, model session, or external tools available to an agent. `allowed_tools` is declared in the manifest for the host or adapter; CES does not enforce it. Isolation status is therefore `not_attested` unless an adapter reports otherwise, and an adapter's report is not independently verified.

For Streamable HTTP, the server defaults to `127.0.0.1:8080/mcp`. Binding to a non-loopback interface requires a bearer token. The server supports direct TLS certificate and key files or TLS termination at a trusted proxy. It applies Go's cross-origin request protection and limits idle MCP session lifetime to 30 minutes. MCP protocol sessions live in process memory and clients must reconnect after a server restart; CES runs remain in the persistent data directory. The static bearer token is a shared secret, not an OAuth authorization server.

Host-managed mode lets an MCP client create fresh phase agents and submit their results. CES requires a distinct session identifier for each dispatch but cannot prove that those contexts are fresh.

Server-managed mode uses an optional `PhaseRuntime` adapter. The included command adapter launches one process per dispatch, sends only the manifest on stdin, and accepts one JSON artifact on stdout. It does not inherit environment variables unless named in `CES_RUNTIME_ENV_ALLOWLIST`. This process boundary is not a sandbox; host OS policy still controls process permissions. The command adapter reports no isolation attestation. Both modes use the same submission, validation, acceptance, and transition logic.

## Persistence and recovery

Set `CES_DATA_DIR` to a directory writable by the server. Each run is saved independently. File locking prevents two server processes from committing against the same run at once. Accept and submit calls support idempotency keys. The engine uses compare-and-swap state versions so a proposal prepared against an earlier version cannot commit after another update.

After restart, the server reloads the run file and replays accepted events. Exports include the current replayed state, accepted event history, dispatches, proposals, decisions, rejections, and audit events. The JSON run file is the persistent source; temporary files are replaced atomically during a commit.

## Reuse and versioning

The source for this server is based on the existing CES v0.2.0 state and cognition packages in Ghost. Phase definitions, projection rules, relation validation, and mutation checks are reused in this repository. `rejected_hypotheses` is an additive artifact field that records a contradiction-backed rejection through append-only status and relation events.

The MCP resources expose the protocol envelope schema at `ces://spec/v0.2.0` and phase instructions and schemas at `ces://phase/{phase}`. The run resource template exposes projection, history, and export views.
