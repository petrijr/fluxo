package engine

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/petrijr/fluxo/pkg/api"
)

type timerEngineFactory func(t *testing.T) api.Engine

func timerInMemoryEngine(t *testing.T) api.Engine {
	t.Helper()
	return NewInMemoryEngine()
}

func timerSQLiteEngine(t *testing.T) api.Engine {
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

func TestSleepStep_DelaysExecutionAtLeastDuration(t *testing.T) {
	factories := map[string]timerEngineFactory{
		"in-memory": timerInMemoryEngine,
		"sqlite":    timerSQLiteEngine,
	}

	for name, factory := range factories {
		t.Run(name, func(t *testing.T) {
			engine := factory(t)

			sleep := 30 * time.Millisecond

			wf := api.WorkflowDefinition{
				Name: "sleep-workflow",
				Steps: []api.StepDefinition{
					{
						Name: "start",
						Fn: func(ctx context.Context, input any) (any, error) {
							return "before-sleep", nil
						},
					},
					{
						Name: "sleep",
						Fn:   api.SleepStep(sleep),
					},
					{
						Name: "after",
						Fn: func(ctx context.Context, input any) (any, error) {
							// Pass through the same value to assert the pipeline is intact.
							if input != "before-sleep" {
								return nil, errors.New("unexpected input after sleep")
							}
							return "done", nil
						},
					},
				},
			}

			if err := engine.RegisterWorkflow(wf); err != nil {
				t.Fatalf("RegisterWorkflow failed: %v", err)
			}

			start := time.Now()
			inst, err := engine.Run(context.Background(), "sleep-workflow", nil)
			elapsed := time.Since(start)

			if err != nil {
				t.Fatalf("Run failed: %v", err)
			}
			if inst.Status != api.StatusCompleted {
				t.Fatalf("expected COMPLETED, got %q", inst.Status)
			}
			if inst.Output != "done" {
				t.Fatalf("unexpected output: %v", inst.Output)
			}

			if elapsed < sleep {
				t.Fatalf("expected elapsed >= %v, got %v", sleep, elapsed)
			}
		})
	}
}

func TestSleepUntilStep_WaitsUntilDeadline(t *testing.T) {
	factories := map[string]timerEngineFactory{
		"in-memory": timerInMemoryEngine,
		"sqlite":    timerSQLiteEngine,
	}

	for name, factory := range factories {
		t.Run(name, func(t *testing.T) {
			engine := factory(t)

			sleep := 40 * time.Millisecond
			deadline := time.Now().Add(sleep)

			wf := api.WorkflowDefinition{
				Name: "sleep-until-workflow",
				Steps: []api.StepDefinition{
					{
						Name: "start",
						Fn: func(ctx context.Context, input any) (any, error) {
							return "foo", nil
						},
					},
					{
						Name: "sleep-until",
						Fn:   api.SleepUntilStep(deadline),
					},
					{
						Name: "end",
						Fn: func(ctx context.Context, input any) (any, error) {
							if input != "foo" {
								return nil, errors.New("unexpected input after sleep-until")
							}
							return "bar", nil
						},
					},
				},
			}

			if err := engine.RegisterWorkflow(wf); err != nil {
				t.Fatalf("RegisterWorkflow failed: %v", err)
			}

			start := time.Now()
			inst, err := engine.Run(context.Background(), "sleep-until-workflow", nil)
			elapsed := time.Since(start)

			if err != nil {
				t.Fatalf("Run failed: %v", err)
			}
			if inst.Status != api.StatusCompleted {
				t.Fatalf("expected COMPLETED, got %q", inst.Status)
			}
			if inst.Output != "bar" {
				t.Fatalf("unexpected output: %v", inst.Output)
			}

			if elapsed < sleep {
				t.Fatalf("expected elapsed >= %v, got %v", sleep, elapsed)
			}
		})
	}
}

func TestSleepStep_CanBeCancelled(t *testing.T) {
	factories := map[string]timerEngineFactory{
		"in-memory": timerInMemoryEngine,
		"sqlite":    timerSQLiteEngine,
	}

	for name, factory := range factories {
		t.Run(name, func(t *testing.T) {
			engine := factory(t)

			sleep := 200 * time.Millisecond

			wf := api.WorkflowDefinition{
				Name: "sleep-cancel",
				Steps: []api.StepDefinition{
					{
						Name: "sleep",
						Fn:   api.SleepStep(sleep),
					},
				},
			}

			if err := engine.RegisterWorkflow(wf); err != nil {
				t.Fatalf("RegisterWorkflow failed: %v", err)
			}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			// Cancel part-way through the sleep duration.
			go func() {
				time.Sleep(sleep / 3)
				cancel()
			}()

			start := time.Now()
			inst, err := engine.Run(ctx, "sleep-cancel", nil)
			elapsed := time.Since(start)

			if err == nil {
				t.Fatalf("expected Run to fail due to cancellation")
			}
			if inst.Status != api.StatusFailed {
				t.Fatalf("expected FAILED, got %q", inst.Status)
			}
			if inst.Err == nil || !errors.Is(inst.Err, context.Canceled) {
				t.Fatalf("expected context.Canceled, got %v", inst.Err)
			}
			if inst.CurrentStep != 0 {
				t.Fatalf("expected failure at step index 0, got %d", inst.CurrentStep)
			}

			// It should fail before the full sleep duration.
			if elapsed > sleep {
				t.Fatalf("cancellation didn't short-circuit sleep; elapsed=%v", elapsed)
			}
		})
	}
}
