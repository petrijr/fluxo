# **Fluxo — Lightweight Workflow Engine for Go**
![Coverage](https://img.shields.io/badge/Coverage-88.2%25-brightgreen)

[![Go Reference](https://pkg.go.dev/badge/github.com/petrijr/fluxo.svg)](https://pkg.go.dev/github.com/petrijr/fluxo)
[![Go Report Card](https://goreportcard.com/badge/github.com/petrijr/fluxo)](https://goreportcard.com/report/github.com/petrijr/fluxo)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](../../Downloads/fluxo_with_new_examples/LICENSE)
[![Tests](https://github.com/petrijr/fluxo/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/petrijr/fluxo/actions/workflows/tests.yml)

Fluxo is a **fast, embeddable, deterministic workflow engine** written in pure Go.
It is designed as a practical alternative to Temporal/Camunda for teams that want workflow reliability **without running
workflow infrastructure**.

Fluxo runs inside your Go service, supports multiple persistence backends, and uses a simple, ergonomic API.

---

## ✨ Features (MVP)

* **Deterministic, retryable workflow execution**
* **Pluggable persistence**

    * In-memory (testing/dev)
    * SQLite
    * PostgreSQL
    * Redis
    * MongoDB
* **Built-in asynchronous worker**
* **Strongly-typed workflow support** (via `TypedStep`, `TypedLoop`, `TypedWhile`)
* **Parallel, conditional, and looping control flow**
* **Timers & signals**
* **LocalRunner** for in-process testing (non-durable)

Fluxo is a *library* — not a service. You embed it directly into your application.

---

## 📦 Installation

```sh
go get github.com/petrijr/fluxo
```

Go **1.21+** is recommended.

---

## 🚀 Quick Start

Define a workflow using the builder API and run it using an engine:

```go
package main

import (
	"context"
	"log"
	"github.com/petrijr/fluxo"
)

func createAccount(ctx context.Context, input any) (any, error) {
	return map[string]any{"userID": "123"}, nil
}

func sendWelcomeEmail(ctx context.Context, input any) (any, error) {
	state := input.(map[string]any)
	log.Printf("sending welcome email to %s", state["userID"])
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

## 🗄 Persistence Backends

Fluxo supports multiple backends. Definitions are always in-memory; instances and execution history depend on your
backend choice.

### In-Memory

Use for tests or ephemeral/local execution:

```go
eng := fluxo.NewInMemoryEngine()
```

### SQLite

Embedded durability; ideal default for single-node services:

```go
db, _ := sql.Open("sqlite", "file:fluxo.db?_journal=WAL")
eng, _ := fluxo.NewSQLiteEngine(db)
```

### PostgreSQL

```go
db, _ := sql.Open("pgx", "postgres://user:pass@localhost:5432/fluxo")
eng, _ := fluxo.NewPostgresEngine(db)
```

### Redis

```go
rdb := redis.NewClient(&redis.Options{ Addr: "localhost:6379" })
eng := fluxo.NewRedisEngine(rdb)
```

### MongoDB

```go
client, _ := mongo.Connect(ctx, options.Client().ApplyURI("mongodb://localhost:27017"))
eng := fluxo.NewMongoEngine(client)
```

Backend choice does **not** change workflow code.

---

## 🔧 Control Flow

Fluxo provides simple, composable workflow primitives.

### Sequential Steps

```go
flow.Step("a", stepA).Step("b", stepB)
```

### Conditionals

```go
flow.If("check-limit",
func (input any) bool { return input.(int) < 100 },
fluxo.StepFunc(func (ctx context.Context, in any) (any, error) { return "ok", nil }),
fluxo.StepFunc(func (ctx context.Context, in any) (any, error) { return "too large", nil }),
)
```

### Parallel Work

```go
flow.Parallel("prepare",
stepFetchUser,
stepFetchSettings,
stepWarmCache,
)
```

### Loops

Fixed-count:

```go
flow.Loop("repeat", 3, body)
```

While-condition:

```go
flow.While("until-ready",
func (input any) bool { return !input.(State).Ready },
body,
)
```

### Typed Helpers

Avoid `any` by using strongly-typed steps:

```go
flow.Step("typed", fluxo.TypedStep(func(ctx context.Context, s Counter) (Counter, error) {
s.Value++
return s, nil
}))
```

Typed looping:

```go
flow.Step("loop", fluxo.TypedWhile(
func(s Counter) bool { return s.Value < 5 },
func (ctx context.Context, s Counter) (Counter, error) {
s.Value++
return s, nil
},
))
```

---

## 🧵 Workers (Asynchronous Execution)

Fluxo workers pull tasks from the task queue and execute them:

```go
w := fluxo.NewWorker(eng, queue)
go w.Run(ctx)
```

Workers can be horizontally scaled.

---

## 🧪 LocalRunner (In-Process Testing)

LocalRunner bundles engine + queue + worker for easy test setups.

```go
runner := fluxo.NewLocalRunner()
runner.StartWorkers(ctx, 1)
runner.StartWorkflowAsync(ctx, "MyFlow", input)
```

⚠️ **Not crash-durable** — for tests & dev only.

---

## 📊 Observability

Fluxo exposes an `Observer` interface for logging and metrics.

### Logging

```go
obs := fluxo.NewLoggingObserver(nil) // uses slog.Default()
eng := fluxo.NewInMemoryEngineWithObserver(obs)
```

### Metrics

```go
metrics := &fluxo.BasicMetrics{}
eng := fluxo.NewSQLiteEngineWithObserver(db, metrics)
snapshot := metrics.Snapshot()
```

---

## ⚙️ Performance

Fluxo targets **< 1ms overhead per step** on typical hardware (excluding user logic).
This is enforced via a performance regression test in the repository.

Actual performance varies with backend choice.

---

## 🧱 Guarantees & Limitations (Honest MVP)

### ✔ Engine Guarantees

* Deterministic workflow planning
* At-least-once step execution
* Durable workflow state when using persistent backends
* Worker crash recovery (persistent backends only)

### ✔ Non-Guarantees (Current MVP)

* No global saga/compensation framework
* No distributed transaction guarantees
* No cross-workflow coordination primitives
* No built-in admin UI or orchestration service
* LocalRunner is **not** durable and cannot recover from process crashes

---

## 🗺 Roadmap (Post-MVP)

These are intentionally **not** implemented yet but may come next:

* Saga helpers (compensation patterns)
* Better observability integrations (Prometheus, OpenTelemetry)
* Workflow versioning helpers
* CLI tooling for inspecting workflows
* Kafka/NATS queue backends
* More ergonomic DSL for workflow definitions
* Optional workflow visualization tooling

---

## 🤝 Contributing

Issues, PRs, and feedback are welcome!
This project is still evolving and contributions are encouraged.

---

## 📄 License

MIT — see `LICENSE`.

## 📘 API Reference (MVP)

This is the public API surface area for Fluxo’s MVP release.

### Top-Level Constructors

```go
func New(name string) *FlowBuilder
func Run(ctx context.Context, eng Engine, workflow string, input any) (*Instance, error)
```

### Engines

```go
func NewInMemoryEngine() Engine
func NewInMemoryEngineWithObserver(o Observer) Engine

func NewSQLiteEngine(db *sql.DB) (Engine, error)
func NewSQLiteEngineWithObserver(db *sql.DB, o Observer) (Engine, error)

func NewPostgresEngine(db *sql.DB) (Engine, error)
func NewPostgresEngineWithObserver(db *sql.DB, o Observer) (Engine, error)

func NewRedisEngine(client *redis.Client) Engine
func NewRedisEngineWithObserver(client *redis.Client, o Observer) Engine

func NewMongoEngine(client *mongo.Client) Engine
func NewMongoEngineWithObserver(client *mongo.Client, o Observer) Engine
```

### Task Queues

```go
func NewInMemoryQueue(capacity int) TaskQueue
func NewSQLiteQueue(db *sql.DB) (TaskQueue, error)
func NewPostgresQueue(db *sql.DB) (TaskQueue, error)
func NewRedisQueue(client *redis.Client) TaskQueue
func NewMongoQueue(client *mongo.Client) TaskQueue
```

### Worker

```go
func NewWorker(eng Engine, q TaskQueue) *Worker
func NewWorkerWithConfig(eng Engine, q TaskQueue, cfg worker.Config) *Worker
```

Key methods:

```go
func (w *Worker) Run(ctx context.Context) error
func (w *Worker) ProcessOne(ctx context.Context) (bool, error)
```

### LocalRunner

```go
type LocalRunner struct {
Engine Engine
Queue  TaskQueue
Worker *Worker
}

func NewLocalRunner() *LocalRunner
func (r *LocalRunner) StartWorkers(ctx context.Context, n int) error
func (r *LocalRunner) StartWorkflowAsync(ctx context.Context, name string, input any) error
func (r *LocalRunner) SignalAsync(ctx context.Context, instanceID string, signal string, payload any) error
```

### Observability

```go
type Observer interface {
// lifecycle + metrics events
}

func NewLoggingObserver(logger *slog.Logger) Observer
func NewCompositeObserver(obs ...Observer) Observer
func NewNoopObserver() Observer

type BasicMetrics struct { /* counters */ }
func (m *BasicMetrics) Snapshot() BasicMetricsSnapshot
```

### Builder API

```go
type FlowBuilder struct {
// ...
}

func (b *FlowBuilder) Step(name string, fn StepFunc) *FlowBuilder
func (b *FlowBuilder) If(name string, cond ConditionFunc, then StepFunc, els StepFunc) *FlowBuilder
func (b *FlowBuilder) Parallel(name string, steps ...StepFunc) *FlowBuilder
func (b *FlowBuilder) Loop(name string, times int, body StepFunc) *FlowBuilder
func (b *FlowBuilder) While(name string, cond ConditionFunc, body StepFunc) *FlowBuilder
func (b *FlowBuilder) WaitForSignal(name, signal string) *FlowBuilder
func (b *FlowBuilder) Sleep(name string, dur time.Duration) *FlowBuilder
func (b *FlowBuilder) SleepUntil(name string, t time.Time) *FlowBuilder
```

### Step Types

```go
type StepFunc func (ctx context.Context, input any) (any, error)
type ConditionFunc func(input any) bool
```

### Typed Helpers

```go
func TypedStep[I, O any](fn func(context.Context, I) (O, error)) StepFunc
func TypedWhile[I any](cond func (I) bool, body func (context.Context, I) (I, error)) StepFunc
func TypedLoop[I any](times int, body func (context.Context, I) (I, error)) StepFunc
```
