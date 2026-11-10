package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/haha-systems/ces-mcp/internal/engine"
	"github.com/haha-systems/ces-mcp/internal/epistemic"
)

func main() {
	ctx := context.Background()

	dir, err := os.MkdirTemp("", "ces-demo-")
	if err != nil {
		fail(err)
	}
	defer os.RemoveAll(dir)

	manager, err := engine.NewManager(dir, nil)
	if err != nil {
		fail(err)
	}

	run, err := manager.Create(ctx, "verify the reported regression", "")
	if err != nil {
		fail(err)
	}

	for phaseNumber := 1; phaseNumber <= 5; phaseNumber++ {
		projectionValue, err := manager.Project(ctx, run.RunID)
		if err != nil {
			fail(err)
		}

		projection := projectionValue["projection"].(engine.PhaseProjection)

		dispatch, err := manager.Dispatch(ctx, engine.DispatchRequest{
			RunID: run.RunID, StateVersion: run.StateVersion,
			Mode: "host_managed", SessionID: fmt.Sprintf("demo-fresh-context-%d", phaseNumber),
			AllowedTools: []string{"read_file", "run_check", "write_file"},
		})
		if err != nil {
			fail(err)
		}

		artifact, err := json.Marshal(artifactFor(projection.Phase, projection.State))
		if err != nil {
			fail(err)
		}

		submission, err := manager.Submit(ctx, engine.SubmitRequest{
			RunID: run.RunID, IdempotencyKey: "demo-submit-" + string(projection.Phase),
			Dispatch: dispatch.Manifest.DispatchReference, Result: artifact,
		})
		if err != nil {
			fail(err)
		}

		validation, err := manager.Validate(ctx, run.RunID, submission.Proposal.ProposalID)
		if err != nil || !validation.Valid {
			fail(fmt.Errorf("validate %s: %v %v", projection.Phase, validation.Errors, err))
		}

		accepted, err := manager.Accept(
			ctx,
			run.RunID,
			submission.Proposal.ProposalID,
			run.StateVersion,
			"demo-accept-"+string(projection.Phase),
		)
		if err != nil {
			fail(err)
		}

		run, err = manager.Advance(ctx, run.RunID, accepted.StateVersion)
		if err != nil {
			fail(err)
		}
	}

	exported, err := manager.Export(ctx, run.RunID)
	if err != nil {
		fail(err)
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")

	if err := encoder.Encode(exported); err != nil {
		fail(err)
	}
}

func artifactFor(phase epistemic.Phase, state epistemic.State) map[string]any {
	qac := map[string]any{
		"direction": "release", "uncertainty": 0.3, "novelty": 0.2,
		"expected_gain": 0.4, "failed_attempts": 0, "reason": "",
		"unresolved": "", "evidence": []string{}, "attempted": []string{}, "recommended_focus": "",
	}

	switch phase {
	case epistemic.PhaseTriage:
		return map[string]any{
			"classification": "reported regression",
			"boundaries":     []string{"one service"},
			"observations": []any{
				map[string]any{"local_ref": "o1", "content": "the command reports the regression"},
			},
			"claims": []any{
				map[string]any{
					"local_ref":    "c1",
					"text":         "the regression is reproducible",
					"evidence_ref": "o1",
				},
			},
			"unknowns": []any{
				map[string]any{
					"local_ref": "u1",
					"question":  "does the fix pass the full check suite?",
				},
			},
			"resolved_unknowns":  []any{},
			"next_investigation": "compare the two execution paths",
			"qac_request":        qac,
		}
	case epistemic.PhaseAbduce:
		observationID := string(state.Observations[0].ID)

		return map[string]any{
			"hypotheses": []any{
				map[string]any{
					"local_ref": "h1",
					"mechanism": "the shared path mishandles the result",
					"falsifier": "the shared path emits the expected result",
				},
				map[string]any{
					"local_ref": "h2",
					"mechanism": "the provider returned incomplete data",
					"falsifier": "the provider payload is complete",
				},
			},
			"rejected_hypotheses": []any{
				map[string]any{"hypothesis_ref": "h2", "evidence_ref": observationID},
			},
			"relations": []any{
				map[string]any{
					"local_ref": "r1",
					"kind":      "supports",
					"source":    observationID,
					"target":    "h1",
				},
				map[string]any{
					"local_ref": "r2",
					"kind":      "contradicts",
					"source":    observationID,
					"target":    "h2",
				},
			},
			"leading_hypothesis_ref": "h1",
			"remaining_uncertainty":  "the provider path is not fully isolated",
			"qac_request":            qac,
		}
	case epistemic.PhaseFrame:
		return map[string]any{
			"frame": map[string]any{
				"local_ref": "f1",
				"name":      "fix shared result handling",
				"summary":   "change the shared path and check its consumers",
				"completion_conditions": []string{
					"all checks pass",
				},
				"disconfirmation_conditions": []string{"the regression still reproduces"},
			},
			"constraints": []any{
				map[string]any{"local_ref": "k1", "text": "preserve the external result format"},
			},
			"relations": []any{
				map[string]any{
					"local_ref": "r1",
					"kind":      "depends_on",
					"source":    "f1",
					"target":    string(state.Task.LeadingHypothesis),
				},
			},
			"supersedes_frame_ref": "",
			"qac_request":          qac,
		}
	case epistemic.PhaseExecute:
		frameID := string(state.Task.ActiveFrame)

		return map[string]any{
			"observations": []any{
				map[string]any{"local_ref": "o2", "content": "all checks pass after the change"},
			},
			"actions": []any{
				map[string]any{
					"local_ref":   "a1",
					"description": "updated the shared result handling",
				},
			},
			"outcomes": []any{
				map[string]any{
					"local_ref":   "x1",
					"description": "all checks pass after the change",
				},
			},
			"relations": []any{
				map[string]any{
					"local_ref": "r1",
					"kind":      "implements",
					"source":    "a1",
					"target":    frameID,
				},
				map[string]any{
					"local_ref": "r2",
					"kind":      "tests",
					"source":    "x1",
					"target":    frameID,
				},
			},
			"qac_request": qac,
		}
	case epistemic.PhaseClose:
		frameID := string(state.Task.ActiveFrame)
		unknownID := ""

		for _, unknown := range state.Unknowns {
			if unknown.Status == epistemic.UnknownOpen ||
				unknown.Status == epistemic.UnknownReopened {
				unknownID = string(unknown.ID)
				break
			}
		}

		return map[string]any{
			"observations": []any{
				map[string]any{"local_ref": "o3", "content": "all checks pass"},
			},
			"relations": []any{
				map[string]any{
					"local_ref": "r1",
					"kind":      "tests",
					"source":    "o3",
					"target":    frameID,
				},
			},
			"resolved_unknowns": []any{
				map[string]any{
					"local_ref": "r2",
					"kind":      "resolves",
					"source":    "o3",
					"target":    unknownID,
				},
			},
			"residual_uncertainty":   "",
			"completion_recommended": true,
			"qac_request":            qac,
		}
	default:
		panic("unknown CES phase: " + string(phase))
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "ces-demo:", err)
	os.Exit(1)
}
