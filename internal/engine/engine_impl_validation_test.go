package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/petrijr/fluxo/pkg/api"
)

func dummyStep() api.StepFunc {
	return func(ctx context.Context, input any) (any, error) {
		return input, nil
	}
}

func TestRegisterWorkflowRequiresName(t *testing.T) {
	engine := NewInMemoryEngine()

	wf := api.WorkflowDefinition{
		Name: "",
		Steps: []api.StepDefinition{
			{Name: "step", Fn: dummyStep()},
		},
	}

	err := engine.RegisterWorkflow(wf)
	if err == nil {
		t.Fatalf("expected error for empty workflow name, got nil")
	}

	if !strings.Contains(err.Error(), "workflow name is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRegisterWorkflowRequiresAtLeastOneStep(t *testing.T) {
	engine := NewInMemoryEngine()

	wf := api.WorkflowDefinition{
		Name:  "no-steps",
		Steps: nil,
	}

	err := engine.RegisterWorkflow(wf)
	if err == nil {
		t.Fatalf("expected error for workflow without steps, got nil")
	}

	if !strings.Contains(err.Error(), "workflow must have at least one step") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRegisterWorkflowDuplicateNameFails(t *testing.T) {
	engine := NewInMemoryEngine()

	wf := api.WorkflowDefinition{
		Name: "duplicate",
		Steps: []api.StepDefinition{
			{Name: "step", Fn: dummyStep()},
		},
	}

	if err := engine.RegisterWorkflow(wf); err != nil {
		t.Fatalf("first RegisterWorkflow failed: %v", err)
	}

	err := engine.RegisterWorkflow(wf)
	if err == nil {
		t.Fatalf("expected error for duplicate workflow registration, got nil")
	}

	if !strings.Contains(err.Error(), "already registered") {
		t.Fatalf("unexpected error: %v", err)
	}
}
