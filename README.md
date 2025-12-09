# Fluxo - Lightweight Workflow Engine for Go

A high-performance, embeddable, deterministic workflow engine written entirely in Go.  
Designed as a **lightweight alternative to Temporal and Camunda**, suitable for microservices, CLIs, and on-prem
deployments.

---

## 🚀 Why This Exists

Existing workflow engines are powerful but often:

- Operationally complex
- Heavy to deploy
- Hard to embed
- Overkill for small and medium services

Fluxo aims to provide:

- A small, pure-Go library you can import into any service
- Deterministic, retryable workflows with durable state
    - Note that `LocalRunner` is **not crash durable**; it’s for development, tests, and single-process
      best-effort usage
    - Durability guarantees refer to **workflow state** given a persistent persistence backend; async
      queue durability depends on which queue implementation is used.
- Pluggable persistence (in-memory, SQLite, Postgres, Redis, MongoDB)
- Simple, testable APIs

---

## 📦 Installation

```bash
go get github.com/petrijr/fluxo
````

Go 1.21+ is recommended.

---

## 🧪 Quick Start

Define a workflow using the fluent builder and run it with the in-memory engine:

```go
package main

import (
	"context"
	"log"

	"github.com/petrijr/fluxo"
)

func createAccount(ctx context.Context, input any) (any, error) {
	// do some work...
	return map[string]any{"userID": "123"}, nil
}

func sendWelcomeEmail(ctx context.Context, input any) (any, error) {
	state := input.(map[string]any)
	log.Printf("sending welcome email to user %s", state["userID"])
	return state, nil
}

func main() {
	ctx := context.Background()

	flow := fluxo.New("OnboardUser").
		Step("createAccount", createAccount).
		Step("sendWelcomeEmail", sendWelcomeEmail)

	eng := fluxo.NewInMemoryEngine()

	if err := flow.Register(eng); err != nil {
		log.Fatal(err)
	}

	inst, err := fluxo.Run(ctx, eng, flow.Name(), nil)
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("workflow completed: id=%s status=%s", inst.ID, inst.Status)
}
```

---

## 🗄️ Persistence Backends

Fluxo separates workflow definitions (always in-memory) from instance persistence.
Out of the box you get:

* **In-memory** – great for tests and local development
* **SQLite**
* **PostgreSQL**
* **Redis**
* **MongoDB**

### In-memory

```go
eng := fluxo.NewInMemoryEngine()
```

### SQLite

```go
db, err := sql.Open("sqlite", "file:fluxo.db?_journal=WAL")
eng, err := fluxo.NewSQLiteEngine(db)
```

### PostgreSQL

```go
db, err := sql.Open("pgx", "postgres://user:pass@localhost:5432/fluxo")
eng, err := fluxo.NewPostgresEngine(db)
```

### Redis

```go
client := redis.NewClient(&redis.Options{
Addr: "localhost:6379",
})
eng := fluxo.NewRedisEngine(client)
```

### MongoDB

```go
client, err := mongo.Connect(ctx, options.Client().ApplyURI("mongodb://localhost:27017"))
if err != nil { /* handle */ }

// Uses database "fluxo" and collection "instances" by default.
eng := fluxo.NewMongoEngine(client)
```

All persistent engines share the same API, so switching backends doesn’t change your workflow code.

---

Gotcha — we’ll:

1. Add a **README control-flow section** showing `While` / `Loop`.
2. Add **typed helpers** (`TypedStep`, `TypedWhile`, `TypedLoop`) + a small test so they’re covered.

I’ll keep everything concrete so you can paste it in.

---

## 1. Typed helpers in the core API (`pkg/api`)

### 1.1 Add generic helpers in `pkg/api/workflow.go`

Near the existing `StepFunc`, `ConditionFunc`, `WhileStep`, `LoopStep` definitions, add:

```go
// TypedStep wraps a strongly-typed function into a generic StepFunc.
// It performs a type assertion on input at runtime and returns an error
// if the input is not of the expected type.
func TypedStep[I, O any](fn func(context.Context, I) (O, error)) StepFunc {
return func (ctx context.Context, input any) (any, error) {
var zeroO O

// Handle nil input for pointer/slice/map types: let zero-value be used.
var typedI I
if input != nil {
v, ok := input.(I)
if !ok {
return nil, fmt.Errorf("TypedStep: expected input of type %T, got %T", typedI, input)
}
typedI = v
}

out, err := fn(ctx, typedI)
if err != nil {
return zeroO, err
}
return out, nil
}
}

// TypedWhile returns a StepFunc that repeatedly executes a strongly-typed
// body while cond(input) is true. The loop is treated as a single engine step.
func TypedWhile[I any](cond func(I) bool, body func (context.Context, I) (I, error)) StepFunc {
return WhileStep(
func (input any) bool {
if input == nil {
var zero I
return cond(zero)
}
v, ok := input.(I)
if !ok {
return false
}
return cond(v)
},
func (ctx context.Context, input any) (any, error) {
var typed I
if input != nil {
v, ok := input.(I)
if !ok {
return nil, fmt.Errorf("TypedWhile: expected input of type %T, got %T", typed, input)
}
typed = v
}
return body(ctx, typed)
},
)
}

// TypedLoop returns a StepFunc that executes a strongly-typed body a fixed
// number of times. The loop is treated as a single engine step.
func TypedLoop[I any](times int, body func (context.Context, I) (I, error)) StepFunc {
return LoopStep(times, func (ctx context.Context, input any) (any, error) {
var typed I
if input != nil {
v, ok := input.(I)
if !ok {
return nil, fmt.Errorf("TypedLoop: expected input of type %T, got %T", typed, input)
}
typed = v
}
return body(ctx, typed)
})
}
```

You’ll need `fmt` imported at the top of `pkg/api/workflow.go` if it isn’t already:

```go
import (
"context"
"fmt"
// ...
)
```

These helpers:

* Keep all determinism properties (no randomness/time).
* Fail fast with a clear error if the input type is wrong.
* Don’t require generic methods (so you’re safe on older Go versions).

---

## 2. Public typed helpers in `fluxo` (`steps.go`)

Expose them at the root package so users don’t have to import `pkg/api`.

**File:** `steps.go`

Add:

```go
// TypedStep wraps a strongly-typed function into a StepFunc.
// Example:
//
//   fluxo.TypedStep(func(ctx context.Context, s MyState) (MyState, error) { ... })
//
func TypedStep[I, O any](fn func(context.Context, I) (O, error)) StepFunc {
return api.TypedStep(fn)
}

// TypedWhile returns a step that repeatedly executes a strongly-typed body
// while cond(input) is true.
func TypedWhile[I any](cond func(I) bool, body func (context.Context, I) (I, error)) StepFunc {
return api.TypedWhile(cond, body)
}

// TypedLoop returns a step that executes a strongly-typed body a fixed number
// of times.
func TypedLoop[I any](times int, body func (context.Context, I) (I, error)) StepFunc {
return api.TypedLoop(times, body)
}
```

Usage from a user’s POV:

```go
type State struct {
Count int
}

flow := fluxo.New("typed-loop").
Step("loop", fluxo.TypedLoop(3, func (ctx context.Context, s State) (State, error) {
s.Count++
return s, nil
}))
```

No changes needed to `FlowBuilder` — `Step` already accepts `StepFunc`, and these helpers produce exactly that.

---

## 3. Tiny test for typed helpers

Let’s add a simple test that:

* Uses `TypedWhile` and `TypedLoop`.
* Verifies determinism & type safety at a basic level.

**File:** `loops_typed_test.go` (repo root)

```go
package fluxo

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type typedCounter struct {
	Value int
}

func TestTypedLoopAndTypedWhile(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	eng := NewInMemoryEngine()

	flow := New("typed-loop-while").
		Step("loop-3-times", TypedLoop(3, func(ctx context.Context, s typedCounter) (typedCounter, error) {
			s.Value++
			return s, nil
		})).
		Step("while-less-than-5", TypedWhile(
			func(s typedCounter) bool { return s.Value < 5 },
			func(ctx context.Context, s typedCounter) (typedCounter, error) {
				s.Value++
				return s, nil
			},
		))

	require.NoError(t, flow.Register(eng))

	inst, err := Run(ctx, eng, flow.Name(), typedCounter{})
	require.NoError(t, err)
	require.NotNil(t, inst)
	require.Equal(t, StatusCompleted, inst.Status)

	out, ok := inst.Output.(typedCounter)
	require.True(t, ok, "expected typedCounter output, got %T", inst.Output)
	require.Equal(t, 5, out.Value, "expected final Value 5 (3 from loop + 2 from while)")

	// Run again to assert determinism.
	inst2, err := Run(ctx, eng, flow.Name(), typedCounter{})
	require.NoError(t, err)
	out2, ok := inst2.Output.(typedCounter)
	require.True(t, ok)
	require.Equal(t, 5, out2.Value)
}
```

You can add a separate negative test for wrong type if you like, but this hits the happy path and makes sure generics +
loops behave as expected.

---

## 🧭 Control Flow

Fluxo gives you simple, composable control-flow building blocks.

### Conditional branches

Use `If` for simple branching:

```go
flow := fluxo.New("payment").
If("check-limit",
func (input any) bool {
p, _ := input.(PaymentRequest)
return p.Amount <= 100
},
fluxo.StepFunc(func (ctx context.Context, input any) (any, error) {
// happy path
return approve(ctx, input)
}),
fluxo.StepFunc(func (ctx context.Context, input any) (any, error) {
// escalate
return requestManagerApproval(ctx, input)
}),
)
```

### Parallel steps

Use `Parallel` to run multiple steps concurrently and wait for all of them:

```go
flow := fluxo.New("fan-out").
Parallel("prepare-resources",
stepA,
stepB,
stepC,
)
```

Each sub-step receives the same input and runs in its own worker.

### Loops

Fluxo supports simple looping constructs that are executed **within a single engine step** (so retries/backoff apply to
the whole loop):

```go
// Loop a fixed number of times.
flow := fluxo.New("count-to-3").
Loop("loop-3-times", 3, func (ctx context.Context, input any) (any, error) {
state, _ := input.(Counter)
state.Value++
return state, nil
})

// While a condition holds.
flow = fluxo.New("while-less-than-5").
While("while-less-than-5",
func (input any) bool {
state, _ := input.(Counter)
return state.Value < 5
},
func (ctx context.Context, input any) (any, error) {
state, _ := input.(Counter)
state.Value++
return state, nil
},
)
```

Loops are deterministic as long as your condition and body are deterministic.

### Typed helpers

Working with `any` everywhere can get noisy. Fluxo provides typed helpers to keep your
workflow logic strongly-typed while still using the generic engine:

```go
type Counter struct {
Value int
}

flow := fluxo.New("typed-loop").
Step("loop-and-while",
fluxo.TypedWhile(
func (s Counter) bool { return s.Value < 5 },
func (ctx context.Context, s Counter) (Counter, error) {
s.Value++
return s, nil
},
),
)
```

You can also wrap strongly-typed steps and loops:

```go
flow := fluxo.New("typed").
Step("typed-step",
fluxo.TypedStep(func (ctx context.Context, s Counter) (Counter, error) {
s.Value += 10
return s, nil
}),
).
Step("typed-loop",
fluxo.TypedLoop(3, func (ctx context.Context, s Counter) (Counter, error) {
s.Value++
return s, nil
}),
)
```

Under the hood these helpers perform a runtime type assertion on the input and emit a
clear error if the type doesn’t match.

## 📊 Logging & Metrics

Fluxo has a pluggable **Observer** interface used for structured logging and basic metrics.

Common implementations:

* `fluxo.LoggingObserver` + `fluxo.NewLoggingObserver`
* `fluxo.BasicMetrics` (+ `BasicMetricsSnapshot`)
* `fluxo.CompositeObserver` to combine multiple observers
* `fluxo.NoopObserver` (the default)

### Structured Logging

```go
logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

obs := fluxo.NewLoggingObserver(logger)

// Use a constructor that accepts an Observer:
eng := fluxo.NewInMemoryEngineWithObserver(obs)
```

### Basic Metrics

```go
metrics := &fluxo.BasicMetrics{}

obs := fluxo.NewCompositeObserver(
fluxo.NewLoggingObserver(nil), // uses slog.Default()
metrics,
)

eng, err := fluxo.NewSQLiteEngineWithObserver(db, obs)

// ... run some workflows ...

snapshot := metrics.Snapshot()
// snapshot.WorkflowsCompleted, snapshot.StepsCompleted, etc.
```

You can expose these counters via your preferred metrics system by periodically reading the snapshot.

---

## 📄 License

MIT — see `LICENSE` for details.

---

## 🧭 Project Status

Currently in **MVP implementation**.
APIs are usable but may evolve; feedback and contributions are very welcome.
