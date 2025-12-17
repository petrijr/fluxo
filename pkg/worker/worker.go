package worker

import (
	"context"
	"crypto/rand"
	"encoding/gob"
	"encoding/hex"
	"errors"
	"time"

	"github.com/petrijr/fluxo/internal/taskqueue"
	"github.com/petrijr/fluxo/pkg/api"
)

func randomWorkerID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "worker-" + time.Now().Format("20060102150405.000000000")
	}
	return "worker-" + hex.EncodeToString(b)
}

func init() {
	gob.Register(StartWorkflowPayload{})
}

// StartWorkflowPayload is the payload for a "start-workflow" task.
type StartWorkflowPayload struct {
	Input any
}

// Config controls worker-level retries of tasks.
type Config struct {
	// MaxAttempts is the maximum number of attempts to process a given task,
	// including the initial one. If <= 0, it defaults to 1 (no retries).
	MaxAttempts int

	// Backoff is the delay between failed attempts when re-enqueuing a task.
	// If zero, retries happen immediately (i.e., the task is re-enqueued with NotBefore = now).
	Backoff time.Duration

	// DefaultSignalTimeout is how long the worker should wait after a workflow
	// parks on a signal before scheduling an automatic timeout signal.
	// If zero, no auto-timeout signals are scheduled.
	DefaultSignalTimeout time.Duration

	// WorkerID identifies this worker for task leases.
	// If empty, a random id is generated.
	WorkerID string

	// LeaseTTL is the visibility timeout for dequeued tasks.
	// If zero, a sensible default is used.
	LeaseTTL time.Duration

	// HeartbeatInterval controls how often the worker renews task leases
	// while a task handler is running. If zero, defaults to LeaseTTL / 3
	// with a minimum clamp applied.
	HeartbeatInterval time.Duration
}

// Worker pulls tasks from a Queue and executes them using an Engine.
type Worker struct {
	engine api.Engine
	queue  taskqueue.Queue
	cfg    Config
}

// New creates a new Worker.
func New(engine api.Engine, queue taskqueue.Queue) *Worker {
	return NewWithConfig(engine, queue, Config{
		MaxAttempts: 1,
	})
}

// NewWithConfig creates a Worker with the given retry config.
func NewWithConfig(engine api.Engine, queue taskqueue.Queue, cfg Config) *Worker {
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 1
	}
	if cfg.WorkerID == "" {
		cfg.WorkerID = randomWorkerID()
	}
	if cfg.LeaseTTL <= 0 {
		cfg.LeaseTTL = 30 * time.Second
	}
	// HeartbeatInterval default: derived from LeaseTTL.
	// We clamp later in ProcessOne to avoid very small intervals.
	if cfg.HeartbeatInterval < 0 {
		cfg.HeartbeatInterval = 0
	}
	return &Worker{
		engine: engine,
		queue:  queue,
		cfg:    cfg,
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
		NotBefore:  time.Time{},
		Attempts:   0,
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
		Attempts:   0,
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
		NotBefore:  time.Time{},
		Attempts:   0,
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
		Attempts:   0,
	}
	return w.queue.Enqueue(ctx, t)
}

// ProcessOne pulls a single task from the queue and processes it.
// Returns (processed, error):
//   - processed == false, err == nil: no task processed (only happens if ctx cancelled before a task was obtained)
//   - processed == true: a task was processed; err indicates whether the handler succeeded.
func (w *Worker) ProcessOne(ctx context.Context) (bool, error) {
	task, err := w.queue.Dequeue(ctx, w.cfg.WorkerID, w.cfg.LeaseTTL)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return false, err
		}
		return false, err
	}
	if task == nil {
		return false, nil
	}

	handleErr := func(err error) error {
		if err == nil {
			// Successful processing: ack the leased task.
			if ackErr := w.queue.Ack(ctx, task.ID, w.cfg.WorkerID); ackErr != nil {
				return ackErr
			}
			return nil
		}

		// Check whether we should retry this task at the worker level.
		if task.Attempts+1 < w.cfg.MaxAttempts {
			nextAttempts := task.Attempts + 1
			nb := time.Now()
			if w.cfg.Backoff > 0 {
				nb = nb.Add(w.cfg.Backoff)
			}
			// Release back to queue with updated schedule/attempts.
			if nerr := w.queue.Nack(ctx, task.ID, w.cfg.WorkerID, nb, nextAttempts); nerr != nil {
				// If we can't Nack, fall back to Ack+re-enqueue best effort.
				_ = w.queue.Ack(ctx, task.ID, w.cfg.WorkerID)
				retryTask := *task
				retryTask.Attempts = nextAttempts
				retryTask.NotBefore = nb
				_ = w.queue.Enqueue(ctx, retryTask)
				return err
			}
			return nil
		}

		// No retry: ack (remove) the task and propagate.
		_ = w.queue.Ack(ctx, task.ID, w.cfg.WorkerID)
		return err
	}

	// Heartbeat runs ONLY while the engine call is in-flight (Run/Signal).
	// It renews the task lease to prevent re-delivery for long-running handlers.
	startHeartbeat := func() (stop func()) {
		// Determine heartbeat interval.
		interval := w.cfg.HeartbeatInterval
		if interval <= 0 {
			interval = w.cfg.LeaseTTL / 3
		}
		// Clamp to a sane minimum to avoid tight loops.
		const minHeartbeat = 10 * time.Millisecond
		if interval < minHeartbeat {
			interval = minHeartbeat
		}
		maxHeartbeat := w.cfg.LeaseTTL / 2
		if maxHeartbeat > 0 && interval > maxHeartbeat {
			interval = maxHeartbeat
		}

		hbCtx, cancel := context.WithCancel(context.Background())
		t := time.NewTicker(interval)
		go func(taskID string) {
			defer t.Stop()
			for {
				select {
				case <-hbCtx.Done():
					return
				case <-t.C:
					// Best-effort renewal. If it fails (e.g. ownership lost), the handler
					// still continues, but the task may become visible again.
					_ = w.queue.RenewLease(context.Background(), taskID, w.cfg.WorkerID, w.cfg.LeaseTTL)
				}
			}
		}(task.ID)

		return func() { cancel() }
	}

	switch task.Type {
	case taskqueue.TaskTypeStartWorkflow:
		payload, ok := task.Payload.(StartWorkflowPayload)
		if !ok {
			return true, errors.New("invalid payload type for start-workflow task")
		}

		stopHB := startHeartbeat()
		inst, runErr := w.engine.Run(ctx, task.WorkflowName, payload.Input)
		stopHB()
		if runErr != nil {
			// Special case: workflow is now WAITING for a signal.
			if sigName, ok := api.IsWaitForSignalError(runErr); ok && inst != nil {
				if w.cfg.DefaultSignalTimeout > 0 {
					timeoutTask := taskqueue.Task{
						Type:       taskqueue.TaskTypeSignal,
						InstanceID: inst.ID,
						SignalName: sigName,
						Payload: api.TimeoutPayload{
							Reason: "signal timeout",
						},
						EnqueuedAt: time.Now(),
						NotBefore:  time.Now().Add(w.cfg.DefaultSignalTimeout),
						Attempts:   0,
					}
					_ = w.queue.Enqueue(ctx, timeoutTask)
				}
				// We successfully parked the workflow and possibly scheduled a timeout.
				// This is not a failure of the task itself.
				return true, handleErr(nil)
			}

			// Other errors: use worker-level retry logic.
			return true, handleErr(runErr)
		}

		// Run succeeded normally.
		return true, handleErr(nil)

	case taskqueue.TaskTypeSignal:
		// Deliver the signal.
		stopHB := startHeartbeat()
		_, sigErr := w.engine.Signal(ctx, task.InstanceID, task.SignalName, task.Payload)
		stopHB()

		// If this is an auto-timeout signal, we consider it best effort:
		// - If the instance is no longer waiting (e.g., approved early),
		//   Signal will fail; we just swallow the error and do not retry.
		if _, isTimeout := task.Payload.(api.TimeoutPayload); isTimeout {
			_ = w.queue.Ack(ctx, task.ID, w.cfg.WorkerID)
			return true, handleErr(nil)
		}

		return true, handleErr(sigErr)

	default:
		_ = w.queue.Ack(ctx, task.ID, w.cfg.WorkerID)
		return true, errors.New("unknown task type: " + string(task.Type))
	}
}
