package engine

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/petrijr/fluxo/pkg/api"
)

func TestRunHonorsCancellationBeforeFirstStep(t *testing.T) {
	engine := NewInMemoryEngine()

	var called int32

	wf := api.WorkflowDefinition{
		Name: "cancel-before",
		Steps: []api.StepDefinition{
			{
				Name: "should-not-run",
				Fn: func(ctx context.Context, input any) (any, error) {
					atomic.StoreInt32(&called, 1)
					return nil, nil
				},
			},
		},
	}

	if err := engine.RegisterWorkflow(wf); err != nil {
		t.Fatalf("RegisterWorkflow failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel *before* running

	inst, err := engine.Run(ctx, "cancel-before", nil)
	if err == nil {
		t.Fatalf("expected error due to cancellation, got nil")
	}
	if inst == nil {
		t.Fatalf("expected a WorkflowInstance, got nil")
	}
	if inst.Status != api.StatusFailed {
		t.Fatalf("expected status FAILED, got %q", inst.Status)
	}

	if atomic.LoadInt32(&called) != 0 {
		t.Fatalf("expected step not to be called when context is cancelled before run")
	}
}

func TestRunCancelsLongRunningStepViaContext(t *testing.T) {
	engine := NewInMemoryEngine()

	wf := api.WorkflowDefinition{
		Name: "cancel-during-step",
		Steps: []api.StepDefinition{
			{
				Name: "long-step",
				Fn: func(ctx context.Context, input any) (any, error) {
					// Simulate long work that cooperates with context.
					select {
					case <-ctx.Done():
						return nil, ctx.Err()
					case <-time.After(100 * time.Millisecond):
						return "done", nil
					}
				},
			},
		},
	}

	if err := engine.RegisterWorkflow(wf); err != nil {
		t.Fatalf("RegisterWorkflow failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Cancel shortly after starting Run.
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	inst, err := engine.Run(ctx, "cancel-during-step", nil)
	if err == nil {
		t.Fatalf("expected error due to cancellation, got nil")
	}
	if inst == nil {
		t.Fatalf("expected WorkflowInstance, got nil")
	}
	if inst.Status != api.StatusFailed {
		t.Fatalf("expected status FAILED, got %q", inst.Status)
	}
	if inst.Err == nil {
		t.Fatalf("expected instance error to be set")
	}
	if inst.Err != context.Canceled {
		t.Fatalf("expected context.Canceled, got %v", inst.Err)
	}
}
