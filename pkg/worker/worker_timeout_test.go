package worker

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/petrijr/fluxo/internal/engine"
	_ "modernc.org/sqlite"

	"github.com/petrijr/fluxo/internal/taskqueue"
	"github.com/petrijr/fluxo/pkg/api"
)

func TestWorker_AutoTimeoutViaScheduledSignal(t *testing.T) {
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

	timeout := 50 * time.Millisecond

	w := NewWithConfig(eng, queue, Config{
		MaxAttempts:          1, // no worker-level retries needed here
		Backoff:              0,
		DefaultSignalTimeout: timeout, // auto-timeout after this duration
	})

	// Workflow:
	//   prepare -> wait for "approve" -> finalize (distinguish timeout vs approval).
	wf := api.WorkflowDefinition{
		Name: "approval-with-timeout",
		Steps: []api.StepDefinition{
			{
				Name: "prepare",
				Fn: func(ctx context.Context, input any) (any, error) {
					return "prepared", nil
				},
			},
			{
				Name: "wait-approval",
				Fn:   api.WaitForSignalStep("approve"),
			},
			{
				Name: "finalize",
				Fn: func(ctx context.Context, input any) (any, error) {
					switch v := input.(type) {
					case api.TimeoutPayload:
						return "timeout:" + v.Reason, nil
					case string:
						return "approved:" + v, nil
					default:
						return nil, errors.New("unexpected input type in finalize")
					}
				},
			},
		},
	}

	if err := eng.RegisterWorkflow(wf); err != nil {
		t.Fatalf("RegisterWorkflow failed: %v", err)
	}

	ctx := context.Background()

	// Enqueue start-workflow task.
	if err := w.EnqueueStartWorkflow(ctx, "approval-with-timeout", nil); err != nil {
		t.Fatalf("EnqueueStartWorkflow failed: %v", err)
	}

	start := time.Now()

	// First ProcessOne: run workflow until it waits for approval and schedule timeout.
	processed, err := w.ProcessOne(ctx)
	if err != nil {
		t.Fatalf("first ProcessOne returned error: %v", err)
	}
	if !processed {
		t.Fatalf("expected first task to be processed")
	}

	// Instance should now be WAITING.
	instances, err := eng.ListInstances(ctx, api.InstanceListOptions{
		WorkflowName: "approval-with-timeout",
	})
	if err != nil {
		t.Fatalf("ListInstances failed: %v", err)
	}
	if len(instances) != 1 {
		t.Fatalf("expected 1 instance after first run, got %d", len(instances))
	}
	if instances[0].Status != api.StatusWaiting {
		t.Fatalf("expected WAITING status after first run, got %q", instances[0].Status)
	}

	// Second ProcessOne: should block until timeout signal is due, then deliver it.
	processed, err = w.ProcessOne(ctx)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("second ProcessOne returned error: %v", err)
	}
	if !processed {
		t.Fatalf("expected second task (timeout signal) to be processed")
	}

	instances, err = eng.ListInstances(ctx, api.InstanceListOptions{
		WorkflowName: "approval-with-timeout",
	})
	if err != nil {
		t.Fatalf("ListInstances after timeout failed: %v", err)
	}
	if len(instances) != 1 {
		t.Fatalf("expected 1 instance after timeout, got %d", len(instances))
	}
	inst := instances[0]
	if inst.Status != api.StatusCompleted {
		t.Fatalf("expected COMPLETED after timeout, got %q", inst.Status)
	}

	outStr, ok := inst.Output.(string)
	if !ok {
		t.Fatalf("expected string output, got %T", inst.Output)
	}
	if outStr[:8] != "timeout:" {
		t.Fatalf("expected timeout output, got %q", outStr)
	}

	// Rough check that we waited at least roughly the timeout duration.
	if elapsed < timeout/2 {
		t.Fatalf("expected elapsed >= %v/2, got %v", timeout, elapsed)
	}
}
