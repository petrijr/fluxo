package worker

import (
	"context"
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/petrijr/fluxo/internal/engine"
	"github.com/petrijr/fluxo/internal/taskqueue"
	"github.com/petrijr/fluxo/pkg/api"
)

type engineFactory func(t *testing.T) api.Engine

func inMemoryEngine(t *testing.T) api.Engine {
	t.Helper()
	return engine.NewInMemoryEngine()
}

func sqliteEngine(t *testing.T) api.Engine {
	t.Helper()

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open failed: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	eng, err := engine.NewSQLiteEngine(db)
	if err != nil {
		t.Fatalf("NewSQLiteEngine failed: %v", err)
	}
	return eng
}

func TestWorker_ProcessesStartWorkflowTasks(t *testing.T) {
	factories := map[string]engineFactory{
		"in-memory": inMemoryEngine,
		"sqlite":    sqliteEngine,
	}

	for name, factory := range factories {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			vEngine := factory(t)
			queue := taskqueue.NewInMemoryQueue(10)
			w := New(vEngine, queue)

			// Simple workflow: add 1 to an int input.
			wf := api.WorkflowDefinition{
				Name: "async-add",
				Steps: []api.StepDefinition{
					{
						Name: "add-one",
						Fn: func(ctx context.Context, input any) (any, error) {
							n := input.(int)
							return n + 1, nil
						},
					},
				},
			}

			if err := vEngine.RegisterWorkflow(wf); err != nil {
				t.Fatalf("RegisterWorkflow failed: %v", err)
			}

			// Initially, there should be no instances for this workflow.
			before, err := vEngine.ListInstances(ctx, api.InstanceListOptions{
				WorkflowName: "async-add",
			})
			if err != nil {
				t.Fatalf("ListInstances before enqueue failed: %v", err)
			}
			if len(before) != 0 {
				t.Fatalf("expected 0 instances before enqueue, got %d", len(before))
			}

			// Enqueue a start-workflow task; this should NOT immediately create an instance.
			if err := w.EnqueueStartWorkflow(ctx, "async-add", 41); err != nil {
				t.Fatalf("EnqueueStartWorkflow failed: %v", err)
			}

			mid, err := vEngine.ListInstances(ctx, api.InstanceListOptions{
				WorkflowName: "async-add",
			})
			if err != nil {
				t.Fatalf("ListInstances after enqueue failed: %v", err)
			}
			if len(mid) != 0 {
				t.Fatalf("expected 0 instances after enqueue but before processing, got %d", len(mid))
			}

			// Process one task; this should run the workflow.
			processed, err := w.ProcessOne(ctx)
			if err != nil {
				t.Fatalf("ProcessOne failed: %v", err)
			}
			if !processed {
				t.Fatalf("expected a task to be processed")
			}

			after, err := vEngine.ListInstances(ctx, api.InstanceListOptions{
				WorkflowName: "async-add",
			})
			if err != nil {
				t.Fatalf("ListInstances after processing failed: %v", err)
			}
			if len(after) != 1 {
				t.Fatalf("expected 1 instance after processing, got %d", len(after))
			}

			inst := after[0]
			if inst.Status != api.StatusCompleted {
				t.Fatalf("expected COMPLETED status, got %q", inst.Status)
			}
			if inst.Output != 42 {
				t.Fatalf("expected output 42, got %v", inst.Output)
			}
		})
	}
}
