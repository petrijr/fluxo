package worker

import (
	"context"
	"database/sql"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/petrijr/fluxo/internal/engine"
	_ "modernc.org/sqlite"

	"github.com/petrijr/fluxo/internal/taskqueue"
	"github.com/petrijr/fluxo/pkg/api"
)

func TestWorker_TaskRetriesWithBackoffAndScheduling(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open failed: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	eng, err := engine.NewSQLiteEngine(db)
	if err != nil {
		t.Fatalf("NewSQLiteEngine failed: %v", err)
	}

	queue, err := taskqueue.NewSQLiteQueue(db)
	if err != nil {
		t.Fatalf("NewSQLiteQueue failed: %v", err)
	}

	backoff := 30 * time.Millisecond

	w := NewWithConfig(eng, queue, Config{
		MaxAttempts: 3,
		Backoff:     backoff,
	})

	var calls int32

	wf := api.WorkflowDefinition{
		Name: "task-retry",
		Steps: []api.StepDefinition{
			{
				Name: "flaky",
				Fn: func(ctx context.Context, input any) (any, error) {
					n := atomic.AddInt32(&calls, 1)
					if n < 2 {
						return nil, errors.New("temporary failure")
					}
					return "ok", nil
				},
			},
		},
	}

	if err := eng.RegisterWorkflow(wf); err != nil {
		t.Fatalf("RegisterWorkflow failed: %v", err)
	}

	ctx := context.Background()

	if err := w.EnqueueStartWorkflow(ctx, "task-retry", nil); err != nil {
		t.Fatalf("EnqueueStartWorkflow failed: %v", err)
	}

	start := time.Now()

	// First processing: runs once, fails, and should schedule a retry.
	processed, err := w.ProcessOne(ctx)
	if err != nil {
		t.Fatalf("first ProcessOne returned error: %v", err)
	}
	if !processed {
		t.Fatalf("expected first task to be processed")
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected 1 call to flaky step after first attempt, got %d", got)
	}

	// Immediately processing again should block until the scheduled retry is due.
	processed, err = w.ProcessOne(ctx)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("second ProcessOne returned error: %v", err)
	}
	if !processed {
		t.Fatalf("expected second task to be processed")
	}

	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("expected 2 calls to flaky step after retry, got %d", got)
	}

	// Verify the workflow instance completed and that backoff was respected
	instances, err := eng.ListInstances(ctx, api.InstanceListOptions{
		WorkflowName: "task-retry",
	})
	if err != nil {
		t.Fatalf("ListInstances failed: %v", err)
	}
	if len(instances) != 2 {
		t.Fatalf("expected 2 instances (one failed, one completed), got %d", len(instances))
	}
	var completed, failed int
	var completedOutput any

	for _, inst := range instances {
		switch inst.Status {
		case api.StatusCompleted:
			completed++
			completedOutput = inst.Output
		case api.StatusFailed:
			failed++
		}
	}

	if completed != 1 {
		t.Fatalf("expected exactly 1 COMPLETED instance, got %d", completed)
	}
	if failed != 1 {
		t.Fatalf("expected exactly 1 FAILED instance, got %d", failed)
	}
	if completedOutput != "ok" {
		t.Fatalf("unexpected output for COMPLETED instance: %v", completedOutput)
	}

	// Rough check: total elapsed time should be at least around one backoff interval.
	if elapsed < backoff/2 {
		t.Fatalf("expected elapsed >= %v/2, got %v", backoff, elapsed)
	}
}
