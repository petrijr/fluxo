package engine

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/petrijr/fluxo/pkg/api"
)

type engineFactory func(t *testing.T) api.Engine

func inMemoryEngineFactory(t *testing.T) api.Engine {
	t.Helper()
	return NewInMemoryEngine()
}

func sqliteEngineFactory(t *testing.T) api.Engine {
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

func TestProgressTracking_CompletedWorkflow(t *testing.T) {
	factories := map[string]engineFactory{
		"in-memory": inMemoryEngineFactory,
		"sqlite":    sqliteEngineFactory,
	}

	for name, factory := range factories {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			engine := factory(t)

			wf := api.WorkflowDefinition{
				Name: "three-steps",
				Steps: []api.StepDefinition{
					{
						Name: "step-0",
						Fn: func(ctx context.Context, input any) (any, error) {
							return "s0", nil
						},
					},
					{
						Name: "step-1",
						Fn: func(ctx context.Context, input any) (any, error) {
							return "s1", nil
						},
					},
					{
						Name: "step-2",
						Fn: func(ctx context.Context, input any) (any, error) {
							return "s2", nil
						},
					},
				},
			}

			if err := engine.RegisterWorkflow(wf); err != nil {
				t.Fatalf("RegisterWorkflow failed: %v", err)
			}

			inst, err := engine.Run(ctx, "three-steps", nil)
			if err != nil {
				t.Fatalf("Run failed: %v", err)
			}

			if inst.Status != api.StatusCompleted {
				t.Fatalf("expected COMPLETED, got %q", inst.Status)
			}

			if inst.CurrentStep != len(wf.Steps) {
				t.Fatalf("expected CurrentStep %d after completion, got %d", len(wf.Steps), inst.CurrentStep)
			}

			// Check persisted state as seen via GetInstance.
			fromStore, err := engine.GetInstance(ctx, inst.ID)
			if err != nil {
				t.Fatalf("GetInstance failed: %v", err)
			}
			if fromStore.CurrentStep != len(wf.Steps) {
				t.Fatalf("expected persisted CurrentStep %d, got %d", len(wf.Steps), fromStore.CurrentStep)
			}
		})
	}
}

func TestProgressTracking_FailedWorkflow(t *testing.T) {
	factories := map[string]engineFactory{
		"in-memory": inMemoryEngineFactory,
		"sqlite":    sqliteEngineFactory,
	}

	for name, factory := range factories {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			engine := factory(t)

			wf := api.WorkflowDefinition{
				Name: "fail-at-1",
				Steps: []api.StepDefinition{
					{
						Name: "step-0",
						Fn: func(ctx context.Context, input any) (any, error) {
							return "ok", nil
						},
					},
					{
						Name: "step-1",
						Fn: func(ctx context.Context, input any) (any, error) {
							return nil, assertError("boom")
						},
					},
					{
						Name: "step-2",
						Fn: func(ctx context.Context, input any) (any, error) {
							t.Fatalf("step-2 should not be executed")
							return nil, nil
						},
					},
				},
			}

			if err := engine.RegisterWorkflow(wf); err != nil {
				t.Fatalf("RegisterWorkflow failed: %v", err)
			}

			inst, err := engine.Run(ctx, "fail-at-1", nil)
			if err == nil {
				t.Fatalf("expected Run to fail")
			}

			if inst.Status != api.StatusFailed {
				t.Fatalf("expected FAILED, got %q", inst.Status)
			}
			if inst.CurrentStep != 1 {
				t.Fatalf("expected CurrentStep 1 at failure, got %d", inst.CurrentStep)
			}

			fromStore, err := engine.GetInstance(ctx, inst.ID)
			if err != nil {
				t.Fatalf("GetInstance failed: %v", err)
			}
			if fromStore.CurrentStep != 1 {
				t.Fatalf("expected persisted CurrentStep 1, got %d", fromStore.CurrentStep)
			}
		})
	}
}

func TestProgressTracking_CancellationDuringStep(t *testing.T) {
	factories := map[string]engineFactory{
		"in-memory": inMemoryEngineFactory,
		"sqlite":    sqliteEngineFactory,
	}

	for name, factory := range factories {
		t.Run(name, func(t *testing.T) {
			engine := factory(t)

			wf := api.WorkflowDefinition{
				Name: "cancel-during-1",
				Steps: []api.StepDefinition{
					{
						Name: "step-0",
						Fn: func(ctx context.Context, input any) (any, error) {
							return "ok0", nil
						},
					},
					{
						Name: "step-1",
						Fn: func(ctx context.Context, input any) (any, error) {
							// Long-ish step that cooperates with context.
							select {
							case <-ctx.Done():
								return nil, ctx.Err()
							case <-time.After(100 * time.Millisecond):
								return "ok1", nil
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

			// Cancel shortly after starting.
			go func() {
				time.Sleep(10 * time.Millisecond)
				cancel()
			}()

			inst, err := engine.Run(ctx, "cancel-during-1", nil)
			if err == nil {
				t.Fatalf("expected Run to fail due to cancellation")
			}
			if inst.Status != api.StatusFailed {
				t.Fatalf("expected FAILED, got %q", inst.Status)
			}
			if inst.CurrentStep != 1 {
				t.Fatalf("expected CurrentStep 1 at cancellation, got %d", inst.CurrentStep)
			}

			fromStore, err := engine.GetInstance(context.Background(), inst.ID)
			if err != nil {
				t.Fatalf("GetInstance failed: %v", err)
			}
			if fromStore.CurrentStep != 1 {
				t.Fatalf("expected persisted CurrentStep 1, got %d", fromStore.CurrentStep)
			}
		})
	}
}

// Tiny helper to create errors inline without importing fmt here.
type simpleErr string

func (e simpleErr) Error() string { return string(e) }

func assertError(msg string) error {
	return simpleErr(msg)
}
