package main

import (
	"context"
	"fmt"
	"time"

	"github.com/petrijr/fluxo"
)

func main() {
	ctx := context.Background()
	eng := fluxo.NewInMemoryEngine()

	// Register workflows
	if err := registerSquareChildWorkflow(eng); err != nil {
		panic(fmt.Sprintf("registerSquareChildWorkflow failed: %v", err))
	}
	if err := registerParallelSquaresDurableWorkflow(eng); err != nil {
		panic(fmt.Sprintf("registerParallelSquaresDurableWorkflow failed: %v", err))
	}
	if err := registerParallelSquaresInProcessWorkflow(eng); err != nil {
		panic(fmt.Sprintf("registerParallelSquaresInProcessWorkflow failed: %v", err))
	}

	input := []int{1, 2, 3, 4, 5}

	fmt.Println("=== In-process parallel (ParallelMapStep) ===")
	runAndPrint(ctx, eng, "parallel-squares-inprocess", input)

	fmt.Println("\n=== Durable parallel (child workflows + join) ===")
	runAndPrint(ctx, eng, "parallel-squares-durable", input)
}

func runAndPrint(ctx context.Context, eng fluxo.Engine, name string, input []int) {
	inst, err := eng.Run(ctx, name, input)
	if err != nil {
		panic(fmt.Sprintf("Run(%s) failed: %v", name, err))
	}

	if inst == nil {
		panic("expected non-nil WorkflowInstance")
	}

	fmt.Printf("workflow name=%s id=%s status=%s output=%v\n",
		name, inst.ID, inst.Status, inst.Output)

	sum, ok := inst.Output.(int)
	if !ok {
		panic(fmt.Sprintf("expected int output from %s, got %T", name, inst.Output))
	}
	fmt.Printf("sum of squares of %v is %d\n", input, sum)
}

// -----------------------------------------------------------------------------
// Child workflow: squares a single integer
// -----------------------------------------------------------------------------

func registerSquareChildWorkflow(eng fluxo.Engine) error {
	wf := fluxo.WorkflowDefinition{
		Name: "square-child",
		Steps: []fluxo.StepDefinition{
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
	return eng.RegisterWorkflow(wf)
}

// -----------------------------------------------------------------------------
// Durable parallel workflow: fan-out to child instances, then join.
// -----------------------------------------------------------------------------

func registerParallelSquaresDurableWorkflow(eng fluxo.Engine) error {
	wf := fluxo.WorkflowDefinition{
		Name: "parallel-squares-durable",
		Steps: []fluxo.StepDefinition{
			{
				Name: "start-children",
				Fn: fluxo.StartChildrenStep(func(input any) ([]fluxo.ChildWorkflowSpec, error) {
					values, ok := input.([]int)
					if !ok {
						return nil, fmt.Errorf("start-children: expected []int, got %T", input)
					}

					specs := make([]fluxo.ChildWorkflowSpec, 0, len(values))
					for _, v := range values {
						specs = append(specs, fluxo.ChildWorkflowSpec{
							Name:  "square-child",
							Input: v,
						})
					}
					return specs, nil
				}),
			},
			{
				Name: "wait-for-children",
				Fn: fluxo.WaitForChildrenStep(
					func(input any) []string {
						ids, ok := input.([]string)
						if !ok {
							panic(fmt.Sprintf("wait-for-children: expected []string, got %T", input))
						}
						return ids
					},
					2*time.Second, // re-check interval; persisted and scheduled
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
	return eng.RegisterWorkflow(wf)
}

// -----------------------------------------------------------------------------
// In-process parallel workflow: uses ParallelMapStep
// -----------------------------------------------------------------------------

func registerParallelSquaresInProcessWorkflow(eng fluxo.Engine) error {
	wf := fluxo.WorkflowDefinition{
		Name: "parallel-squares-inprocess",
		Steps: []fluxo.StepDefinition{
			{
				Name: "square-each-in-parallel",
				Fn: fluxo.ParallelMapStep(func(ctx context.Context, input any) (any, error) {
					n, ok := input.(int)
					if !ok {
						return nil, fmt.Errorf("square-each-in-parallel: expected int, got %T", input)
					}
					return n * n, nil
				}),
			},
			{
				Name: "sum-results",
				Fn: func(ctx context.Context, input any) (any, error) {
					values, ok := input.([]any)
					if !ok {
						return nil, fmt.Errorf("sum-results: expected []any, got %T", input)
					}

					sum := 0
					for i, v := range values {
						n, ok := v.(int)
						if !ok {
							return nil, fmt.Errorf("sum-results: index %d: expected int, got %T", i, v)
						}
						sum += n
					}
					return sum, nil
				},
			},
		},
	}
	return eng.RegisterWorkflow(wf)
}
