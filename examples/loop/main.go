package main

import (
	"context"
	"fmt"

	"github.com/petrijr/fluxo"
)

type Counter struct {
	Value int
}

func main() {
	ctx := context.Background()

	eng := fluxo.NewInMemoryEngine()

	// Condition: continue looping while Value < 5.
	cond := func(input any) bool {
		c, ok := input.(Counter)
		if !ok {
			return false
		}
		return c.Value < 5
	}

	// Body: increment the counter and print the value.
	body := func(ctx context.Context, input any) (any, error) {
		c, ok := input.(Counter)
		if !ok {
			c = Counter{}
		}
		c.Value++
		fmt.Printf("loop body: value=%d\n", c.Value)
		return c, nil
	}

	flow := fluxo.New("loop-example").
		// First, run the body a fixed number of times.
		Loop("loop-3-times", 3, body).
		// Then, continue looping while the condition holds.
		While("while-less-than-5", cond, body)

	if err := flow.Register(eng); err != nil {
		panic(err)
	}

	inst, err := fluxo.Run(ctx, eng, flow.Name(), Counter{})
	if err != nil {
		panic(err)
	}

	final, ok := inst.Output.(Counter)
	if !ok {
		panic(fmt.Sprintf("unexpected output type %T", inst.Output))
	}

	fmt.Printf("workflow completed: id=%s final value=%d\n", inst.ID, final.Value)
}
