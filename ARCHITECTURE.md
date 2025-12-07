Planned architecture is shown below.

```asciidoc
+-------------------------------------------------------------+
|                         Application                         |
|  (Your project using the workflow engine as a library)      |
+---------------------------+---------------------------------+
                            |
                            v
+-------------------------------------------------------------+
|                     Workflow Engine API                     |
|  High-level API for defining flows, starting workflows,     |
|  sending signals, querying status                           |
+---------------------------+---------------------------------+
                            |
                            v
+-------------------------------------------------------------+
|                      Workflow Runtime                       |
|  +--------------------+   +------------------------------+  |
|  | State Machine Exec |   | Step Scheduler / Worker Pool |  |
|  +--------------------+   +------------------------------+  |
|  | Deterministic      |   | Task execution               |  |
|  | step transitions   |   | Retry & backoff              |  |
|  +--------------------+   +------------------------------+  |
+---------------------------+---------------------------------+
                            |
                            v
+-------------------------------------------------------------+
|                   Persistence Layer (Pluggable)             |
|   +--------------------+   +------------------------------+ |
|   | Event Store        |   | Snapshot Store               | |
|   | (append-only log)  |   | (state serialization)        | |
|   +--------------------+   +------------------------------+ |
|   | In-Memory          |   | SQLite                       | |
|   | PostgreSQL         |   | Custom adapters              | |
+---------------------------+---------------------------------+
                            |
                            v
+-------------------------------------------------------------+
|                      Infrastructure Layer                   |
|   Logging (zap/slog), Metrics, Tracing, Config              |
+-------------------------------------------------------------+
```

Planned directory structure:

```asciidoc
fluxo/
│
├── cmd/
│   └── wfctl/                     # (future) CLI for debugging, introspection
│
├── internal/
│   ├── engine/
│   │   ├── executor.go            # Step executor (task runner)
│   │   ├── scheduler.go           # Worker pool, task scheduling
│   │   ├── runtime.go             # Core runtime logic
│   │   ├── determinism.go         # Deterministic replay helpers
│   │   ├── registry.go            # Registry of workflows/steps
│   │   └── signals.go             # Signal handling
│   │
│   ├── persistence/
│   │   ├── interface.go           # Persistence provider interface
│   │   ├── memory/
│   │   │   └── memory_store.go    # In-memory persistence
│   │   ├── sqlite/
│   │   │   └── sqlite_store.go    # SQLite backend
│   │   └── postgres/
│   │       └── postgres_store.go  # PostgreSQL backend
│   │
│   ├── proto/                     # internal serialization (optional)
│   │   └── workflow_state.pb.go
│   │
│   └── util/
│       ├── backoff.go             # Retry/backoff calculations
│       ├── sync.go                # Concurrency primitives
│       └── logging.go             # Shared logging support
│
├── pkg/
│   └── api/
│       ├── workflow.go            # Workflow definition API
│       ├── step.go                # Step input/output definitions
│       ├── engine.go              # Main high-level Engine interface
│       ├── signal.go              # Send/receive signal API
│       ├── options.go             # Engine configuration options
│       └── metrics.go             # Metrics hooks
│
├── examples/
│   ├── onboarding/
│   │   └── main.go                # Example workflow
│   └── parallel/
│       └── main.go
│
├── README.md
└── go.mod
```
