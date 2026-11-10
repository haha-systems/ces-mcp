package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/haha-systems/ces-mcp/internal/cognition"
	"github.com/haha-systems/ces-mcp/internal/epistemic"
)

type DispatchRequest struct {
	RunID        string
	StateVersion uint64
	SessionID    string
	Mode         string
	AllowedTools []string
}

type DispatchResult struct {
	Manifest      DispatchManifest  `json:"manifest"`
	Proposal      *SubmissionRecord `json:"proposal,omitempty"`
	IsolationNote IsolationReport   `json:"isolation_note"`
}

type SubmitRequest struct {
	RunID          string
	IdempotencyKey string
	Dispatch       DispatchReference
	Result         json.RawMessage
}

type SubmissionResult struct {
	Proposal   SubmissionRecord `json:"proposal"`
	Validation ValidationResult `json:"validation"`
}

type preparedProjection struct {
	Output         PhaseProjection
	CES            epistemic.Projection
	Prompt         PromptBundle
	StateHash      string
	ProjectionHash string
	PromptHash     string
}

func (m *Manager) Project(ctx context.Context, runID string) (map[string]any, error) {
	run, store, err := m.loadWithStore(ctx, runID)
	if err != nil {
		return nil, err
	}

	prepared, err := prepareProjection(run, store)
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"projection":      prepared.Output,
		"projection_hash": prepared.ProjectionHash,
		"prompt_hash":     prepared.PromptHash,
		"instructions":    prepared.Prompt.Instructions,
		"output_schema":   prepared.Prompt.OutputSchema,
		"state_hash":      prepared.StateHash,
		"state_version":   run.StateVersion,
	}, nil
}

func prepareProjection(run RunRecord, store *epistemic.Store) (preparedProjection, error) {
	state := store.State()

	base, err := store.Project(epistemic.ProjectionRequest{Phase: state.Task.Phase})
	if err != nil {
		return preparedProjection{}, fmt.Errorf("project current CES state: %w", err)
	}

	stateHash, err := acceptedStateHash(run)
	if err != nil {
		return preparedProjection{}, fmt.Errorf("hash accepted state: %w", err)
	}

	contract, ok := cognition.Contract(state.Task.Phase)
	if !ok {
		return preparedProjection{}, engineError(
			"invalid_phase",
			"no phase contract exists for current phase",
		)
	}

	outputSchema := cognition.PhaseOutputSchema(state.Task.Phase, base)
	if outputSchema == nil {
		return preparedProjection{}, engineError(
			"invalid_phase",
			"no output schema exists for current phase",
		)
	}

	output := PhaseProjection{
		SchemaVersion: CESVersion,
		RunID:         run.RunID,
		StateVersion:  run.StateVersion,
		StateHash:     stateHash,
		Phase:         state.Task.Phase,
		State:         base.State,
		IDs:           append([]epistemic.ID(nil), base.IDs...),
		Evidence:      evidenceFrom(base.State.Relations),
		Decisions:     append([]DecisionRecord(nil), run.Decisions...),
	}
	prompt := PromptBundle{Instructions: contract.String(), OutputSchema: outputSchema}

	projectionHash, err := hashJSON(output)
	if err != nil {
		return preparedProjection{}, fmt.Errorf("hash authorized projection: %w", err)
	}

	promptHash, err := hashJSON(prompt)
	if err != nil {
		return preparedProjection{}, fmt.Errorf("hash phase prompt: %w", err)
	}

	return preparedProjection{
		Output: output, CES: base, Prompt: prompt, StateHash: stateHash,
		ProjectionHash: projectionHash, PromptHash: promptHash,
	}, nil
}

func evidenceFrom(relations []epistemic.Relation) []EvidenceRecord {
	result := make([]EvidenceRecord, 0, len(relations))
	for _, relation := range relations {
		result = append(result, EvidenceRecord{
			ID: relation.ID, Kind: relation.Kind, SourceID: relation.SourceID,
			TargetID: relation.TargetID, Status: relation.Status,
			Provenance: relation.ProducedBy, CreatedAt: relation.CreatedAt,
		})
	}

	return result
}

func (m *Manager) Dispatch(ctx context.Context, request DispatchRequest) (DispatchResult, error) {
	mode := strings.ReplaceAll(strings.TrimSpace(strings.ToLower(request.Mode)), "-", "_")
	if mode != "host_managed" && mode != "server_managed" {
		return DispatchResult{}, engineError(
			"invalid_mode",
			"mode must be host_managed or server_managed",
		)
	}

	if mode == "server_managed" && m.runtime == nil {
		return DispatchResult{}, engineError(
			"runtime_unavailable",
			"server-managed mode needs a configured phase runtime adapter",
		)
	}

	if mode == "host_managed" && strings.TrimSpace(request.SessionID) == "" {
		return DispatchResult{}, engineError(
			"session_required",
			"host-managed dispatch needs a host session identifier",
		)
	}

	if mode == "server_managed" && strings.TrimSpace(request.SessionID) == "" {
		id, err := randomID("ses_")
		if err != nil {
			return DispatchResult{}, err
		}

		request.SessionID = id
	}

	if strings.TrimSpace(request.SessionID) == "" || len(request.SessionID) > 512 {
		return DispatchResult{}, engineError(
			"invalid_session",
			"session identifier must contain 1 to 512 characters",
		)
	}

	allowedTools := normalizeTools(request.AllowedTools)

	var manifest DispatchManifest

	err := m.edit(ctx, request.RunID, func(run *RunRecord, store *epistemic.Store) error {
		if run.StateVersion != request.StateVersion {
			return engineError("stale_state", "state version changed before dispatch")
		}

		for _, previous := range run.Dispatches {
			if previous.SessionID == request.SessionID {
				return engineError(
					"session_reused",
					"each phase dispatch must identify a fresh agent session",
				)
			}
		}

		state := store.State()
		if state.Task.Status != epistemic.WorkActive {
			return engineError("run_terminal", "CES run is terminal")
		}

		prepared, err := prepareProjection(*run, store)
		if err != nil {
			return err
		}

		dispatchID, err := randomID("dsp_")
		if err != nil {
			return err
		}

		isolation := IsolationReport{
			Status: "not_attested",
			Detail: "CES verifies dispatch references and projection boundaries. The host or runtime must enforce fresh agent context and tool isolation.",
		}
		if mode == "server_managed" {
			isolation.Detail = "The runtime adapter receives only this manifest. Fresh model context and external tool restrictions are not verified by CES."
		}

		manifest = DispatchManifest{
			DispatchReference: DispatchReference{
				DispatchID: dispatchID, RunID: run.RunID, Phase: state.Task.Phase,
				StateVersion: run.StateVersion, StateHash: prepared.StateHash,
				ProjectionHash: prepared.ProjectionHash, PromptHash: prepared.PromptHash,
				SessionID: request.SessionID, ParentRunID: run.ParentRunID,
			},
			Mode: mode, Projection: prepared.Output, Instructions: prepared.Prompt.Instructions,
			OutputSchema: prepared.Prompt.OutputSchema, AllowedTools: allowedTools,
			ToolEnforcement: "declared_for_host_or_adapter; not_enforced_by_ces_mcp",
			Isolation:       isolation, CreatedAt: time.Now().UTC(),
		}
		run.Dispatches = append(run.Dispatches, manifest)
		appendAudit(run, "phase_dispatched", state.Task.Phase, dispatchID, "", mode)

		return nil
	})
	if err != nil {
		return DispatchResult{}, err
	}

	result := DispatchResult{Manifest: manifest, IsolationNote: manifest.Isolation}
	if mode != "server_managed" {
		return result, nil
	}

	artifact, attested, runErr := m.runtime.RunPhase(ctx, manifest)
	if runErr != nil {
		_ = m.edit(ctx, request.RunID, func(run *RunRecord, _ *epistemic.Store) error {
			appendRejected(
				run,
				RejectedAttempt{
					DispatchID: manifest.DispatchID,
					Code:       "runtime_failed",
					Reason:     runErr.Error(),
				},
			)

			return nil
		})

		return result, engineError("runtime_failed", runErr.Error())
	}

	if attested {
		result.IsolationNote = IsolationReport{
			Status: "adapter_attested",
			Detail: "The runtime adapter reported context isolation. CES does not independently verify this claim.",
		}
	}

	submission, submitErr := m.Submit(ctx, SubmitRequest{
		RunID: request.RunID, IdempotencyKey: "runtime:" + manifest.DispatchID,
		Dispatch: manifest.DispatchReference, Result: artifact,
	})
	if submitErr != nil {
		result.Proposal = &submission.Proposal
		return result, submitErr
	}

	result.Proposal = &submission.Proposal

	return result, nil
}

func normalizeTools(tools []string) []string {
	seen := map[string]struct{}{}

	result := make([]string, 0, len(tools))
	for _, tool := range tools {
		tool = strings.TrimSpace(tool)
		if tool == "" {
			continue
		}

		if _, exists := seen[tool]; exists {
			continue
		}

		seen[tool] = struct{}{}
		result = append(result, tool)
	}

	sort.Strings(result)

	return result
}

func (m *Manager) Submit(ctx context.Context, request SubmitRequest) (SubmissionResult, error) {
	if strings.TrimSpace(request.IdempotencyKey) == "" {
		return SubmissionResult{}, engineError(
			"idempotency_required",
			"submission idempotency key is required",
		)
	}

	if len(request.IdempotencyKey) > 256 {
		return SubmissionResult{}, engineError(
			"invalid_idempotency_key",
			"submission idempotency key is too long",
		)
	}

	if len(request.Result) == 0 {
		request.Result = json.RawMessage("null")
	}

	canonicalResult := append(json.RawMessage(nil), request.Result...)
	if validateUniqueJSONKeys(request.Result) == nil {
		var value any

		decoder := json.NewDecoder(strings.NewReader(string(request.Result)))
		decoder.UseNumber()

		if decoder.Decode(&value) == nil {
			if encoded, err := canonicalJSON(value); err == nil {
				canonicalResult = encoded
			}
		}
	}

	resultHash := hashBytes(canonicalResult)

	var (
		outcome     SubmissionResult
		returnedErr error
	)

	err := m.edit(ctx, request.RunID, func(run *RunRecord, store *epistemic.Store) error {
		if existingID, exists := run.SubmissionKeys[request.IdempotencyKey]; exists {
			existing, ok := findSubmission(*run, existingID)

			manifest, hasManifest := findDispatch(*run, request.Dispatch.DispatchID)
			if !ok || !hasManifest || existing.ResultHash != resultHash ||
				existing.RunID != request.RunID ||
				existing.DispatchID != request.Dispatch.DispatchID ||
				!sameReference(request.Dispatch, manifest.DispatchReference) {
				return engineError(
					"idempotency_conflict",
					"submission key was already used for different content",
				)
			}

			outcome = SubmissionResult{
				Proposal: existing,
				Validation: ValidationResult{
					ProposalID: existing.ProposalID,
					Valid:      existing.ValidationStatus == SubmissionValid,
					Errors:     existing.ValidationErrors,
				},
			}
			if existing.ValidationStatus == SubmissionInvalid {
				returnedErr = engineError(
					"invalid_submission",
					joinErrors(existing.ValidationErrors),
				)
			}

			return nil
		}

		proposalID, idErr := randomID("prp_")
		if idErr != nil {
			return idErr
		}

		proposal := SubmissionRecord{
			ProposalID:       proposalID,
			RunID:            run.RunID,
			DispatchID:       request.Dispatch.DispatchID,
			IdempotencyKey:   request.IdempotencyKey,
			Result:           append(json.RawMessage(nil), canonicalResult...),
			ResultHash:       resultHash,
			ValidationStatus: SubmissionValid,
			CreatedAt:        time.Now().UTC(),
		}

		failures, _, verifyErr := validateCandidate(*run, store, request.Dispatch, request.Result)
		if verifyErr != nil && len(failures) == 0 {
			failures = append(failures, verifyErr.Error())
		}

		failures = trimmedErrors(failures)
		if len(failures) > 0 {
			proposal.ValidationStatus = SubmissionInvalid
			proposal.ValidationErrors = failures
			returnedErr = engineError(codeForFailures(failures, verifyErr), joinErrors(failures))
			appendRejected(run, RejectedAttempt{
				DispatchID:     request.Dispatch.DispatchID,
				ProposalID:     proposalID,
				IdempotencyKey: request.IdempotencyKey,
				Code:           ErrorCode(returnedErr),
				Reason: joinErrors(
					failures,
				),
				Artifact: append(json.RawMessage(nil), request.Result...),
			})
			appendAudit(
				run,
				"proposal_rejected",
				request.Dispatch.Phase,
				request.Dispatch.DispatchID,
				proposalID,
				joinErrors(failures),
			)
		} else {
			appendAudit(
				run,
				"proposal_submitted",
				request.Dispatch.Phase,
				request.Dispatch.DispatchID,
				proposalID,
				"structured artifact passed validation",
			)
		}

		run.Submissions = append(run.Submissions, proposal)
		run.SubmissionKeys[request.IdempotencyKey] = proposalID
		outcome = SubmissionResult{
			Proposal: proposal,
			Validation: ValidationResult{
				ProposalID: proposalID,
				Valid:      len(failures) == 0,
				Errors:     failures,
			},
		}

		return nil
	})
	if err != nil {
		return outcome, err
	}

	return outcome, returnedErr
}

func validateCandidate(
	run RunRecord,
	store *epistemic.Store,
	receipt DispatchReference,
	result json.RawMessage,
) ([]string, *cognition.PhaseArtifact, error) {
	manifest, ok := findDispatch(run, receipt.DispatchID)
	if !ok {
		return nil, nil, engineError(
			"dispatch_not_found",
			"artifact references an unknown dispatch",
		)
	}

	if !sameReference(receipt, manifest.DispatchReference) {
		return nil, nil, engineError(
			"dispatch_mismatch",
			"artifact dispatch reference does not match the original manifest",
		)
	}

	state := store.State()
	if receipt.RunID != run.RunID || receipt.StateVersion != run.StateVersion ||
		receipt.StateHash != currentStateHash(run) ||
		receipt.Phase != state.Task.Phase {
		return nil, nil, engineError(
			"stale_projection",
			"artifact references a stale state version, hash, run, or phase",
		)
	}

	latest, hasLatest := latestDispatch(run)
	if !hasLatest || latest.DispatchID != receipt.DispatchID {
		return nil, nil, engineError(
			"stale_dispatch",
			"artifact does not reference the latest phase dispatch",
		)
	}

	if receipt.SessionID == "" || receipt.SessionID != manifest.SessionID ||
		receipt.ParentRunID != run.ParentRunID {
		return nil, nil, engineError(
			"dispatch_mismatch",
			"session or parent run does not match the original dispatch",
		)
	}

	prepared, err := prepareProjection(run, store)
	if err != nil {
		return nil, nil, err
	}

	if receipt.StateHash != prepared.StateHash ||
		receipt.ProjectionHash != prepared.ProjectionHash ||
		receipt.PromptHash != prepared.PromptHash ||
		manifest.ProjectionHash != prepared.ProjectionHash ||
		manifest.PromptHash != prepared.PromptHash {
		return nil, nil, engineError(
			"hash_mismatch",
			"state, projection, or prompt hash does not match the current dispatch",
		)
	}

	if err := validateUniqueJSONKeys(result); err != nil {
		return []string{err.Error()}, nil, engineError("malformed_artifact", err.Error())
	}

	var generic any

	decoder := json.NewDecoder(strings.NewReader(string(result)))
	decoder.UseNumber()

	if err := decoder.Decode(&generic); err != nil {
		return []string{
				err.Error(),
			}, nil, engineError(
				"malformed_artifact",
				"artifact result must be valid JSON",
			)
	}

	schemaFailures := validateAgainstSchema(generic, prepared.Prompt.OutputSchema, "result")
	if len(schemaFailures) != 0 {
		return schemaFailures, nil, engineError(
			"schema_validation_failed",
			joinErrors(schemaFailures),
		)
	}

	phaseArtifact, err := cognition.ParsePhaseArtifact(state.Task.Phase, result)
	if err != nil {
		return []string{err.Error()}, nil, engineError("malformed_artifact", err.Error())
	}

	if err := phaseArtifact.Validate(prepared.CES); err != nil {
		return []string{err.Error()}, nil, engineError("unauthorized_mutation", err.Error())
	}

	delta, err := phaseArtifact.Delta()
	if err != nil {
		return []string{err.Error()}, nil, engineError("malformed_artifact", err.Error())
	}

	applyCompletionGate(state, phaseArtifact, &delta)

	producer := producerFor(manifest)
	if err := store.ValidateCommit(
		epistemic.CommitRequest{
			Phase:      state.Task.Phase,
			Producer:   producer,
			Delta:      delta,
			Projection: prepared.CES,
		},
	); err != nil {
		return []string{err.Error()}, nil, engineError("unauthorized_mutation", err.Error())
	}

	return nil, &phaseArtifact, nil
}

func producerFor(manifest DispatchManifest) epistemic.Producer {
	return epistemic.Producer{
		Phase: manifest.Phase, Process: manifest.SessionID, Artifact: manifest.DispatchID,
		Runtime: manifest.Mode, Model: "external-agent",
	}
}

func applyCompletionGate(
	state epistemic.State,
	artifact cognition.PhaseArtifact,
	delta *epistemic.Delta,
) {
	if state.Task.Phase != epistemic.PhaseClose || !completionGate(state, artifact, *delta) {
		return
	}

	delta.StatusChanges = append(delta.StatusChanges, epistemic.StatusChange{
		Kind:   epistemic.ObjectHypothesis,
		Ref:    string(state.Task.LeadingHypothesis),
		Status: string(epistemic.HypothesisConfirmed),
	})
}

func completionGate(
	state epistemic.State,
	artifact cognition.PhaseArtifact,
	delta epistemic.Delta,
) bool {
	if !artifact.CompletionRecommended() || artifact.ClosureResidualUncertainty() != "" ||
		len(delta.Observations) == 0 {
		return false
	}

	if state.Task.LeadingHypothesis == "" || state.Task.ActiveFrame == "" {
		return false
	}

	for _, frame := range state.Frames {
		if frame.ID != state.Task.ActiveFrame || frame.Status != epistemic.FrameActive {
			continue
		}

		for _, condition := range frame.CompletionConditions {
			matched := false

			for _, observation := range delta.Observations {
				if strings.Contains(
					strings.ToLower(observation.Content),
					strings.ToLower(condition),
				) {
					matched = true
					break
				}
			}

			for _, outcome := range delta.Outcomes {
				if strings.Contains(
					strings.ToLower(outcome.Description),
					strings.ToLower(condition),
				) {
					matched = true
					break
				}
			}

			if !matched {
				return false
			}
		}

		return true
	}

	return false
}

func sameReference(a, b DispatchReference) bool {
	return a.DispatchID == b.DispatchID && a.RunID == b.RunID && a.Phase == b.Phase &&
		a.StateVersion == b.StateVersion && a.StateHash == b.StateHash && a.ProjectionHash == b.ProjectionHash &&
		a.PromptHash == b.PromptHash && a.SessionID == b.SessionID && a.ParentRunID == b.ParentRunID
}

func currentStateHash(run RunRecord) string {
	hash, _ := acceptedStateHash(run)
	return hash
}

func findDispatch(run RunRecord, id string) (DispatchManifest, bool) {
	for i := len(run.Dispatches) - 1; i >= 0; i-- {
		if run.Dispatches[i].DispatchID == id {
			return run.Dispatches[i], true
		}
	}

	return DispatchManifest{}, false
}

func findSubmission(run RunRecord, id string) (SubmissionRecord, bool) {
	for i := len(run.Submissions) - 1; i >= 0; i-- {
		if run.Submissions[i].ProposalID == id {
			return run.Submissions[i], true
		}
	}

	return SubmissionRecord{}, false
}

func proposalStatus(run RunRecord, proposalID string) ProposalState {
	status := ProposalPending

	for _, event := range run.Audit {
		if event.ProposalID != proposalID {
			continue
		}

		switch event.Kind {
		case "proposal_accepted":
			status = ProposalAccepted
		case "proposal_rejected", "submission_rejected":
			status = ProposalRejected
		}
	}

	return status
}

func joinErrors(failures []string) string {
	return strings.Join(trimmedErrors(failures), "; ")
}

func codeForFailures(failures []string, err error) string {
	if err != nil {
		return ErrorCode(err)
	}

	if len(failures) > 0 {
		return "invalid_artifact"
	}

	return "invalid_artifact"
}
