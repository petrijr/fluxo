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
