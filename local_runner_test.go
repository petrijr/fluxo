package fluxo

import (
	"context"
	"testing"
	"time"
)

// TestLocalRunner_SyncAndAsync verifies that LocalRunner can run workflows
// both synchronously (direct Run) and asynchronously via StartWorkflowAsync
// + worker loop.
func TestLocalRunner_SyncAndAsync(t *testing.T) {
	t.Helper()

	runner := NewLocalRunner()

	// Simple workflow: (n + 1) * 2
	flow := New("localrunner-sync-async").
		Step("inc", func(ctx context.Context, input any) (any, error) {
			n, ok := input.(int)
			if !ok {
				t.Fatalf("inc: expected int input, got %T", input)
			}
			return n + 1, nil
		}).
		Step("double", func(ctx context.Context, input any) (any, error) {
			n, ok := input.(int)
			if !ok {
				t.Fatalf("double: expected int input, got %T", input)
			}
			return n * 2, nil
		})

	flow.MustRegister(runner.Engine)

	ctx := context.Background()

	// --- Synchronous run ---

	syncInst, err := Run(ctx, runner.Engine, flow.Name(), 1)
	if err != nil {
		t.Fatalf("sync Run failed: %v", err)
	}

	if syncInst.Status != StatusCompleted {
		t.Fatalf("expected sync instance status %v, got %v", StatusCompleted, syncInst.Status)
	}

	out, ok := syncInst.Output.(int)
	if !ok {
		t.Fatalf("expected int output from sync instance, got %T (%v)", syncInst.Output, syncInst.Output)
	}
	// (1 + 1) * 2 = 4
	if out != 4 {
		t.Fatalf("expected sync output 4, got %d", out)
	}

	// --- Asynchronous run via worker/queue ---

	if err := runner.StartWorkers(ctx, 2); err != nil {
		t.Fatalf("StartWorkers failed: %v", err)
	}
	defer runner.Stop()

	if err := runner.StartWorkflowAsync(ctx, flow.Name(), 3); err != nil {
		t.Fatalf("StartWorkflowAsync failed: %v", err)
	}

	// Poll for the async instance to appear and complete.
	deadline := time.Now().Add(2 * time.Second)
	var asyncInst *WorkflowInstance

	for time.Now().Before(deadline) {
		instances, err := ListInstances(ctx, runner.Engine, InstanceListOptions{})
		if err != nil {
			t.Fatalf("ListInstances failed: %v", err)
		}

		for _, inst := range instances {
			if inst.Name != flow.Name() {
				continue
			}
			if inst.Status != StatusCompleted {
				continue
			}
			// For input=3: (3 + 1) * 2 = 8
			if v, ok := inst.Output.(int); ok && v == 8 {
				asyncInst = inst
				break
			}
		}

		if asyncInst != nil {
			break
		}

		time.Sleep(10 * time.Millisecond)
	}

	if asyncInst == nil {
		t.Fatalf("did not observe completed async instance with expected output before timeout")
	}
}

// TestLocalRunner_StartWorkersTwice ensures that StartWorkers cannot be
// called twice without Stop in between.
func TestLocalRunner_StartWorkersTwice(t *testing.T) {
	runner := NewLocalRunner()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer runner.Stop()

	if err := runner.StartWorkers(ctx, 1); err != nil {
		t.Fatalf("first StartWorkers failed: %v", err)
	}

	if err := runner.StartWorkers(ctx, 1); err == nil {
		t.Fatalf("expected error from second StartWorkers call, got nil")
	}
}

// TestLocalRunner_StopWithoutStart ensures Stop is safe when workers were
// never started.
func TestLocalRunner_StopWithoutStart(t *testing.T) {
	runner := NewLocalRunner()
	// Should not panic or deadlock.
	runner.Stop()
}

// TestLocalRunner_SignalAsync verifies that SignalAsync works with a workflow
// that waits for a signal and then continues.
func TestLocalRunner_SignalAsync(t *testing.T) {
	runner := NewLocalRunner()
	ctx := context.Background()

	flow := New("localrunner-signal").
		// First step: wait for signal "go".
		WaitForSignal("wait-for-go", "go").
		// Second step: mark completion.
		Step("after-signal", func(ctx context.Context, input any) (any, error) {
			return "done", nil
		})

	flow.MustRegister(runner.Engine)

	if err := runner.StartWorkers(ctx, 1); err != nil {
		t.Fatalf("StartWorkers failed: %v", err)
	}
	defer runner.Stop()

	// Start the workflow asynchronously.
	if err := runner.StartWorkflowAsync(ctx, flow.Name(), nil); err != nil {
		t.Fatalf("StartWorkflowAsync failed: %v", err)
	}

	// Poll for an instance that is waiting on the signal.
	var waitingInst *WorkflowInstance
	deadline := time.Now().Add(2 * time.Second)

	for time.Now().Before(deadline) {
		instances, err := ListInstances(ctx, runner.Engine, InstanceListOptions{})
		if err != nil {
			t.Fatalf("ListInstances failed: %v", err)
		}

		for _, inst := range instances {
			if inst.Name != flow.Name() {
				continue
			}
			if inst.Status == StatusWaiting {
				waitingInst = inst
				break
			}
		}

		if waitingInst != nil {
			break
		}

		time.Sleep(10 * time.Millisecond)
	}

	if waitingInst == nil {
		t.Fatalf("expected to find a waiting instance before timeout")
	}

	// Send the signal asynchronously.
	if err := runner.SignalAsync(ctx, waitingInst.ID, "go", nil); err != nil {
		t.Fatalf("SignalAsync failed: %v", err)
	}

	// Poll for the same instance to complete with the expected output.
	var completedInst *WorkflowInstance

	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		instances, err := ListInstances(ctx, runner.Engine, InstanceListOptions{})
		if err != nil {
			t.Fatalf("ListInstances failed: %v", err)
		}

		for _, inst := range instances {
			if inst.ID != waitingInst.ID {
				continue
			}
			if inst.Status == StatusCompleted {
				completedInst = inst
				break
			}
		}

		if completedInst != nil {
			break
		}

		time.Sleep(10 * time.Millisecond)
	}

	if completedInst == nil {
		t.Fatalf("expected instance %s to complete after signal, but it did not", waitingInst.ID)
	}

	if completedInst.Output != "done" {
		t.Fatalf("expected output \"done\", got %T (%v)", completedInst.Output, completedInst.Output)
	}
}
