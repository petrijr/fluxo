package main

import (
	"context"
	"database/sql"
	"encoding/gob"
	"errors"
	"fmt"
	"time"

	"github.com/petrijr/fluxo"
	_ "modernc.org/sqlite"
)

// ApprovalRequest is the input to the approval workflow.
type ApprovalRequest struct {
	RequestID string
	Requester string
	Amount    float64
}

// Register ApprovalRequest for gob, because it is wrapped inside StartWorkflowPayload
// and persisted in the SQLite task queue.
func init() {
	gob.Register(ApprovalRequest{})
}

func main() {
	ctx := context.Background()

	// Single SQLite DB used by both eng and queue.
	db, err := sql.Open("sqlite", "file:approval_demo.db?mode=memory&cache=shared")
	if err != nil {
		panic(err)
	}
	defer db.Close()

	eng, err := fluxo.NewSQLiteEngine(db)
	if err != nil {
		panic(err)
	}

	queue, err := fluxo.NewSQLiteQueue(db)
	if err != nil {
		panic(err)
	}

	w := fluxo.NewWorkerWithConfig(eng, queue, fluxo.Config{
		MaxAttempts:          3,
		Backoff:              200 * time.Millisecond,
		DefaultSignalTimeout: 1 * time.Second, // auto-timeout after 1s
	})

	if err := registerApprovalWorkflow(eng); err != nil {
		panic(err)
	}

	fmt.Println("=== Scenario 1: auto-timeout approval ===")
	if err := runAutoTimeoutScenario(ctx, eng, w); err != nil {
		fmt.Printf("auto-timeout scenario failed: %v\n", err)
	}

	fmt.Println()
	fmt.Println("=== Scenario 2: manual approval before timeout ===")
	if err := runManualApprovalScenario(ctx, eng, w); err != nil {
		fmt.Printf("manual-approval scenario failed: %v\n", err)
	}
}

// registerApprovalWorkflow sets up a simple approval workflow:
//
//	prepare -> wait-approval -> finalize
//
// finalize inspects whether the input is a TimeoutPayload or a
// real approval payload.
func registerApprovalWorkflow(engine fluxo.Engine) error {
	wf := fluxo.WorkflowDefinition{
		Name: "purchase-approval",
		Steps: []fluxo.StepDefinition{
			{
				Name: "prepare",
				Fn: func(ctx context.Context, input any) (any, error) {
					req, ok := input.(ApprovalRequest)
					if !ok {
						return nil, fmt.Errorf("expected ApprovalRequest, got %T", input)
					}
					fmt.Printf("[prepare] request %s by %s for %.2f€\n",
						req.RequestID, req.Requester, req.Amount)
					// Pass the same request to the next step.
					return req, nil
				},
			},
			{
				Name: "wait-approval",
				Fn:   fluxo.WaitForSignalStep("approve"),
			},
			{
				Name: "finalize",
				Fn: func(ctx context.Context, input any) (any, error) {
					switch v := input.(type) {
					case fluxo.TimeoutPayload:
						fmt.Printf("[finalize] request timed out: %s\n", v.Reason)
						return "timeout:" + v.Reason, nil
					case string:
						fmt.Printf("[finalize] request approved: %s\n", v)
						return "approved:" + v, nil
					default:
						return nil, fmt.Errorf("unexpected finalize input type %T", input)
					}
				},
			},
		},
	}
	return engine.RegisterWorkflow(wf)
}

// runAutoTimeoutScenario:
//   - Enqueues a start-workflow task.
//   - First ProcessOne runs workflow until wait-approval and schedules a timeout signal.
//   - Second ProcessOne blocks until timeout, delivers the timeout signal, and completes the workflow.
func runAutoTimeoutScenario(ctx context.Context, engine fluxo.Engine, w *fluxo.Worker) error {
	req := ApprovalRequest{
		RequestID: "REQ-001",
		Requester: "Alice",
		Amount:    123.45,
	}

	if err := w.EnqueueStartWorkflow(ctx, "purchase-approval", req); err != nil {
		return fmt.Errorf("EnqueueStartWorkflow failed: %w", err)
	}

	start := time.Now()

	// First task: start workflow, park on wait-approval, schedule timeout.
	fmt.Println("[worker] processing start-workflow (will park & schedule timeout)...")
	processed, err := w.ProcessOne(ctx)
	if err != nil {
		return fmt.Errorf("first ProcessOne returned error: %w", err)
	}
	if !processed {
		return errors.New("expected first task to be processed")
	}

	instances, err := engine.ListInstances(ctx, fluxo.InstanceListOptions{
		WorkflowName: "purchase-approval",
	})
	if err != nil {
		return fmt.Errorf("ListInstances failed: %w", err)
	}
	if len(instances) != 1 {
		return fmt.Errorf("expected 1 instance after first run, got %d", len(instances))
	}
	if instances[0].Status != fluxo.StatusWaiting {
		return fmt.Errorf("expected WAITING, got %s", instances[0].Status)
	}
	fmt.Printf("[main] instance %s is WAITING for approval\n", instances[0].ID)

	// Second task: will block until DefaultSignalTimeout, then deliver timeout signal.
	fmt.Println("[worker] processing timeout signal (will block until timeout)...")
	processed, err = w.ProcessOne(ctx)
	elapsed := time.Since(start)

	if err != nil {
		return fmt.Errorf("second ProcessOne returned error: %w", err)
	}
	if !processed {
		return errors.New("expected timeout signal task to be processed")
	}

	instances, err = engine.ListInstances(ctx, fluxo.InstanceListOptions{
		WorkflowName: "purchase-approval",
	})
	if err != nil {
		return fmt.Errorf("ListInstances after timeout failed: %w", err)
	}
	if len(instances) != 1 {
		return fmt.Errorf("expected 1 instance after timeout, got %d", len(instances))
	}
	inst := instances[0]
	fmt.Printf("[main] after timeout: instance %s status=%s output=%v (elapsed=%v)\n",
		inst.ID, inst.Status, inst.Output, elapsed)

	return nil
}

// runManualApprovalScenario:
//   - Enqueues a start-workflow task.
//   - First ProcessOne runs workflow until wait-approval and (again) schedules a timeout.
//   - Before the timeout fires, main enqueues a manual approval signal.
//   - Second ProcessOne delivers the approval signal and completes the workflow.
//   - The later timeout signal (if still in the queue) becomes a no-op.
func runManualApprovalScenario(ctx context.Context, engine fluxo.Engine, w *fluxo.Worker) error {
	req := ApprovalRequest{
		RequestID: "REQ-002",
		Requester: "Bob",
		Amount:    777.00,
	}

	if err := w.EnqueueStartWorkflow(ctx, "purchase-approval", req); err != nil {
		return fmt.Errorf("EnqueueStartWorkflow failed: %w", err)
	}

	// First task: start and park.
	fmt.Println("[worker] processing start-workflow (will park & schedule timeout)...")
	processed, err := w.ProcessOne(ctx)
	if err != nil {
		return fmt.Errorf("first ProcessOne returned error: %w", err)
	}
	if !processed {
		return errors.New("expected first task to be processed")
	}

	instances, err := engine.ListInstances(ctx, fluxo.InstanceListOptions{
		WorkflowName: "purchase-approval",
	})
	if err != nil {
		return fmt.Errorf("ListInstances failed: %w", err)
	}
	if len(instances) == 0 {
		return errors.New("expected at least 1 instance after first run")
	}

	// Pick the most recent waiting instance.
	var waiting *fluxo.WorkflowInstance
	for _, inst := range instances {
		if inst.Status == fluxo.StatusWaiting {
			instCopy := inst
			waiting = instCopy
			break
		}
	}
	if waiting == nil {
		return errors.New("no WAITING instance found")
	}
	fmt.Printf("[main] instance %s is WAITING for approval\n", waiting.ID)

	// Simulate manager approving before timeout.
	fmt.Println("[main] enqueuing manual approval signal...")
	if err := w.EnqueueSignal(ctx, waiting.ID, "approve", "APPROVED_BY_MANAGER"); err != nil {
		return fmt.Errorf("EnqueueSignal failed: %w", err)
	}

	// Second task: deliver approval signal, complete workflow.
	fmt.Println("[worker] processing manual approval signal...")
	processed, err = w.ProcessOne(ctx)
	if err != nil {
		return fmt.Errorf("second ProcessOne returned error: %w", err)
	}
	if !processed {
		return errors.New("expected approval signal task to be processed")
	}

	instances, err = engine.ListInstances(ctx, fluxo.InstanceListOptions{
		WorkflowName: "purchase-approval",
	})
	if err != nil {
		return fmt.Errorf("ListInstances after approval failed: %w", err)
	}
	if len(instances) == 0 {
		return errors.New("expected instances after approval")
	}

	// Find the completed instance from this scenario.
	var completed *fluxo.WorkflowInstance
	for _, inst := range instances {
		if inst.Status == fluxo.StatusCompleted {
			instCopy := inst
			completed = instCopy
			break
		}
	}
	if completed == nil {
		return errors.New("no COMPLETED instance found after approval")
	}

	fmt.Printf("[main] after manual approval: instance %s status=%s output=%v\n",
		completed.ID, completed.Status, completed.Output)

	fmt.Println("[main] note: an auto-timeout task may still be in the queue,")
	fmt.Println("       but when processed it will see the instance is already")
	fmt.Println("       completed and act as a no-op (best-effort timeout).")

	return nil
}
