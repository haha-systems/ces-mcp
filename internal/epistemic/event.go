package epistemic

import (
	"encoding/json"
	"time"
)

type EventKind string

const (
	EventTaskCreated            EventKind = "TaskCreated"
	EventObjectCreated          EventKind = "ObjectCreated"
	EventObjectStatusChanged    EventKind = "ObjectStatusChanged"
	EventUnknownStatusChanged   EventKind = "UnknownStatusChanged"
	EventFrameStatusChanged     EventKind = "FrameStatusChanged"
	EventRelationCreated        EventKind = "RelationCreated"
	EventRelationStatusChanged  EventKind = "RelationStatusChanged"
	EventPhaseChanged           EventKind = "PhaseChanged"
	EventWorkStatusChanged      EventKind = "WorkStatusChanged"
	EventTerminalReasonRecorded EventKind = "TerminalReasonRecorded"
)

type Event struct {
	Sequence   uint64          `json:"sequence"`
	Kind       EventKind       `json:"kind"`
	WorkID     string          `json:"work_id"`
	ObjectKind ObjectKind      `json:"object_kind"`
	ObjectID   ID              `json:"object_id"`
	RevisionID ID              `json:"revision_id"`
	At         time.Time       `json:"at"`
	Payload    json.RawMessage `json:"payload"`
}

type ObjectCreatedPayload struct {
	Kind   ObjectKind      `json:"kind"`
	Object json.RawMessage `json:"object"`
}

type objectStatusPayload struct {
	Kind ObjectKind `json:"kind"`
	ID   ID         `json:"id"`
	From string     `json:"from"`
	To   string     `json:"to"`
}

type relationStatusPayload struct {
	ID        ID             `json:"id"`
	From      RelationStatus `json:"from"`
	To        RelationStatus `json:"to"`
	ReasonRef ID             `json:"reason_ref"`
}

type phasePayload struct {
	From        Phase      `json:"from"`
	To          Phase      `json:"to"`
	Reason      string     `json:"reason"`
	TargetID    ID         `json:"target_id,omitempty"`
	TargetKind  ObjectKind `json:"target_kind,omitempty"`
	ReopenCount int        `json:"reopen_count"`
}

type workStatusPayload struct {
	From   WorkStatus `json:"from"`
	To     WorkStatus `json:"to"`
	Reason string     `json:"reason,omitempty"`
}

type SemanticEventKind string

const (
	SemanticPhaseChanged            SemanticEventKind = "PhaseChanged"
	SemanticHypothesisBecameLeading SemanticEventKind = "HypothesisBecameLeading"
	SemanticHypothesisRejected      SemanticEventKind = "HypothesisRejected"
	SemanticFrameActivated          SemanticEventKind = "FrameActivated"
	SemanticContradictionRecorded   SemanticEventKind = "ContradictionRecorded"
	SemanticWorkReopened            SemanticEventKind = "WorkReopened"
	SemanticWorkCompleted           SemanticEventKind = "WorkCompleted"
	SemanticWorkIncomplete          SemanticEventKind = "WorkIncomplete"
)

type SemanticEvent struct {
	Kind       SemanticEventKind
	At         time.Time
	WorkID     string
	From, To   Phase
	ObjectID   ID
	ObjectKind ObjectKind
	Reason     string
}
