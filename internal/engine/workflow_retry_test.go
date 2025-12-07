package engine

import (
	"context"
	"database/sql"
	"errors"
	"sync/atomic"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/petrijr/fluxo/pkg/api"
)

type retryEngineFactory func(t *testing.T) api.Engine

func retryInMemoryEngine(t *testing.T) api.Engine {
	t.Helper()
	return NewInMemoryEngine()
}

func retrySQLiteEngine(t *testing.T) api.Engine {
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

func TestRetryPolicy_StepEventuallySucceeds(t *testing.T) {
	factories := map[string]retryEngineFactory{
		"in-memory": retryInMemoryEngine,
		"sqlite":    retrySQLiteEngine,
	}

	for name, factory := range factories {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			engine := factory(t)

			var calls int32

			wf := api.WorkflowDefinition{
				Name: "retry-success",
				Steps: []api.StepDefinition{
					{
						Name: "flaky-step",
						Fn: func(ctx context.Context, input any) (any, error) {
							n := atomic.AddInt32(&calls, 1)

							// Fail first two times, succeed on third.
							if n < 3 {
								return nil, errors.New("temporary failure")
							}
							return "ok-after-3", nil
						},
						Retry: &api.RetryPolicy{
							MaxAttempts: 3,
						},
					},
				},
			}

			if err := engine.RegisterWorkflow(wf); err != nil {
				t.Fatalf("RegisterWorkflow failed: %v", err)
			}

			inst, err := engine.Run(ctx, "retry-success", nil)
			if err != nil {
				t.Fatalf("Run failed: %v", err)
			}

			if inst.Status != api.StatusCompleted {
				t.Fatalf("expected COMPLETED, got %q", inst.Status)
			}
			if inst.Output != "ok-after-3" {
				t.Fatalf("unexpected output: %v", inst.Output)
			}

			if got := atomic.LoadInt32(&calls); got != 3 {
				t.Fatalf("expected 3 calls to flaky-step, got %d", got)
			}

			if inst.CurrentStep != len(wf.Steps) {
				t.Fatalf("expected CurrentStep %d, got %d", len(wf.Steps), inst.CurrentStep)
			}
		})
	}
}

func TestRetryPolicy_StepEventuallyFailsAfterMaxAttempts(t *testing.T) {
	factories := map[string]retryEngineFactory{
		"in-memory": retryInMemoryEngine,
		"sqlite":    retrySQLiteEngine,
	}

	for name, factory := range factories {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			engine := factory(t)

			var calls int32

			wf := api.WorkflowDefinition{
				Name: "retry-fail",
				Steps: []api.StepDefinition{
					{
						Name: "always-fail",
						Fn: func(ctx context.Context, input any) (any, error) {
							atomic.AddInt32(&calls, 1)
							return nil, errors.New("permanent failure")
						},
						Retry: &api.RetryPolicy{
							MaxAttempts: 3,
						},
					},
				},
			}

			if err := engine.RegisterWorkflow(wf); err != nil {
				t.Fatalf("RegisterWorkflow failed: %v", err)
			}

			inst, err := engine.Run(ctx, "retry-fail", nil)
			if err == nil {
				t.Fatalf("expected Run to fail")
			}

			if inst.Status != api.StatusFailed {
				t.Fatalf("expected FAILED, got %q", inst.Status)
			}
			if inst.Err == nil || inst.Err.Error() != "permanent failure" {
				t.Fatalf("unexpected error: %v", inst.Err)
			}

			if got := atomic.LoadInt32(&calls); got != 3 {
				t.Fatalf("expected 3 calls to always-fail, got %d", got)
			}

			if inst.CurrentStep != 0 {
				t.Fatalf("expected failure at step index 0, got %d", inst.CurrentStep)
			}
		})
	}
}

func TestRetryPolicy_DefaultNoRetry(t *testing.T) {
	factories := map[string]retryEngineFactory{
		"in-memory": retryInMemoryEngine,
		"sqlite":    retrySQLiteEngine,
	}

	for name, factory := range factories {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			engine := factory(t)

			var calls int32

			wf := api.WorkflowDefinition{
				Name: "no-retry",
				Steps: []api.StepDefinition{
					{
						Name: "once-fail",
						Fn: func(ctx context.Context, input any) (any, error) {
							atomic.AddInt32(&calls, 1)
							return nil, errors.New("boom")
						},
						// No RetryPolicy => MaxAttempts = 1
					},
				},
			}

			if err := engine.RegisterWorkflow(wf); err != nil {
				t.Fatalf("RegisterWorkflow failed: %v", err)
			}

			inst, err := engine.Run(ctx, "no-retry", nil)
			if err == nil {
				t.Fatalf("expected Run to fail")
			}

			if inst.Status != api.StatusFailed {
				t.Fatalf("expected FAILED, got %q", inst.Status)
			}
			if got := atomic.LoadInt32(&calls); got != 1 {
				t.Fatalf("expected 1 call when no RetryPolicy, got %d", got)
			}
		})
	}
}
