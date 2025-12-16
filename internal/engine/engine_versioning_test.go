package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/petrijr/fluxo/pkg/api"
)

func TestVersioning_RegisterTwoVersions_Succeeds(t *testing.T) {
	e := NewInMemoryEngine()

	wfV1 := api.WorkflowDefinition{
		Name:    "order",
		Version: "v1",
		Steps: []api.StepDefinition{
			{Name: "step1", Fn: func(ctx context.Context, input any) (any, error) { return "v1", nil }},
		},
	}
	wfV2 := api.WorkflowDefinition{
		Name:    "order",
		Version: "v2",
		Steps: []api.StepDefinition{
			{Name: "step1", Fn: func(ctx context.Context, input any) (any, error) { return "v2", nil }},
		},
	}

	if err := e.RegisterWorkflow(wfV1); err != nil {
		t.Fatalf("RegisterWorkflow(v1) failed: %v", err)
	}
	if err := e.RegisterWorkflow(wfV2); err != nil {
		t.Fatalf("RegisterWorkflow(v2) failed: %v", err)
	}
}

func TestVersioning_RunWithoutVersion_WhenMultipleVersionsRegistered_Errors(t *testing.T) {
	e := NewInMemoryEngine()

	_ = e.RegisterWorkflow(api.WorkflowDefinition{
		Name: "order", Version: "v1",
		Steps: []api.StepDefinition{{Name: "s", Fn: func(ctx context.Context, input any) (any, error) { return "v1", nil }}},
	})
	_ = e.RegisterWorkflow(api.WorkflowDefinition{
		Name: "order", Version: "v2",
		Steps: []api.StepDefinition{{Name: "s", Fn: func(ctx context.Context, input any) (any, error) { return "v2", nil }}},
	})

	_, err := e.Run(context.Background(), "order", nil)
	if err == nil {
		t.Fatalf("expected error when calling Run without version and multiple versions exist")
	}
}

func TestVersioning_RunVersion_BindsInstanceVersionAndFingerprint(t *testing.T) {
	e := NewInMemoryEngine()

	wf := api.WorkflowDefinition{
		Name: "order", Version: "v1",
		Steps: []api.StepDefinition{{Name: "s", Fn: func(ctx context.Context, input any) (any, error) { return "ok", nil }}},
	}
	if err := e.RegisterWorkflow(wf); err != nil {
		t.Fatalf("RegisterWorkflow failed: %v", err)
	}

	inst, err := e.RunVersion(context.Background(), "order", "v1", nil)
	if err != nil {
		t.Fatalf("RunVersion failed: %v", err)
	}
	if inst.Version != "v1" {
		t.Fatalf("expected instance version v1, got %q", inst.Version)
	}
	if inst.Fingerprint == "" {
		t.Fatalf("expected instance fingerprint to be set")
	}
}

func TestVersioning_ResumeFailsOnDefinitionMismatch(t *testing.T) {
	// This test describes the deterministic replay guarantee:
	// if the registered definition fingerprint doesn't match the persisted instance fingerprint,
	// resume should fail with ErrWorkflowDefinitionMismatch.

	e := NewInMemoryEngine()

	// Register v1, run once (creates persisted instance with fingerprint)
	wf := api.WorkflowDefinition{
		Name: "order", Version: "v1",
		Steps: []api.StepDefinition{{Name: "s", Fn: func(ctx context.Context, input any) (any, error) { return "ok", nil }}},
	}
	if err := e.RegisterWorkflow(wf); err != nil {
		t.Fatalf("RegisterWorkflow failed: %v", err)
	}

	inst, err := e.RunVersion(context.Background(), "order", "v1", nil)
	if err != nil {
		t.Fatalf("RunVersion failed: %v", err)
	}

	// Simulate a "changed code" situation: register a different v1 definition.
	// (In real life we'd prevent re-registering the same version, so implementation may provide
	// a test hook or allow this in tests only; this test is asserting the *resume check*.)
	//
	// We'll represent this by directly mutating the persisted instance fingerprint to a value
	// that won't match the currently registered workflow.
	inst.Fingerprint = "definitely-not-the-real-fingerprint"
	// Persist the mutation
	if err := e.(*engineImpl).instances.UpdateInstance(inst); err != nil {
		t.Fatalf("UpdateInstance failed: %v", err)
	}

	_, err = e.GetInstance(context.Background(), inst.ID)
	if err == nil {
		t.Fatalf("expected resume/get to fail on fingerprint mismatch")
	}
	if !errors.Is(err, api.ErrWorkflowDefinitionMismatch) {
		t.Fatalf("expected ErrWorkflowDefinitionMismatch, got: %v", err)
	}
}
