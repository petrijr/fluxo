package worker_test

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/petrijr/fluxo"
	"github.com/petrijr/fluxo/pkg/worker"
)

// ExampleWorker demonstrates constructing a Worker explicitly and using it
// to process tasks from a queue.
func ExampleWorker() {
	ctx := context.Background()

	// Engine and queue (use Fluxo helpers so this matches real usage).
	eng := fluxo.NewInMemoryEngine()
	queue := fluxo.NewInMemoryQueue(1024)

	// Define and register a simple workflow.
	flow := fluxo.New("BackgroundJob").
		Step("doWork", func(ctx context.Context, input any) (any, error) {
			log.Printf("[doWork] processing input: %v", input)
			return fmt.Sprintf("processed:%v", input), nil
		})

	if err := flow.Register(eng); err != nil {
		log.Fatal(err)
	}

	// Configure the worker (with a simple retry policy).
	w := worker.NewWithConfig(eng, queue, worker.Config{
		MaxAttempts: 3,
		Backoff:     10 * time.Millisecond,
	})

	// Enqueue a workflow start task.
	if err := w.EnqueueStartWorkflow(ctx, flow.Name(), "payload"); err != nil {
		log.Fatal(err)
	}

	// Process a single task. In a real application you would run ProcessOne
	// in a loop or via LocalRunner / your own worker loop.
	processed, err := w.ProcessOne(ctx)
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("processed=%v", processed)
}
