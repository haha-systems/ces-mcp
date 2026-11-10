package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/haha-systems/ces-mcp/internal/epistemic"
)

func (m *Manager) Validate(
	ctx context.Context,
	runID, proposalID string,
) (ValidationResult, error) {
	var result ValidationResult

	err := m.edit(ctx, runID, func(run *RunRecord, store *epistemic.Store) error {
		proposal, ok := findSubmission(*run, proposalID)
		if !ok {
			return engineError("proposal_not_found", "proposal does not exist")
		}

		result.ProposalID = proposalID

		status := proposalStatus(*run, proposalID)
		if status == ProposalRejected || proposal.ValidationStatus == SubmissionInvalid {
			result.Valid = false

			result.Errors = append([]string(nil), proposal.ValidationErrors...)
			appendAudit(
				run,
				"proposal_validated",
				"",
				proposal.DispatchID,
				proposalID,
				"proposal is rejected",
			)

			return nil
		}

		if status == ProposalAccepted {
			result.Valid = true

			appendAudit(
				run,
				"proposal_validated",
				"",
				proposal.DispatchID,
				proposalID,
				"proposal is already accepted",
			)

			return nil
		}

		manifest, _ := findDispatch(*run, proposal.DispatchID)

		failures, _, err := validateCandidate(
			*run,
			store,
			manifest.DispatchReference,
			proposal.Result,
		)
		if err != nil && len(failures) == 0 {
			failures = append(failures, err.Error())
		}

		failures = trimmedErrors(failures)
		result.Valid = len(failures) == 0

		result.Errors = failures
		if !result.Valid {
			appendRejected(run, RejectedAttempt{
				DispatchID:     proposal.DispatchID,
				ProposalID:     proposalID,
				IdempotencyKey: proposal.IdempotencyKey,
				Code:           codeForFailures(failures, err),
				Reason: joinErrors(
					failures,
				),
				Artifact: append(json.RawMessage(nil), proposal.Result...),
			})
			appendAudit(
				run,
				"proposal_rejected",
				manifest.Phase,
				proposal.DispatchID,
				proposalID,
				joinErrors(failures),
			)

			return nil
		}

		appendAudit(
			run,
			"proposal_validated",
			manifest.Phase,
			proposal.DispatchID,
			proposalID,
			"structured artifact passed validation",
		)

		return nil
	})

	return result, err
}

func (m *Manager) Accept(
	ctx context.Context,
	runID, proposalID string,
	expectedVersion uint64,
	idempotencyKey string,
) (AcceptResult, error) {
	if strings.TrimSpace(idempotencyKey) == "" {
		return AcceptResult{}, engineError(
			"idempotency_required",
			"accept idempotency key is required",
		)
	}

	if len(idempotencyKey) > 256 {
		return AcceptResult{}, engineError(
			"invalid_idempotency_key",
			"accept idempotency key is too long",
		)
	}

	var result AcceptResult

	err := m.edit(ctx, runID, func(run *RunRecord, store *epistemic.Store) error {
		if previous, exists := run.AcceptKeys[idempotencyKey]; exists {
			if previous.ProposalID != proposalID {
				return engineError(
					"idempotency_conflict",
					"accept key was already used for another proposal",
				)
			}

			result = AcceptResult{
				ProposalID:   previous.ProposalID,
				StateVersion: previous.Version,
				StateHash:    previous.StateHash,
				Decision:     previous.Decision,
			}

			return nil
		}

		if run.StateVersion != expectedVersion {
			return engineError("stale_state", "state version changed before acceptance")
		}

		proposal, ok := findSubmission(*run, proposalID)
		if !ok {
			return engineError("proposal_not_found", "proposal does not exist")
		}

		if proposalStatus(*run, proposalID) != ProposalPending ||
			proposal.ValidationStatus == SubmissionInvalid {
			return engineError("proposal_not_pending", "proposal is not pending acceptance")
		}

		manifest, ok := findDispatch(*run, proposal.DispatchID)
		if !ok {
			return engineError("dispatch_not_found", "proposal dispatch no longer exists")
		}

		failures, phaseArtifact, validationErr := validateCandidate(
			*run,
			store,
			manifest.DispatchReference,
			proposal.Result,
		)
		if validationErr != nil && len(failures) == 0 {
			failures = append(failures, validationErr.Error())
		}

		failures = trimmedErrors(failures)
		if len(failures) > 0 || phaseArtifact == nil {
			reason := joinErrors(failures)
			appendRejected(run, RejectedAttempt{
				DispatchID:     proposal.DispatchID,
				ProposalID:     proposalID,
				IdempotencyKey: proposal.IdempotencyKey,
				Code:           codeForFailures(failures, validationErr),
				Reason:         reason,
				Artifact:       append(json.RawMessage(nil), proposal.Result...),
			})
			appendAudit(
				run,
				"proposal_rejected",
				manifest.Phase,
				proposal.DispatchID,
				proposalID,
				reason,
			)

			return engineError("invalid_submission", reason)
		}

		delta, err := phaseArtifact.Delta()
		if err != nil {
			return err
		}

		state := store.State()

		prepared, err := prepareProjection(*run, store)
		if err != nil {
			return err
		}

		applyCompletionGate(state, *phaseArtifact, &delta)

		commit, err := store.Commit(epistemic.CommitRequest{
			Phase:      manifest.Phase,
			Producer:   producerFor(manifest),
			Delta:      delta,
			Projection: prepared.CES,
		})
		if err != nil {
			return engineError("commit_rejected", err.Error())
		}

		run.StateVersion++
		decision := DecisionRecord{
			ID:                    proposalID,
			Kind:                  "artifact_acceptance",
			Phase:                 manifest.Phase,
			Decision:              "accepted",
			Reason:                "state engine validation passed",
			Summary:               phaseArtifact.PhaseSummary(),
			Boundaries:            phaseArtifact.TriageBoundaries(),
			NextInvestigation:     phaseArtifact.NextInvestigation(),
			Uncertainty:           phaseArtifact.PhaseUncertainty(),
			CompletionRecommended: phaseArtifact.CompletionRecommendation(),
			DispatchID:            manifest.DispatchID,
			ProposalID:            proposalID,
			StateVersion:          run.StateVersion,
			CreatedAt:             time.Now().UTC(),
		}
		run.Decisions = append(run.Decisions, decision)
		appendAudit(
			run,
			"proposal_accepted",
			manifest.Phase,
			manifest.DispatchID,
			proposalID,
			"accepted state committed",
		)
		run.Events = store.Events()

		newHash, err := acceptedStateHash(*run)
		if err != nil {
			return err
		}

		result = AcceptResult{
			ProposalID: proposalID, StateVersion: run.StateVersion, StateHash: newHash,
			CreatedIDs: commit.IDs, Decision: decision,
		}
		run.AcceptKeys[idempotencyKey] = AcceptedReplay{
			ProposalID: proposalID,
			Version:    run.StateVersion,
			StateHash:  newHash,
			Decision:   decision,
		}

		return nil
	})

	return result, err
}

func (m *Manager) Reject(
	ctx context.Context,
	runID, proposalID, reason string,
) (RejectedAttempt, error) {
	if strings.TrimSpace(reason) == "" {
		return RejectedAttempt{}, engineError(
			"rejection_reason_required",
			"rejection reason must not be empty",
		)
	}

	var result RejectedAttempt

	err := m.edit(ctx, runID, func(run *RunRecord, _ *epistemic.Store) error {
		proposal, ok := findSubmission(*run, proposalID)
		if !ok {
			return engineError("proposal_not_found", "proposal does not exist")
		}

		status := proposalStatus(*run, proposalID)
		if status == ProposalAccepted {
			return engineError("proposal_already_accepted", "accepted proposals cannot be rejected")
		}

		if status == ProposalRejected || proposal.ValidationStatus == SubmissionInvalid {
			for i := len(run.Rejected) - 1; i >= 0; i-- {
				if run.Rejected[i].ProposalID == proposalID {
					result = run.Rejected[i]
					return nil
				}
			}
		}

		manifest, _ := findDispatch(*run, proposal.DispatchID)
		result = RejectedAttempt{
			ID:             "",
			DispatchID:     proposal.DispatchID,
			ProposalID:     proposalID,
			IdempotencyKey: proposal.IdempotencyKey,
			Reason:         strings.TrimSpace(reason),
			Code:           "rejected_by_overseer",
			Artifact:       append(json.RawMessage(nil), proposal.Result...),
		}
		appendRejected(run, result)
		result = run.Rejected[len(run.Rejected)-1]
		appendAudit(
			run,
			"proposal_rejected",
			manifest.Phase,
			proposal.DispatchID,
			proposalID,
			result.Reason,
		)

		return nil
	})

	return result, err
}

func (m *Manager) Advance(
	ctx context.Context,
	runID string,
	expectedVersion uint64,
) (RunStatus, error) {
	var result RunStatus

	err := m.edit(ctx, runID, func(run *RunRecord, store *epistemic.Store) error {
		if run.StateVersion != expectedVersion {
			return engineError("stale_state", "state version changed before phase advance")
		}

		state := store.State()
		if state.Task.Status != epistemic.WorkActive {
			return engineError("run_terminal", "CES run is terminal")
		}

		manifest, ok := latestDispatch(*run)
		if !ok || manifest.Phase != state.Task.Phase ||
			manifest.StateVersion+1 != run.StateVersion {
			return engineError(
				"phase_not_ready",
				"current phase has no accepted proposal to advance",
			)
		}

		var accepted *SubmissionRecord

		for i := len(run.Submissions) - 1; i >= 0; i-- {
			proposal := run.Submissions[i]
			if proposal.DispatchID == manifest.DispatchID &&
				proposalStatus(*run, proposal.ProposalID) == ProposalAccepted {
				copy := proposal
				accepted = &copy

				break
			}
		}

		if accepted == nil {
			return engineError("phase_not_ready", "current phase proposal is not accepted")
		}

		decision := DecisionRecord{
			ID: accepted.ProposalID, Kind: "phase_transition", Phase: state.Task.Phase,
			DispatchID: manifest.DispatchID, ProposalID: accepted.ProposalID,
			StateVersion: run.StateVersion + 1, CreatedAt: time.Now().UTC(),
		}
		if state.Task.Phase == epistemic.PhaseClose {
			status := epistemic.WorkIncomplete

			reason := "underdetermined"
			if hasConfirmedLeadingHypothesis(state) {
				if _, err := store.SetTerminal(epistemic.WorkComplete, ""); err == nil {
					status, reason = epistemic.WorkComplete, ""
				} else {
					if _, fallbackErr := store.SetTerminal(
						epistemic.WorkIncomplete,
						"underdetermined",
					); fallbackErr != nil {
						return fmt.Errorf(
							"close run: complete and incomplete status transitions failed: %v; %w",
							err,
							fallbackErr,
						)
					}
				}
			} else if _, err := store.SetTerminal(epistemic.WorkIncomplete, reason); err != nil {
				return err
			}

			decision.Decision = string(status)
			decision.Reason = reason
		} else {
			next := nextPhase(state.Task.Phase)
			if next == "" {
				return engineError("invalid_transition", "current phase has no permitted successor")
			}

			if _, err := store.TransitionPhase(
				epistemic.TransitionRequest{
					To:     next,
					Reason: "accepted " + string(state.Task.Phase) + " artifact",
				},
			); err != nil {
				return engineError("invalid_transition", err.Error())
			}

			decision.Decision = "advanced"
			decision.Reason = "accepted phase artifact"
		}

		run.StateVersion++
		run.Decisions = append(run.Decisions, decision)
		appendAudit(
			run,
			"phase_advanced",
			state.Task.Phase,
			manifest.DispatchID,
			accepted.ProposalID,
			decision.Decision,
		)
		result = m.statusFrom(*run, store)

		return nil
	})

	return result, err
}

func nextPhase(phase epistemic.Phase) epistemic.Phase {
	switch phase {
	case epistemic.PhaseTriage:
		return epistemic.PhaseAbduce
	case epistemic.PhaseAbduce:
		return epistemic.PhaseFrame
	case epistemic.PhaseFrame:
		return epistemic.PhaseExecute
	case epistemic.PhaseExecute:
		return epistemic.PhaseClose
	default:
		return ""
	}
}

func hasConfirmedLeadingHypothesis(state epistemic.State) bool {
	for _, hypothesis := range state.Hypotheses {
		if hypothesis.ID == state.Task.LeadingHypothesis &&
			hypothesis.Status == epistemic.HypothesisConfirmed {
			return true
		}
	}

	return false
}
