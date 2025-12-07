package worker

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

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
			eng := factory(t)
			queue := taskqueue.NewInMemoryQueue(10)
			w := New(eng, queue)

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

			if err := eng.RegisterWorkflow(wf); err != nil {
				t.Fatalf("RegisterWorkflow failed: %v", err)
			}

			// Initially, there should be no instances for this workflow.
			before, err := eng.ListInstances(ctx, api.InstanceListOptions{
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

			mid, err := eng.ListInstances(ctx, api.InstanceListOptions{
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

			after, err := eng.ListInstances(ctx, api.InstanceListOptions{
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

func TestWorker_ProcessesSignalTasks(t *testing.T) {
	factories := map[string]engineFactory{
		"in-memory": inMemoryEngine,
		"sqlite":    sqliteEngine,
	}

	for name, factory := range factories {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			eng := factory(t)
			queue := taskqueue.NewInMemoryQueue(10)
			w := New(eng, queue)

			// approval-like workflow:
			//   prepare -> wait for "approve" -> finalize using payload.
			wf := api.WorkflowDefinition{
				Name: "async-approval",
				Steps: []api.StepDefinition{
					{
						Name: "prepare",
						Fn: func(ctx context.Context, input any) (any, error) {
							return "prepared", nil
						},
					},
					{
						Name: "wait-approve",
						Fn:   api.WaitForSignalStep("approve"),
					},
					{
						Name: "finalize",
						Fn: func(ctx context.Context, input any) (any, error) {
							s, ok := input.(string)
							if !ok {
								return nil, errors.New("expected string payload in finalize")
							}
							return "done:" + s, nil
						},
					},
				},
			}

			if err := eng.RegisterWorkflow(wf); err != nil {
				t.Fatalf("RegisterWorkflow failed: %v", err)
			}

			// Start workflow synchronously; it should park in WAITING at wait-approve.
			inst, err := eng.Run(ctx, "async-approval", nil)
			if err == nil {
				t.Fatalf("expected Run to return wait-for-signal error")
			}
			if inst.Status != api.StatusWaiting {
				t.Fatalf("expected WAITING, got %q", inst.Status)
			}

			// Enqueue a signal task; this should not immediately change the instance.
			if err := w.EnqueueSignal(ctx, inst.ID, "approve", "APPROVED"); err != nil {
				t.Fatalf("EnqueueSignal failed: %v", err)
			}

			instMid, err := eng.GetInstance(ctx, inst.ID)
			if err != nil {
				t.Fatalf("GetInstance failed: %v", err)
			}
			if instMid.Status != api.StatusWaiting {
				t.Fatalf("expected instance to still be WAITING before worker processing, got %q", instMid.Status)
			}

			// Process the signal task; this should resume and complete the workflow.
			processed, err := w.ProcessOne(ctx)
			if err != nil {
				t.Fatalf("ProcessOne failed: %v", err)
			}
			if !processed {
				t.Fatalf("expected a task to be processed")
			}

			instFinal, err := eng.GetInstance(ctx, inst.ID)
			if err != nil {
				t.Fatalf("GetInstance after signal processing failed: %v", err)
			}
			if instFinal.Status != api.StatusCompleted {
				t.Fatalf("expected COMPLETED after signal, got %q", instFinal.Status)
			}
			if instFinal.Output != "done:APPROVED" {
				t.Fatalf("unexpected output after signal: %v", instFinal.Output)
			}
		})
	}
}

func TestWorker_ProcessesScheduledStartWorkflowTask(t *testing.T) {
	// Use SQLite eng + SQLite queue together.
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open failed: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	eng := sqliteEngine(t)

	queue, err := taskqueue.NewSQLiteQueue(db)
	if err != nil {
		t.Fatalf("NewSQLiteQueue failed: %v", err)
	}

	w := New(eng, queue)

	wf := api.WorkflowDefinition{
		Name: "scheduled-add",
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

	if err := eng.RegisterWorkflow(wf); err != nil {
		t.Fatalf("RegisterWorkflow failed: %v", err)
	}

	delay := 50 * time.Millisecond
	at := time.Now().Add(delay)

	start := time.Now()
	if err := w.EnqueueStartWorkflowAt(context.Background(), "scheduled-add", 41, at); err != nil {
		t.Fatalf("EnqueueStartWorkflowAt failed: %v", err)
	}

	// Process one task; this should block until 'at'.
	processed, err := w.ProcessOne(context.Background())
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("ProcessOne failed: %v", err)
	}
	if !processed {
		t.Fatalf("expected a task to be processed")
	}

	instances, err := eng.ListInstances(context.Background(), api.InstanceListOptions{
		WorkflowName: "scheduled-add",
	})
	if err != nil {
		t.Fatalf("ListInstances failed: %v", err)
	}
	if len(instances) != 1 {
		t.Fatalf("expected 1 instance, got %d", len(instances))
	}
	if instances[0].Output != 42 {
		t.Fatalf("expected output 42, got %v", instances[0].Output)
	}

	if elapsed < delay/2 {
		t.Fatalf("expected elapsed >= %v/2, got %v", delay, elapsed)
	}
}
