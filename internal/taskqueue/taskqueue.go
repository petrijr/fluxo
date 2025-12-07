package taskqueue

import (
	"context"
	"time"
)

// TaskType identifies what the worker should do.
type TaskType string

const (
	TaskTypeStartWorkflow TaskType = "start-workflow"
	TaskTypeSignal        TaskType = "signal"
)

// Task represents a unit of work for the worker.
// First iteration: only "start workflow" tasks.
type Task struct {
	ID   string
	Type TaskType

	// For start-workflow tasks
	WorkflowName string

	// For signal tasks
	InstanceID string
	SignalName string

	// Payload is task-type specific:
	//   - start-workflow: StartWorkflowPayload
	//   - signal: arbitrary payload to pass to engine.Signal
	Payload any

	EnqueuedAt time.Time

	// NotBefore is the earliest time this task should be eligible
	// for processing. Zero value means "immediately" (i.e., at enqueue time).
	NotBefore time.Time
}

// Queue is a simple async task queue interface.
type Queue interface {
	// Enqueue adds a task to the queue. It should respect ctx for cancellation.
	Enqueue(ctx context.Context, t Task) error

	// Dequeue removes and returns the next task, blocking until one is available
	// or the context is cancelled.
	Dequeue(ctx context.Context) (*Task, error)

	// Len returns the approximate number of tasks queued.
	Len() int
}
