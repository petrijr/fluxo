package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/petrijr/fluxo/pkg/api"
)

func TestStepLevelIdempotency_SkipSuccessfulStepsOnResume(t *testing.T) {
	e := NewInMemoryEngine()

	var step1Count int
	var step2Count int

	def := api.WorkflowDefinition{
		Name: "idempotency-test",
		Steps: []api.StepDefinition{
			{
				Name: "step-1",
				Fn: func(ctx context.Context, input any) (any, error) {
					step1Count++
					// Input is an int; increment by 1.
					n, ok := input.(int)
					if !ok {
						return nil, errors.New("step-1: expected int input")
					}
					return n + 1, nil
				},
			},
			{
				Name: "step-2",
				Fn: func(ctx context.Context, input any) (any, error) {
					step2Count++
					// First time we run this step, fail deliberately.
					if step2Count == 1 {
						return nil, errors.New("step-2: simulated failure")
					}
					// On subsequent attempts, succeed and pass value through.
					return input, nil
				},
			},
		},
	}

	if err := e.RegisterWorkflow(def); err != nil {
		t.Fatalf("RegisterWorkflow: %v", err)
	}

	ctx := context.Background()

	// First run: expect failure at step-2.
	inst, err := e.Run(ctx, def.Name, 10)
	if err == nil {
		t.Fatalf("expected first Run to fail, got nil error")
	}
	if inst.Status != api.StatusFailed {
		t.Fatalf("expected StatusFailed after first run, got %v", inst.Status)
	}
	if step1Count != 1 {
		t.Fatalf("expected step-1 to run once on first run, got %d", step1Count)
	}
	if step2Count != 1 {
		t.Fatalf("expected step-2 to run once on first run, got %d", step2Count)
	}

	// Now resume the failed instance.
	resumed, err := e.Resume(ctx, inst.ID)
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if resumed.Status != api.StatusCompleted {
		t.Fatalf("expected StatusCompleted after resume, got %v", resumed.Status)
	}

	// Idempotency expectations:
	//   - step-1 SHOULD NOT run again on Resume (its output should be cached).
	//   - step-2 SHOULD run again (it failed previously) and succeed this time.
	if step1Count != 1 {
		t.Fatalf("expected step-1 to run only once across Run+Resume, got %d", step1Count)
	}
	if step2Count != 2 {
		t.Fatalf("expected step-2 to run twice across Run+Resume, got %d", step2Count)
	}

	// Also check that data flow is still correct:
	// Input: 10 -> step-1 (+1) -> 11, then step-2 returns that value.
	out, ok := resumed.Output.(int)
	if !ok {
		t.Fatalf("expected int output, got %T (%v)", resumed.Output, resumed.Output)
	}
	if out != 11 {
		t.Fatalf("expected final output 11, got %d", out)
	}
}
