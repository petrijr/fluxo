package api

import (
	"context"
	"errors"
)

var ErrWorkflowDefinitionMismatch = errors.New("workflow definition mismatch")

// Engine is the high-level engine API (iteration 1: synchronous).
type Engine interface {
	// RegisterWorkflow registers a definition by name.
	RegisterWorkflow(def WorkflowDefinition) error

	// Run starts and runs the workflow to completion (synchronously).
	Run(ctx context.Context, name string, input any) (*WorkflowInstance, error)

	// RunVersion starts and runs a specific workflow version.
	RunVersion(ctx context.Context, name string, version string, input any) (*WorkflowInstance, error)

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

	// Signal delivers a named signal to a waiting workflow instance and
	// resumes it from the step that requested the signal.
	Signal(ctx context.Context, id string, name string, payload any) (*WorkflowInstance, error)

	// RecoverStuckInstances scans for in-flight workflow instances that are
	// still marked as StatusRunning (for example after a process crash) and
	// marks them as StatusFailed with a standard error message.
	//
	// It returns the number of instances it updated.
	//
	// This method is intended to be called on process startup *before*
	// starting workers or accepting new work, so that no instance is
	// legitimately running when it is executed.
	RecoverStuckInstances(ctx context.Context) (int, error)
}
