# Cumulative Epistemic State (CES) v0.2.0

**Specification status:** Protocol reference for the `ces-mcp` v0.2.0 implementation.

This document describes CES's general reasoning model and the invariants enforced by this server. It is **not** a formal axiomatization of non-monotonic logic or a guarantee that an agent's conclusions are correct. For exact machine-readable schemas, use the MCP resource `ces://spec/v0.2.0`, the phase resources `ces://phase/{phase}`, and the executable definitions in `internal/epistemic` and `internal/cognition`. The [MCP API](MCP_API.md) documents tool inputs and outputs.

## 1. Purpose

CES is a framework for reasoning under uncertainty using an explicit, cumulative, revisable state. It distinguishes observations, evidence-backed claims, hypotheses, unknowns, constraints, plans, actions, and observed outcomes.

The framework aims to discourage premature solutions and unsupported certainty. Each phase has limited authority; proposals undergo validation and explicit acceptance. New evidence can support, contradict, or supersede previously accepted beliefs without erasing the record of how they arose.

CES may be used for investigation, research, planning, analysis, and engineering tasks. **This version's phase contracts are still written chiefly for engineering investigations**, so general-purpose use may require adapting those contracts rather than assuming that every reasoning task fits them unchanged.

## 2. Non-monotonic reasoning

In [non-monotonic logic](https://en.wikipedia.org/wiki/Non-monotonic_logic), the addition of information can invalidate a conclusion that was previously justified. CES supports *revisable reasoning behaviour*: hypotheses can be contradicted or rejected, frames can be superseded, and uncertainties can remain unresolved. It keeps prior records as evidence of the reasoning history.

This is an operational discipline, not a new formal entailment relation. CES does not prove that a statement follows from its evidence, and its validation rules are not a truth oracle.

## 3. Phase contract

The conceptual phases are:

| Phase | Responsibility | Boundary |
| --- | --- | --- |
| **TRIAGE** | Establish the task boundary, known evidence, and unknowns. | Do not choose the explanation or solution. |
| **ABDUCE** | Compare plausible hypotheses and their falsifiers. | Do not treat a leading hypothesis as confirmed. |
| **FRAME** | Specify one bounded action or test with completion and disconfirmation conditions. | Do not execute or silently expand the frame. |
| **EXECUTE** | Perform the active frame; record actions, evidence, outcomes, and contradictions. | Do not silently change the hypothesis or declare completion. |
| **CLOSE** | Attempt independent disconfirmation and evaluate completion conditions. | Do not repair failures or promote recommendations to verified fact. |

This server implements the **fixed forward progression**:

`TRIAGE → ABDUCE → FRAME → EXECUTE → CLOSE`

Each phase has structured output requirements and permissible record mutations. A proposal that violates its phase contract is rejected. A run's final status can be complete or incomplete; the model cannot declare completion by itself.

**Scope note:** The current server enforces this forward phase sequence. It does not provide a general automatic loop back to earlier phases after a contradiction. Such a loop would require additional orchestration and must not be inferred from the broader CES model.

## 4. State and evidence

The accepted state is reconstructed from typed, append-only events. It includes:

- **Observations:** recorded evidence or direct findings.
- **Claims:** assertions linked to supporting observations where required.
- **Hypotheses:** tentative explanations with falsifiers.
- **Unknowns:** explicit open questions.
- **Constraints:** boundaries on permissible work.
- **Frames:** bounded plans with completion and disconfirmation conditions.
- **Actions and outcomes:** what was done and what was observed to result.
- **Relations:** typed connections, including `supports`, `contradicts`, `resolves`, `depends_on`, `implements`, `tests`, and `supersedes`.

Record kinds, relation endpoint legality, phase permissions, and JSON schemas are defined by the implementation. An assertion that lacks valid supporting evidence cannot bypass its phase's validation rules.

Corrections, status changes, and retractions are represented as new events referring to prior records. Rejected proposals are retained in a separate operational history and do not enter the accepted event stream.

## 5. Projections and dispatches

A **projection** is the phase-authorised view derived from accepted state. The agent should receive the projection, phase instructions, and output schema rather than the unrestricted conversation transcript.

Each dispatch includes:

- Run and phase identifiers.
- Accepted state version and hash.
- Projection hash.
- Prompt/instructions hash.
- A unique session identifier.
- Optional parent run identifier.

The server verifies dispatch references and hashes when an artifact is submitted and accepted. It also checks the current phase and state version. A stale or mismatched artifact cannot be accepted as if it had been produced for the current state.

**A matching hash is not isolation attestation.** The host must enforce fresh model context and tool restrictions. The MCP server cannot inspect a model's hidden context; an adapter's isolation report is not independently verified.

Projections and fresh contexts can reduce the risk of carrying forward unsupported assumptions, and can make it harder for a phase agent to defend an earlier decision when its assigned role is to question that decision. They do not eliminate confirmation bias, goal-directed distortion, or goal-hacking.

## 6. Proposal lifecycle

A host-managed run uses this sequence:

1. `ces_create` establishes a run in TRIAGE.
2. `ces_project` retrieves the authorised state, instructions, and output schema.
3. `ces_dispatch` registers a manifest bound to that state. A phase agent receives the manifest.
4. `ces_submit` sends a structured artifact with its dispatch reference.
5. `ces_validate` checks the proposal against the latest state and rules.
6. An overseer calls `ces_accept` or `ces_reject`.
7. `ces_advance` moves to the next phase after acceptance.

Validation is repeated at acceptance with state-version checks. Accepted records cannot be replaced by an unvalidated conversational summary. The engine maintains event replay and operational audit history.

With server-managed execution, the optional runtime adapter performs the dispatch and submission, but **does not** automatically accept or advance.

## 7. Completion

CLOSE records verification evidence and any remaining uncertainty. The overseer can recommend completion, but the engine decides whether its completion gate passes. The gate requires evidence supporting the active frame's completion conditions and the leading hypothesis according to the implementation's checks.

This is a procedural evidence gate, not scientific proof or semantic verification. An incomplete run is a valid outcome.

## 8. Interoperability and limits

The normative machine-readable interface is available through:

- `ces://spec/v0.2.0`: schema for the CES protocol envelope and records.
- `ces://phase/{phase}`: phase instructions and output schemas.
- `ces://runs/{run_id}/projection`: current authorised projection.
- `ces://runs/{run_id}/history`: accepted and operational event history.
- `ces://runs/{run_id}/export`: full run export.

See [MCP API](MCP_API.md) for full tool/resource details and [Architecture](ARCHITECTURE.md) for storage, replay, hashing, isolation, and recovery behaviour.

The implementation is the authority when this descriptive document and machine-readable schema differ. Changes to the model or guarantees should be versioned explicitly.
