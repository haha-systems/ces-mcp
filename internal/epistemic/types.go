package epistemic

import "time"

type Producer struct {
	Phase         Phase  `json:"phase"`
	Process       string `json:"process"`
	Artifact      string `json:"artifact"`
	Runtime       string `json:"runtime,omitempty"`
	Model         string `json:"model,omitempty"`
	Deterministic bool   `json:"deterministic,omitempty"`
	Transform     string `json:"transform,omitempty"`
	Version       string `json:"version,omitempty"`
}

type Task struct {
	ID                ID                 `json:"id"`
	Goal              string             `json:"goal"`
	Status            WorkStatus         `json:"status"`
	Phase             Phase              `json:"phase"`
	ReopenCount       int                `json:"reopen_count"`
	TerminalReason    string             `json:"terminal_reason,omitempty"`
	LeadingHypothesis ID                 `json:"leading_hypothesis,omitempty"`
	ActiveFrame       ID                 `json:"active_frame,omitempty"`
	LastTransition    *TransitionSummary `json:"last_transition,omitempty"`
	CreatedAt         time.Time          `json:"created_at"`
	UpdatedAt         time.Time          `json:"updated_at"`
}

type Observation struct {
	ID         ID        `json:"id"`
	Content    string    `json:"content"`
	ProducedBy Producer  `json:"provenance"`
	RevisionID ID        `json:"revision_id"`
	CreatedAt  time.Time `json:"created_at"`
}

type Claim struct {
	ID         ID        `json:"id"`
	Text       string    `json:"text"`
	ProducedBy Producer  `json:"provenance"`
	RevisionID ID        `json:"revision_id"`
	CreatedAt  time.Time `json:"created_at"`
}

type HypothesisStatus string

const (
	HypothesisProposed   HypothesisStatus = "proposed"
	HypothesisLive       HypothesisStatus = "live"
	HypothesisLeading    HypothesisStatus = "leading"
	HypothesisWeakened   HypothesisStatus = "weakened"
	HypothesisRejected   HypothesisStatus = "rejected"
	HypothesisSuperseded HypothesisStatus = "superseded"
	HypothesisConfirmed  HypothesisStatus = "confirmed"
)

type Hypothesis struct {
	ID         ID               `json:"id"`
	Mechanism  string           `json:"mechanism"`
	Falsifier  string           `json:"falsifier"`
	Status     HypothesisStatus `json:"status"`
	ProducedBy Producer         `json:"provenance"`
	RevisionID ID               `json:"revision_id"`
	CreatedAt  time.Time        `json:"created_at"`
}

type UnknownStatus string

const (
	UnknownOpen     UnknownStatus = "open"
	UnknownResolved UnknownStatus = "resolved"
	UnknownReopened UnknownStatus = "reopened"
)

type Unknown struct {
	ID         ID            `json:"id"`
	Question   string        `json:"question"`
	Status     UnknownStatus `json:"status"`
	ProducedBy Producer      `json:"provenance"`
	RevisionID ID            `json:"revision_id"`
	CreatedAt  time.Time     `json:"created_at"`
}

type Constraint struct {
	ID         ID        `json:"id"`
	Text       string    `json:"text"`
	ProducedBy Producer  `json:"provenance"`
	RevisionID ID        `json:"revision_id"`
	CreatedAt  time.Time `json:"created_at"`
}

type FrameStatus string

const (
	FramePending    FrameStatus = "pending"
	FrameActive     FrameStatus = "active"
	FrameSuperseded FrameStatus = "superseded"
)

type Frame struct {
	ID                        ID          `json:"id"`
	Name                      string      `json:"name"`
	Summary                   string      `json:"summary"`
	CompletionConditions      []string    `json:"completion_conditions"`
	DisconfirmationConditions []string    `json:"disconfirmation_conditions"`
	Status                    FrameStatus `json:"status"`
	ProducedBy                Producer    `json:"provenance"`
	RevisionID                ID          `json:"revision_id"`
	CreatedAt                 time.Time   `json:"created_at"`
}

type Action struct {
	ID          ID        `json:"id"`
	Description string    `json:"description"`
	Status      string    `json:"status"`
	ProducedBy  Producer  `json:"provenance"`
	RevisionID  ID        `json:"revision_id"`
	CreatedAt   time.Time `json:"created_at"`
}

type Outcome struct {
	ID          ID        `json:"id"`
	Description string    `json:"description"`
	ProducedBy  Producer  `json:"provenance"`
	RevisionID  ID        `json:"revision_id"`
	CreatedAt   time.Time `json:"created_at"`
}

type RelationKind string

const (
	RelationSupports    RelationKind = "supports"
	RelationContradicts RelationKind = "contradicts"
	RelationDerivedFrom RelationKind = "derived_from"
	RelationDependsOn   RelationKind = "depends_on"
	RelationImplements  RelationKind = "implements"
	RelationTests       RelationKind = "tests"
	RelationResolves    RelationKind = "resolves"
	RelationSupersedes  RelationKind = "supersedes"
)

type RelationStatus string

const (
	RelationActive    RelationStatus = "active"
	RelationRetracted RelationStatus = "retracted"
)

type Relation struct {
	ID         ID             `json:"id"`
	Kind       RelationKind   `json:"kind"`
	SourceID   ID             `json:"source_id"`
	TargetID   ID             `json:"target_id"`
	Status     RelationStatus `json:"status"`
	ReasonRef  ID             `json:"reason_ref,omitempty"`
	ProducedBy Producer       `json:"provenance"`
	RevisionID ID             `json:"revision_id"`
	CreatedAt  time.Time      `json:"created_at"`
}

type State struct {
	Task         Task          `json:"task"`
	Observations []Observation `json:"observations"`
	Claims       []Claim       `json:"claims"`
	Hypotheses   []Hypothesis  `json:"hypotheses"`
	Unknowns     []Unknown     `json:"unknowns"`
	Constraints  []Constraint  `json:"constraints"`
	Frames       []Frame       `json:"frames"`
	Actions      []Action      `json:"actions"`
	Outcomes     []Outcome     `json:"outcomes"`
	Relations    []Relation    `json:"relations"`
}

type ObservationInput struct {
	LocalRef, Content string
	DerivedFrom       string
}
type ClaimInput struct{ LocalRef, Text string }
type HypothesisInput struct{ LocalRef, Mechanism, Falsifier string }
type UnknownInput struct{ LocalRef, Question string }
type ConstraintInput struct{ LocalRef, Text string }
type FrameInput struct {
	LocalRef, Name, Summary                         string
	CompletionConditions, DisconfirmationConditions []string
}
type ActionInput struct{ LocalRef, Description string }
type OutcomeInput struct{ LocalRef, Description string }
type RelationInput struct {
	LocalRef       string
	Kind           RelationKind
	Source, Target string
}
type RetractionInput struct{ Relation, ReasonRef string }
type StatusChange struct {
	Kind        ObjectKind
	Ref, Status string
}

type Delta struct {
	Observations      []ObservationInput
	Claims            []ClaimInput
	Hypotheses        []HypothesisInput
	Unknowns          []UnknownInput
	Constraints       []ConstraintInput
	Frames            []FrameInput
	Actions           []ActionInput
	Outcomes          []OutcomeInput
	Relations         []RelationInput
	Retractions       []RetractionInput
	StatusChanges     []StatusChange
	LeadingHypothesis string
	ActiveFrame       string
	WorkStatus        *WorkStatus
	TerminalReason    string
}

type CommitRequest struct {
	Phase      Phase
	Producer   Producer
	Delta      Delta
	Projection Projection
}

type Projection struct {
	Phase Phase `json:"phase"`
	State State `json:"state"`
	IDs   []ID  `json:"ids"`
}

type ProjectionRequest struct {
	Phase   Phase
	Include []ID
}

type TransitionRequest struct {
	To         Phase
	Reason     string
	TargetID   ID
	TargetKind ObjectKind
}

type TransitionResult struct{ Events []Event }

func (p Projection) Contains(id ID) bool {
	for _, included := range p.IDs {
		if included == id {
			return true
		}
	}

	return false
}

type CommitResult struct {
	IDs    map[string]ID
	Events []Event
}
