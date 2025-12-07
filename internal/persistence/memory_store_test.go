package persistence

import (
	"context"
	"testing"

	"github.com/petrijr/fluxo/pkg/api"
)

func TestInMemoryStore_SaveAndGetWorkflow(t *testing.T) {
	store := NewInMemoryStore()

	def := api.WorkflowDefinition{
		Name: "test-wf",
		Steps: []api.StepDefinition{
			{Name: "step-1", Fn: func(ctx context.Context, input any) (any, error) {
				return input, nil
			}},
		},
	}

	if err := store.SaveWorkflow(def); err != nil {
		t.Fatalf("SaveWorkflow failed: %v", err)
	}

	got, err := store.GetWorkflow("test-wf")
	if err != nil {
		t.Fatalf("GetWorkflow failed: %v", err)
	}

	if got.Name != def.Name {
		t.Fatalf("expected workflow name %q, got %q", def.Name, got.Name)
	}
	if len(got.Steps) != 1 || got.Steps[0].Name != "step-1" {
		t.Fatalf("unexpected workflow steps: %+v", got.Steps)
	}
}

func TestInMemoryStore_GetWorkflowNotFound(t *testing.T) {
	store := NewInMemoryStore()

	_, err := store.GetWorkflow("does-not-exist")
	if err == nil {
		t.Fatalf("expected error for missing workflow")
	}
	if err != ErrWorkflowNotFound {
		t.Fatalf("expected ErrWorkflowNotFound, got %v", err)
	}
}

func TestInMemoryStore_SaveUpdateAndGetInstance(t *testing.T) {
	store := NewInMemoryStore()

	inst := &api.WorkflowInstance{
		ID:     "wf-1",
		Name:   "test-wf",
		Status: api.StatusRunning,
	}

	if err := store.SaveInstance(inst); err != nil {
		t.Fatalf("SaveInstance failed: %v", err)
	}

	// Update status and output.
	inst.Status = api.StatusCompleted
	inst.Output = "result"

	if err := store.UpdateInstance(inst); err != nil {
		t.Fatalf("UpdateInstance failed: %v", err)
	}

	got, err := store.GetInstance("wf-1")
	if err != nil {
		t.Fatalf("GetInstance failed: %v", err)
	}

	if got.ID != "wf-1" {
		t.Fatalf("expected ID wf-1, got %q", got.ID)
	}
	if got.Status != api.StatusCompleted {
		t.Fatalf("expected status COMPLETED, got %q", got.Status)
	}
	if got.Output != "result" {
		t.Fatalf("unexpected output: %v", got.Output)
	}
}

func TestInMemoryStore_GetInstanceNotFound(t *testing.T) {
	store := NewInMemoryStore()

	_, err := store.GetInstance("does-not-exist")
	if err == nil {
		t.Fatalf("expected error for missing instance")
	}
	if err != ErrInstanceNotFound {
		t.Fatalf("expected ErrInstanceNotFound, got %v", err)
	}
}
