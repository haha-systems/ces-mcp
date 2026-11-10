package engine

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gofrs/flock"
	"github.com/haha-systems/ces-mcp/internal/epistemic"
)

type Manager struct {
	dir     string
	runtime PhaseRuntime
}

type EngineError struct {
	Code    string
	Message string
}

func (e *EngineError) Error() string { return e.Message }

func engineError(code, message string) error { return &EngineError{Code: code, Message: message} }

func ErrorCode(err error) string {
	var coded *EngineError
	if errors.As(err, &coded) {
		return coded.Code
	}

	return "internal_error"
}

func NewManager(dataDir string, runtime PhaseRuntime) (*Manager, error) {
	if strings.TrimSpace(dataDir) == "" {
		dataDir = "data"
	}

	dir, err := filepath.Abs(dataDir)
	if err != nil {
		return nil, fmt.Errorf("resolve data directory: %w", err)
	}

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}

	return &Manager{dir: dir, runtime: runtime}, nil
}

func (m *Manager) DataDir() string { return m.dir }

func (m *Manager) Create(ctx context.Context, goal, parentRunID string) (RunStatus, error) {
	if strings.TrimSpace(goal) == "" {
		return RunStatus{}, engineError("invalid_goal", "goal must not be empty")
	}

	if parentRunID != "" {
		if _, err := m.load(ctx, parentRunID); err != nil {
			return RunStatus{}, engineError("invalid_parent_run", "parent run does not exist")
		}
	}

	store, err := epistemic.NewStore(goal)
	if err != nil {
		return RunStatus{}, fmt.Errorf("create CES state: %w", err)
	}

	state := store.State()
	now := state.Task.CreatedAt

	run := RunRecord{
		SchemaVersion:  CESVersion,
		RunID:          string(state.Task.ID),
		ParentRunID:    parentRunID,
		Goal:           goal,
		CreatedAt:      now,
		UpdatedAt:      now,
		StateVersion:   0,
		Events:         store.Events(),
		SubmissionKeys: map[string]string{},
		AcceptKeys:     map[string]AcceptedReplay{},
		Dispatches:     []DispatchManifest{},
		Submissions:    []SubmissionRecord{},
		Decisions:      []DecisionRecord{},
		Rejected:       []RejectedAttempt{},
		Audit:          []AuditEvent{},
	}
	if err := m.saveNew(ctx, run); err != nil {
		return RunStatus{}, err
	}

	return m.statusFrom(run, store), nil
}

func (m *Manager) Status(ctx context.Context, runID string) (RunStatus, error) {
	run, store, err := m.loadWithStore(ctx, runID)
	if err != nil {
		return RunStatus{}, err
	}

	return m.statusFrom(run, store), nil
}

func (m *Manager) Export(ctx context.Context, runID string) (Export, error) {
	run, store, err := m.loadWithStore(ctx, runID)
	if err != nil {
		return Export{}, err
	}

	hash, err := acceptedStateHash(run)
	if err != nil {
		return Export{}, err
	}

	return Export{CESVersion: CESVersion, Run: run, State: store.State(), StateHash: hash}, nil
}

func (m *Manager) History(
	ctx context.Context,
	runID string,
	offset, limit int,
) (map[string]any, error) {
	run, store, err := m.loadWithStore(ctx, runID)
	if err != nil {
		return nil, err
	}

	if offset < 0 || limit < 0 || limit > 1000 {
		return nil, engineError(
			"invalid_pagination",
			"offset must be non-negative and limit must be from 0 to 1000",
		)
	}

	events := store.Events()

	end := len(events)
	if offset > end {
		offset = end
	}

	stop := end
	if limit > 0 && offset+limit < stop {
		stop = offset + limit
	}

	return map[string]any{
		"run_id":            run.RunID,
		"state_version":     run.StateVersion,
		"accepted_events":   events[offset:stop],
		"decisions":         run.Decisions,
		"dispatches":        run.Dispatches,
		"submissions":       run.Submissions,
		"rejected_attempts": run.Rejected,
		"audit":             run.Audit,
		"offset":            offset,
		"limit":             limit,
	}, nil
}

func (m *Manager) saveNew(ctx context.Context, run RunRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	lock := flock.New(m.lockPath(run.RunID))
	if err := lock.Lock(); err != nil {
		return fmt.Errorf("lock new CES run: %w", err)
	}
	defer lock.Unlock()

	if _, err := os.Stat(m.runPath(run.RunID)); err == nil {
		return engineError("run_exists", "generated run identifier already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect run path: %w", err)
	}

	return m.writeAtomic(run)
}

func (m *Manager) load(ctx context.Context, runID string) (RunRecord, error) {
	run, _, err := m.loadWithStore(ctx, runID)
	return run, err
}

func (m *Manager) loadWithStore(
	ctx context.Context,
	runID string,
) (RunRecord, *epistemic.Store, error) {
	if !validRunID(runID) {
		return RunRecord{}, nil, engineError("run_not_found", "CES run does not exist")
	}

	if err := ctx.Err(); err != nil {
		return RunRecord{}, nil, err
	}

	lock := flock.New(m.lockPath(runID))
	if err := lock.RLock(); err != nil {
		return RunRecord{}, nil, fmt.Errorf("lock CES run: %w", err)
	}
	defer lock.Unlock()

	run, store, err := m.readUnlocked(runID)

	return run, store, err
}

func (m *Manager) edit(
	ctx context.Context,
	runID string,
	fn func(*RunRecord, *epistemic.Store) error,
) error {
	if !validRunID(runID) {
		return engineError("run_not_found", "CES run does not exist")
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	lock := flock.New(m.lockPath(runID))
	if err := lock.Lock(); err != nil {
		return fmt.Errorf("lock CES run: %w", err)
	}
	defer lock.Unlock()

	run, store, err := m.readUnlocked(runID)
	if err != nil {
		return err
	}

	before, _ := json.Marshal(run)
	operationErr := fn(&run, store)
	run.Events = store.Events()

	after, marshalErr := json.Marshal(run)
	if marshalErr != nil {
		return fmt.Errorf("encode CES run: %w", marshalErr)
	}

	if string(before) != string(after) {
		run.UpdatedAt = time.Now().UTC()
		if err := m.writeAtomic(run); err != nil {
			return fmt.Errorf("persist CES run: %w", err)
		}
	}

	return operationErr
}

func (m *Manager) readUnlocked(runID string) (RunRecord, *epistemic.Store, error) {
	data, err := os.ReadFile(m.runPath(runID))
	if errors.Is(err, os.ErrNotExist) {
		return RunRecord{}, nil, engineError("run_not_found", "CES run does not exist")
	}

	if err != nil {
		return RunRecord{}, nil, fmt.Errorf("read CES run: %w", err)
	}

	var run RunRecord

	decoderErr := json.Unmarshal(data, &run)
	if decoderErr != nil {
		return RunRecord{}, nil, fmt.Errorf("decode CES run: %w", decoderErr)
	}

	if run.SchemaVersion != CESVersion || run.RunID != runID {
		return RunRecord{}, nil, engineError(
			"corrupt_run",
			"CES run schema version or run identifier is invalid",
		)
	}

	if run.SubmissionKeys == nil {
		run.SubmissionKeys = map[string]string{}
	}

	if run.AcceptKeys == nil {
		run.AcceptKeys = map[string]AcceptedReplay{}
	}

	store, err := epistemic.Replay(run.Events)
	if err != nil {
		return RunRecord{}, nil, fmt.Errorf("replay CES event log: %w", err)
	}

	if string(store.State().Task.ID) != runID {
		return RunRecord{}, nil, engineError(
			"corrupt_run",
			"CES event log belongs to a different run",
		)
	}

	return run, store, nil
}

func (m *Manager) writeAtomic(run RunRecord) error {
	data, err := json.MarshalIndent(run, "", "  ")
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp(m.dir, ".ces-run-*.tmp")
	if err != nil {
		return err
	}

	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}

	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}

	if err := tmp.Close(); err != nil {
		return err
	}

	if err := os.Rename(tmpName, m.runPath(run.RunID)); err != nil {
		return err
	}

	if dir, err := os.Open(m.dir); err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}

	return nil
}

func (m *Manager) runPath(runID string) string  { return filepath.Join(m.dir, runID+".json") }
func (m *Manager) lockPath(runID string) string { return filepath.Join(m.dir, runID+".lock") }

func validRunID(runID string) bool {
	if len(runID) != 36 || !strings.HasPrefix(runID, "ces_") {
		return false
	}

	_, err := hex.DecodeString(strings.TrimPrefix(runID, "ces_"))

	return err == nil
}

func randomID(prefix string) (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}

	return prefix + hex.EncodeToString(raw[:]), nil
}

func hashJSON(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}

	sum := sha256.Sum256(data)

	return hex.EncodeToString(sum[:]), nil
}

func hashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func acceptedStateHash(run RunRecord) (string, error) {
	return hashJSON(struct {
		Events    []epistemic.Event `json:"accepted_events"`
		Decisions []DecisionRecord  `json:"accepted_decisions"`
	}{Events: run.Events, Decisions: run.Decisions})
}

func (m *Manager) statusFrom(run RunRecord, store *epistemic.Store) RunStatus {
	state := store.State()
	hash, _ := acceptedStateHash(run)

	status := RunStatus{
		RunID:        run.RunID,
		ParentRunID:  run.ParentRunID,
		Goal:         run.Goal,
		Phase:        state.Task.Phase,
		WorkStatus:   state.Task.Status,
		StateVersion: run.StateVersion,
		StateHash:    hash,
		PendingWork:  "prepare a dispatch for the current phase",
	}
	for _, unknown := range state.Unknowns {
		if unknown.Status == epistemic.UnknownOpen || unknown.Status == epistemic.UnknownReopened {
			status.OpenUnknowns++
		}
	}

	for _, relation := range state.Relations {
		if relation.Kind == epistemic.RelationContradicts &&
			relation.Status == epistemic.RelationActive {
			status.Contradictions++
		}
	}

	if state.Task.Status != epistemic.WorkActive {
		status.PendingWork = ""
		return status
	}

	if dispatch, ok := latestDispatch(run); ok && dispatch.Phase == state.Task.Phase {
		if dispatch.StateVersion == run.StateVersion && dispatch.StateHash == hash {
			status.PendingWork = "submit a result from the current dispatch"
		}

		for i := len(run.Submissions) - 1; i >= 0; i-- {
			proposal := run.Submissions[i]

			proposalStatus := proposalStatus(run, proposal.ProposalID)
			if proposal.DispatchID == dispatch.DispatchID && proposalStatus == ProposalPending &&
				proposal.ValidationStatus != SubmissionInvalid {
				status.PendingProposal = proposal.ProposalID
				status.PendingWork = "validate and accept or reject the submitted proposal"

				return status
			}

			if proposal.DispatchID == dispatch.DispatchID && proposalStatus == ProposalAccepted &&
				dispatch.StateVersion+1 == run.StateVersion {
				status.PendingWork = "advance to the next phase"
				return status
			}
		}
	}

	return status
}

func latestDispatch(run RunRecord) (DispatchManifest, bool) {
	if len(run.Dispatches) == 0 {
		return DispatchManifest{}, false
	}

	return run.Dispatches[len(run.Dispatches)-1], true
}

func appendAudit(
	run *RunRecord,
	kind string,
	phase epistemic.Phase,
	dispatchID, proposalID, detail string,
) {
	run.Audit = append(run.Audit, AuditEvent{
		Sequence:     uint64(len(run.Audit) + 1),
		Kind:         kind,
		RunID:        run.RunID,
		Phase:        phase,
		DispatchID:   dispatchID,
		ProposalID:   proposalID,
		StateVersion: run.StateVersion,
		Detail:       detail,
		At:           time.Now().UTC(),
	})
}

func appendRejected(run *RunRecord, attempt RejectedAttempt) {
	if attempt.ID == "" {
		attempt.ID, _ = randomID("rej_")
	}

	attempt.RunID = run.RunID
	if attempt.At.IsZero() {
		attempt.At = time.Now().UTC()
	}

	run.Rejected = append(run.Rejected, attempt)
	appendAudit(
		run,
		"submission_rejected",
		"",
		attempt.DispatchID,
		attempt.ProposalID,
		attempt.Code+": "+attempt.Reason,
	)
}
