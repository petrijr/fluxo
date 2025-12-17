package fluxo

import (
	"context"
	"errors"
	"log"
	"sync"

	"github.com/petrijr/fluxo/internal/taskqueue"
	"github.com/petrijr/fluxo/pkg/worker"
)

// LocalRunner bundles an in-memory Engine, an in-memory task queue, and a Worker
// to provide a simple "local runner" for development and debugging.
//
// Typical usage:
//
//	runner := fluxo.NewLocalRunner()
//	flow := fluxo.New("my-flow").Step(...)
//	flow.MustRegister(runner.Engine)
//
//	// Synchronous run (no queue/worker involved):
//	inst, err := fluxo.Run(ctx, runner.Engine, flow.Name(), input)
//
//	// Asynchronous run:
//	_ = runner.StartWorkers(ctx, 2)
//	_ = runner.StartWorkflowAsync(ctx, flow.Name(), input)
//	...
//	runner.Stop()
type LocalRunner struct {
	// Engine is the in-memory workflow engine used by this runner.
	Engine Engine

	// Queue is the in-memory task queue used by the Worker.
	Queue taskqueue.Queue

	// Worker processes tasks from Queue using Engine.
	Worker *worker.Worker

	mu      sync.Mutex
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	running bool
}

// NewLocalRunner constructs a LocalRunner backed by an in-memory engine,
// in-memory queue, and a Worker with default config.
//
// This is intended for local development, tests, and simple single-process
// deployments.
func NewLocalRunner() *LocalRunner {
	eng := NewInMemoryEngine()
	q := taskqueue.NewInMemoryQueue(1024)
	w := worker.New(eng, q)

	return &LocalRunner{
		Engine: eng,
		Queue:  q,
		Worker: w,
	}
}

// StartWorkers starts 'concurrency' worker goroutines that continuously call
// Worker.ProcessOne(ctx) until the context is cancelled via Stop.
//
// If StartWorkers is called more than once without Stop, it returns an error.
func (r *LocalRunner) StartWorkers(ctx context.Context, concurrency int) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.running {
		return errors.New("fluxo: LocalRunner already started")
	}

	if concurrency <= 0 {
		concurrency = 1
	}

	ctx, cancel := context.WithCancel(ctx)
	r.cancel = cancel
	r.running = true

	r.wg.Add(concurrency)
	for i := 0; i < concurrency; i++ {
		go func() {
			defer r.wg.Done()

			for {
				processed, err := r.Worker.ProcessOne(ctx)
				if err != nil {
					// For local runner we treat cancellation as a clean shutdown signal.
					if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
						return
					}
					// For other errors, log and keep going so a single bad task
					// doesn't kill the worker loop.
					log.Printf("fluxo: local runner worker error: %v", err)
					continue
				}
				if !processed {
					// This only happens if ctx was cancelled before a task was obtained.
					// Loop will exit on next Dequeue when err == context.Canceled.
					continue
				}
			}
		}()
	}

	return nil
}

// Stop cancels all worker goroutines started by StartWorkers and waits
// for them to exit.
func (r *LocalRunner) Stop() {
	r.mu.Lock()
	if !r.running {
		r.mu.Unlock()
		return
	}
	cancel := r.cancel
	r.running = false
	r.cancel = nil
	r.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	r.wg.Wait()
}

// StartWorkflowAsync enqueues a task to start the given workflow asynchronously.
// The workflow must already be registered on LocalRunner.Engine.
func (r *LocalRunner) StartWorkflowAsync(ctx context.Context, workflowName string, input any) error {
	return r.Worker.EnqueueStartWorkflow(ctx, workflowName, input)
}

// SignalAsync enqueues a task to deliver a signal to a workflow instance.
// The instance will process the signal when a worker picks up the task.
func (r *LocalRunner) SignalAsync(ctx context.Context, instanceID, name string, payload any) error {
	return r.Worker.EnqueueSignal(ctx, instanceID, name, payload)
}
