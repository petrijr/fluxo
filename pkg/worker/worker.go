package worker

import (
	"context"
	"encoding/gob"
	"errors"
	"time"

	"github.com/petrijr/fluxo/internal/taskqueue"
	"github.com/petrijr/fluxo/pkg/api"
)

func init() {
	gob.Register(StartWorkflowPayload{})
}

// StartWorkflowPayload is the payload for a "start-workflow" task.
type StartWorkflowPayload struct {
	Input any
}

// Worker pulls tasks from a Queue and executes them using an Engine.
type Worker struct {
	engine api.Engine
	queue  taskqueue.Queue
}

// New creates a new Worker.
func New(engine api.Engine, queue taskqueue.Queue) *Worker {
	return &Worker{
		engine: engine,
		queue:  queue,
	}
}

// EnqueueStartWorkflow enqueues a task to start a workflow asynchronously.
// It does NOT run the workflow itself; that is done by ProcessOne.
func (w *Worker) EnqueueStartWorkflow(ctx context.Context, workflowName string, input any) error {
	t := taskqueue.Task{
		ID:           "", // optional; could be filled with a UUID later
		Type:         taskqueue.TaskTypeStartWorkflow,
		WorkflowName: workflowName,
		Payload: StartWorkflowPayload{
			Input: input,
		},
		EnqueuedAt: time.Now(),
	}
	return w.queue.Enqueue(ctx, t)
}

// EnqueueStartWorkflowAt enqueues a task to start a workflow no earlier than
// the given time 'at'.
func (w *Worker) EnqueueStartWorkflowAt(ctx context.Context, workflowName string, input any, at time.Time) error {
	t := taskqueue.Task{
		ID:           "",
		Type:         taskqueue.TaskTypeStartWorkflow,
		WorkflowName: workflowName,
		Payload: StartWorkflowPayload{
			Input: input,
		},
		EnqueuedAt: time.Now(),
		NotBefore:  at,
	}
	return w.queue.Enqueue(ctx, t)
}

// EnqueueSignal enqueues a task to deliver a signal to a waiting workflow
// instance. The signal will be processed asynchronously by ProcessOne.
func (w *Worker) EnqueueSignal(ctx context.Context, instanceID string, name string, payload any) error {
	t := taskqueue.Task{
		ID:         "", // can be filled with a UUID later if desired
		Type:       taskqueue.TaskTypeSignal,
		InstanceID: instanceID,
		SignalName: name,
		Payload:    payload,
		EnqueuedAt: time.Now(),
	}
	return w.queue.Enqueue(ctx, t)
}

// EnqueueSignalAt enqueues a signal task that will be delivered to the
// target instance no earlier than 'at'.
func (w *Worker) EnqueueSignalAt(ctx context.Context, instanceID string, name string, payload any, at time.Time) error {
	t := taskqueue.Task{
		ID:         "",
		Type:       taskqueue.TaskTypeSignal,
		InstanceID: instanceID,
		SignalName: name,
		Payload:    payload,
		EnqueuedAt: time.Now(),
		NotBefore:  at,
	}
	return w.queue.Enqueue(ctx, t)
}

// ProcessOne pulls a single task from the queue and processes it.
// Returns (processed, error):
//   - processed == false, err == nil: no task processed (only happens if ctx cancelled before a task was obtained)
//   - processed == true: a task was processed; err indicates whether the handler succeeded.
func (w *Worker) ProcessOne(ctx context.Context) (bool, error) {
	task, err := w.queue.Dequeue(ctx)
	if err != nil {
		// Context cancellation or other dequeue error: nothing processed.
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return false, err
		}
		return false, err
	}
	if task == nil {
		return false, nil
	}

	switch task.Type {
	case taskqueue.TaskTypeStartWorkflow:
		payload, ok := task.Payload.(StartWorkflowPayload)
		if !ok {
			return true, errors.New("invalid payload type for start-workflow task")
		}
		_, runErr := w.engine.Run(ctx, task.WorkflowName, payload.Input)
		return true, runErr

	case taskqueue.TaskTypeSignal:
		// Payload is passed directly to engine.Signal.
		_, sigErr := w.engine.Signal(ctx, task.InstanceID, task.SignalName, task.Payload)
		return true, sigErr

	default:
		// Unknown task type; mark as processed but return an error so this isn't silently ignored.
		return true, errors.New("unknown task type: " + string(task.Type))
	}
}
