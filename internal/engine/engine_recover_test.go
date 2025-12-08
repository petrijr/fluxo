package engine

import (
	"context"
	"testing"

	"github.com/petrijr/fluxo/internal/persistence"
	"github.com/petrijr/fluxo/pkg/api"
)

func TestRecoverStuckInstances_MarksRunningAsFailed(t *testing.T) {
	mem := persistence.NewInMemoryStore()

	eng := NewEngineWithConfig(Config{
		Persistence: persistence.Persistence{
			Workflows: mem,
			Instances: mem,
		},
		Observer: api.NoopObserver{},
	}).(*engineImpl)

	// Create a "stuck" running instance directly in the store.
	stuck := &api.WorkflowInstance{
		ID:          "inst-stuck",
		Name:        "wf-stuck",
		Status:      api.StatusRunning,
		Input:       42,
		CurrentStep: 1,
		StepResults: nil,
	}

	if err := mem.SaveInstance(stuck); err != nil {
		t.Fatalf("SaveInstance: %v", err)
	}

	// Also create a completed instance that should NOT be touched.
	completed := &api.WorkflowInstance{
		ID:          "inst-done",
		Name:        "wf-stuck",
		Status:      api.StatusCompleted,
		Input:       1,
		Output:      2,
		CurrentStep: 2,
	}

	if err := mem.SaveInstance(completed); err != nil {
		t.Fatalf("SaveInstance (completed): %v", err)
	}

	ctx := context.Background()

	// Act: recover stuck instances.
	n, err := eng.RecoverStuckInstances(ctx)
	if err != nil {
		t.Fatalf("RecoverStuckInstances: %v", err)
	}

	if n != 1 {
		t.Fatalf("expected 1 recovered instance, got %d", n)
	}

	// Verify the stuck instance is now failed with a non-nil error.
	gotStuck, err := eng.GetInstance(ctx, "inst-stuck")
	if err != nil {
		t.Fatalf("GetInstance(inst-stuck): %v", err)
	}

	if gotStuck.Status != api.StatusFailed {
		t.Fatalf("expected inst-stuck status %v, got %v", api.StatusFailed, gotStuck.Status)
	}
	if gotStuck.Err == nil {
		t.Fatalf("expected inst-stuck.Err to be non-nil after recovery")
	}

	// Verify the completed instance is unchanged.
	gotCompleted, err := eng.GetInstance(ctx, "inst-done")
	if err != nil {
		t.Fatalf("GetInstance(inst-done): %v", err)
	}

	if gotCompleted.Status != api.StatusCompleted {
		t.Fatalf("expected inst-done status %v, got %v", api.StatusCompleted, gotCompleted.Status)
	}
	if gotCompleted.Output.(int) != 2 {
		t.Fatalf("expected inst-done output 2, got %v", gotCompleted.Output)
	}
}
