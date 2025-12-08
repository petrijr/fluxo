package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/petrijr/fluxo"
)

func main() {
	ctx := context.Background()

	runner := fluxo.NewLocalRunner()

	// Define a simple workflow using the builder API.
	flow := fluxo.New("greet-user").
		Step("say-hello", sayHello).
		Step("decorate", decorateMessage)

	// Register the workflow on the runner's engine.
	flow.MustRegister(runner.Engine)

	// --- Synchronous run (debug-style) ---

	inst, err := fluxo.Run(ctx, runner.Engine, flow.Name(), "world")
	if err != nil {
		log.Fatalf("sync Run failed: %v", err)
	}

	log.Printf("[sync] status=%v output=%v", inst.Status, inst.Output)

	// --- Asynchronous run with local worker/queue ---

	if err := runner.StartWorkers(ctx, 2); err != nil {
		log.Fatalf("StartWorkers failed: %v", err)
	}
	defer runner.Stop()

	if err := runner.StartWorkflowAsync(ctx, flow.Name(), "async-world"); err != nil {
		log.Fatalf("StartWorkflowAsync failed: %v", err)
	}

	// Give the worker some time to process the queued workflow.
	time.Sleep(200 * time.Millisecond)

	// In a real app you'd track the instance ID from the worker task or
	// use ListInstances; here we just list everything and log it.
	instances, err := fluxo.ListInstances(ctx, runner.Engine, fluxo.InstanceListOptions{})
	if err != nil {
		log.Fatalf("ListInstances failed: %v", err)
	}

	log.Printf("[async] instances:")
	for _, inst := range instances {
		log.Printf("- id=%s name=%s status=%v output=%v", inst.ID, inst.Name, inst.Status, inst.Output)
	}
}

// sayHello is the first step; it expects a string input and returns a greeting.
func sayHello(ctx context.Context, input any) (any, error) {
	name, ok := input.(string)
	if !ok {
		return nil, fmt.Errorf("sayHello: expected string input, got %T", input)
	}
	msg := fmt.Sprintf("hello, %s", name)
	log.Printf("[sayHello] %s", msg)
	return msg, nil
}

// decorateMessage is the second step; it expects a string and decorates it.
func decorateMessage(ctx context.Context, input any) (any, error) {
	msg, ok := input.(string)
	if !ok {
		return nil, fmt.Errorf("decorateMessage: expected string input, got %T", input)
	}
	out := fmt.Sprintf(">>> %s <<<", msg)
	log.Printf("[decorateMessage] %s", out)
	return out, nil
}
