package engine

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/petrijr/fluxo/internal/persistence"
	"github.com/petrijr/fluxo/internal/taskqueue"
	"github.com/petrijr/fluxo/pkg/api"
	"github.com/petrijr/fluxo/pkg/worker"
)

func TestQueueFirst_StartSignalResumeAndHistory(t *testing.T) {
	ctx := context.Background()

	store := persistence.NewInMemoryStore()
	q := taskqueue.NewInMemoryQueue()

	eng := NewEngineWithConfig(Config{
		Persistence: persistence.Persistence{
			Workflows: store,
			Instances: store,
			Events:    store,
		},
		Queue: q,
	})

	wf := api.WorkflowDefinition{
		Name: "approval-flow",
		Steps: []api.StepDefinition{
			{
				Name: "prepare",
				Fn: func(ctx context.Context, input any) (any, error) {
					return "prepared", nil
				},
			},
			{
				Name: "wait-approval",
				Fn:   api.WaitForAnySignalStep("approve"),
			},
			{
				Name: "finalize",
				Fn: func(ctx context.Context, input any) (any, error) {
					p, ok := input.(api.SignalPayload)
					if !ok {
						return nil, errors.New("expected SignalPayload")
					}
					s, ok := p.Data.(string)
					if !ok {
						return nil, errors.New("expected string payload in finalize")
					}
					return "result:" + s, nil
				},
			},
		},
	}
	if err := eng.RegisterWorkflow(wf); err != nil {
		t.Fatalf("RegisterWorkflow failed: %v", err)
	}

	starter, ok := eng.(api.AsyncStarter)
	if !ok {
		t.Fatalf("engine does not implement api.AsyncStarter")
	}

	inst, err := starter.Start(ctx, "approval-flow", nil)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	if inst.Status != api.StatusPending {
		t.Fatalf("expected PENDING after Start, got %q", inst.Status)
	}

	w := worker.NewWithConfig(eng, q, worker.Config{
		WorkerID: "w1",
		LeaseTTL: 100 * time.Millisecond,
	})

	// Start task should advance the workflow to waiting.
	processed, err := w.ProcessOne(ctx)
	if !processed || err == nil {
		// The workflow should be waiting, which is surfaced as an error from Run/RunInstance.
		t.Fatalf("expected ProcessOne to return wait error, processed=%v err=%v", processed, err)
	}
	if _, ok := api.IsWaitForSignalError(err); !ok {
		t.Fatalf("expected wait-for-signal error, got %v", err)
	}
	inst2, err := eng.GetInstance(ctx, inst.ID)
	if err != nil {
		t.Fatalf("GetInstance failed: %v", err)
	}
	if inst2.Status != api.StatusWaiting {
		t.Fatalf("expected WAITING after start processing, got %q", inst2.Status)
	}

	// Signal should enqueue work; it must NOT run inline.
	_, err = eng.Signal(ctx, inst.ID, "approve", "ok")
	if err != nil {
		t.Fatalf("Signal failed: %v", err)
	}
	inst3, _ := eng.GetInstance(ctx, inst.ID)
	if inst3.Status != api.StatusWaiting {
		t.Fatalf("expected still WAITING after Signal enqueue, got %q", inst3.Status)
	}

	// Process signal task -> should complete.
	processed, err = w.ProcessOne(ctx)
	if !processed || err != nil {
		t.Fatalf("expected signal task to process cleanly, processed=%v err=%v", processed, err)
	}
	final, _ := eng.GetInstance(ctx, inst.ID)
	if final.Status != api.StatusCompleted {
		t.Fatalf("expected COMPLETED, got %q", final.Status)
	}

	// Minimal history should contain the key milestones in order.
	hr, ok := eng.(api.HistoryReader)
	if !ok {
		t.Fatalf("engine does not implement api.HistoryReader")
	}
	events, err := hr.ListEvents(ctx, inst.ID)
	if err != nil {
		t.Fatalf("ListEvents failed: %v", err)
	}
	if len(events) == 0 {
		t.Fatalf("expected non-empty history")
	}

	want := []api.EventType{
		api.EventWorkflowEnqueued,
		api.EventWorkflowStarted,
		api.EventStepStarted,
		api.EventStepCompleted,
		api.EventStepStarted,
		api.EventWorkflowWaiting,
		api.EventSignalReceived,
		api.EventWorkflowResumed,
		api.EventWorkflowCompleted,
	}
	// subsequence match
	j := 0
	for _, ev := range events {
		if j < len(want) && ev.Type == want[j] {
			j++
		}
	}
	if j != len(want) {
		types := make([]api.EventType, 0, len(events))
		for _, ev := range events {
			types = append(types, ev.Type)
		}
		t.Fatalf("history missing expected subsequence. got=%v wantSubseq=%v", types, want)
	}
}
