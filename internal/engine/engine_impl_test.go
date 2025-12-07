package engine

import (
	"context"
	"fmt"
	"testing"

	"github.com/petrijr/fluxo/pkg/api"
)

type OnboardingInput struct {
	Email string
}

type UserRecord struct {
	ID    string
	Email string
}

type ProvisionedResult struct {
	UserID string
	Env    string
}

func TestSequentialWorkflowCompletes(t *testing.T) {
	ctx := context.Background()

	engine := NewInMemoryEngine()

	wf := api.WorkflowDefinition{
		Name: "onboarding",
		Steps: []api.StepDefinition{
			{
				Name: "create-user",
				Fn: func(ctx context.Context, input any) (any, error) {
					in, ok := input.(OnboardingInput)
					if !ok {
						return nil, fmt.Errorf("expected OnboardingInput, got %T", input)
					}
					return UserRecord{
						ID:    "user-123",
						Email: in.Email,
					}, nil
				},
			},
			{
				Name: "provision",
				Fn: func(ctx context.Context, input any) (any, error) {
					user, ok := input.(UserRecord)
					if !ok {
						return nil, fmt.Errorf("expected UserRecord, got %T", input)
					}
					return ProvisionedResult{
						UserID: user.ID,
						Env:    "dev",
					}, nil
				},
			},
		},
	}

	if err := engine.RegisterWorkflow(wf); err != nil {
		t.Fatalf("RegisterWorkflow failed: %v", err)
	}

	inst, err := engine.Run(ctx, "onboarding", OnboardingInput{Email: "alice@example.com"})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if inst.Status != api.StatusCompleted {
		t.Fatalf("expected workflow status %q, got %q", api.StatusCompleted, inst.Status)
	}

	res, ok := inst.Output.(ProvisionedResult)
	if !ok {
		t.Fatalf("expected ProvisionedResult output, got %T", inst.Output)
	}

	if res.UserID != "user-123" || res.Env != "dev" {
		t.Fatalf("unexpected result: %+v", res)
	}
}

func TestWorkflowFailsOnStepError(t *testing.T) {
	ctx := context.Background()
	engine := NewInMemoryEngine()

	wf := api.WorkflowDefinition{
		Name: "failing",
		Steps: []api.StepDefinition{
			{
				Name: "fail-step",
				Fn: func(ctx context.Context, input any) (any, error) {
					return nil, fmt.Errorf("boom")
				},
			},
		},
	}

	if err := engine.RegisterWorkflow(wf); err != nil {
		t.Fatalf("RegisterWorkflow failed: %v", err)
	}

	inst, err := engine.Run(ctx, "failing", nil)
	if err == nil {
		t.Fatalf("expected Run to return error")
	}
	if inst == nil {
		t.Fatalf("expected WorkflowInstance, got nil")
	}
	if inst.Status != api.StatusFailed {
		t.Fatalf("expected status FAILED, got %q", inst.Status)
	}
}

func TestGetInstanceReturnsCompletedInstance(t *testing.T) {
	ctx := context.Background()
	engine := NewInMemoryEngine()

	wf := api.WorkflowDefinition{
		Name: "simple",
		Steps: []api.StepDefinition{
			{
				Name: "echo",
				Fn: func(ctx context.Context, input any) (any, error) {
					return fmt.Sprintf("echo:%v", input), nil
				},
			},
		},
	}

	if err := engine.RegisterWorkflow(wf); err != nil {
		t.Fatalf("RegisterWorkflow failed: %v", err)
	}

	startInst, err := engine.Run(ctx, "simple", "hello")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if startInst.ID == "" {
		t.Fatalf("expected non-empty instance ID")
	}

	got, err := engine.GetInstance(ctx, startInst.ID)
	if err != nil {
		t.Fatalf("GetInstance failed: %v", err)
	}

	if got.ID != startInst.ID {
		t.Fatalf("expected instance ID %q, got %q", startInst.ID, got.ID)
	}
	if got.Status != api.StatusCompleted {
		t.Fatalf("expected status COMPLETED, got %q", got.Status)
	}
	if got.Output != "echo:hello" {
		t.Fatalf("unexpected output: %v", got.Output)
	}
}

func TestGetInstanceUnknownIDReturnsError(t *testing.T) {
	ctx := context.Background()
	engine := NewInMemoryEngine()

	_, err := engine.GetInstance(ctx, "does-not-exist")
	if err == nil {
		t.Fatalf("expected error for unknown instance ID")
	}
}
