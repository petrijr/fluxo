package engine

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/petrijr/fluxo/internal/persistence"
	"github.com/petrijr/fluxo/pkg/api"
	"github.com/redis/go-redis/v9"
)

// engineImpl is a simple, synchronous, in-process engine implementation.
type engineImpl struct {
	workflows persistence.WorkflowStore
	instances persistence.InstanceStore

	mu       sync.Mutex // only for nextID
	nextID   int64
	observer api.Observer
}

// Config describes how to construct an engineImpl.
// Only used inside this package; external callers use the helper functions.
type Config struct {
	Persistence persistence.Persistence
	Observer    api.Observer
}

func NewInMemoryEngine() api.Engine {
	mem := persistence.NewInMemoryStore()
	return NewEngine(persistence.Persistence{
		Workflows: mem,
		Instances: mem,
	})
}

func NewSQLiteEngine(db *sql.DB) (api.Engine, error) {
	inst, err := persistence.NewSQLiteInstanceStore(db)
	if err != nil {
		return nil, err
	}
	// For now, workflow definitions remain in-memory.
	memWF := persistence.NewInMemoryStore()

	return NewEngine(persistence.Persistence{
		Workflows: memWF,
		Instances: inst,
	}), nil
}

func NewPostgresEngine(db *sql.DB) (api.Engine, error) {
	inst, err := persistence.NewPostgresInstanceStore(db)
	if err != nil {
		return nil, err
	}
	// For now, workflow definitions remain in-memory, just like SQLite.
	memWF := persistence.NewInMemoryStore()

	return NewEngine(persistence.Persistence{
		Workflows: memWF,
		Instances: inst,
	}), nil
}

// NewRedisEngine creates an engine that uses Redis for instance persistence
// and task queue (if your engine wiring supports pluggable queues).
func NewRedisEngine(client *redis.Client) api.Engine {
	instStore := persistence.NewRedisInstanceStore(client, "fluxo:")
	memWF := persistence.NewInMemoryStore()

	// If your engine has a way to pass the queue in, hook it up here.
	// For now, we only swap Instances and keep the same queue as before,
	// unless you already have queue pluggability wired.
	return NewEngine(persistence.Persistence{
		Workflows: memWF,
		Instances: instStore,
	})
}

// NewEngineWithConfig creates a new Engine using the given configuration.
func NewEngineWithConfig(cfg Config) api.Engine {
	obs := cfg.Observer
	if obs == nil {
		obs = api.NoopObserver{}
	}
	return &engineImpl{
		workflows: cfg.Persistence.Workflows,
		instances: cfg.Persistence.Instances,
		observer:  obs,
	}
}

// NewEngine returns an Engine backed by an in-memory registry and
// an in-memory persistence store. External users access this via fluxo.NewInMemoryEngine.
func NewEngine(p persistence.Persistence) api.Engine {
	return NewEngineWithConfig(Config{
		Persistence: p,
	})
}

func (e *engineImpl) RegisterWorkflow(def api.WorkflowDefinition) error {
	if def.Name == "" {
		return errors.New("workflow name is required")
	}
	if len(def.Steps) == 0 {
		return errors.New("workflow must have at least one step")
	}

	// Check for duplicates via the store.
	if existing, err := e.workflows.GetWorkflow(def.Name); err == nil && existing.Name != "" {
		return fmt.Errorf("workflow already registered: %s", def.Name)
	} else if err != nil && !errors.Is(err, persistence.ErrWorkflowNotFound) {
		// Unexpected store error.
		return err
	}

	return e.workflows.SaveWorkflow(def)
}

func (e *engineImpl) Run(ctx context.Context, name string, input any) (*api.WorkflowInstance, error) {
	def, err := e.workflows.GetWorkflow(name)
	if err != nil {
		if errors.Is(err, persistence.ErrWorkflowNotFound) {
			return nil, fmt.Errorf("unknown workflow: %s", name)
		}
		return nil, err
	}

	inst := &api.WorkflowInstance{
		ID:          e.nextInstanceID(),
		Name:        def.Name,
		Status:      api.StatusRunning,
		Input:       input,
		CurrentStep: 0,
	}

	// Notify observer that the instance has started.
	e.observer.OnWorkflowStart(ctx, inst)

	// Persist the instance as soon as it starts.
	if err := e.instances.SaveInstance(inst); err != nil {
		inst.Status = api.StatusFailed
		inst.Err = err
		// Saving failed – treat this as a workflow failure.
		e.observer.OnWorkflowFailed(ctx, inst, err)
		return inst, err
	}

	return e.executeSteps(ctx, def, inst, 0, input)
}

func (e *engineImpl) GetInstance(ctx context.Context, id string) (*api.WorkflowInstance, error) {
	inst, err := e.instances.GetInstance(id)
	if err != nil {
		if errors.Is(err, persistence.ErrInstanceNotFound) {
			return nil, fmt.Errorf("instance not found: %s", id)
		}
		return nil, err
	}
	return inst, nil
}

func (e *engineImpl) ListInstances(ctx context.Context, opts api.InstanceListOptions) ([]*api.WorkflowInstance, error) {
	filter := persistence.InstanceFilter{
		WorkflowName: opts.WorkflowName,
		Status:       opts.Status,
	}
	return e.instances.ListInstances(filter)
}

func (e *engineImpl) Resume(ctx context.Context, id string) (*api.WorkflowInstance, error) {
	inst, err := e.instances.GetInstance(id)
	if err != nil {
		if errors.Is(err, persistence.ErrInstanceNotFound) {
			return nil, fmt.Errorf("instance not found: %s", id)
		}
		return nil, err
	}

	if inst.Status != api.StatusFailed {
		return nil, fmt.Errorf("cannot resume instance %s in status %s", id, inst.Status)
	}

	def, err := e.workflows.GetWorkflow(inst.Name)
	if err != nil {
		if errors.Is(err, persistence.ErrWorkflowNotFound) {
			return nil, fmt.Errorf("workflow definition not found for instance %s (name=%s)", id, inst.Name)
		}
		return nil, err
	}

	// Reset runtime fields and replay from the beginning.
	inst.Status = api.StatusRunning
	inst.Err = nil
	inst.Output = nil
	inst.CurrentStep = 0

	if err := e.instances.UpdateInstance(inst); err != nil {
		return inst, err
	}

	return e.executeSteps(ctx, def, inst, 0, inst.Input)
}

func (e *engineImpl) Signal(ctx context.Context, id string, name string, payload any) (*api.WorkflowInstance, error) {
	inst, err := e.instances.GetInstance(id)
	if err != nil {
		if errors.Is(err, persistence.ErrInstanceNotFound) {
			return nil, fmt.Errorf("instance not found: %s", id)
		}
		return nil, err
	}

	if inst.Status != api.StatusWaiting {
		return nil, fmt.Errorf("cannot signal instance %s in status %s", id, inst.Status)
	}

	def, err := e.workflows.GetWorkflow(inst.Name)
	if err != nil {
		if errors.Is(err, persistence.ErrWorkflowNotFound) {
			return nil, fmt.Errorf("workflow definition not found for instance %s (name=%s)", id, inst.Name)
		}
		return nil, err
	}

	// Prepare signal payload as the new input to the waiting step.
	inst.Status = api.StatusRunning
	inst.Err = nil
	inst.Input = api.SignalPayload{
		Name: name,
		Data: payload,
	}

	if err := e.instances.UpdateInstance(inst); err != nil {
		return inst, err
	}

	// Resume from the waiting step.
	return e.executeSteps(ctx, def, inst, inst.CurrentStep, inst.Input)
}

func (e *engineImpl) nextInstanceID() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.nextID++
	return fmt.Sprintf("wf-%d", e.nextID)
}

func (e *engineImpl) executeSteps(
	ctx context.Context,
	def api.WorkflowDefinition,
	inst *api.WorkflowInstance,
	startIndex int,
	input any,
) (*api.WorkflowInstance, error) {
	current := input

	// Iterate through all steps
	for i := startIndex; i < len(def.Steps); i++ {
		step := def.Steps[i]

		inst.CurrentStep = i
		_ = e.instances.UpdateInstance(inst)

		// Determine max attempts for this step.
		maxAttempts := 1
		var (
			backoff    time.Duration // current backoff value
			maxBackoff time.Duration
			multiplier float64
		)

		if step.Retry != nil {
			if step.Retry.MaxAttempts > 0 {
				maxAttempts = step.Retry.MaxAttempts
			}
			// Determine initial backoff:
			//   1) Prefer InitialBackoff if set.
			//   2) Fall back to deprecated Backoff.
			backoff = step.Retry.InitialBackoff
			if backoff <= 0 {
				backoff = step.Retry.Backoff
			}

			maxBackoff = step.Retry.MaxBackoff

			// Backoff multiplier:
			//   - If explicitly set to > 0, use it.
			//   - Otherwise default to 2.0 (standard exponential backoff).
			multiplier = step.Retry.BackoffMultiplier
			if multiplier <= 0 {
				multiplier = 2.0
			}
		}

		var lastErr error

		for attempt := 1; attempt <= maxAttempts; attempt++ {
			select {
			case <-ctx.Done():
				inst.Status = api.StatusFailed
				inst.Err = ctx.Err()
				_ = e.instances.UpdateInstance(inst)
				e.observer.OnWorkflowFailed(ctx, inst, ctx.Err())
				return inst, ctx.Err()
			default:
			}

			// Attach engine to the context for this step invocation so that
			// StartChildrenStep / WaitForChildrenStep (and similar helpers)
			// can use api.EngineFromContext(ctx).
			stepCtx := api.WithEngine(ctx, e)

			startTime := time.Now()
			e.observer.OnStepStart(stepCtx, inst, step.Name, i)

			// Execute step
			next, err := step.Fn(stepCtx, current)

			duration := time.Since(startTime)
			e.observer.OnStepCompleted(stepCtx, inst, step.Name, i, err, duration)

			if err == nil {
				// Success: advance to next step with new value.
				current = next
				lastErr = nil
				break
			}

			// NEW: wait-for-signal special case (sigName = _)
			if _, ok := api.IsWaitForSignalError(err); ok {
				inst.Status = api.StatusWaiting
				inst.Err = nil
				// We could track sigName somewhere later if needed.
				_ = e.instances.UpdateInstance(inst)
				return inst, err
			}

			// NEW: wait-for-children special case (durable join)
			var waitChildren *api.WaitForChildrenError
			if errors.As(err, &waitChildren) {
				inst.Status = api.StatusWaiting
				inst.Err = nil
				_ = e.instances.UpdateInstance(inst)
				return inst, err
			}

			if api.IsWaitForAnyChildError(err) {
				inst.Status = api.StatusWaiting
				inst.Err = nil
				_ = e.instances.UpdateInstance(inst)
				return inst, err
			}

			lastErr = err

			// If this was the last allowed attempt, mark failed.
			if attempt == maxAttempts {
				inst.Status = api.StatusFailed
				inst.Err = lastErr
				_ = e.instances.UpdateInstance(inst)
				e.observer.OnWorkflowFailed(ctx, inst, lastErr)
				return inst, lastErr
			}

			// Wait before next attempt, if backoff is configured.
			if backoff > 0 {
				// Apply per-attempt delay with optional cap.
				delay := backoff
				if maxBackoff > 0 && delay > maxBackoff {
					delay = maxBackoff
				}

				select {
				case <-ctx.Done():
					inst.Status = api.StatusFailed
					inst.Err = ctx.Err()
					_ = e.instances.UpdateInstance(inst)
					return inst, ctx.Err()
				case <-time.After(delay):
					// continue to next attempt
				}

				// Increase backoff for the next retry.
				if multiplier > 0 {
					nextBackoff := time.Duration(float64(backoff) * multiplier)
					if maxBackoff > 0 && nextBackoff > maxBackoff {
						backoff = maxBackoff
					} else {
						backoff = nextBackoff
					}
				}
			}
		}
	}

	inst.Status = api.StatusCompleted
	inst.Output = current
	inst.CurrentStep = len(def.Steps)
	_ = e.instances.UpdateInstance(inst)

	e.observer.OnWorkflowCompleted(ctx, inst)

	return inst, nil
}
