package main

import (
	"context"
	"fmt"
	"time"

	"github.com/petrijr/fluxo"
)

type Request struct {
	Message string
}

func main() {
	ctx := context.Background()

	eng := fluxo.NewInMemoryEngine()
	q := fluxo.NewInMemoryQueue(128)

	// Auto-timeout any "waiting for signal" park after 2 seconds.
	w := fluxo.NewWorkerWithConfig(eng, q, fluxo.Config{
		DefaultSignalTimeout: 2 * time.Second,
	})

	flow := fluxo.New("wait-or-timeout").
		Step("request", func(ctx context.Context, input any) (any, error) {
			req := input.(Request)
			fmt.Printf("[request] %s\n", req.Message)
			return req, nil
		}).Step("wait", fluxo.WaitForSignalStep("done")).Step("finalize", func(ctx context.Context, input any) (any, error) {
		switch v := input.(type) {
		case fluxo.TimeoutPayload:
			return "TIMED OUT (" + v.Reason + ")", nil
		default:
			// Whatever was passed via Signal(instanceID, "done", payload)
			return fmt.Sprintf("GOT SIGNAL: %v", v), nil
		}
	})

	flow.MustRegister(eng)

	fmt.Println("== case 1: signal arrives before timeout ==")
	runCase(ctx, eng, w, flow.Name(), Request{Message: "please approve"}, true)

	fmt.Println()
	fmt.Println("== case 2: no signal; worker auto-delivers timeout ==")
	runCase(ctx, eng, w, flow.Name(), Request{Message: "please approve (but nobody does)"}, false)
}

func runCase(ctx context.Context, eng fluxo.Engine, w *fluxo.Worker, workflowName string, input any, sendSignal bool) {
	// Start asynchronously.
	if err := w.EnqueueStartWorkflow(ctx, workflowName, input); err != nil {
		panic(err)
	}

	// First ProcessOne starts the workflow. It will park WAITING on the signal step.
	if _, err := w.ProcessOne(ctx); err != nil {
		panic(err)
	}

	instances, err := eng.ListInstances(ctx, fluxo.InstanceListOptions{WorkflowName: workflowName})
	if err != nil {
		panic(err)
	}
	if len(instances) == 0 {
		panic("expected at least one instance")
	}
	instID := instances[len(instances)-1].ID

	if sendSignal {
		go func() {
			time.Sleep(500 * time.Millisecond)
			// enqueue a signal task (could also call eng.Signal directly).
			_ = w.EnqueueSignal(ctx, instID, "done", "approved ✅")
		}()
	}

	// Keep processing tasks until the instance completes (signal or timeout).
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		processed, err := w.ProcessOne(ctx)
		if err != nil {
			panic(err)
		}
		if !processed {
			continue
		}

		inst, err := eng.GetInstance(ctx, instID)
		if err != nil {
			panic(err)
		}
		if inst.Status == fluxo.StatusCompleted || inst.Status == fluxo.StatusFailed {
			fmt.Printf("instance %s finished: status=%s output=%v\n", inst.ID, inst.Status, inst.Output)
			return
		}
	}

	panic("timed out waiting for instance to finish")
}
