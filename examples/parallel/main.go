package main

import (
	"context"
	"fmt"

	"github.com/petrijr/fluxo/internal/engine"
	"github.com/petrijr/fluxo/pkg/api"
)

// This example demonstrates a simple data-parallel workflow:
//
//  1. Take a []int as input.
//  2. Square each value in parallel (in-process goroutines).
//  3. Sum the results in a final step.
func main() {
	ctx := context.Background()
	eng := engine.NewInMemoryEngine()

	if err := registerParallelSquaresWorkflow(eng); err != nil {
		panic(fmt.Sprintf("registerParallelSquaresWorkflow failed: %v", err))
	}

	input := []int{1, 2, 3, 4, 5}

	inst, err := eng.Run(ctx, "parallel-squares", input)
	if err != nil {
		panic(fmt.Sprintf("Run failed: %v", err))
	}

	fmt.Printf("workflow id=%s status=%s output=%v\n", inst.ID, inst.Status, inst.Output)

	sum, ok := inst.Output.(int)
	if !ok {
		panic(fmt.Sprintf("expected int output, got %T", inst.Output))
	}
	fmt.Printf("sum of squares of %v is %d\n", input, sum)
}

func registerParallelSquaresWorkflow(eng api.Engine) error {
	wf := api.WorkflowDefinition{
		Name: "parallel-squares",
		Steps: []api.StepDefinition{
			{
				Name: "square-each-in-parallel",
				Fn: api.ParallelMapStep(func(ctx context.Context, input any) (any, error) {
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
