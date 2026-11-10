// Package epistemic contains the read-only CES contract used by the app.
package epistemic

// ID is an opaque identifier owned by the CES store.
type ID string

// ObjectKind names a CES object type.
type ObjectKind string

const (
	ObjectKindTask        ObjectKind = "task"
	ObjectKindObservation ObjectKind = "observation"
	ObjectKindClaim       ObjectKind = "claim"
	ObjectKindHypothesis  ObjectKind = "hypothesis"
	ObjectKindUnknown     ObjectKind = "unknown"
	ObjectKindConstraint  ObjectKind = "constraint"
	ObjectKindFrame       ObjectKind = "frame"
	ObjectKindAction      ObjectKind = "action"
	ObjectKindOutcome     ObjectKind = "outcome"
	ObjectKindRelation    ObjectKind = "relation"
	ObjectObservation                = ObjectKindObservation
	ObjectClaim                      = ObjectKindClaim
	ObjectHypothesis                 = ObjectKindHypothesis
	ObjectUnknown                    = ObjectKindUnknown
	ObjectConstraint                 = ObjectKindConstraint
	ObjectFrame                      = ObjectKindFrame
	ObjectAction                     = ObjectKindAction
	ObjectOutcome                    = ObjectKindOutcome
	ObjectRelation                   = ObjectKindRelation
)

// Phase is the current kind of reasoning performed by CES.
type Phase string

const (
	PhaseTriage  Phase = "triage"
	PhaseAbduce  Phase = "abduce"
	PhaseFrame   Phase = "frame"
	PhaseExecute Phase = "execute"
	PhaseClose   Phase = "close"
)

// WorkStatus is the user task lifecycle, separate from QAC resource state.
type WorkStatus string

const (
	WorkActive     WorkStatus = "active"
	WorkComplete   WorkStatus = "complete"
	WorkIncomplete WorkStatus = "incomplete"
)

// OperatorView is the frozen, compact CES projection shown by Ghost's app.
// It intentionally excludes the active QAC resource.
type OperatorView struct {
	WorkID             string             `json:"work_id"`
	Goal               string             `json:"goal"`
	WorkStatus         WorkStatus         `json:"work_status"`
	Phase              Phase              `json:"phase"`
	ReopenCount        int                `json:"reopen_count"`
	LeadingHypothesis  *ObjectSummary     `json:"leading_hypothesis,omitempty"`
	ActiveFrame        *ObjectSummary     `json:"active_frame,omitempty"`
	OpenUnknownCount   int                `json:"open_unknown_count"`
	ContradictionCount int                `json:"contradiction_count"`
	LastTransition     *TransitionSummary `json:"last_transition,omitempty"`
}

// OperatorSupplement carries UX details outside the frozen CES read model.
type OperatorSupplement struct {
	TerminalReason    string
	InvalidatedPhases []Phase
}

// ObjectSummary contains the small amount of object data used for orientation.
type ObjectSummary struct {
	ID      ID     `json:"id"`
	Label   string `json:"label"`
	Summary string `json:"summary"`
	Status  string `json:"status"`
}

// TransitionSummary contains backend-selected routing and its reason.
type TransitionSummary struct {
	From       Phase      `json:"from"`
	To         Phase      `json:"to"`
	Reason     string     `json:"reason"`
	TargetID   ID         `json:"target_id,omitempty"`
	TargetKind ObjectKind `json:"target_kind,omitempty"`
}
