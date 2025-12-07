# `examples/parallel` — Data-Parallel Workflow Example

This example shows how to use Fluxo's *in-process* parallel helpers to run
steps in parallel.

The `parallel-squares` workflow:

1. Takes a `[]int` as input.
2. Uses `ParallelMapStep` to square each number in parallel (one goroutine per item).
3. Sums the results in a final step.

Run it with:

```bash
go run ./examples/parallel
````

You should see output similar to:

```text
workflow id=... status=COMPLETED output=55
sum of squares of [1 2 3 4 5] is 55
```

This demonstrates:

* Fan-out over a slice (`ParallelMapStep`)
* Fan-in of the results into a single value
* How parallelism lives entirely inside a step while the engine
  still sees it as a single deterministic step.
