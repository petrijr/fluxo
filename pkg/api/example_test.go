// example_test.go
package api_test

import (
	"context"
	"fmt"
	"log"

	"github.com/petrijr/fluxo"
	"github.com/petrijr/fluxo/pkg/api"
)

// ExampleWorkflowDefinition shows how to build a workflow definition directly
// using the api package and register it on an Engine.
func ExampleWorkflowDefinition() {
	ctx := context.Background()

	// Build a simple definition manually.
	def := api.WorkflowDefinition{
		Name: "AddPrefix",
		Steps: []api.StepDefinition{
			{
				Name: "addPrefix",
				Fn: func(ctx context.Context, input any) (any, error) {
					s, ok := input.(string)
					if !ok {
						return nil, fmt.Errorf("expected string input, got %T", input)
					}
					return "prefix:" + s, nil
				},
			},
		},
	}

	// Use a real engine implementation from the fluxo package.
	eng := fluxo.NewInMemoryEngine()

	if err := eng.RegisterWorkflow(def); err != nil {
		log.Fatal(err)
	}

	inst, err := eng.Run(ctx, def.Name, "value")
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("instance %s finished with status %s and output %v\n",
		inst.ID, inst.Status, inst.Output)
}
