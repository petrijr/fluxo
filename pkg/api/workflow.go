package api

import (
	"context"
	"encoding/gob"
	"errors"
	"fmt"
	"reflect"
	"sync"
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

// ConditionFunc decides whether a branch should be taken based on the
// current input. It must be deterministic.
type ConditionFunc func(input any) bool

// SelectorFunc picks a branch key from the input. It must be deterministic.
type SelectorFunc func(input any) string

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

// WaitForAnySignalStep returns a step that parks the workflow until a signal
// with any of the given names is delivered via Engine.Signal.
//
// Behavior:
//   - At first invocation (normal forward execution), it ignores its input and
//     returns NewWaitForSignalError(names[0]) which causes the engine to mark
//     the instance as WAITING.
//   - When resumed via Engine.Signal, the engine passes a SignalPayload as input
//     (Name, Data). If Name is in the allowed list, this step returns that
//     SignalPayload as its output and the workflow continues.
//   - If a signal with an unexpected name is delivered, the step will request
//     to wait again via NewWaitForSignalError(names[0]).
//
// This is useful for branching decisions such as approve/reject flows, where
// the next step can switch on the SignalPayload.Name to decide behavior.
func WaitForAnySignalStep(names ...string) StepFunc {
	if len(names) == 0 {
		// Degenerate case: behave like a wait-for-nothing; this is effectively
		// a bug in the workflow definition, but we avoid panicking here.
		return func(ctx context.Context, input any) (any, error) {
			return nil, NewWaitForSignalError("unknown")
		}
	}

	primary := names[0]
	allowed := make(map[string]struct{}, len(names))
	for _, n := range names {
		allowed[n] = struct{}{}
	}

	return func(ctx context.Context, input any) (any, error) {
		if sp, ok := input.(SignalPayload); ok {
			if _, ok := allowed[sp.Name]; ok {
				// Resumed with one of the expected signals: pass the payload
				// (including Name and Data) to the next step.
				return sp, nil
			}
		}
		// First invocation or unexpected signal: request to wait again.
		return nil, NewWaitForSignalError(primary)
	}
}

// IfStep returns a step that evaluates cond on the input and, depending
// on the result, runs thenStep or elseStep.
//
// The selected branch is executed as a nested step (i.e. the engine only
// sees this as a single step); retries/backoff for this step apply to the
// entire nested execution.
//
// If elseStep is nil, the input is passed through unchanged on the false
// branch.
func IfStep(cond ConditionFunc, thenStep StepFunc, elseStep StepFunc) StepFunc {
	return func(ctx context.Context, input any) (any, error) {
		if cond == nil {
			// Degenerate case: no condition; pass-through.
			return input, nil
		}

		if cond(input) {
			if thenStep == nil {
				return input, nil
			}
			return thenStep(ctx, input)
		}

		if elseStep == nil {
			return input, nil
		}
		return elseStep(ctx, input)
	}
}

// SwitchStep returns a step that selects one of several branches based on
// selector(input). The branches map holds per-key steps; if no branch
// matches, defaultStep is used (if non-nil), otherwise the input is
// passed through unchanged.
//
// As with IfStep, the chosen branch is executed as a nested step from
// the engine's perspective.
func SwitchStep(selector SelectorFunc, branches map[string]StepFunc, defaultStep StepFunc) StepFunc {
	return func(ctx context.Context, input any) (any, error) {
		if selector == nil {
			// No selector: just run default or pass-through.
			if defaultStep == nil {
				return input, nil
			}
			return defaultStep(ctx, input)
		}

		key := selector(input)
		if key != "" && branches != nil {
			if step, ok := branches[key]; ok && step != nil {
				return step(ctx, input)
			}
		}

		if defaultStep == nil {
			return input, nil
		}
		return defaultStep(ctx, input)
	}
}

// ParallelStep returns a StepFunc that runs multiple child steps in parallel
// and collects their outputs.
//
// Semantics:
//   - All non-nil child steps receive the same input value.
//   - The result is a []any whose length equals len(steps), preserving order.
//   - If any child step returns an error, the *first* error is returned and
//     no further processing is done after all goroutines finish.
//   - If ctx is cancelled, ctx.Err() is returned.
//
// NOTE: This is an in-process parallelism helper. The underlying engine still
// sees this as a single step, so retries/backoff apply to the whole parallel
// group as a unit.
func ParallelStep(steps ...StepFunc) StepFunc {
	return func(ctx context.Context, input any) (any, error) {
		if len(steps) == 0 {
			// Nothing to do, just pass the value through.
			return input, nil
		}

		type result struct {
			index int
			value any
			err   error
		}

		results := make([]any, len(steps))
		var wg sync.WaitGroup
		var firstErr error
		var errOnce sync.Once

		for i, step := range steps {
			i, step := i, step // capture

			if step == nil {
				// Treat nil child step as a no-op that passes the input through.
				results[i] = input
				continue
			}

			wg.Add(1)
			go func() {
				defer wg.Done()

				// Early abort if context already cancelled.
				select {
				case <-ctx.Done():
					errOnce.Do(func() {
						firstErr = ctx.Err()
					})
					return
				default:
				}

				out, err := step(ctx, input)
				if err != nil {
					errOnce.Do(func() {
						firstErr = err
					})
					return
				}
				results[i] = out
			}()
		}

		wg.Wait()

		if firstErr != nil {
			return nil, firstErr
		}

		return results, nil
	}
}

// ParallelMapStep returns a StepFunc that expects the input to be a slice or
// array and runs fn in parallel for each element.
//
// Input:
//   - A slice or array value (e.g. []T).
//
// Behavior:
//   - For each element input[i], fn(ctx, input[i]) is executed in its own goroutine.
//   - The outputs are collected into a []any of the same length, preserving
//     the original order.
//   - If any call returns an error, the first error is returned.
//   - If ctx is cancelled, ctx.Err() is returned.
//
// This is a convenient fan-out/fan-in helper for data-parallel workloads.
func ParallelMapStep(fn StepFunc) StepFunc {
	return func(ctx context.Context, input any) (any, error) {
		if fn == nil {
			return input, nil
		}

		v := reflect.ValueOf(input)
		kind := v.Kind()
		if kind != reflect.Slice && kind != reflect.Array {
			return nil, fmt.Errorf("ParallelMapStep expects slice or array input, got %T", input)
		}

		n := v.Len()
		if n == 0 {
			// Nothing to process.
			return []any{}, nil
		}

		results := make([]any, n)
		var wg sync.WaitGroup
		var firstErr error
		var errOnce sync.Once

		for i := 0; i < n; i++ {
			i := i
			item := v.Index(i).Interface()

			wg.Add(1)
			go func() {
				defer wg.Done()

				select {
				case <-ctx.Done():
					errOnce.Do(func() {
						firstErr = ctx.Err()
					})
					return
				default:
				}

				out, err := fn(ctx, item)
				if err != nil {
					errOnce.Do(func() {
						firstErr = err
					})
					return
				}
				results[i] = out
			}()
		}

		wg.Wait()

		if firstErr != nil {
			return nil, firstErr
		}

		return results, nil
	}
}

// StartChildrenStep returns a step that starts one or more child workflows
// and returns their instance IDs as []string.
//
// The specsFn callback is given the current input and must deterministically
// construct the list of child workflows to start.
func StartChildrenStep(specsFn func(input any) ([]ChildWorkflowSpec, error)) StepFunc {
	return func(ctx context.Context, input any) (any, error) {
		if specsFn == nil {
			return nil, errors.New("StartChildrenStep: specsFn must not be nil")
		}

		engine := EngineFromContext(ctx)
		if engine == nil {
			return nil, errors.New("StartChildrenStep: engine not available in context")
		}

		specs, err := specsFn(input)
		if err != nil {
			return nil, err
		}
		if len(specs) == 0 {
			return []string{}, nil
		}

		ids := make([]string, 0, len(specs))
		for _, s := range specs {
			if s.Name == "" {
				return nil, fmt.Errorf("StartChildrenStep: child workflow name is empty")
			}
			inst, err := engine.Run(ctx, s.Name, s.Input)
			if err != nil {
				return nil, fmt.Errorf("StartChildrenStep: starting child %q failed: %w", s.Name, err)
			}
			ids = append(ids, inst.ID)
		}

		return ids, nil
	}
}

// WaitForChildrenStep returns a step that waits until all specified child
// workflow instances have completed.
//
// getIDs extracts the child instance IDs from the input (for example, from
// the []string returned by StartChildrenStep).
//
// Durable semantics:
//   - Each invocation checks child states exactly once.
//   - If some children are still running/pending, the step returns a
//     WaitForChildrenError with a suggested PollAfter duration.
//   - The engine must treat WaitForChildrenError like a "WAITING" signal:
//     mark the instance as WAITING and schedule a resume after PollAfter.
//   - When the workflow is resumed, this step is re-invoked with the same
//     input, re-checks statuses, and eventually returns the children outputs
//     once all are completed.
func WaitForChildrenStep(
	getIDs func(input any) []string,
	pollInterval time.Duration,
) StepFunc {
	if pollInterval <= 0 {
		pollInterval = 500 * time.Millisecond
	}

	return func(ctx context.Context, input any) (any, error) {
		if getIDs == nil {
			return nil, errors.New("WaitForChildrenStep: getIDs must not be nil")
		}

		engine := EngineFromContext(ctx)
		if engine == nil {
			return nil, errors.New("WaitForChildrenStep: engine not available in context")
		}

		ids := getIDs(input)
		if len(ids) == 0 {
			return []any{}, nil
		}

		// Single poll pass: either we have all results, or we ask the engine
		// to park and recheck later.
		allDone := true
		outputs := make([]any, len(ids))

		for i, id := range ids {
			inst, err := engine.GetInstance(ctx, id)
			if err != nil {
				return nil, fmt.Errorf("WaitForChildrenStep: GetInstance(%q) failed: %w", id, err)
			}

			switch inst.Status {
			case StatusCompleted:
				outputs[i] = inst.Output
			case StatusFailed:
				if inst.Err != nil {
					return nil, fmt.Errorf("WaitForChildrenStep: child %s failed: %w", id, inst.Err)
				}
				return nil, fmt.Errorf("WaitForChildrenStep: child %s failed", id)
			default:
				allDone = false
			}
		}

		if allDone {
			return outputs, nil
		}

		// Not all children are finished yet – ask the engine to park & reschedule.
		return nil, &WaitForChildrenError{
			ChildIDs:  ids,
			PollAfter: pollInterval,
		}
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

// WaitForChildrenError is a sentinel error used by WaitForChildrenStep to
// tell the engine that the parent workflow should be parked (WAITING) and
// resumed later to re-check the child statuses.
//
// The engine is responsible for scheduling a resume after PollAfter.
type WaitForChildrenError struct {
	ChildIDs  []string
	PollAfter time.Duration
}

func (e *WaitForChildrenError) Error() string {
	return fmt.Sprintf("waiting for %d child workflows", len(e.ChildIDs))
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

// --- Child workflow support & engine-in-context helpers --- //

type ChildWorkflowSpec struct {
	// Name is the workflow definition name to start as a child.
	Name string
	// Input is the initial input for the child workflow.
	Input any
}

// engineContextKey is an unexported key type for storing the Engine in
// context. Using a dedicated type avoids collisions.
type engineContextKey struct{}

// WithEngine attaches an Engine to ctx so that StepFunc implementations can
// discover the engine (for example to start or inspect child workflows).
//
// This is called internally by the engine implementation before invoking
// step functions.
func WithEngine(ctx context.Context, e Engine) context.Context {
	if e == nil {
		return ctx
	}
	return context.WithValue(ctx, engineContextKey{}, e)
}

// EngineFromContext retrieves the Engine previously attached with WithEngine.
// It returns nil if no engine is present in ctx.
func EngineFromContext(ctx context.Context) Engine {
	if v := ctx.Value(engineContextKey{}); v != nil {
		if e, ok := v.(Engine); ok {
			return e
		}
	}
	return nil
}
