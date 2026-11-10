package engine

import (
	"context"
	"encoding/json"
	"time"

	"github.com/haha-systems/ces-mcp/internal/epistemic"
)

const CESVersion = "0.2.0"

type DispatchReference struct {
	DispatchID     string          `json:"dispatch_id"`
	RunID          string          `json:"run_id"`
	Phase          epistemic.Phase `json:"phase"`
	StateVersion   uint64          `json:"state_version"`
	StateHash      string          `json:"state_hash"`
	ProjectionHash string          `json:"projection_hash"`
	PromptHash     string          `json:"prompt_hash"`
	SessionID      string          `json:"session_id"`
	ParentRunID    string          `json:"parent_run_id,omitempty"`
}

type EvidenceRecord struct {
	ID         epistemic.ID             `json:"id"`
	Kind       epistemic.RelationKind   `json:"kind"`
	SourceID   epistemic.ID             `json:"source_id"`
	TargetID   epistemic.ID             `json:"target_id"`
	Status     epistemic.RelationStatus `json:"status"`
	Provenance epistemic.Producer       `json:"provenance"`
	CreatedAt  time.Time                `json:"created_at"`
}

type DecisionRecord struct {
	ID                    string          `json:"id"`
	Kind                  string          `json:"kind"`
	Phase                 epistemic.Phase `json:"phase"`
	Decision              string          `json:"decision"`
	Reason                string          `json:"reason,omitempty"`
	Summary               string          `json:"summary,omitempty"`
	Boundaries            []string        `json:"boundaries,omitempty"`
	NextInvestigation     string          `json:"next_investigation,omitempty"`
	Uncertainty           string          `json:"uncertainty,omitempty"`
	CompletionRecommended *bool           `json:"completion_recommended,omitempty"`
	DispatchID            string          `json:"dispatch_id,omitempty"`
	ProposalID            string          `json:"proposal_id,omitempty"`
	StateVersion          uint64          `json:"state_version"`
	CreatedAt             time.Time       `json:"created_at"`
}

type PhaseProjection struct {
	SchemaVersion string           `json:"schema_version"`
	RunID         string           `json:"run_id"`
	StateVersion  uint64           `json:"state_version"`
	StateHash     string           `json:"state_hash"`
	Phase         epistemic.Phase  `json:"phase"`
	State         epistemic.State  `json:"state"`
	IDs           []epistemic.ID   `json:"ids"`
	Evidence      []EvidenceRecord `json:"evidence"`
	Decisions     []DecisionRecord `json:"accepted_decisions"`
}

type PromptBundle struct {
	Instructions string         `json:"instructions"`
	OutputSchema map[string]any `json:"output_schema"`
}

type IsolationReport struct {
	Status string `json:"status"`
	Detail string `json:"detail"`
}

type DispatchManifest struct {
	DispatchReference
	Mode            string          `json:"mode"`
	Projection      PhaseProjection `json:"projection"`
	Instructions    string          `json:"instructions"`
	OutputSchema    map[string]any  `json:"output_schema"`
	AllowedTools    []string        `json:"allowed_tools"`
	ToolEnforcement string          `json:"tool_enforcement"`
	Isolation       IsolationReport `json:"isolation"`
	CreatedAt       time.Time       `json:"created_at"`
}

type ProposalState string

const (
	ProposalPending  ProposalState = "pending"
	ProposalAccepted ProposalState = "accepted"
	ProposalRejected ProposalState = "rejected"
)

const (
	SubmissionValid   = "valid"
	SubmissionInvalid = "invalid"
)

type SubmissionRecord struct {
	ProposalID       string          `json:"proposal_id"`
	RunID            string          `json:"run_id"`
	DispatchID       string          `json:"dispatch_id"`
	IdempotencyKey   string          `json:"idempotency_key"`
	Result           json.RawMessage `json:"result"`
	ResultHash       string          `json:"result_hash"`
	ValidationStatus string          `json:"validation_status"`
	ValidationErrors []string        `json:"validation_errors,omitempty"`
	CreatedAt        time.Time       `json:"created_at"`
}

type RejectedAttempt struct {
	ID             string          `json:"id"`
	RunID          string          `json:"run_id"`
	DispatchID     string          `json:"dispatch_id,omitempty"`
	ProposalID     string          `json:"proposal_id,omitempty"`
	IdempotencyKey string          `json:"idempotency_key,omitempty"`
	Reason         string          `json:"reason"`
	Code           string          `json:"code"`
	Artifact       json.RawMessage `json:"artifact,omitempty"`
	At             time.Time       `json:"at"`
}

type AuditEvent struct {
	Sequence     uint64          `json:"sequence"`
	Kind         string          `json:"kind"`
	RunID        string          `json:"run_id"`
	Phase        epistemic.Phase `json:"phase,omitempty"`
	DispatchID   string          `json:"dispatch_id,omitempty"`
	ProposalID   string          `json:"proposal_id,omitempty"`
	StateVersion uint64          `json:"state_version"`
	Detail       string          `json:"detail,omitempty"`
	At           time.Time       `json:"at"`
}

type AcceptedReplay struct {
	ProposalID string         `json:"proposal_id"`
	Version    uint64         `json:"version"`
	StateHash  string         `json:"state_hash"`
	Decision   DecisionRecord `json:"decision"`
}

type RunRecord struct {
	SchemaVersion  string                    `json:"schema_version"`
	RunID          string                    `json:"run_id"`
	ParentRunID    string                    `json:"parent_run_id,omitempty"`
	Goal           string                    `json:"goal"`
	CreatedAt      time.Time                 `json:"created_at"`
	UpdatedAt      time.Time                 `json:"updated_at"`
	StateVersion   uint64                    `json:"state_version"`
	Events         []epistemic.Event         `json:"accepted_events"`
	Dispatches     []DispatchManifest        `json:"dispatches"`
	Submissions    []SubmissionRecord        `json:"submissions"`
	SubmissionKeys map[string]string         `json:"submission_idempotency"`
	AcceptKeys     map[string]AcceptedReplay `json:"accept_idempotency"`
	Decisions      []DecisionRecord          `json:"decisions"`
	Rejected       []RejectedAttempt         `json:"rejected_attempts"`
	Audit          []AuditEvent              `json:"audit"`
}

type RunStatus struct {
	RunID           string               `json:"run_id"`
	ParentRunID     string               `json:"parent_run_id,omitempty"`
	Goal            string               `json:"goal"`
	Phase           epistemic.Phase      `json:"phase"`
	WorkStatus      epistemic.WorkStatus `json:"work_status"`
	StateVersion    uint64               `json:"state_version"`
	StateHash       string               `json:"state_hash"`
	PendingWork     string               `json:"pending_work,omitempty"`
	PendingProposal string               `json:"pending_proposal,omitempty"`
	OpenUnknowns    int                  `json:"open_unknowns"`
	Contradictions  int                  `json:"contradictions"`
}

type ValidationResult struct {
	ProposalID string   `json:"proposal_id"`
	Valid      bool     `json:"valid"`
	Errors     []string `json:"errors,omitempty"`
}

type AcceptResult struct {
	ProposalID   string                  `json:"proposal_id"`
	StateVersion uint64                  `json:"state_version"`
	StateHash    string                  `json:"state_hash"`
	CreatedIDs   map[string]epistemic.ID `json:"created_ids,omitempty"`
	Decision     DecisionRecord          `json:"decision"`
}

type Export struct {
	CESVersion string          `json:"ces_version"`
	Run        RunRecord       `json:"run"`
	State      epistemic.State `json:"state"`
	StateHash  string          `json:"state_hash"`
}

type PhaseRuntime interface {
	RunPhase(context.Context, DispatchManifest) (json.RawMessage, bool, error)
}
