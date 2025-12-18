package api

// StartWorkflowPayload is the payload for a "start-workflow" task placed on a task queue.
// It is public so both engine and worker can depend on a common type without creating
// an import cycle.
type StartWorkflowPayload struct {
	Input any
}
