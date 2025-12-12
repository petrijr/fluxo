// examples/typed/main.go
package main

import (
	"context"
	"fmt"

	"github.com/petrijr/fluxo"
)

type Input struct {
	Start int
}

type Counter struct {
	Value int
}

func main() {
	ctx := context.Background()

	eng := fluxo.NewInMemoryEngine()

	flow := fluxo.New("typed-example").Step("init", fluxo.TypedStep(func(ctx context.Context, in Input) (Counter, error) {
		return Counter{Value: in.Start}, nil
	})).Step("loop+1 (3x)", fluxo.TypedLoop(3, func(ctx context.Context, c Counter) (Counter, error) {
		c.Value++
		return c, nil
	})).Step("while < 10 (+2)", fluxo.TypedWhile(func(c Counter) bool { return c.Value < 10 },
		func(ctx context.Context, c Counter) (Counter, error) {
			c.Value += 2
			return c, nil
		}))

	flow.MustRegister(eng)

	inst, err := fluxo.Run(ctx, eng, flow.Name(), Input{Start: 0})
	if err != nil {
		panic(err)
	}

	out, ok := inst.Output.(Counter)
	if !ok {
		panic(fmt.Errorf("unexpected output type: %T", inst.Output))
	}

	fmt.Printf("workflow %s(%s) completed: %+v\n", inst.Name, inst.ID, out)
}
