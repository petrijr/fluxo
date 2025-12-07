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
