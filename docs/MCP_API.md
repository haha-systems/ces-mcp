# MCP tools and resources

The server speaks MCP over stdio by default, or Streamable HTTP when `CES_TRANSPORT=http`. HTTP clients connect to `/mcp` by default. Tool inputs use strict JSON Schemas with `additionalProperties: false`. Each tool advertises an output schema and returns MCP structured content shaped as:

```json
{"ok":true,"data":{}}
```

On failure:

```json
{"ok":false,"error":{"code":"stale_state","message":"state version changed before acceptance"}}
```

The server preserves this response shape for input and engine errors. See `internal/mcpserver/server.go` for the executable schemas and `internal/mcpserver/server_test.go` for protocol-level checks.

## Tools

| Tool | Required input | Effect |
| --- | --- | --- |
| `ces_create` | `goal`; optional `parent_run_id` | Creates a persistent run in TRIAGE. |
| `ces_status` | `run_id` | Returns phase, work status, state version/hash, pending work, open unknown count, and contradiction count. |
| `ces_project` | `run_id` | Builds an authorised projection from accepted state and returns instructions, output schema, and hashes. |
| `ces_dispatch` | `run_id`, `state_version`, `mode` | Stores a hash-bound manifest. `mode` is `host_managed` or `server_managed`; host mode needs `session_id`. `allowed_tools` is an informational host/adapter allow-list. |
| `ces_submit` | `run_id`, `idempotency_key`, `artifact` | Submits `{dispatch, result}`. The dispatch object must match the original manifest. The result must match the phase output schema. |
| `ces_validate` | `run_id`, `proposal_id` | Revalidates a pending proposal against the latest accepted state and phase. |
| `ces_accept` | `run_id`, `proposal_id`, `state_version`, `idempotency_key` | Validates again and commits the proposal with compare-and-swap version checking. |
| `ces_reject` | `run_id`, `proposal_id`, `reason` | Records an overseer rejection without changing accepted state. |
| `ces_advance` | `run_id`, `state_version` | Advances one phase after its accepted proposal. In CLOSE, marks the run complete or incomplete. |
| `ces_history` | `run_id`; optional `offset`, `limit` | Returns accepted events and the dispatch, submission, decision, rejection, and audit ledgers. Default limit is 100; maximum is 1000. |
| `ces_export` | `run_id` | Returns a complete run record and replayed state. |

`ces_dispatch` in server-managed mode runs the configured adapter and submits its returned artifact. It does not auto-accept or advance. The overseer still validates, accepts, and advances.

## Dispatch reference

The `artifact.dispatch` object must include the values returned by `ces_dispatch`:

```json
{
  "dispatch_id": "dsp_...",
  "run_id": "ces_...",
  "phase": "triage",
  "state_version": 0,
  "state_hash": "...",
  "projection_hash": "...",
  "prompt_hash": "...",
  "session_id": "agent-session-1",
  "parent_run_id": "ces_parent..."
}
```

`parent_run_id` is omitted when no parent exists. The exact dispatch reference and all three hashes are checked at submission and acceptance. A hash proves the referenced content; it does not prove that the phase agent saw only that content.

## Resources

| URI | Contents |
| --- | --- |
| `ces://spec/v0.2.0` | JSON Schema for the CES version, phases, record kinds, dispatch reference, artifact envelope, and projection envelope. |
| `ces://phase/triage` | TRIAGE instructions and structured output schema. |
| `ces://phase/abduce` | ABDUCE instructions and structured output schema. |
| `ces://phase/frame` | FRAME instructions and structured output schema. |
| `ces://phase/execute` | EXECUTE instructions and structured output schema. |
| `ces://phase/close` | CLOSE instructions and structured output schema. |
| `ces://runs/{run_id}/projection` | Current projection, phase instructions, output schema, and hash values. |
| `ces://runs/{run_id}/history?offset=N&limit=N` | Accepted events and append-only operational history. |
| `ces://runs/{run_id}/export` | Complete run export. |

The `ces_project` tool and projection resource both derive their content from the accepted event store. A conversational summary cannot be submitted as a replacement projection.

## Streamable HTTP deployment

Configure `CES_HTTP_ADDR` (default `127.0.0.1:8080`), `CES_HTTP_PATH` (default `/mcp`), and optional TLS certificate and key files. A non-loopback bind requires `CES_HTTP_BEARER_TOKEN`. Clients send it in the standard `Authorization: Bearer <token>` header. A TLS-terminating reverse proxy can handle public TLS; keep the application token check enabled and configure the proxy to forward the authorization header.

The checked-in remote client example shows the endpoint and header shape. Client configuration fields vary, so adapt the example to the target MCP client. The bearer token is static shared-secret authentication. The server does not implement OAuth discovery or token issuance.

## Phase artifact shape

Read the active phase resource or `ces_project.output_schema` before creating an artifact. All phases use `qac_request` to record the epistemic resource request. Local references identify records created by one artifact; the engine converts them to CES identifiers after acceptance.

- TRIAGE records observations, evidence-backed claims, unknowns, scope, classification, and the next investigation.
- ABDUCE records competing hypotheses, evidence relations, a leading hypothesis, and uncertainty. `rejected_hypotheses` can reject a referenced projected or locally declared hypothesis only when cited observation or claim evidence contradicts it. The prior hypothesis and contradiction remain in history.
- FRAME records one work frame, completion and disconfirmation conditions, constraints, and dependencies.
- EXECUTE records actions, outcomes, observations, and relations. Reported progress remains separate from verified evidence.
- CLOSE records verification observations, tests, resolved unknowns, residual uncertainty, and a completion recommendation. The engine decides final run status.
