package engine

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/petrijr/fluxo/internal/persistence"
	"github.com/petrijr/fluxo/pkg/api"
)

// fakeObserver records all calls from the engine so we can assert on them.
type fakeObserver struct {
	mu sync.Mutex

	workflowStarts    []workflowEvent
	workflowCompletes []workflowEvent
	workflowFails     []workflowEvent

	stepStarts    []stepEvent
	stepCompletes []stepEvent
}

type workflowEvent struct {
	Workflow   string
	InstanceID string
	Err        error
}

type stepEvent struct {
	Workflow   string
	InstanceID string
	StepName   string
	StepIndex  int
	Err        error
	Duration   time.Duration
}

func (o *fakeObserver) OnWorkflowStart(ctx context.Context, inst *api.WorkflowInstance) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.workflowStarts = append(o.workflowStarts, workflowEvent{
		Workflow:   inst.Name,
		InstanceID: inst.ID,
	})
}

func (o *fakeObserver) OnWorkflowCompleted(ctx context.Context, inst *api.WorkflowInstance) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.workflowCompletes = append(o.workflowCompletes, workflowEvent{
		Workflow:   inst.Name,
		InstanceID: inst.ID,
	})
}

func (o *fakeObserver) OnWorkflowFailed(ctx context.Context, inst *api.WorkflowInstance, err error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.workflowFails = append(o.workflowFails, workflowEvent{
		Workflow:   inst.Name,
		InstanceID: inst.ID,
		Err:        err,
	})
}

func (o *fakeObserver) OnStepStart(ctx context.Context, inst *api.WorkflowInstance, stepName string, idx int) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.stepStarts = append(o.stepStarts, stepEvent{
		Workflow:   inst.Name,
		InstanceID: inst.ID,
		StepName:   stepName,
		StepIndex:  idx,
	})
}

func (o *fakeObserver) OnStepCompleted(ctx context.Context, inst *api.WorkflowInstance, stepName string, idx int, err error, d time.Duration) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.stepCompletes = append(o.stepCompletes, stepEvent{
		Workflow:   inst.Name,
		InstanceID: inst.ID,
		StepName:   stepName,
		StepIndex:  idx,
		Err:        err,
		Duration:   d,
	})
}

// --- Tests ---

func TestObserverHooksOnSuccessfulWorkflow(t *testing.T) {
	mem := persistence.NewInMemoryStore()

	obs := &fakeObserver{}

	eng := NewEngineWithConfig(Config{
		Persistence: persistence.Persistence{
			Workflows: mem,
			Instances: mem,
		},
		Observer: obs,
	})

	def := api.WorkflowDefinition{
		Name: "observer-success",
		Steps: []api.StepDefinition{
			{
				Name: "step-1",
				Fn: func(ctx context.Context, input any) (any, error) {
					v, ok := input.(int)
					if !ok {
						return nil, errors.New("step-1: expected int input")
					}
					return v + 1, nil
				},
			},
			{
				Name: "step-2",
				Fn: func(ctx context.Context, input any) (any, error) {
					v, ok := input.(int)
					if !ok {
						return nil, errors.New("step-2: expected int input")
					}
					return v * 2, nil
				},
			},
		},
	}

	if err := eng.RegisterWorkflow(def); err != nil {
		t.Fatalf("RegisterWorkflow: %v", err)
	}

	ctx := context.Background()
	inst, err := eng.Run(ctx, def.Name, 1)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if inst.Status != api.StatusCompleted {
		t.Fatalf("expected StatusCompleted, got %v", inst.Status)
	}

	out, ok := inst.Output.(int)
	if !ok {
		t.Fatalf("expected int output, got %T (%v)", inst.Output, inst.Output)
	}
	if out != 4 {
		t.Fatalf("expected output 4, got %d", out)
	}

	// Assertions on observer calls.
	obs.mu.Lock()
	defer obs.mu.Unlock()

	if len(obs.workflowStarts) != 1 {
		t.Fatalf("expected 1 workflow start, got %d", len(obs.workflowStarts))
	}
	if len(obs.workflowCompletes) != 1 {
		t.Fatalf("expected 1 workflow complete, got %d", len(obs.workflowCompletes))
	}
	if len(obs.workflowFails) != 0 {
		t.Fatalf("expected 0 workflow fails, got %d", len(obs.workflowFails))
	}

	if len(obs.stepStarts) != 2 {
		t.Fatalf("expected 2 step starts, got %d", len(obs.stepStarts))
	}
	if len(obs.stepCompletes) != 2 {
		t.Fatalf("expected 2 step completes, got %d", len(obs.stepCompletes))
	}

	// Check ordering and names.
	if obs.stepStarts[0].StepName != "step-1" || obs.stepStarts[0].StepIndex != 0 {
		t.Fatalf("first stepStart = (%s,%d), want (step-1,0)",
			obs.stepStarts[0].StepName, obs.stepStarts[0].StepIndex)
	}
	if obs.stepStarts[1].StepName != "step-2" || obs.stepStarts[1].StepIndex != 1 {
		t.Fatalf("second stepStart = (%s,%d), want (step-2,1)",
			obs.stepStarts[1].StepName, obs.stepStarts[1].StepIndex)
	}

	if obs.stepCompletes[0].StepName != "step-1" || obs.stepCompletes[0].StepIndex != 0 {
		t.Fatalf("first stepCompleted = (%s,%d), want (step-1,0)",
			obs.stepCompletes[0].StepName, obs.stepCompletes[0].StepIndex)
	}
	if obs.stepCompletes[0].Err != nil {
		t.Fatalf("expected first stepCompleted error nil, got %v", obs.stepCompletes[0].Err)
	}
	if obs.stepCompletes[1].StepName != "step-2" || obs.stepCompletes[1].StepIndex != 1 {
		t.Fatalf("second stepCompleted = (%s,%d), want (step-2,1)",
			obs.stepCompletes[1].StepName, obs.stepCompletes[1].StepIndex)
	}
	if obs.stepCompletes[1].Err != nil {
		t.Fatalf("expected second stepCompleted error nil, got %v", obs.stepCompletes[1].Err)
	}

	// Durations should be non-negative and typically > 0; we only check non-negative.
	for i, ev := range obs.stepCompletes {
		if ev.Duration < 0 {
			t.Fatalf("stepCompletes[%d].Duration is negative: %s", i, ev.Duration)
		}
	}

	// Workflow IDs should match.
	start := obs.workflowStarts[0]
	complete := obs.workflowCompletes[0]
	if start.InstanceID != inst.ID || complete.InstanceID != inst.ID {
		t.Fatalf("observer instance IDs mismatch: start=%s complete=%s inst=%s",
			start.InstanceID, complete.InstanceID, inst.ID)
	}
}

func TestObserverHooksOnFailedWorkflow(t *testing.T) {
	mem := persistence.NewInMemoryStore()

	obs := &fakeObserver{}

	eng := NewEngineWithConfig(Config{
		Persistence: persistence.Persistence{
			Workflows: mem,
			Instances: mem,
		},
		Observer: obs,
	})

	expectedErr := errors.New("boom")

	def := api.WorkflowDefinition{
		Name: "observer-fail",
		Steps: []api.StepDefinition{
			{
				Name: "ok-step",
				Fn: func(ctx context.Context, input any) (any, error) {
					return input, nil
				},
			},
			{
				Name: "failing-step",
				Fn: func(ctx context.Context, input any) (any, error) {
					return nil, expectedErr
				},
				// Ensure we only attempt once to keep expectations simple.
				Retry: &api.RetryPolicy{
					MaxAttempts: 1,
					Backoff:     0,
				},
			},
		},
	}

	if err := eng.RegisterWorkflow(def); err != nil {
		t.Fatalf("RegisterWorkflow: %v", err)
	}

	ctx := context.Background()
	inst, err := eng.Run(ctx, def.Name, 1)
	if err == nil {
		t.Fatalf("expected Run to fail, got nil error")
	}
	if inst.Status != api.StatusFailed {
		t.Fatalf("expected StatusFailed, got %v", inst.Status)
	}

	obs.mu.Lock()
	defer obs.mu.Unlock()

	if len(obs.workflowStarts) != 1 {
		t.Fatalf("expected 1 workflow start, got %d", len(obs.workflowStarts))
	}
	if len(obs.workflowCompletes) != 0 {
		t.Fatalf("expected 0 workflow completes, got %d", len(obs.workflowCompletes))
	}
	if len(obs.workflowFails) != 1 {
		t.Fatalf("expected 1 workflow fail, got %d", len(obs.workflowFails))
	}

	failEv := obs.workflowFails[0]
	if failEv.InstanceID != inst.ID {
		t.Fatalf("workflowFails instance ID = %s, want %s", failEv.InstanceID, inst.ID)
	}
	if failEv.Err == nil {
		t.Fatalf("expected non-nil error in workflowFails")
	}

	// We expect both steps to have started, and both to have completed (second with error).
	if len(obs.stepStarts) != 2 {
		t.Fatalf("expected 2 step starts, got %d", len(obs.stepStarts))
	}
	if len(obs.stepCompletes) != 2 {
		t.Fatalf("expected 2 step completes, got %d", len(obs.stepCompletes))
	}

	if obs.stepCompletes[1].StepName != "failing-step" {
		t.Fatalf("second stepCompleted.StepName = %s, want failing-step", obs.stepCompletes[1].StepName)
	}
	if obs.stepCompletes[1].Err == nil {
		t.Fatalf("expected error for failing-step completion, got nil")
	}
}
