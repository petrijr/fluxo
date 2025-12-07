package taskqueue

import (
	"context"
	"testing"
	"time"
)

func TestInMemoryQueue_EnqueueDequeueOrder(t *testing.T) {
	q := NewInMemoryQueue(10)

	ctx := context.Background()

	t1 := Task{ID: "1", Type: TaskTypeStartWorkflow, WorkflowName: "wf1"}
	t2 := Task{ID: "2", Type: TaskTypeStartWorkflow, WorkflowName: "wf2"}
	t3 := Task{ID: "3", Type: TaskTypeStartWorkflow, WorkflowName: "wf3"}

	if err := q.Enqueue(ctx, t1); err != nil {
		t.Fatalf("Enqueue t1 failed: %v", err)
	}
	if err := q.Enqueue(ctx, t2); err != nil {
		t.Fatalf("Enqueue t2 failed: %v", err)
	}
	if err := q.Enqueue(ctx, t3); err != nil {
		t.Fatalf("Enqueue t3 failed: %v", err)
	}

	if q.Len() != 3 {
		t.Fatalf("expected Len 3, got %d", q.Len())
	}

	got1, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("Dequeue 1 failed: %v", err)
	}
	got2, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("Dequeue 2 failed: %v", err)
	}
	got3, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("Dequeue 3 failed: %v", err)
	}

	if got1.ID != "1" || got2.ID != "2" || got3.ID != "3" {
		t.Fatalf("unexpected dequeue order: %q, %q, %q", got1.ID, got2.ID, got3.ID)
	}

	if q.Len() != 0 {
		t.Fatalf("expected Len 0 after dequeues, got %d", q.Len())
	}
}

func TestInMemoryQueue_DequeueHonorsContextCancellation(t *testing.T) {
	q := NewInMemoryQueue(1)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	// No tasks enqueued, Dequeue should return ctx error.
	_, err := q.Dequeue(ctx)
	if err == nil {
		t.Fatalf("expected Dequeue to fail due to context cancellation")
	}
}
