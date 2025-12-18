package api

import "time"

// EventType identifies a workflow history event.
type EventType string

const (
	EventWorkflowEnqueued  EventType = "workflow.enqueued"
	EventWorkflowStarted   EventType = "workflow.started"
	EventWorkflowResumed   EventType = "workflow.resumed"
	EventWorkflowWaiting   EventType = "workflow.waiting"
	EventWorkflowCompleted EventType = "workflow.completed"
	EventWorkflowFailed    EventType = "workflow.failed"

	EventSignalReceived EventType = "signal.received"

	EventStepStarted   EventType = "step.started"
	EventStepCompleted EventType = "step.completed"
	EventStepFailed    EventType = "step.failed"
)

// WorkflowEvent is a minimal append-only history record for audit/debugging.
// It is intentionally small and stable; richer history can be layered later.
type WorkflowEvent struct {
	InstanceID string
	At         time.Time
	Type       EventType

	// Optional context.
	WorkflowName    string
	WorkflowVersion string
	Step            int

	// Small, human-oriented details (e.g. signal name, error string).
	// Keep this low-volume: do NOT dump large payloads here.
	Detail string
}
