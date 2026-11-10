package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/haha-systems/ces-mcp/internal/epistemic"
)

func TestCompleteFivePhaseRunAndFreshProjections(t *testing.T) {
	ctx := context.Background()
	manager := testManager(t, nil)

	run, err := manager.Create(ctx, "verify the reported regression", "")
	if err != nil {
		t.Fatal(err)
	}

	phases := []epistemic.Phase{
		epistemic.PhaseTriage, epistemic.PhaseAbduce, epistemic.PhaseFrame,
		epistemic.PhaseExecute, epistemic.PhaseClose,
	}
	for i, phase := range phases {
		if run.Phase != phase {
			t.Fatalf("phase %d = %s, want %s", i, run.Phase, phase)
		}

		projected, err := manager.Project(ctx, run.RunID)
		if err != nil {
			t.Fatal(err)
		}

		projection := projected["projection"].(PhaseProjection)
		if projection.Phase != phase || projection.StateVersion != run.StateVersion {
			t.Fatalf(
				"projection phase/version = %s/%d, want %s/%d",
				projection.Phase,
				projection.StateVersion,
				phase,
				run.StateVersion,
			)
		}

		assertPhaseProjection(t, phase, projection)

		dispatch, err := manager.Dispatch(ctx, DispatchRequest{
			RunID:        run.RunID,
			StateVersion: run.StateVersion,
			Mode:         "host_managed",
			SessionID: fmt.Sprintf(
				"host-session-%d",
				i+1,
			),
			AllowedTools: []string{"read_file", "read_file"},
		})
		if err != nil {
			t.Fatal(err)
		}

		if dispatch.Manifest.DispatchID == "" || dispatch.Manifest.StateHash == "" ||
			dispatch.Manifest.ProjectionHash == "" ||
			dispatch.Manifest.PromptHash == "" {
			t.Fatalf(
				"dispatch is missing verification fields: %+v",
				dispatch.Manifest.DispatchReference,
			)
		}

		if dispatch.Manifest.Isolation.Status != "not_attested" {
			t.Fatalf("host isolation status = %q", dispatch.Manifest.Isolation.Status)
		}

		submission, err := manager.Submit(ctx, SubmitRequest{
			RunID:          run.RunID,
			IdempotencyKey: "submit-" + string(phase),
			Dispatch:       dispatch.Manifest.DispatchReference,
			Result:         phaseArtifact(phase, dispatch.Manifest.Projection.State),
		})
		if err != nil {
			t.Fatalf("submit %s: %v (validation=%+v)", phase, err, submission.Validation)
		}

		validated, err := manager.Validate(ctx, run.RunID, submission.Proposal.ProposalID)
		if err != nil || !validated.Valid {
			t.Fatalf("validate %s: result=%+v err=%v", phase, validated, err)
		}

		accepted, err := manager.Accept(
			ctx,
			run.RunID,
			submission.Proposal.ProposalID,
			run.StateVersion,
			"accept-"+string(phase),
		)
		if err != nil {
			t.Fatalf("accept %s: %v", phase, err)
		}

		if accepted.StateVersion != run.StateVersion+1 || accepted.StateHash == "" {
			t.Fatalf("accept result has wrong version/hash: %+v", accepted)
		}
		// Repeating an acceptance with the same key is idempotent.
		again, err := manager.Accept(
			ctx,
			run.RunID,
			submission.Proposal.ProposalID,
			run.StateVersion,
			"accept-"+string(phase),
		)
		if err != nil || again.StateHash != accepted.StateHash ||
			again.StateVersion != accepted.StateVersion {
			t.Fatalf("idempotent accept differs: first=%+v second=%+v err=%v", accepted, again, err)
		}

		run, err = manager.Advance(ctx, run.RunID, accepted.StateVersion)
		if err != nil {
			t.Fatalf("advance after %s: %v", phase, err)
		}

		if i < len(phases)-1 && run.Phase != phases[i+1] {
			t.Fatalf("advance from %s produced %s", phase, run.Phase)
		}
	}

	if run.WorkStatus != epistemic.WorkComplete {
		t.Fatalf("work status = %s, want complete", run.WorkStatus)
	}

	if run.StateVersion != 10 {
		t.Fatalf("state version = %d, want 10", run.StateVersion)
	}

	if run.OpenUnknowns != 0 {
		t.Fatalf("open unknown count = %d, want 0", run.OpenUnknowns)
	}

	exported, err := manager.Export(ctx, run.RunID)
	if err != nil {
		t.Fatal(err)
	}

	if len(exported.State.Hypotheses) != 2 || len(exported.State.Relations) < 6 {
		t.Fatalf(
			"expected accepted hypotheses and evidence relations, got %d hypotheses and %d relations",
			len(exported.State.Hypotheses),
			len(exported.State.Relations),
		)
	}

	if exported.State.Hypotheses[1].Status != epistemic.HypothesisRejected {
		t.Fatalf(
			"contradicted hypothesis status = %s, want rejected",
			exported.State.Hypotheses[1].Status,
		)
	}

	var abductionDecision, closureDecision *DecisionRecord

	for i := range exported.Run.Decisions {
		decision := &exported.Run.Decisions[i]
		if decision.Kind != "artifact_acceptance" {
			continue
		}

		if decision.Phase == epistemic.PhaseAbduce {
			abductionDecision = decision
		}

		if decision.Phase == epistemic.PhaseClose {
			closureDecision = decision
		}
	}

	if abductionDecision == nil || abductionDecision.Uncertainty == "" || closureDecision == nil ||
		closureDecision.CompletionRecommended == nil ||
		!*closureDecision.CompletionRecommended {
		t.Fatalf(
			"accepted uncertainty or closure recommendation was not preserved: %+v",
			exported.Run.Decisions,
		)
	}

	var hasSupport, hasContradiction bool
	for _, relation := range exported.State.Relations {
		hasSupport = hasSupport || relation.Kind == epistemic.RelationSupports
		hasContradiction = hasContradiction || relation.Kind == epistemic.RelationContradicts
	}

	if !hasSupport || !hasContradiction {
		t.Fatalf("contradictory evidence was not preserved: %+v", exported.State.Relations)
	}
}

func TestRejectsStaleProjectionAndMismatchedDispatchHashes(t *testing.T) {
	ctx := context.Background()
	manager := testManager(t, nil)

	run, err := manager.Create(ctx, "check stale receipts", "")
	if err != nil {
		t.Fatal(err)
	}

	dispatch, err := manager.Dispatch(
		ctx,
		DispatchRequest{
			RunID:        run.RunID,
			StateVersion: 0,
			Mode:         "host_managed",
			SessionID:    "host-a",
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	badReceipt := dispatch.Manifest.DispatchReference
	badReceipt.PromptHash = "wrong-prompt-hash"

	_, err = manager.Submit(
		ctx,
		SubmitRequest{
			RunID:          run.RunID,
			IdempotencyKey: "bad-hash",
			Dispatch:       badReceipt,
			Result:         phaseArtifact(epistemic.PhaseTriage),
		},
	)
	if err == nil || ErrorCode(err) != "dispatch_mismatch" {
		t.Fatalf("mismatched dispatch error = %v (%s)", err, ErrorCode(err))
	}

	stored, err := manager.Export(ctx, run.RunID)
	if err != nil {
		t.Fatal(err)
	}

	if len(stored.Run.Rejected) != 1 || stored.Run.StateVersion != 0 {
		t.Fatalf("mismatch was not preserved as a rejected attempt: %+v", stored.Run.Rejected)
	}

	valid, err := manager.Dispatch(
		ctx,
		DispatchRequest{
			RunID:        run.RunID,
			StateVersion: 0,
			Mode:         "host_managed",
			SessionID:    "host-b",
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	proposal, err := manager.Submit(
		ctx,
		SubmitRequest{
			RunID:          run.RunID,
			IdempotencyKey: "triage-valid",
			Dispatch:       valid.Manifest.DispatchReference,
			Result:         phaseArtifact(epistemic.PhaseTriage),
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := manager.Accept(
		ctx,
		run.RunID,
		proposal.Proposal.ProposalID,
		0,
		"accept-triage",
	); err != nil {
		t.Fatal(err)
	}

	_, err = manager.Submit(
		ctx,
		SubmitRequest{
			RunID:          run.RunID,
			IdempotencyKey: "stale-submit",
			Dispatch:       valid.Manifest.DispatchReference,
			Result:         phaseArtifact(epistemic.PhaseTriage),
		},
	)
	if err == nil || ErrorCode(err) != "stale_projection" {
		t.Fatalf("stale state error = %v (%s)", err, ErrorCode(err))
	}

	stored, err = manager.Export(ctx, run.RunID)
	if err != nil {
		t.Fatal(err)
	}

	if len(stored.Run.Rejected) != 2 || stored.Run.StateVersion != 1 {
		t.Fatalf("stale submit was not rejected without changing accepted state")
	}
}

func TestDispatchRequiresUniqueSessionIDAndSubmissionIsCanonicallyIdempotent(t *testing.T) {
	ctx := context.Background()
	manager := testManager(t, nil)

	run, err := manager.Create(ctx, "keep phase sessions distinct", "")
	if err != nil {
		t.Fatal(err)
	}

	dispatch, err := manager.Dispatch(
		ctx,
		DispatchRequest{
			RunID:        run.RunID,
			StateVersion: 0,
			Mode:         "host_managed",
			SessionID:    "host-session-1",
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = manager.Dispatch(
		ctx,
		DispatchRequest{
			RunID:        run.RunID,
			StateVersion: 0,
			Mode:         "host_managed",
			SessionID:    "host-session-1",
		},
	)
	if err == nil || ErrorCode(err) != "session_reused" {
		t.Fatalf("reused session error = %v (%s)", err, ErrorCode(err))
	}

	result := phaseArtifact(epistemic.PhaseTriage)

	var value any
	if err := json.Unmarshal(result, &value); err != nil {
		t.Fatal(err)
	}

	reordered, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}

	first, err := manager.Submit(
		ctx,
		SubmitRequest{
			RunID:          run.RunID,
			IdempotencyKey: "same-submission",
			Dispatch:       dispatch.Manifest.DispatchReference,
			Result:         result,
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	forgedReceipt := dispatch.Manifest.DispatchReference
	forgedReceipt.ProjectionHash = "different-projection"

	_, err = manager.Submit(
		ctx,
		SubmitRequest{
			RunID:          run.RunID,
			IdempotencyKey: "same-submission",
			Dispatch:       forgedReceipt,
			Result:         result,
		},
	)
	if err == nil || ErrorCode(err) != "idempotency_conflict" {
		t.Fatalf(
			"idempotency retry accepted a changed dispatch reference: %v (%s)",
			err,
			ErrorCode(err),
		)
	}

	second, err := manager.Submit(
		ctx,
		SubmitRequest{
			RunID:          run.RunID,
			IdempotencyKey: "same-submission",
			Dispatch:       dispatch.Manifest.DispatchReference,
			Result:         reordered,
		},
	)
	if err != nil || second.Proposal.ProposalID != first.Proposal.ProposalID {
		t.Fatalf(
			"equivalent retry was not idempotent: first=%+v second=%+v err=%v",
			first,
			second,
			err,
		)
	}
}

func TestRejectsPhaseUnauthorizedMutationAndKeepsAttempt(t *testing.T) {
	ctx := context.Background()
	manager := testManager(t, nil)

	run, err := manager.Create(ctx, "protect phase authority", "")
	if err != nil {
		t.Fatal(err)
	}

	dispatch, err := manager.Dispatch(
		ctx,
		DispatchRequest{
			RunID:        run.RunID,
			StateVersion: 0,
			Mode:         "host_managed",
			SessionID:    "host-a",
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	result := []byte(
		`{"classification":"bug","boundaries":[],"observations":[{"local_ref":"o1","content":"reported issue"}],"claims":[],"unknowns":[],"resolved_unknowns":[],"next_investigation":"inspect logs","hypotheses":[{"local_ref":"h1","mechanism":"unauthorized diagnosis","falsifier":""}],"qac_request":{"direction":"release","uncertainty":0.3,"novelty":0.2,"expected_gain":0.4,"failed_attempts":0,"reason":"","unresolved":"","evidence":[],"attempted":[],"recommended_focus":""}}`,
	)

	submission, err := manager.Submit(ctx, SubmitRequest{
		RunID:          run.RunID,
		IdempotencyKey: "phase-escape",
		Dispatch:       dispatch.Manifest.DispatchReference,
		Result:         result,
	})
	if err == nil || ErrorCode(err) != "schema_validation_failed" {
		t.Fatalf("unauthorized field error = %v (%s)", err, ErrorCode(err))
	}

	if submission.Validation.Valid || submission.Proposal.ValidationStatus != SubmissionInvalid {
		t.Fatalf("unauthorized proposal not rejected: %+v", submission)
	}

	exported, err := manager.Export(ctx, run.RunID)
	if err != nil {
		t.Fatal(err)
	}

	if len(exported.State.Hypotheses) != 0 || len(exported.Run.Rejected) != 1 ||
		exported.Run.StateVersion != 0 {
		t.Fatalf(
			"unauthorized mutation reached accepted state: state=%+v rejections=%d",
			exported.State,
			len(exported.Run.Rejected),
		)
	}
}

func TestRecoveryAndDeterministicReplay(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	manager, err := NewManager(dir, nil)
	if err != nil {
		t.Fatal(err)
	}

	run, err := manager.Create(ctx, "recover after restart", "")
	if err != nil {
		t.Fatal(err)
	}

	dispatch, err := manager.Dispatch(
		ctx,
		DispatchRequest{
			RunID:        run.RunID,
			StateVersion: 0,
			Mode:         "host_managed",
			SessionID:    "host-recovery",
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	proposal, err := manager.Submit(
		ctx,
		SubmitRequest{
			RunID:          run.RunID,
			IdempotencyKey: "recovery-submit",
			Dispatch:       dispatch.Manifest.DispatchReference,
			Result:         phaseArtifact(epistemic.PhaseTriage),
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := manager.Accept(
		ctx,
		run.RunID,
		proposal.Proposal.ProposalID,
		0,
		"recovery-accept",
	); err != nil {
		t.Fatal(err)
	}

	restarted, err := NewManager(dir, nil)
	if err != nil {
		t.Fatal(err)
	}

	status, err := restarted.Status(ctx, run.RunID)
	if err != nil {
		t.Fatal(err)
	}

	if status.StateVersion != 1 || status.Phase != epistemic.PhaseTriage ||
		status.PendingWork != "advance to the next phase" {
		t.Fatalf("recovered status = %+v", status)
	}

	exported, err := restarted.Export(ctx, run.RunID)
	if err != nil {
		t.Fatal(err)
	}

	first, err := epistemic.Replay(exported.Run.Events)
	if err != nil {
		t.Fatal(err)
	}

	second, err := epistemic.Replay(exported.Run.Events)
	if err != nil {
		t.Fatal(err)
	}

	firstBytes, _ := json.Marshal(first.State())

	secondBytes, _ := json.Marshal(second.State())
	if !reflect.DeepEqual(firstBytes, secondBytes) ||
		currentStateHash(exported.Run) != exported.StateHash {
		t.Fatal("event replay was not deterministic")
	}
}

func TestConcurrentAcceptsUseCompareAndSwapVersion(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	firstManager, err := NewManager(dir, nil)
	if err != nil {
		t.Fatal(err)
	}

	secondManager, err := NewManager(dir, nil)
	if err != nil {
		t.Fatal(err)
	}

	run, err := firstManager.Create(ctx, "serialize concurrent commits", "")
	if err != nil {
		t.Fatal(err)
	}

	dispatch, err := firstManager.Dispatch(
		ctx,
		DispatchRequest{
			RunID:        run.RunID,
			StateVersion: 0,
			Mode:         "host_managed",
			SessionID:    "host-concurrency",
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	firstProposal, err := firstManager.Submit(
		ctx,
		SubmitRequest{
			RunID:          run.RunID,
			IdempotencyKey: "concurrent-one",
			Dispatch:       dispatch.Manifest.DispatchReference,
			Result:         phaseArtifact(epistemic.PhaseTriage),
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	secondResult := json.RawMessage(
		strings.Replace(
			string(phaseArtifact(epistemic.PhaseTriage)),
			"reported regression",
			"reported second regression",
			1,
		),
	)

	secondProposal, err := secondManager.Submit(
		ctx,
		SubmitRequest{
			RunID:          run.RunID,
			IdempotencyKey: "concurrent-two",
			Dispatch:       dispatch.Manifest.DispatchReference,
			Result:         secondResult,
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})

	var wait sync.WaitGroup

	results := make(chan error, 2)

	for _, request := range []struct {
		manager    *Manager
		proposalID string
		key        string
	}{
		{firstManager, firstProposal.Proposal.ProposalID, "accept-one"},
		{secondManager, secondProposal.Proposal.ProposalID, "accept-two"},
	} {
		wait.Add(1)
		go func(request struct {
			manager    *Manager
			proposalID string
			key        string
		}) {
			defer wait.Done()

			<-start

			_, err := request.manager.Accept(ctx, run.RunID, request.proposalID, 0, request.key)
			results <- err
		}(request)
	}

	close(start)
	wait.Wait()
	close(results)

	successes, stale := 0, 0

	for err := range results {
		if err == nil {
			successes++
		} else if ErrorCode(err) == "stale_state" {
			stale++
		} else {
			t.Fatalf("unexpected concurrent accept error: %v", err)
		}
	}

	status, err := firstManager.Status(ctx, run.RunID)
	if err != nil {
		t.Fatal(err)
	}

	if successes != 1 || stale != 1 || status.StateVersion != 1 {
		t.Fatalf(
			"concurrent accept result successes=%d stale=%d status=%+v",
			successes,
			stale,
			status,
		)
	}
}

func TestSimultaneousRunsRemainIndependent(t *testing.T) {
	ctx := context.Background()
	manager := testManager(t, nil)

	firstRun, err := manager.Create(ctx, "investigate first independent task", "")
	if err != nil {
		t.Fatal(err)
	}

	secondRun, err := manager.Create(ctx, "investigate second independent task", "")
	if err != nil {
		t.Fatal(err)
	}

	firstDispatch, err := manager.Dispatch(
		ctx,
		DispatchRequest{
			RunID:        firstRun.RunID,
			StateVersion: 0,
			Mode:         "host_managed",
			SessionID:    "shared-client-session-1",
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	secondDispatch, err := manager.Dispatch(
		ctx,
		DispatchRequest{
			RunID:        secondRun.RunID,
			StateVersion: 0,
			Mode:         "host_managed",
			SessionID:    "shared-client-session-1",
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	firstArtifact := json.RawMessage(
		strings.Replace(
			string(phaseArtifact(epistemic.PhaseTriage)),
			"the command reports the regression",
			"first run evidence",
			1,
		),
	)
	secondArtifact := json.RawMessage(
		strings.Replace(
			string(phaseArtifact(epistemic.PhaseTriage)),
			"the command reports the regression",
			"second run evidence",
			1,
		),
	)
	start := make(chan struct{})
	results := make(chan error, 2)

	for _, request := range []struct {
		runID, key string
		dispatch   DispatchReference
		artifact   json.RawMessage
	}{
		{firstRun.RunID, "first-run-submit", firstDispatch.Manifest.DispatchReference, firstArtifact},
		{secondRun.RunID, "second-run-submit", secondDispatch.Manifest.DispatchReference, secondArtifact},
	} {
		go func(request struct {
			runID, key string
			dispatch   DispatchReference
			artifact   json.RawMessage
		}) {
			<-start

			submission, err := manager.Submit(
				ctx,
				SubmitRequest{
					RunID:          request.runID,
					IdempotencyKey: request.key,
					Dispatch:       request.dispatch,
					Result:         request.artifact,
				},
			)
			if err == nil {
				_, err = manager.Accept(
					ctx,
					request.runID,
					submission.Proposal.ProposalID,
					0,
					"accept-"+request.key,
				)
			}

			if err == nil {
				_, err = manager.Advance(ctx, request.runID, 1)
			}

			results <- err
		}(request)
	}

	close(start)

	for i := 0; i < 2; i++ {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}

	firstExport, err := manager.Export(ctx, firstRun.RunID)
	if err != nil {
		t.Fatal(err)
	}

	secondExport, err := manager.Export(ctx, secondRun.RunID)
	if err != nil {
		t.Fatal(err)
	}

	if firstExport.State.Task.Goal != "investigate first independent task" ||
		secondExport.State.Task.Goal != "investigate second independent task" {
		t.Fatalf(
			"simultaneous runs crossed goals: first=%q second=%q",
			firstExport.State.Task.Goal,
			secondExport.State.Task.Goal,
		)
	}

	if len(firstExport.State.Observations) != 1 ||
		firstExport.State.Observations[0].Content != "first run evidence" ||
		len(secondExport.State.Observations) != 1 ||
		secondExport.State.Observations[0].Content != "second run evidence" {
		t.Fatalf(
			"simultaneous run evidence crossed: first=%+v second=%+v",
			firstExport.State.Observations,
			secondExport.State.Observations,
		)
	}
}

func TestConcurrentRunIsolationAndHostServerEquivalence(t *testing.T) {
	ctx := context.Background()
	hostManager := testManager(t, nil)
	serverManager := testManager(t, fixtureRuntime{})

	hostRun, err := hostManager.Create(ctx, "same task", "")
	if err != nil {
		t.Fatal(err)
	}

	serverRun, err := serverManager.Create(ctx, "same task", "")
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 5; i++ {
		hostRun = advanceHostFixture(t, hostManager, hostRun, i)

		dispatched, err := serverManager.Dispatch(
			ctx,
			DispatchRequest{
				RunID:        serverRun.RunID,
				StateVersion: serverRun.StateVersion,
				Mode:         "server_managed",
			},
		)
		if err != nil {
			t.Fatal(err)
		}

		if dispatched.Proposal == nil {
			t.Fatalf(
				"server-managed dispatch did not submit its result at phase %s",
				dispatched.Manifest.Phase,
			)
		}

		accepted, err := serverManager.Accept(
			ctx,
			serverRun.RunID,
			dispatched.Proposal.ProposalID,
			serverRun.StateVersion,
			"server-accept-"+string(dispatched.Manifest.Phase),
		)
		if err != nil {
			t.Fatal(err)
		}

		serverRun, err = serverManager.Advance(ctx, serverRun.RunID, accepted.StateVersion)
		if err != nil {
			t.Fatal(err)
		}
	}

	hostExport, err := hostManager.Export(ctx, hostRun.RunID)
	if err != nil {
		t.Fatal(err)
	}

	serverExport, err := serverManager.Export(ctx, serverRun.RunID)
	if err != nil {
		t.Fatal(err)
	}

	if semanticState(hostExport.State) != semanticState(serverExport.State) {
		t.Fatalf(
			"host/server state differs:\nhost=%s\nserver=%s",
			semanticState(hostExport.State),
			semanticState(serverExport.State),
		)
	}

	if hostExport.Run.RunID == serverExport.Run.RunID {
		t.Fatal("independent runs share an identifier")
	}

	if hostExport.State.Task.Goal != serverExport.State.Task.Goal {
		t.Fatal("runs with the same task unexpectedly interfered")
	}
}

func advanceHostFixture(t *testing.T, manager *Manager, run RunStatus, index int) RunStatus {
	t.Helper()

	ctx := context.Background()

	dispatch, err := manager.Dispatch(
		ctx,
		DispatchRequest{
			RunID:        run.RunID,
			StateVersion: run.StateVersion,
			Mode:         "host_managed",
			SessionID:    fmt.Sprintf("host-%d", index),
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	submission, err := manager.Submit(
		ctx,
		SubmitRequest{
			RunID:          run.RunID,
			IdempotencyKey: fmt.Sprintf("host-submit-%d", index),
			Dispatch:       dispatch.Manifest.DispatchReference,
			Result: phaseArtifact(
				dispatch.Manifest.Phase,
				dispatch.Manifest.Projection.State,
			),
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	accepted, err := manager.Accept(
		ctx,
		run.RunID,
		submission.Proposal.ProposalID,
		run.StateVersion,
		fmt.Sprintf("host-accept-%d", index),
	)
	if err != nil {
		t.Fatal(err)
	}

	result, err := manager.Advance(ctx, run.RunID, accepted.StateVersion)
	if err != nil {
		t.Fatal(err)
	}

	return result
}

func semanticState(state epistemic.State) string {
	ids := map[epistemic.ID]string{}

	var value struct {
		Goal         string
		Phase        epistemic.Phase
		WorkStatus   epistemic.WorkStatus
		Observations []string
		Claims       []string
		Hypotheses   []string
		Unknowns     []string
		Constraints  []string
		Frames       []string
		Actions      []string
		Outcomes     []string
		Relations    []string
	}

	value.Goal, value.Phase, value.WorkStatus = state.Task.Goal, state.Task.Phase, state.Task.Status
	for i, object := range state.Observations {
		label := fmt.Sprintf("observation-%d", i)
		ids[object.ID] = label
		value.Observations = append(value.Observations, object.Content)
	}

	for i, object := range state.Claims {
		label := fmt.Sprintf("claim-%d", i)
		ids[object.ID] = label
		value.Claims = append(value.Claims, object.Text)
	}

	for i, object := range state.Hypotheses {
		label := fmt.Sprintf("hypothesis-%d", i)
		ids[object.ID] = label
		value.Hypotheses = append(value.Hypotheses, object.Mechanism+":"+string(object.Status))
	}

	for i, object := range state.Unknowns {
		label := fmt.Sprintf("unknown-%d", i)
		ids[object.ID] = label
		value.Unknowns = append(value.Unknowns, object.Question+":"+string(object.Status))
	}

	for i, object := range state.Constraints {
		label := fmt.Sprintf("constraint-%d", i)
		ids[object.ID] = label
		value.Constraints = append(value.Constraints, object.Text)
	}

	for i, object := range state.Frames {
		label := fmt.Sprintf("frame-%d", i)
		ids[object.ID] = label
		value.Frames = append(
			value.Frames,
			object.Name+":"+object.Summary+":"+string(object.Status),
		)
	}

	for i, object := range state.Actions {
		label := fmt.Sprintf("action-%d", i)
		ids[object.ID] = label
		value.Actions = append(value.Actions, object.Description)
	}

	for i, object := range state.Outcomes {
		label := fmt.Sprintf("outcome-%d", i)
		ids[object.ID] = label
		value.Outcomes = append(value.Outcomes, object.Description)
	}

	for _, relation := range state.Relations {
		value.Relations = append(
			value.Relations,
			string(
				relation.Kind,
			)+":"+ids[relation.SourceID]+"->"+ids[relation.TargetID]+":"+string(
				relation.Status,
			),
		)
	}

	sort.Strings(value.Relations)
	encoded, _ := json.Marshal(value)

	return string(encoded)
}

type fixtureRuntime struct{}

func (fixtureRuntime) RunPhase(
	_ context.Context,
	manifest DispatchManifest,
) (json.RawMessage, bool, error) {
	return phaseArtifact(manifest.Phase, manifest.Projection.State), false, nil
}

func testManager(t *testing.T, runtime PhaseRuntime) *Manager {
	t.Helper()

	manager, err := NewManager(t.TempDir(), runtime)
	if err != nil {
		t.Fatal(err)
	}

	return manager
}

func phaseArtifact(phase epistemic.Phase, projected ...epistemic.State) json.RawMessage {
	qac := `"qac_request":{"direction":"release","uncertainty":0.3,"novelty":0.2,"expected_gain":0.4,"failed_attempts":0,"reason":"","unresolved":"","evidence":[],"attempted":[],"recommended_focus":""}`

	var source string

	switch phase {
	case epistemic.PhaseTriage:
		source = `{"classification":"reported regression","boundaries":["one service"],"observations":[{"local_ref":"o1","content":"the command reports the regression"}],"claims":[{"local_ref":"c1","text":"the regression is reproducible","evidence_ref":"o1"}],"unknowns":[{"local_ref":"u1","question":"does the fix pass the full check suite?"}],"resolved_unknowns":[],"next_investigation":"compare the two execution paths",` + qac + `}`
	case epistemic.PhaseAbduce:
		source = `{"hypotheses":[{"local_ref":"h1","mechanism":"the shared path mishandles the result","falsifier":"the shared path emits the expected result"},{"local_ref":"h2","mechanism":"the provider returned incomplete data","falsifier":"the provider payload is complete"}],"rejected_hypotheses":[{"hypothesis_ref":"h2","evidence_ref":"o1"}],"relations":[{"local_ref":"r1","kind":"supports","source":"o1","target":"h1"},{"local_ref":"r2","kind":"contradicts","source":"o1","target":"h2"}],"leading_hypothesis_ref":"h1","remaining_uncertainty":"the provider path is not fully isolated",` + qac + `}`
	case epistemic.PhaseFrame:
		source = `{"frame":{"local_ref":"f1","name":"fix shared result handling","summary":"change the shared path and check its consumers","completion_conditions":["all checks pass"],"disconfirmation_conditions":["the regression still reproduces"]},"constraints":[{"local_ref":"k1","text":"preserve the external result format"}],"relations":[{"local_ref":"r1","kind":"depends_on","source":"f1","target":"h1"}],` + qac + `}`
	case epistemic.PhaseExecute:
		source = `{"observations":[{"local_ref":"o2","content":"all checks pass after the change"}],"actions":[{"local_ref":"a1","description":"updated the shared result handling"}],"outcomes":[{"local_ref":"x1","description":"all checks pass after the change"}],"relations":[{"local_ref":"r1","kind":"implements","source":"a1","target":"f1"},{"local_ref":"r2","kind":"tests","source":"x1","target":"f1"}],` + qac + `}`
	case epistemic.PhaseClose:
		source = `{"observations":[{"local_ref":"o3","content":"all checks pass"}],"relations":[{"local_ref":"r1","kind":"tests","source":"o3","target":"f1"}],"resolved_unknowns":[{"local_ref":"r2","kind":"resolves","source":"o3","target":"u1"}],"residual_uncertainty":"","completion_recommended":true,` + qac + `}`
	default:
		return json.RawMessage(`{}`)
	}

	if len(projected) > 0 {
		state := projected[0]
		if phase == epistemic.PhaseAbduce && len(state.Observations) > 0 {
			source = strings.ReplaceAll(
				source,
				`"source":"o1"`,
				`"source":"`+string(state.Observations[0].ID)+`"`,
			)
			source = strings.ReplaceAll(
				source,
				`"evidence_ref":"o1"`,
				`"evidence_ref":"`+string(state.Observations[0].ID)+`"`,
			)
		}

		if phase == epistemic.PhaseFrame && state.Task.LeadingHypothesis != "" {
			source = strings.ReplaceAll(
				source,
				`"target":"h1"`,
				`"target":"`+string(state.Task.LeadingHypothesis)+`"`,
			)
		}

		if phase == epistemic.PhaseExecute && state.Task.ActiveFrame != "" {
			source = strings.ReplaceAll(
				source,
				`"target":"f1"`,
				`"target":"`+string(state.Task.ActiveFrame)+`"`,
			)
		}

		if phase == epistemic.PhaseClose {
			if state.Task.ActiveFrame != "" {
				source = strings.ReplaceAll(
					source,
					`"target":"f1"`,
					`"target":"`+string(state.Task.ActiveFrame)+`"`,
				)
			}

			for _, unknown := range state.Unknowns {
				if unknown.Status == epistemic.UnknownOpen ||
					unknown.Status == epistemic.UnknownReopened {
					source = strings.ReplaceAll(
						source,
						`"target":"u1"`,
						`"target":"`+string(unknown.ID)+`"`,
					)

					break
				}
			}
		}
	}

	return json.RawMessage(source)
}

func assertPhaseProjection(t *testing.T, phase epistemic.Phase, projection PhaseProjection) {
	t.Helper()

	state := projection.State

	switch phase {
	case epistemic.PhaseTriage:
		if len(state.Hypotheses) != 0 || len(state.Observations) != 0 {
			// The first TRIAGE projection has the initial state only.
			t.Fatalf("triage projection leaked or accumulated wrong objects: %+v", state)
		}
	case epistemic.PhaseAbduce:
		if len(state.Observations) != 1 || len(state.Unknowns) != 1 || len(state.Hypotheses) != 0 {
			t.Fatalf("abduce projection was not built from accepted triage state: %+v", state)
		}
	case epistemic.PhaseFrame:
		if len(state.Hypotheses) != 1 || state.Hypotheses[0].ID != state.Task.LeadingHypothesis ||
			len(state.Frames) != 0 {
			t.Fatalf("frame projection missing accepted abduction: %+v", state)
		}

		foundUncertainty := false

		for _, decision := range projection.Decisions {
			if decision.Kind == "artifact_acceptance" && decision.Phase == epistemic.PhaseAbduce &&
				decision.Uncertainty != "" {
				foundUncertainty = true
			}
		}

		if !foundUncertainty {
			t.Fatal("frame projection omitted accepted abduction uncertainty")
		}
	case epistemic.PhaseExecute:
		if len(state.Frames) != 1 || state.Task.ActiveFrame == "" || len(state.Constraints) != 1 {
			t.Fatalf("execute projection missing accepted frame: %+v", state)
		}
	case epistemic.PhaseClose:
		if len(state.Actions) != 1 || len(state.Outcomes) != 1 || len(state.Observations) != 2 {
			t.Fatalf("close projection missing accepted execution evidence: %+v", state)
		}
	}
}
