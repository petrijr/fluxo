package fluxo

import (
	"context"
	"testing"
	"time"
)

func TestFluxo_TopLevelWrappers_RunGetListSignalRecover(t *testing.T) {
	ctx := context.Background()
	eng := NewInMemoryEngine()

	// Build a tiny workflow that exercises SleepStep and WaitForSignalStep wrappers.
	flow := New("wrap-test").
		Step("sleep", SleepStep(0)).
		Step("wait", WaitForSignalStep("go"))

	flow.MustRegister(eng)

	// Start the workflow via top-level Run wrapper.
	inst, err := Run(ctx, eng, flow.Name(), 0)
	// Running a workflow that waits for a signal may return a wait error; that's acceptable here.
	if inst == nil {
		t.Fatalf("run returned nil instance: %v", err)
	}
	if inst.Name != flow.Name() {
		t.Fatalf("unexpected instance name: %s", inst.Name)
	}

	// After the first step, it should wait for a signal.
	if inst.Status != StatusWaiting {
		t.Fatalf("expected waiting status, got: %s", inst.Status)
	}

	// GetInstance wrapper should return the same instance.
	got, err := GetInstance(ctx, eng, inst.ID)
	if err != nil || got.ID != inst.ID {
		t.Fatalf("get instance mismatch: %v", err)
	}

	// ListInstances wrapper with filters
	lst, err := ListInstances(ctx, eng, InstanceListOptions{WorkflowName: flow.Name(), Status: StatusWaiting})
	if err != nil || len(lst) == 0 {
		t.Fatalf("expected to list waiting instance: %v len=%d", err, len(lst))
	}

	// RecoverStuckInstances should be harmless on a healthy engine.
	if n, err := RecoverStuckInstances(ctx, eng); err != nil || n < 0 {
		t.Fatalf("recover failed: n=%d err=%v", n, err)
	}

	// Deliver the signal; instance should complete with the payload.
	payload := "done"
	after, err := Signal(ctx, eng, inst.ID, "go", payload)
	if err != nil {
		t.Fatalf("signal failed: %v", err)
	}
	if after.Status != StatusCompleted {
		t.Fatalf("expected completed, got %s", after.Status)
	}
	if after.Output != payload {
		t.Fatalf("unexpected output: %#v", after.Output)
	}
}

func TestFluxo_QueuesAndWorkers_Constructors(t *testing.T) {
	eng := NewInMemoryEngine()
	q := NewInMemoryQueue(16)
	if q == nil {
		t.Fatalf("queue is nil")
	}
	w := NewWorker(eng, q)
	if w == nil {
		t.Fatalf("worker is nil")
	}
	// Also exercise NewWorkerWithConfig path using supported fields.
	w2 := NewWorkerWithConfig(eng, q, Config{MaxAttempts: 2, DefaultSignalTimeout: 1 * time.Millisecond})
	if w2 == nil {
		t.Fatalf("worker2 is nil")
	}
}
