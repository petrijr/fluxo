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

type backoffEngineFactory func(t *testing.T) api.Engine

func backoffInMemoryEngine(t *testing.T) api.Engine {
	t.Helper()
	return NewInMemoryEngine()
}

func backoffSQLiteEngine(t *testing.T) api.Engine {
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

func TestRetryBackoff_TotalDelayRoughlyMatchesPolicy(t *testing.T) {
	factories := map[string]backoffEngineFactory{
		"in-memory": backoffInMemoryEngine,
		"sqlite":    backoffSQLiteEngine,
	}

	for name, factory := range factories {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			engine := factory(t)

			backoff := 20 * time.Millisecond
			maxAttempts := 3 // 1 initial + 2 retries => 2 * backoff total expected

			wf := api.WorkflowDefinition{
				Name: "backoff-success",
				Steps: []api.StepDefinition{
					{
						Name: "flaky-with-backoff",
						Fn: func(ctx context.Context, input any) (any, error) {
							// Always fail for the first two attempts, succeed on the third.
							if v, ok := input.(int); ok {
								if v < 2 {
									return nil, errors.New("temporary failure")
								}
							}
							return "ok", nil
						},
						Retry: &api.RetryPolicy{
							MaxAttempts: maxAttempts,
							Backoff:     backoff,
						},
					},
				},
			}

			if err := engine.RegisterWorkflow(wf); err != nil {
				t.Fatalf("RegisterWorkflow failed: %v", err)
			}

			start := time.Now()
			// We drive the step attempts using the input as a counter.
			// Simpler approach: just have a closure with a counter, but we also want to
			// measure elapsed time, not exact call counts, here.
			attempt := 0
			wf.Steps[0].Fn = func(ctx context.Context, input any) (any, error) {
				if attempt < 2 {
					attempt++
					return nil, errors.New("temporary failure")
				}
				return "ok", nil
			}

			inst, err := engine.Run(ctx, "backoff-success", nil)
			elapsed := time.Since(start)

			if err != nil {
				t.Fatalf("Run failed: %v", err)
			}
			if inst.Status != api.StatusCompleted {
				t.Fatalf("expected COMPLETED, got %q", inst.Status)
			}

			// There are (maxAttempts - 1) backoff intervals.
			expectedMin := time.Duration(maxAttempts-1) * backoff

			if elapsed < expectedMin {
				t.Fatalf("expected elapsed >= %v, got %v", expectedMin, elapsed)
			}
		})
	}
}

func TestRetryBackoff_CanBeCancelledDuringWait(t *testing.T) {
	factories := map[string]backoffEngineFactory{
		"in-memory": backoffInMemoryEngine,
		"sqlite":    backoffSQLiteEngine,
	}

	for name, factory := range factories {
		t.Run(name, func(t *testing.T) {
			engine := factory(t)

			backoff := 100 * time.Millisecond

			wf := api.WorkflowDefinition{
				Name: "backoff-cancel",
				Steps: []api.StepDefinition{
					{
						Name: "always-fail-with-backoff",
						Fn: func(ctx context.Context, input any) (any, error) {
							return nil, errors.New("fail")
						},
						Retry: &api.RetryPolicy{
							MaxAttempts: 3,
							Backoff:     backoff,
						},
					},
				},
			}

			if err := engine.RegisterWorkflow(wf); err != nil {
				t.Fatalf("RegisterWorkflow failed: %v", err)
			}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			// Cancel shortly after starting, before the second attempt's backoff
			// has fully elapsed.
			go func() {
				time.Sleep(backoff / 2)
				cancel()
			}()

			start := time.Now()
			inst, err := engine.Run(ctx, "backoff-cancel", nil)
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

			// We expect it to stop before the full 2*backoff time.
			if elapsed > 3*backoff {
				t.Fatalf("cancellation did not appear to short-circuit backoff; elapsed=%v", elapsed)
			}
		})
	}
}
