package api

import (
	"context"
)

// Status represents the lifecycle state of a workflow instance.
type Status string

const (
	StatusPending   Status = "PENDING"
	StatusRunning   Status = "RUNNING"
	StatusCompleted Status = "COMPLETED"
	StatusFailed    Status = "FAILED"
)

// StepFunc is a single step in a workflow.
// Iteration 1: keep it simple with `any`, we can add generics later.
type StepFunc func(ctx context.Context, input any) (any, error)

// StepDefinition describes a named step.
type StepDefinition struct {
	Name string
	Fn   StepFunc
}

// WorkflowDefinition describes a workflow as a sequence of steps.
type WorkflowDefinition struct {
	Name  string
	Steps []StepDefinition
}

// WorkflowInstance holds the result of a run.
type WorkflowInstance struct {
	ID     string
	Name   string
	Status Status
	Output any
	Err    error

	// Input is the original input provided to Run when this instance
	// was first started. It is used for deterministic replay on resume.
	Input any

	// CurrentStep tracks progress through the workflow steps.
	// Semantics:
	//   - Before any steps run: 0 (default)
	//   - While running step i: i
	//   - After successful completion: len(steps)
	//   - On failure: index of the step that failed (or was cancelled).
	CurrentStep int
}

// InstanceListOptions controls how instances are listed.
// Zero values mean "no filter" for that field.
type InstanceListOptions struct {
	// WorkflowName, if non-empty, limits results to instances of the given workflow.
	WorkflowName string

	// Status, if non-empty, limits results to instances with the given status.
	Status Status
}

// Engine is the high-level engine API (iteration 1: synchronous).
type Engine interface {
	// RegisterWorkflow registers a definition by name.
	RegisterWorkflow(def WorkflowDefinition) error

	// Run starts and runs the workflow to completion (synchronously).
	Run(ctx context.Context, name string, input any) (*WorkflowInstance, error)

	// GetInstance looks up a workflow instance by ID.
	// Returns an error if the instance is not found.
	GetInstance(ctx context.Context, id string) (*WorkflowInstance, error)

	// ListInstances returns workflow instances matching the given options.
	// If options are zero-valued, all instances are returned.
	ListInstances(ctx context.Context, opts InstanceListOptions) ([]*WorkflowInstance, error)

	// Resume restarts a previously failed workflow instance.
	// Semantics (first iteration):
	//   - Only FAILED instances can be resumed.
	//   - The instance is replayed from the beginning using its stored Input.
	//   - The same instance ID is reused; Status/Err/Output/CurrentStep are updated.
	Resume(ctx context.Context, id string) (*WorkflowInstance, error)
}
