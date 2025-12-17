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

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	// Make tasks immediately runnable.
	nb := time.Now().Add(-time.Millisecond)

	t1 := Task{ID: "1", Type: TaskTypeStartWorkflow, WorkflowName: "wf1", Payload: "a", NotBefore: nb}
	t2 := Task{ID: "2", Type: TaskTypeStartWorkflow, WorkflowName: "wf2", Payload: "b", NotBefore: nb}
	t3 := Task{ID: "3", Type: TaskTypeStartWorkflow, WorkflowName: "wf3", Payload: "c", NotBefore: nb}

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

	got1, err := q.Dequeue(ctx, "w1", 200*time.Millisecond)
	if err != nil {
		t.Fatalf("Dequeue 1 failed: %v", err)
	}
	got2, err := q.Dequeue(ctx, "w1", 200*time.Millisecond)
	if err != nil {
		t.Fatalf("Dequeue 2 failed: %v", err)
	}
	got3, err := q.Dequeue(ctx, "w1", 200*time.Millisecond)
	if err != nil {
		t.Fatalf("Dequeue 3 failed: %v", err)
	}

	if got1.WorkflowName != "wf1" || got2.WorkflowName != "wf2" || got3.WorkflowName != "wf3" {
		t.Fatalf("unexpected dequeue order: %q, %q, %q", got1.WorkflowName, got2.WorkflowName, got3.WorkflowName)
	}

	if got1.Payload != "a" || got2.Payload != "b" || got3.Payload != "c" {
		t.Fatalf("unexpected payloads: %v, %v, %v", got1.Payload, got2.Payload, got3.Payload)
	}

	// New: ACK tasks to remove them from the queue.
	if err := q.Ack(ctx, got1.ID, "w1"); err != nil {
		t.Fatalf("Ack 1 failed: %v", err)
	}
	if err := q.Ack(ctx, got2.ID, "w1"); err != nil {
		t.Fatalf("Ack 2 failed: %v", err)
	}
	if err := q.Ack(ctx, got3.ID, "w1"); err != nil {
		t.Fatalf("Ack 3 failed: %v", err)
	}

	if q.Len() != 0 {
		t.Fatalf("expected Len 0 after ACKs, got %d", q.Len())
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
		tk, err := q.Dequeue(ctx, "w1", 200*time.Millisecond)
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

	_, err := q.Dequeue(ctx, "w1", 200*time.Millisecond)
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
	first, err := q.Dequeue(ctx, "w1", 200*time.Millisecond)
	if err != nil {
		t.Fatalf("Dequeue first failed: %v", err)
	}
	if first.WorkflowName != "immediate" || first.Payload != "A" {
		t.Fatalf("expected immediate task first, got %+v", first)
	}

	// Second Dequeue should block until notBefore is reached.
	start := time.Now()
	second, err := q.Dequeue(ctx, "w1", 200*time.Millisecond)
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
	_, err := q.Dequeue(ctx, "w1", 200*time.Millisecond)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatalf("expected Dequeue to fail due to context cancellation")
	}

	if elapsed > delay {
		t.Fatalf("Dequeue did not appear to honor cancellation; elapsed=%v, delay=%v", elapsed, delay)
	}
}

func TestSQLiteQueue_VisibilityTimeout_TaskRedeliveredAfterLeaseExpiry(t *testing.T) {
	q := newTestSQLiteQueue(t)
	ctx := context.Background()

	// enqueue one runnable task
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

	got1, err := q.Dequeue(ctx, "worker-1", 30*time.Millisecond)
	if err != nil {
		t.Fatalf("Dequeue 1: %v", err)
	}
	if got1 == nil {
		t.Fatalf("expected task")
	}

	// Don't ack. Wait for lease to expire, then it should be re-delivered.
	time.Sleep(50 * time.Millisecond)

	got2, err := q.Dequeue(ctx, "worker-2", 30*time.Millisecond)
	if err != nil {
		t.Fatalf("Dequeue 2: %v", err)
	}
	if got2 == nil {
		t.Fatalf("expected task after lease expiry")
	}
	if got2.ID != got1.ID {
		t.Fatalf("expected same task ID redelivered, got %q vs %q", got2.ID, got1.ID)
	}
}

func TestSQLiteQueue_Ack_RemovesTask(t *testing.T) {
	q := newTestSQLiteQueue(t)
	ctx := context.Background()

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

	got, err := q.Dequeue(ctx, "worker-1", 200*time.Millisecond)
	if err != nil || got == nil {
		t.Fatalf("Dequeue: got=%v err=%v", got, err)
	}
	if err := q.Ack(ctx, got.ID, "worker-1"); err != nil {
		t.Fatalf("Ack: %v", err)
	}

	// Ensure it doesn't come back.
	ctx2, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	got2, _ := q.Dequeue(ctx2, "worker-2", 10*time.Millisecond)
	if got2 != nil {
		t.Fatalf("expected no task after ack, got %#v", got2)
	}
}

func TestSQLiteQueue_Nack_RequeuesWithScheduleAndAttempts(t *testing.T) {
	q := newTestSQLiteQueue(t)
	ctx := context.Background()

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

	got, err := q.Dequeue(ctx, "worker-1", 200*time.Millisecond)
	if err != nil || got == nil {
		t.Fatalf("Dequeue: got=%v err=%v", got, err)
	}

	nb := time.Now().Add(40 * time.Millisecond)
	if err := q.Nack(ctx, got.ID, "worker-1", nb, got.Attempts+1); err != nil {
		t.Fatalf("Nack: %v", err)
	}

	// Should not be runnable immediately.
	ctx2, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	got2, _ := q.Dequeue(ctx2, "worker-2", 20*time.Millisecond)
	if got2 != nil {
		t.Fatalf("expected nil before notBefore, got %#v", got2)
	}

	// After schedule passes, it should be delivered with incremented attempts.
	time.Sleep(60 * time.Millisecond)
	got3, err := q.Dequeue(ctx, "worker-2", 200*time.Millisecond)
	if err != nil || got3 == nil {
		t.Fatalf("Dequeue after schedule: got=%v err=%v", got3, err)
	}
	if got3.ID != got.ID {
		t.Fatalf("expected same task id, got %q vs %q", got3.ID, got.ID)
	}
	if got3.Attempts != 1 {
		t.Fatalf("expected attempts 1, got %d", got3.Attempts)
	}
}
