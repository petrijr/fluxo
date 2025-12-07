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
)

// engineImpl is a simple, synchronous, in-process engine implementation.
type engineImpl struct {
	workflows persistence.WorkflowStore
	instances persistence.InstanceStore

	mu     sync.Mutex // only for nextID
	nextID int64
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

// NewEngine returns an Engine backed by an in-memory registry and
// an in-memory persistence store. External users access this via fluxo.NewInMemoryEngine.
func NewEngine(p persistence.Persistence) api.Engine {
	return &engineImpl{
		workflows: p.Workflows,
		instances: p.Instances,
	}
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

	// Persist the instance as soon as it starts.
	if err := e.instances.SaveInstance(inst); err != nil {
		inst.Status = api.StatusFailed
		inst.Err = err
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

	for i := startIndex; i < len(def.Steps); i++ {
		step := def.Steps[i]

		inst.CurrentStep = i
		_ = e.instances.UpdateInstance(inst)

		// Determine max attempts for this step.
		maxAttempts := 1
		var backoff time.Duration
		if step.Retry != nil {
			if step.Retry.MaxAttempts > 0 {
				maxAttempts = step.Retry.MaxAttempts
			}
			backoff = step.Retry.Backoff
		}

		var lastErr error

		for attempt := 1; attempt <= maxAttempts; attempt++ {
			select {
			case <-ctx.Done():
				inst.Status = api.StatusFailed
				inst.Err = ctx.Err()
				_ = e.instances.UpdateInstance(inst)
				return inst, ctx.Err()
			default:
			}

			next, err := step.Fn(ctx, current)
			if err == nil {
				// Success: advance to next step with new value.
				current = next
				lastErr = nil
				break
			}

			lastErr = err

			// If this was the last allowed attempt, mark failed.
			if attempt == maxAttempts {
				inst.Status = api.StatusFailed
				inst.Err = lastErr
				_ = e.instances.UpdateInstance(inst)
				return inst, lastErr
			}

			// Wait before next attempt, if backoff is configured.
			if backoff > 0 {
				select {
				case <-ctx.Done():
					inst.Status = api.StatusFailed
					inst.Err = ctx.Err()
					_ = e.instances.UpdateInstance(inst)
					return inst, ctx.Err()
				case <-time.After(backoff):
					// continue to next attempt
				}
			}
		}
	}

	inst.Status = api.StatusCompleted
	inst.Output = current
	inst.CurrentStep = len(def.Steps)
	_ = e.instances.UpdateInstance(inst)

	return inst, nil
}
