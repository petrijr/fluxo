package engine

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/petrijr/fluxo/pkg/api"
)

type engFactory func(t *testing.T) api.Engine

func newInMemoryEngine(t *testing.T) api.Engine {
	t.Helper()
	return NewInMemoryEngine()
}

func newSQLiteEngine(t *testing.T) api.Engine {
	t.Helper()

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open failed: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	eng, err := NewSQLiteEngine(db)
	if err != nil {
		t.Fatalf("NewSQLiteEngine failed: %v", err)
	}
	return eng
}

func TestResume_ReplaysFailedWorkflowFromInput(t *testing.T) {
	factories := map[string]engFactory{
		"in-memory": newInMemoryEngine,
		"sqlite":    newSQLiteEngine,
	}

	for name, factory := range factories {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			engine := factory(t)
			tryCount := 0

			wf := api.WorkflowDefinition{
				Name: "retry-wf",
				Steps: []api.StepDefinition{
					{
						Name: "step-0",
						Fn: func(ctx context.Context, input any) (any, error) {
							// Build up a string so we can see replay.
							s, _ := input.(string)
							return s + "-s0", nil
						},
					},
					{
						Name: "step-1",
						Fn: func(ctx context.Context, input any) (any, error) {
							if tryCount > 0 {
								// Resumed execution will succeed
								s, _ := input.(string)
								return s + "-s2", nil
							}
							tryCount++
							// First execution will fail
							return nil, errors.New("boom " + strconv.Itoa(tryCount))
						},
					},
					{
						Name: "step-2",
						Fn: func(ctx context.Context, input any) (any, error) {
							// Not reached on first run, but reached after we
							// patch the workflow in the test.
							s, _ := input.(string)
							return s + "-s2", nil
						},
					},
				},
			}

			if err := engine.RegisterWorkflow(wf); err != nil {
				t.Fatalf("RegisterWorkflow failed: %v", err)
			}

			// First run: expected to fail at step-1.
			inst, err := engine.Run(ctx, "retry-wf", "start")
			if err == nil {
				t.Fatalf("expected Run to fail")
			}
			if inst.Status != api.StatusFailed {
				t.Fatalf("expected FAILED, got %q", inst.Status)
			}
			if inst.Input != "start" {
				t.Fatalf("expected Input 'start', got %v", inst.Input)
			}
			if inst.CurrentStep != 1 {
				t.Fatalf("expected failure at step index 1, got %d", inst.CurrentStep)
			}

			// Resume: replays from the beginning with Input="start".
			resumed, err := engine.Resume(ctx, inst.ID)
			if err != nil {
				t.Fatalf("Resume failed: %v", err)
			}
			if resumed.ID != inst.ID {
				t.Fatalf("expected same instance ID on resume")
			}
			if resumed.Status != api.StatusCompleted {
				t.Fatalf("expected COMPLETED after resume, got %q", resumed.Status)
			}
			if resumed.CurrentStep != len(wf.Steps) {
				t.Fatalf("expected CurrentStep %d, got %d", len(wf.Steps), resumed.CurrentStep)
			}
		})
	}
}

func TestResume_RejectsCompletedInstances(t *testing.T) {
	engine := newInMemoryEngine(t)

	wf := api.WorkflowDefinition{
		Name: "simple",
		Steps: []api.StepDefinition{
			{
				Name: "only",
				Fn: func(ctx context.Context, input any) (any, error) {
					return "ok", nil
				},
			},
		},
	}

	if err := engine.RegisterWorkflow(wf); err != nil {
		t.Fatalf("RegisterWorkflow failed: %v", err)
	}

	inst, err := engine.Run(context.Background(), "simple", nil)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	_, err = engine.Resume(context.Background(), inst.ID)
	if err == nil {
		t.Fatalf("expected Resume to fail for COMPLETED instance")
	}
}
