package api

import "context"

// AsyncStarter is implemented by engines that support queue-first asynchronous start.
type AsyncStarter interface {
	Engine

	// Start creates a workflow instance and schedules it for execution by a worker.
	// Engines without a configured queue may execute synchronously as a fallback.
	Start(ctx context.Context, name string, input any) (*WorkflowInstance, error)

	// StartVersion is like Start but selects an explicit workflow version.
	StartVersion(ctx context.Context, name, version string, input any) (*WorkflowInstance, error)
}

// HistoryReader allows reading an instance's event history.
type HistoryReader interface {
	// ListEvents returns all events for an instance in chronological order.
	ListEvents(ctx context.Context, instanceID string) ([]WorkflowEvent, error)
}

// WorkerDirect is implemented by engines to allow workers to execute tasks
// without re-enqueueing (avoids queue loops when Engine.Signal/Resume enqueue).
type WorkerDirect interface {
	// RunInstance runs an existing instance that was created earlier (typically by Start/StartVersion).
	RunInstance(ctx context.Context, instanceID string) (*WorkflowInstance, error)

	// SignalNow delivers a signal and continues execution immediately (no enqueue).
	SignalNow(ctx context.Context, id string, name string, payload any) (*WorkflowInstance, error)

	// ResumeNow resumes a failed workflow immediately (no enqueue).
	ResumeNow(ctx context.Context, id string) (*WorkflowInstance, error)
}
