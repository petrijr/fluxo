package engine

import (
	"context"
	"fmt"
	"testing"

	"github.com/petrijr/fluxo/pkg/api"
)

type ParentInput struct {
	Values []int
}

func TestChildWorkflowsFanOutAndFanIn(t *testing.T) {
	ctx := context.Background()
	engine := NewInMemoryEngine()

	// --- Child workflow: square an integer -----------------------------------

	childWF := api.WorkflowDefinition{
		Name: "square-child",
		Steps: []api.StepDefinition{
			{
				Name: "square",
				Fn: func(ctx context.Context, input any) (any, error) {
					n, ok := input.(int)
					if !ok {
						return nil, fmt.Errorf("square-child: expected int input, got %T", input)
					}
					return n * n, nil
				},
			},
		},
	}

	if err := engine.RegisterWorkflow(childWF); err != nil {
		t.Fatalf("RegisterWorkflow(square-child) failed: %v", err)
	}

	// --- Parent workflow: fan-out to child workflows, then fan-in ------------

	parentWF := api.WorkflowDefinition{
		Name: "parent-fanout-fanin",
		Steps: []api.StepDefinition{
			{
				Name: "start-children",
				Fn: api.StartChildrenStep(func(input any) ([]api.ChildWorkflowSpec, error) {
					in, ok := input.(ParentInput)
					if !ok {
						return nil, fmt.Errorf("start-children: expected ParentInput, got %T", input)
					}

					specs := make([]api.ChildWorkflowSpec, 0, len(in.Values))
					for _, v := range in.Values {
						specs = append(specs, api.ChildWorkflowSpec{
							Name:  "square-child",
							Input: v,
						})
					}
					return specs, nil
				}),
			},
			{
				Name: "wait-for-children",
				Fn: api.WaitForChildrenStep(
					func(input any) []string {
						ids, ok := input.([]string)
						if !ok {
							// Returning an empty slice propagates an error from
							// WaitForChildrenStep, but in tests it's fine to panic:
							panic(fmt.Sprintf("wait-for-children: expected []string, got %T", input))
						}
						return ids
					},
					0, // use default poll interval
				),
			},
			{
				Name: "sum-child-results",
				Fn: func(ctx context.Context, input any) (any, error) {
					results, ok := input.([]any)
					if !ok {
						return nil, fmt.Errorf("sum-child-results: expected []any, got %T", input)
					}

					sum := 0
					for i, v := range results {
						n, ok := v.(int)
						if !ok {
							return nil, fmt.Errorf("sum-child-results: index %d: expected int, got %T", i, v)
						}
						sum += n
					}
					return sum, nil
				},
			},
		},
	}

	if err := engine.RegisterWorkflow(parentWF); err != nil {
		t.Fatalf("RegisterWorkflow(parent-fanout-fanin) failed: %v", err)
	}

	// --- Run parent and verify ------------------------------------------------

	input := ParentInput{Values: []int{1, 2, 3, 4}}
	inst, err := engine.Run(ctx, "parent-fanout-fanin", input)
	if err != nil {
		t.Fatalf("Run(parent-fanout-fanin) failed: %v", err)
	}

	if inst == nil {
		t.Fatalf("expected non-nil WorkflowInstance")
	}
	if inst.Status != api.StatusCompleted {
		t.Fatalf("expected parent status COMPLETED, got %q", inst.Status)
	}

	sum, ok := inst.Output.(int)
	if !ok {
		t.Fatalf("expected int output from parent, got %T", inst.Output)
	}

	// squares: 1,4,9,16 => sum=30
	if sum != 30 {
		t.Fatalf("expected sum=30, got %d", sum)
	}
}
