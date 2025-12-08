package engine

import (
	"context"
	"database/sql"
	"errors"
	"sync/atomic"
	"testing"
	"time"

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

// Test that a step is retried up to MaxAttempts with exponential backoff and
// eventually succeeds.
func TestStepRetry_ExponentialBackoff_Succeeds(t *testing.T) {
	e := NewInMemoryEngine()

	attempts := 0

	def := api.WorkflowDefinition{
		Name: "retry-exponential-success",
		Steps: []api.StepDefinition{
			{
				Name: "flaky",
				Retry: &api.RetryPolicy{
					MaxAttempts:       3,
					InitialBackoff:    5 * time.Millisecond,
					BackoffMultiplier: 2.0,
				},
				Fn: func(ctx context.Context, input any) (any, error) {
					attempts++
					// Fail first two attempts, succeed on third.
					if attempts < 3 {
						return nil, errors.New("boom")
					}
					return "ok", nil
				},
			},
		},
	}

	if err := e.RegisterWorkflow(def); err != nil {
		t.Fatalf("RegisterWorkflow: %v", err)
	}

	ctx := context.Background()
	start := time.Now()
	inst, err := e.Run(ctx, def.Name, nil)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if inst.Status != api.StatusCompleted {
		t.Fatalf("expected StatusCompleted, got %s", inst.Status)
	}

	if attempts != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts)
	}

	// We expect at least one sleep to have happened (5ms + 10ms).
	// Use a conservative threshold to avoid flakiness.
	if elapsed < 5*time.Millisecond {
		t.Fatalf("expected total runtime to include backoff (>=5ms), got %s", elapsed)
	}
}

// Test that the engine stops after MaxAttempts and reports failure.
func TestStepRetry_ExponentialBackoff_MaxAttemptsReached(t *testing.T) {
	e := NewInMemoryEngine()

	attempts := 0
	expectedErr := errors.New("always fails")

	def := api.WorkflowDefinition{
		Name: "retry-exponential-fail",
		Steps: []api.StepDefinition{
			{
				Name: "always-fail",
				Retry: &api.RetryPolicy{
					MaxAttempts:       3,
					InitialBackoff:    1 * time.Millisecond,
					BackoffMultiplier: 2.0,
				},
				Fn: func(ctx context.Context, input any) (any, error) {
					attempts++
					return nil, expectedErr
				},
			},
		},
	}

	if err := e.RegisterWorkflow(def); err != nil {
		t.Fatalf("RegisterWorkflow: %v", err)
	}

	ctx := context.Background()
	inst, err := e.Run(ctx, def.Name, nil)
	if err == nil {
		t.Fatalf("expected Run to return error, got nil")
	}

	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected error %q, got %v", expectedErr, err)
	}

	if inst.Status != api.StatusFailed {
		t.Fatalf("expected StatusFailed, got %s", inst.Status)
	}

	if attempts != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts)
	}
}
