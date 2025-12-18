package worker

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/petrijr/fluxo/internal/taskqueue"
	"github.com/petrijr/fluxo/pkg/api"
)

type blockingEngine struct {
	started chan struct{}
	release chan struct{}
}

func (e *blockingEngine) RunVersion(ctx context.Context, name string, version string, input any) (*api.WorkflowInstance, error) {
	panic("should not be called")
}

func (e *blockingEngine) GetInstance(ctx context.Context, id string) (*api.WorkflowInstance, error) {
	panic("should not be called")
}

func (e *blockingEngine) ListInstances(ctx context.Context, opts api.InstanceListOptions) ([]*api.WorkflowInstance, error) {
	panic("should not be called")
}

func newBlockingEngine() *blockingEngine {
	return &blockingEngine{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (e *blockingEngine) RegisterWorkflow(def api.WorkflowDefinition) error { return nil }
func (e *blockingEngine) Resume(ctx context.Context, id string) (*api.WorkflowInstance, error) {
	return nil, nil
}
func (e *blockingEngine) Signal(ctx context.Context, id string, name string, payload any) (*api.WorkflowInstance, error) {
	return &api.WorkflowInstance{ID: id}, nil
}
func (e *blockingEngine) RecoverStuckInstances(ctx context.Context) (int, error) { return 0, nil }

func (e *blockingEngine) Run(ctx context.Context, name string, input any) (*api.WorkflowInstance, error) {
	// Indicate the engine call is in-flight.
	select {
	case <-e.started:
		// already closed
	default:
		close(e.started)
	}

	// Block until released or context canceled.
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-e.release:
		return &api.WorkflowInstance{ID: "inst-1"}, nil
	}
}

type countingQueue struct {
	inner  taskqueue.Queue
	renews atomic.Int64
}

func (q *countingQueue) Enqueue(ctx context.Context, t taskqueue.Task) error {
	return q.inner.Enqueue(ctx, t)
}

func (q *countingQueue) Dequeue(ctx context.Context, owner string, leaseTTL time.Duration) (*taskqueue.Task, error) {
	return q.inner.Dequeue(ctx, owner, leaseTTL)
}

func (q *countingQueue) Ack(ctx context.Context, taskID string, owner string) error {
	return q.inner.Ack(ctx, taskID, owner)
}

func (q *countingQueue) Nack(ctx context.Context, taskID string, owner string, notBefore time.Time, attempts int) error {
	return q.inner.Nack(ctx, taskID, owner, notBefore, attempts)
}

func (q *countingQueue) RenewLease(ctx context.Context, taskID string, owner string, leaseTTL time.Duration) error {
	q.renews.Add(1)
	return q.inner.RenewLease(ctx, taskID, owner, leaseTTL)
}

func (q *countingQueue) Len() int { return q.inner.Len() }

func TestWorker_HeartbeatPreventsRedeliveryDuringLongRun(t *testing.T) {
	engine := newBlockingEngine()
	q := taskqueue.NewInMemoryQueue()

	w := NewWithConfig(engine, q, Config{
		MaxAttempts:       1,
		WorkerID:          "w1",
		LeaseTTL:          40 * time.Millisecond,
		HeartbeatInterval: 10 * time.Millisecond, // ensure multiple renewals
	})

	// Enqueue exactly one runnable task.
	err := q.Enqueue(context.Background(), taskqueue.Task{
		ID:           "t1",
		Type:         taskqueue.TaskTypeStartWorkflow,
		WorkflowName: "wf",
		Payload:      api.StartWorkflowPayload{Input: "x"},
		EnqueuedAt:   time.Now(),
		NotBefore:    time.Now().Add(-time.Millisecond),
		Attempts:     0,
	})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := w.ProcessOne(context.Background())
		done <- err
	}()

	// Wait until Run has started (engine call is in-flight).
	select {
	case <-engine.started:
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("engine.Run did not start")
	}

	// Wait past the original LeaseTTL; without heartbeat, task would become visible again.
	time.Sleep(70 * time.Millisecond)

	// Another owner should NOT be able to dequeue it while Run is still in-flight.
	ctx2, cancel2 := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel2()

	got, err := q.Dequeue(ctx2, "w2", 40*time.Millisecond)
	if err == nil {
		t.Fatalf("expected Dequeue to block until ctx timeout; got task=%v", got)
	}
	if got != nil {
		t.Fatalf("expected no task; got %#v", got)
	}

	// Finish the engine call and ensure worker completes successfully.
	close(engine.release)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ProcessOne returned error: %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("ProcessOne did not return")
	}

	// Task should have been ACKed and removed.
	if q.Len() != 0 {
		t.Fatalf("expected queue Len 0 after completion+ack, got %d", q.Len())
	}
}

func TestWorker_HeartbeatIntervalOverrideHonored(t *testing.T) {
	engine := newBlockingEngine()
	base := taskqueue.NewInMemoryQueue()
	q := &countingQueue{inner: base}

	w := NewWithConfig(engine, q, Config{
		MaxAttempts:       1,
		WorkerID:          "w1",
		LeaseTTL:          120 * time.Millisecond,
		HeartbeatInterval: 20 * time.Millisecond, // should yield several renewals while blocked
	})

	err := q.Enqueue(context.Background(), taskqueue.Task{
		ID:           "t1",
		Type:         taskqueue.TaskTypeStartWorkflow,
		WorkflowName: "wf",
		Payload:      api.StartWorkflowPayload{Input: "x"},
		EnqueuedAt:   time.Now(),
		NotBefore:    time.Now().Add(-time.Millisecond),
		Attempts:     0,
	})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := w.ProcessOne(context.Background())
		done <- err
	}()

	// Wait until Run started.
	select {
	case <-engine.started:
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("engine.Run did not start")
	}

	// Keep Run in-flight long enough to get multiple ticks.
	time.Sleep(120 * time.Millisecond)
	close(engine.release)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ProcessOne error: %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("ProcessOne did not return")
	}

	renewCount := q.renews.Load()
	// Be robust against timing jitter. With 20ms interval over ~120ms, we should see >=3.
	if renewCount < 3 {
		t.Fatalf("expected at least 3 RenewLease calls, got %d", renewCount)
	}
}

func TestWorker_NoHeartbeatWhenNoTaskDequeued(t *testing.T) {
	engine := newBlockingEngine()
	base := taskqueue.NewInMemoryQueue()
	q := &countingQueue{inner: base}

	w := NewWithConfig(engine, q, Config{
		MaxAttempts:       1,
		WorkerID:          "w1",
		LeaseTTL:          50 * time.Millisecond,
		HeartbeatInterval: 10 * time.Millisecond,
	})

	// No tasks in queue.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	ok, err := w.ProcessOne(ctx)
	if err != nil && !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatalf("expected ok=false when no task processed, got ok=true")
	}

	if got := q.renews.Load(); got != 0 {
		t.Fatalf("expected 0 RenewLease calls when no task dequeued, got %d", got)
	}
}

func TestWorker_HeartbeatStopsAfterEngineReturns(t *testing.T) {
	engine := newBlockingEngine()
	base := taskqueue.NewInMemoryQueue()
	q := &countingQueue{inner: base}

	w := NewWithConfig(engine, q, Config{
		MaxAttempts:       1,
		WorkerID:          "w1",
		LeaseTTL:          200 * time.Millisecond,
		HeartbeatInterval: 20 * time.Millisecond,
	})

	// One runnable task.
	err := q.Enqueue(context.Background(), taskqueue.Task{
		ID:           "t1",
		Type:         taskqueue.TaskTypeStartWorkflow,
		WorkflowName: "wf",
		Payload:      api.StartWorkflowPayload{Input: "x"},
		EnqueuedAt:   time.Now(),
		NotBefore:    time.Now().Add(-time.Millisecond),
		Attempts:     0,
	})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := w.ProcessOne(context.Background())
		done <- err
	}()

	// Ensure Run is in-flight.
	select {
	case <-engine.started:
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("engine.Run did not start")
	}

	// Let a few heartbeats happen.
	time.Sleep(80 * time.Millisecond)
	before := q.renews.Load()
	if before == 0 {
		t.Fatalf("expected at least 1 RenewLease call before engine returns")
	}

	// End the engine call -> heartbeat should stop promptly.
	close(engine.release)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ProcessOne error: %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("ProcessOne did not return")
	}

	// Capture renew count right after completion.
	after := q.renews.Load()

	// Wait longer than a couple heartbeat intervals; renew count should not increase.
	time.Sleep(100 * time.Millisecond)
	final := q.renews.Load()

	if final > after+1 {
		t.Fatalf("expected RenewLease to stop after engine returns; renews increased too much: after=%d final=%d (before=%d)", after, final, before)
	}
}
