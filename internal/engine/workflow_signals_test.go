package engine

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/petrijr/fluxo/pkg/api"
)

type signalEngineFactory func(t *testing.T) api.Engine

func signalInMemoryEngine(t *testing.T) api.Engine {
	t.Helper()
	return NewInMemoryEngine()
}

func signalSQLiteEngine(t *testing.T) api.Engine {
	t.Helper()

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open failed: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	eng, err := NewSQLiteEngine(db)
	if err != nil {
		t.Fatalf("NewSQLiteEngine failed: %v", err)
	}
	return eng
}

func TestSignals_WaitAndThenResume(t *testing.T) {
	factories := map[string]signalEngineFactory{
		"in-memory": signalInMemoryEngine,
		"sqlite":    signalSQLiteEngine,
	}

	for name, factory := range factories {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			engine := factory(t)

			wf := api.WorkflowDefinition{
				Name: "approval-flow",
				Steps: []api.StepDefinition{
					{
						Name: "prepare",
						Fn: func(ctx context.Context, input any) (any, error) {
							return "prepared", nil
						},
					},
					{
						Name: "wait-approval",
						Fn:   api.WaitForSignalStep("approve"),
					},
					{
						Name: "finalize",
						Fn: func(ctx context.Context, input any) (any, error) {
							// input should be the signal payload "APPROVED".
							s, ok := input.(string)
							if !ok {
								return nil, errors.New("expected string payload in finalize")
							}
							return "result:" + s, nil
						},
					},
				},
			}

			if err := engine.RegisterWorkflow(wf); err != nil {
				t.Fatalf("RegisterWorkflow failed: %v", err)
			}

			// First run: should stop at wait-approval and mark instance WAITING.
			inst, err := engine.Run(ctx, "approval-flow", nil)
			if err == nil {
				t.Fatalf("expected Run to return wait-for-signal error")
			}

			sigName, ok := api.IsWaitForSignalError(err)
			if !ok || sigName != "approve" {
				t.Fatalf("expected wait-for-signal error for 'approve', got: %v", err)
			}

			if inst.Status != api.StatusWaiting {
				t.Fatalf("expected status WAITING, got %q", inst.Status)
			}
			if inst.CurrentStep != 1 {
				t.Fatalf("expected CurrentStep 1 at wait, got %d", inst.CurrentStep)
			}

			// Now deliver the signal and resume.
			resumed, err := engine.Signal(ctx, inst.ID, "approve", "APPROVED")
			if err != nil {
				t.Fatalf("Signal failed: %v", err)
			}

			if resumed.ID != inst.ID {
				t.Fatalf("expected same instance ID on resume")
			}
			if resumed.Status != api.StatusCompleted {
				t.Fatalf("expected COMPLETED after signal, got %q", resumed.Status)
			}
			if resumed.Output != "result:APPROVED" {
				t.Fatalf("unexpected output after signal: %v", resumed.Output)
			}
			if resumed.CurrentStep != len(wf.Steps) {
				t.Fatalf("expected CurrentStep %d, got %d", len(wf.Steps), resumed.CurrentStep)
			}
		})
	}
}

func TestSignals_CannotSignalNonWaitingInstance(t *testing.T) {
	engine := signalInMemoryEngine(t)

	wf := api.WorkflowDefinition{
		Name: "simple",
		Steps: []api.StepDefinition{
			{
				Name: "only",
				Fn: func(ctx context.Context, input any) (any, error) {
					return "ok", nil
				},
			},
		},
	}

	if err := engine.RegisterWorkflow(wf); err != nil {
		t.Fatalf("RegisterWorkflow failed: %v", err)
	}

	inst, err := engine.Run(context.Background(), "simple", nil)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if inst.Status != api.StatusCompleted {
		t.Fatalf("expected COMPLETED, got %q", inst.Status)
	}

	_, err = engine.Signal(context.Background(), inst.ID, "whatever", "payload")
	if err == nil {
		t.Fatalf("expected error when signaling non-WAITING instance")
	}
}

//goland:noinspection GoDfaErrorMayBeNotNil
func TestSignals_WrongSignalNameKeepsWaiting(t *testing.T) {
	engine := signalInMemoryEngine(t)

	wf := api.WorkflowDefinition{
		Name: "approval-flow-mismatch",
		Steps: []api.StepDefinition{
			{
				Name: "wait-approval",
				Fn:   api.WaitForSignalStep("approve"),
			},
		},
	}

	if err := engine.RegisterWorkflow(wf); err != nil {
		t.Fatalf("RegisterWorkflow failed: %v", err)
	}

	ctx := context.Background()

	inst, err := engine.Run(ctx, "approval-flow-mismatch", nil)
	if err == nil {
		t.Fatalf("expected wait-for-signal error")
	}

	if inst.Status != api.StatusWaiting {
		t.Fatalf("expected WAITING, got %q", inst.Status)
	}

	// Signal with the wrong name; the step will see a mismatched SignalPayload
	// and request to wait again.
	resumed, err := engine.Signal(ctx, inst.ID, "wrong-name", "nope")
	if err == nil {
		t.Fatalf("expected another wait-for-signal error")
	}
	sigName, ok := api.IsWaitForSignalError(err)
	if !ok || sigName != "approve" {
		t.Fatalf("expected wait-for-signal error for 'approve', got %v", err)
	}

	if resumed.Status != api.StatusWaiting {
		t.Fatalf("expected WAITING after wrong signal, got %q", resumed.Status)
	}
	if resumed.CurrentStep != 0 {
		// There is only one step in this workflow, index 0.
		t.Fatalf("expected CurrentStep 0, got %d", resumed.CurrentStep)
	}
}

func TestSignals_WaitForAnySignal_ApproveOrReject(t *testing.T) {
	factories := map[string]signalEngineFactory{
		"in-memory": signalInMemoryEngine,
		"sqlite":    signalSQLiteEngine,
	}

	for name, factory := range factories {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			engine := factory(t)

			// Workflow:
			//   prepare -> wait-decision (approve/reject) -> finalize
			wf := api.WorkflowDefinition{
				Name: "approval-decisions",
				Steps: []api.StepDefinition{
					{
						Name: "prepare",
						Fn: func(ctx context.Context, input any) (any, error) {
							return "request-123", nil
						},
					},
					{
						Name: "wait-decision",
						Fn:   api.WaitForAnySignalStep("approve", "reject"),
					},
					{
						Name: "finalize",
						Fn: func(ctx context.Context, input any) (any, error) {
							sp, ok := input.(api.SignalPayload)
							if !ok {
								return nil, errors.New("expected SignalPayload in finalize")
							}
							switch sp.Name {
							case "approve":
								return "approved:" + sp.Data.(string), nil
							case "reject":
								return "rejected:" + sp.Data.(string), nil
							default:
								return nil, errors.New("unexpected signal name " + sp.Name)
							}
						},
					},
				},
			}

			if err := engine.RegisterWorkflow(wf); err != nil {
				t.Fatalf("RegisterWorkflow failed: %v", err)
			}

			// --- Branch 1: approve ---
			inst, err := engine.Run(ctx, "approval-decisions", nil)
			if err == nil {
				t.Fatalf("expected wait-for-signal error for approve branch")
			}
			if inst.Status != api.StatusWaiting {
				t.Fatalf("expected WAITING, got %q", inst.Status)
			}

			// Send "approve" signal
			instApproved, err := engine.Signal(ctx, inst.ID, "approve", "Manager-A")
			if err != nil {
				t.Fatalf("Signal approve failed: %v", err)
			}
			if instApproved.Status != api.StatusCompleted {
				t.Fatalf("expected COMPLETED on approve branch, got %q", instApproved.Status)
			}
			if instApproved.Output != "approved:Manager-A" {
				t.Fatalf("unexpected output in approve branch: %v", instApproved.Output)
			}

			// --- Branch 2: reject ---
			inst2, err := engine.Run(ctx, "approval-decisions", nil)
			if err == nil {
				t.Fatalf("expected wait-for-signal error for reject branch")
			}
			if inst2.Status != api.StatusWaiting {
				t.Fatalf("expected WAITING, got %q", inst2.Status)
			}

			// Send "reject" signal
			instRejected, err := engine.Signal(ctx, inst2.ID, "reject", "Manager-B")
			if err != nil {
				t.Fatalf("Signal reject failed: %v", err)
			}
			if instRejected.Status != api.StatusCompleted {
				t.Fatalf("expected COMPLETED on reject branch, got %q", instRejected.Status)
			}
			if instRejected.Output != "rejected:Manager-B" {
				t.Fatalf("unexpected output in reject branch: %v", instRejected.Output)
			}
		})
	}
}

func TestSignals_WaitForAnySignal_IgnoresUnexpectedSignal(t *testing.T) {
	engine := signalInMemoryEngine(t)

	wf := api.WorkflowDefinition{
		Name: "approval-decisions-unexpected",
		Steps: []api.StepDefinition{
			{
				Name: "wait-decision",
				Fn:   api.WaitForAnySignalStep("approve", "reject"),
			},
		},
	}

	if err := engine.RegisterWorkflow(wf); err != nil {
		t.Fatalf("RegisterWorkflow failed: %v", err)
	}

	ctx := context.Background()

	inst, err := engine.Run(ctx, "approval-decisions-unexpected", nil)
	if err == nil {
		t.Fatalf("expected wait-for-signal error")
	}
	if inst.Status != api.StatusWaiting {
		t.Fatalf("expected WAITING, got %q", inst.Status)
	}

	// Send an unexpected signal name; the step should re-request to wait.
	inst2, err := engine.Signal(ctx, inst.ID, "other-signal", "ignored")
	if err == nil {
		t.Fatalf("expected wait-for-signal error again after unexpected signal")
	}
	if inst2.Status != api.StatusWaiting {
		t.Fatalf("expected still WAITING after unexpected signal, got %q", inst2.Status)
	}
	if inst2.ID != inst.ID {
		t.Fatalf("expected same instance ID, got %s vs %s", inst2.ID, inst.ID)
	}
}
