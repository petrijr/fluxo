package fluxo_test

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/petrijr/fluxo"
)

// Example_flowBuilder demonstrates defining and running a simple workflow
// using the high-level FlowBuilder API and an in-memory engine.
func Example_flowBuilder() {
	ctx := context.Background()

	flow := fluxo.New("Greeting").
		Step("sayHello", sayHello).
		Step("decorateMessage", decorateMessage)

	eng := fluxo.NewInMemoryEngine()

	if err := flow.Register(eng); err != nil {
		log.Fatal(err)
	}

	inst, err := fluxo.Run(ctx, eng, flow.Name(), "Gopher")
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("workflow %q finished with status %s and output %v\n",
		inst.ID, inst.Status, inst.Output)
}

// Example_localRunner demonstrates using LocalRunner to execute workflows
// with an in-process engine, queue, and worker.
func Example_localRunner() {
	ctx := context.Background()

	runner := fluxo.NewLocalRunner()

	flow := fluxo.New("Greeting").
		Step("sayHello", sayHello).
		Step("decorateMessage", decorateMessage)

	if err := flow.Register(runner.Engine); err != nil {
		log.Fatal(err)
	}

	// Start one worker goroutine.
	if err := runner.StartWorkers(ctx, 1); err != nil {
		log.Fatal(err)
	}
	defer runner.Stop()

	// Enqueue an asynchronous workflow start.
	if err := runner.StartWorkflowAsync(ctx, flow.Name(), "Gopher"); err != nil {
		log.Fatal(err)
	}

	// In a real application you'd wait on instance completion or poll;
	// for example purposes, just give the worker a moment to run.
	time.Sleep(500 * time.Millisecond)
}

func sayHello(ctx context.Context, input any) (any, error) {
	name, ok := input.(string)
	if !ok {
		return nil, fmt.Errorf("sayHello: expected string input, got %T", input)
	}
	msg := fmt.Sprintf("hello, %s", name)
	log.Printf("[sayHello] %s", msg)
	return msg, nil
}

func decorateMessage(ctx context.Context, input any) (any, error) {
	msg, ok := input.(string)
	if !ok {
		return nil, fmt.Errorf("decorateMessage: expected string input, got %T", input)
	}
	out := fmt.Sprintf("*** %s ***", msg)
	log.Printf("[decorateMessage] %s", out)
	return out, nil
}
