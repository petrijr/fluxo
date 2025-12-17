package taskqueue

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func newTestSQLiteQueue(t *testing.T) *SQLiteQueue {
	t.Helper()

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open failed: %v", err)
	}

	t.Cleanup(func() {
		_ = db.Close()
	})

	q, err := NewSQLiteQueue(db)
	if err != nil {
		t.Fatalf("NewSQLiteQueue failed: %v", err)
	}
	return q
}

func TestSQLiteQueue_EnqueueDequeueFIFO(t *testing.T) {
	q := newTestSQLiteQueue(t)
	ctx := context.Background()

	t1 := Task{ID: "1", Type: TaskTypeStartWorkflow, WorkflowName: "wf1", Payload: "a"}
	t2 := Task{ID: "2", Type: TaskTypeStartWorkflow, WorkflowName: "wf2", Payload: "b"}
	t3 := Task{ID: "3", Type: TaskTypeStartWorkflow, WorkflowName: "wf3", Payload: "c"}

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

	if got1.WorkflowName != "wf1" || got2.WorkflowName != "wf2" || got3.WorkflowName != "wf3" {
		t.Fatalf("unexpected dequeue order: %q, %q, %q", got1.WorkflowName, got2.WorkflowName, got3.WorkflowName)
	}

	if got1.Payload != "a" || got2.Payload != "b" || got3.Payload != "c" {
		t.Fatalf("unexpected payloads: %v, %v, %v", got1.Payload, got2.Payload, got3.Payload)
	}

	if q.Len() != 0 {
		t.Fatalf("expected Len 0 after dequeues, got %d", q.Len())
	}
}

func TestSQLiteQueue_DequeueBlocksUntilTaskArrives(t *testing.T) {
	q := newTestSQLiteQueue(t)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	// Start a goroutine that dequeues.
	resultCh := make(chan *Task, 1)
	errCh := make(chan error, 1)

	go func() {
		tk, err := q.Dequeue(ctx)
		if err != nil {
			errCh <- err
			return
		}
		resultCh <- tk
	}()

	// Sleep a bit, then enqueue.
	time.Sleep(50 * time.Millisecond)
	if err := q.Enqueue(context.Background(), Task{
		Type:         TaskTypeStartWorkflow,
		WorkflowName: "wf-delayed",
		Payload:      "x",
	}); err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	select {
	case err := <-errCh:
		t.Fatalf("Dequeue returned error: %v", err)
	case tk := <-resultCh:
		if tk.WorkflowName != "wf-delayed" || tk.Payload != "x" {
			t.Fatalf("unexpected task from Dequeue: %+v", tk)
		}
	case <-ctx.Done():
		t.Fatalf("timeout waiting for Dequeue to return")
	}
}

func TestSQLiteQueue_DequeueHonorsContextCancellation(t *testing.T) {
	q := newTestSQLiteQueue(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	_, err := q.Dequeue(ctx)
	if err == nil {
		t.Fatalf("expected Dequeue to fail due to context cancellation")
	}
}

func TestSQLiteQueue_ScheduledTasksNotDequeuedBeforeNotBefore(t *testing.T) {
	q := newTestSQLiteQueue(t)
	ctx := context.Background()

	delay := 50 * time.Millisecond
	notBefore := time.Now().Add(delay)

	// Enqueue one immediate and one delayed.
	immediate := Task{
		Type:         TaskTypeStartWorkflow,
		WorkflowName: "immediate",
		Payload:      "A",
	}
	delayed := Task{
		Type:         TaskTypeStartWorkflow,
		WorkflowName: "delayed",
		Payload:      "B",
		NotBefore:    notBefore,
	}

	if err := q.Enqueue(ctx, immediate); err != nil {
		t.Fatalf("Enqueue immediate failed: %v", err)
	}
	if err := q.Enqueue(ctx, delayed); err != nil {
		t.Fatalf("Enqueue delayed failed: %v", err)
	}

	// First Dequeue should return the immediate task.
	first, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("Dequeue first failed: %v", err)
	}
	if first.WorkflowName != "immediate" || first.Payload != "A" {
		t.Fatalf("expected immediate task first, got %+v", first)
	}

	// Second Dequeue should block until notBefore is reached.
	start := time.Now()
	second, err := q.Dequeue(ctx)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Dequeue second failed: %v", err)
	}
	if second.WorkflowName != "delayed" || second.Payload != "B" {
		t.Fatalf("expected delayed task second, got %+v", second)
	}

	// We expect at least roughly 'delay' elapsed; allow a bit of slack.
	if elapsed < delay/2 {
		t.Fatalf("expected elapsed >= %v/2, got %v", delay, elapsed)
	}
}

func TestSQLiteQueue_DequeueCancelsWhileWaitingForScheduledTask(t *testing.T) {
	q := newTestSQLiteQueue(t)

	delay := 200 * time.Millisecond
	notBefore := time.Now().Add(delay)

	// Only a delayed task in the queue.
	delayed := Task{
		Type:         TaskTypeStartWorkflow,
		WorkflowName: "delayed",
		Payload:      "X",
		NotBefore:    notBefore,
	}

	if err := q.Enqueue(context.Background(), delayed); err != nil {
		t.Fatalf("Enqueue delayed failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := q.Dequeue(ctx)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatalf("expected Dequeue to fail due to context cancellation")
	}

	if elapsed > delay {
		t.Fatalf("Dequeue did not appear to honor cancellation; elapsed=%v, delay=%v", elapsed, delay)
	}
}

func TestSQLiteQueue_ConcurrentDequeue_NoDuplicates(t *testing.T) {
	q := newTestSQLiteQueue(t)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	task := Task{
		Type:         TaskTypeStartWorkflow,
		WorkflowName: "wf",
		Payload:      "x",
		EnqueuedAt:   time.Now(),
		NotBefore:    time.Now().Add(-time.Millisecond),
		Attempts:     0,
	}
	if err := q.Enqueue(ctx, task); err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	results := make(chan *Task, 2)
	errs := make(chan error, 2)

	deq := func() {
		got, err := q.Dequeue(ctx)
		errs <- err
		results <- got
	}

	go deq()
	go deq()

	var tasks []*Task
	for i := 0; i < 2; i++ {
		_ = <-errs
		tasks = append(tasks, <-results)
	}

	count := 0
	for _, tsk := range tasks {
		if tsk != nil {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly one task dequeued, got %d (%v)", count, tasks)
	}
}
