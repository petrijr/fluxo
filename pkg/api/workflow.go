package api

import (
	"context"
	"encoding/gob"
	"errors"
	"time"
)

func init() {
	gob.Register(SignalPayload{})
	gob.Register(TimeoutPayload{})
}

// Status represents the lifecycle state of a workflow instance.
type Status string

const (
	StatusPending   Status = "PENDING"
	StatusRunning   Status = "RUNNING"
	StatusCompleted Status = "COMPLETED"
	StatusFailed    Status = "FAILED"
	StatusWaiting   Status = "WAITING"
)

// StepFunc is a single step in a workflow.
// Iteration 1: keep it simple with `any`, we can add generics later.
type StepFunc func(ctx context.Context, input any) (any, error)

// SleepStep returns a StepFunc that waits for the given duration
// before passing the input through unchanged.
//
// It is context-aware: if the context is cancelled during the sleep,
// it returns ctx.Err and the workflow will fail at this step.
func SleepStep(d time.Duration) StepFunc {
	return func(ctx context.Context, input any) (any, error) {
		if d <= 0 {
			return input, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(d):
			return input, nil
		}
	}
}

// SleepUntilStep returns a StepFunc that waits until the given deadline
// before passing the input through unchanged.
//
// If the deadline is in the past or equal to now, it returns immediately.
// It is context-aware: if the context is cancelled while waiting,
// it returns ctx.Err and the workflow will fail at this step.
func SleepUntilStep(deadline time.Time) StepFunc {
	return func(ctx context.Context, input any) (any, error) {
		now := time.Now()
		if !deadline.After(now) {
			return input, nil
		}
		d := time.Until(deadline)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(d):
			return input, nil
		}
	}
}

// WaitForSignalStep returns a step that parks the workflow until a signal
// with the given name is delivered via Engine.Signal.
//
// Semantics:
//   - First time the step runs, it ignores its input and returns
//     NewWaitForSignalError(name). The engine marks the instance as WAITING
//     and stops execution.
//   - When Engine.Signal is called, the engine resumes this step with
//     input set to SignalPayload{Name: name, Data: payload}. In that case,
//     the step returns payload (Data) as its output and the workflow
//     continues with the next step.
func WaitForSignalStep(name string) StepFunc {
	return func(ctx context.Context, input any) (any, error) {
		if sp, ok := input.(SignalPayload); ok && sp.Name == name {
			// Resumed with the expected signal; pass the data along.
			return sp.Data, nil
		}
		// First invocation or mismatched signal: request to wait.
		return nil, NewWaitForSignalError(name)
	}
}

// StepDefinition describes a named step.
type StepDefinition struct {
	Name  string
	Fn    StepFunc
	Retry *RetryPolicy
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

// RetryPolicy controls how a step is retried when it returns an error.
// MaxAttempts includes the first attempt. For example:
//
//	MaxAttempts = 1 => no retries (just the initial call)
//	MaxAttempts = 3 => initial call + up to 2 retries
//
// Backoff is the delay between failed attempts. It is not applied before
// the first attempt. If zero, retries happen immediately.
type RetryPolicy struct {
	MaxAttempts int
	Backoff     time.Duration
}

// SignalPayload is used by the engine to resume a workflow step that
// previously requested to wait for a signal.
type SignalPayload struct {
	Name string
	Data any
}

// TimeoutPayload is a special payload used for auto-expiry signals.
// Workflows can check for this type to detect that a wait timed out.
type TimeoutPayload struct {
	Reason string
}

// waitForSignalError is returned by steps that want to park the workflow
// until an external signal with the given name arrives.
type waitForSignalError struct {
	Name string
}

func (e *waitForSignalError) Error() string {
	return "waiting for signal: " + e.Name
}

// NewWaitForSignalError is primarily intended for use by helper step
// constructors (like WaitForSignalStep), but can be used by custom steps
// to integrate with the engine's signal semantics.
func NewWaitForSignalError(name string) error {
	return &waitForSignalError{Name: name}
}

// IsWaitForSignalError returns (signalName, true) if err indicates that
// the step wants to wait for a signal.
func IsWaitForSignalError(err error) (string, bool) {
	var w *waitForSignalError
	if errors.As(err, &w) {
		return w.Name, true
	}
	return "", false
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

	// Signal delivers a named signal to a waiting workflow instance and
	// resumes it from the step that requested the signal.
	Signal(ctx context.Context, id string, name string, payload any) (*WorkflowInstance, error)
}
