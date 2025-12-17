package worker

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/petrijr/fluxo/internal/taskqueue"
)

// fakeQueue is a minimal Queue implementation that records the last enqueued task.
type fakeQueue struct {
	lastTask      taskqueue.Task
	enqueueCalled bool
	enqueueErr    error
	dequeueResult *taskqueue.Task
	dequeueErr    error
	lenValue      int
}

func (f *fakeQueue) Enqueue(ctx context.Context, t taskqueue.Task) error {
	f.enqueueCalled = true
	f.lastTask = t
	return f.enqueueErr
}

func (f *fakeQueue) Dequeue(ctx context.Context, owner string, leaseTTL time.Duration) (*taskqueue.Task, error) {
	return f.dequeueResult, f.dequeueErr
}

func (f *fakeQueue) RenewLease(ctx context.Context, taskID string, owner string, leaseTTL time.Duration) error {
	return nil
}

// Ack acknowledges successful processing of a leased task and removes it from the queue.
func (f *fakeQueue) Ack(ctx context.Context, taskID string, owner string) error {
	return nil
}

// Nack releases a leased task back to the queue, optionally updating its schedule and attempts.
func (f *fakeQueue) Nack(ctx context.Context, taskID string, owner string, notBefore time.Time, attempts int) error {
	return nil
}

func (f *fakeQueue) Len() int {
	return f.lenValue
}

// EnqueueSignalAt should build a signal task with the expected fields and NotBefore set to 'at'.
func TestWorker_EnqueueSignalAt_BuildsTask(t *testing.T) {
	ctx := context.Background()
	q := &fakeQueue{}
	// engine is unused by EnqueueSignalAt; pass nil.
	w := NewWithConfig(nil, q, Config{})

	instanceID := "instance-123"
	signalName := "approved"
	payload := map[string]any{"k": "v"}
	at := time.Now().Add(10 * time.Minute)

	if err := w.EnqueueSignalAt(ctx, instanceID, signalName, payload, at); err != nil {
		t.Fatalf("EnqueueSignalAt returned error: %v", err)
	}

	if !q.enqueueCalled {
		t.Fatalf("expected Enqueue to be called")
	}

	got := q.lastTask
	if got.Type != taskqueue.TaskTypeSignal {
		t.Fatalf("expected TaskTypeSignal, got %q", got.Type)
	}
	if got.InstanceID != instanceID {
		t.Fatalf("expected InstanceID=%q, got %q", instanceID, got.InstanceID)
	}
	if got.SignalName != signalName {
		t.Fatalf("expected SignalName=%q, got %q", signalName, got.SignalName)
	}
	m2, ok := got.Payload.(map[string]any)
	if !ok {
		t.Fatalf("got.Payload is not a map[string]any: %T", got.Payload)
	}
	if !reflect.DeepEqual(m2, payload) {
		t.Fatalf("expected Payload=%v, got %v", payload, got.Payload)
	}
	if got.NotBefore != at {
		t.Fatalf("expected NotBefore=%v, got %v", at, got.NotBefore)
	}
	if got.Attempts != 0 {
		t.Fatalf("expected Attempts=0 on newly enqueued signal task, got %d", got.Attempts)
	}
	if got.EnqueuedAt.IsZero() {
		t.Fatalf("expected EnqueuedAt to be set, got zero value")
	}
}
