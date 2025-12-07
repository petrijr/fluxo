package engine

import (
	"context"
	"fmt"
	"testing"

	"github.com/petrijr/fluxo/pkg/api"
)

func registerSimpleWorkflow(t *testing.T, engine api.Engine, name string, shouldFail bool) {
	t.Helper()

	var fn api.StepFunc
	if shouldFail {
		fn = func(ctx context.Context, input any) (any, error) {
			return nil, fmt.Errorf("boom")
		}
	} else {
		fn = func(ctx context.Context, input any) (any, error) {
			return fmt.Sprintf("%s:%v", name, input), nil
		}
	}

	wf := api.WorkflowDefinition{
		Name: name,
		Steps: []api.StepDefinition{
			{Name: "step-1", Fn: fn},
		},
	}

	if err := engine.RegisterWorkflow(wf); err != nil {
		t.Fatalf("RegisterWorkflow(%q) failed: %v", name, err)
	}
}

func TestListInstancesReturnsAllWhenNoFilters(t *testing.T) {
	ctx := context.Background()
	engine := NewInMemoryEngine()

	registerSimpleWorkflow(t, engine, "wf-success", false)
	registerSimpleWorkflow(t, engine, "wf-fail", true)

	// Create three instances: two success, one failure.
	if _, err := engine.Run(ctx, "wf-success", "a"); err != nil {
		t.Fatalf("Run wf-success(a) failed: %v", err)
	}
	if _, err := engine.Run(ctx, "wf-success", "b"); err != nil {
		t.Fatalf("Run wf-success(b) failed: %v", err)
	}
	if _, err := engine.Run(ctx, "wf-fail", "c"); err == nil {
		t.Fatalf("expected wf-fail to fail")
	}

	instances, err := engine.ListInstances(ctx, api.InstanceListOptions{})
	if err != nil {
		t.Fatalf("ListInstances failed: %v", err)
	}

	if len(instances) != 3 {
		t.Fatalf("expected 3 instances, got %d", len(instances))
	}
}

func TestListInstancesFilterByWorkflowName(t *testing.T) {
	ctx := context.Background()
	engine := NewInMemoryEngine()

	registerSimpleWorkflow(t, engine, "alpha", false)
	registerSimpleWorkflow(t, engine, "beta", false)

	if _, err := engine.Run(ctx, "alpha", "x"); err != nil {
		t.Fatalf("Run alpha(x) failed: %v", err)
	}
	if _, err := engine.Run(ctx, "alpha", "y"); err != nil {
		t.Fatalf("Run alpha(y) failed: %v", err)
	}
	if _, err := engine.Run(ctx, "beta", "z"); err != nil {
		t.Fatalf("Run beta(z) failed: %v", err)
	}

	instances, err := engine.ListInstances(ctx, api.InstanceListOptions{
		WorkflowName: "alpha",
	})
	if err != nil {
		t.Fatalf("ListInstances failed: %v", err)
	}

	if len(instances) != 2 {
		t.Fatalf("expected 2 instances for workflow alpha, got %d", len(instances))
	}

	for _, inst := range instances {
		if inst.Name != "alpha" {
			t.Fatalf("expected instance workflow name alpha, got %q", inst.Name)
		}
	}
}

func TestListInstancesFilterByStatus(t *testing.T) {
	ctx := context.Background()
	engine := NewInMemoryEngine()

	registerSimpleWorkflow(t, engine, "ok", false)
	registerSimpleWorkflow(t, engine, "fail", true)

	if _, err := engine.Run(ctx, "ok", "a"); err != nil {
		t.Fatalf("Run ok(a) failed: %v", err)
	}
	if _, err := engine.Run(ctx, "ok", "b"); err != nil {
		t.Fatalf("Run ok(b) failed: %v", err)
	}
	if _, err := engine.Run(ctx, "fail", "c"); err == nil {
		t.Fatalf("expected fail(c) to fail")
	}

	instances, err := engine.ListInstances(ctx, api.InstanceListOptions{
		Status: api.StatusCompleted,
	})
	if err != nil {
		t.Fatalf("ListInstances failed: %v", err)
	}

	if len(instances) != 2 {
		t.Fatalf("expected 2 COMPLETED instances, got %d", len(instances))
	}

	for _, inst := range instances {
		if inst.Status != api.StatusCompleted {
			t.Fatalf("expected COMPLETED, got %q", inst.Status)
		}
	}
}

func TestListInstancesFilterByWorkflowNameAndStatus(t *testing.T) {
	ctx := context.Background()
	engine := NewInMemoryEngine()

	registerSimpleWorkflow(t, engine, "dual", false)
	registerSimpleWorkflow(t, engine, "other", true)

	// dual -> one success, one fail (using context cancellation to fail second).
	// For simplicity we just reuse the failing workflow.
	if _, err := engine.Run(ctx, "dual", "ok"); err != nil {
		t.Fatalf("Run dual(ok) failed: %v", err)
	}
	if _, err := engine.Run(ctx, "other", "boom"); err == nil {
		t.Fatalf("expected other(boom) to fail")
	}

	instances, err := engine.ListInstances(ctx, api.InstanceListOptions{
		WorkflowName: "dual",
		Status:       api.StatusCompleted,
	})
	if err != nil {
		t.Fatalf("ListInstances failed: %v", err)
	}

	if len(instances) != 1 {
		t.Fatalf("expected 1 instance for workflow=dual status=COMPLETED, got %d", len(instances))
	}
	if instances[0].Name != "dual" || instances[0].Status != api.StatusCompleted {
		t.Fatalf("unexpected instance: %+v", instances[0])
	}
}
