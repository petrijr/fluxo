// Package fluxo provides a lightweight, embeddable workflow engine for Go.
//
// Fluxo is designed for backend services that need reliable asynchronous
// operations, background tasks, or long-lived workflows—without introducing
// external dependencies or heavy infrastructure. It runs fully in Go, supports
// multiple persistence backends, and integrates cleanly into existing codebases.
//
// # Core Concepts
//
// The Fluxo programming model is intentionally small and idiomatic:
//
//  1. Engine
//  2. Worker
//  3. FlowBuilder
//  4. StepFunc
//  5. LocalRunner
//
// These components form a complete workflow system with deterministic execution,
// durable state (when using persistent backends), and a clear mental model.
//
// # Engine
//
// The Engine stores workflow definitions, persists workflow state, manages
// execution plans, and provides APIs to:
//   - start workflows
//   - resume workflows after steps complete
//   - deliver signals
//   - read workflow state and history
//
// Engines can be backed by different storage systems:
//
//   - In-memory (non-durable, best for tests)
//   - SQLite (embedded durability)
//   - Postgres
//   - Redis
//   - MongoDB
//
// Each backend includes a matching task queue implementation so workers can
// reliably fetch work.
//
// Engines are safe for use from background workers or from application code
// that wants to schedule workflows synchronously.
//
// # Worker
//
// A Worker pulls tasks from a configured queue and executes workflow steps.
// Workers run asynchronously and can be scaled horizontally.
//
// Responsibilities include:
//   - polling task queues
//   - executing StepFuncs deterministically
//   - applying retry policies
//   - driving workflows forward to completion
//
// Applications typically run one or more workers as background goroutines or as
// separate processes.
//
// # FlowBuilder
//
// FlowBuilder provides the ergonomic, declarative API used to define workflows.
// It supports common control-flow structures:
//
//   - Sequential steps
//   - Conditionals (If / Switch)
//   - Parallel execution (Parallel / ParallelMap)
//   - Loops (Loop / While, including typed variants)
//   - Timers and sleeps
//   - Signals
//
// Example:
//
//	fluxo.New("Example").
//	    Step("a", doA).
//	    Step("b", doB).
//	    Parallel("c",
//	        fluxo.StepFunc("p1", work1),
//	        fluxo.StepFunc("p2", work2),
//	    )
//
// Definitions created with FlowBuilder are registered into an Engine before use.
//
// # StepFunc
//
// A StepFunc is the fundamental executable unit of a workflow:
//
//	type StepFunc func(ctx context.Context, state *State) error
//
// Steps are:
//   - deterministic: same inputs → same observable behavior
//   - idempotent: may be retried if a worker crashes
//   - isolated: they receive a state object representing workflow data
//
// Typed helpers make it easy to work with structured Go values without manual
// marshaling.
//
// # LocalRunner
//
// LocalRunner bundles an in-memory engine, queue, and worker into a single,
// process-local helper useful for development and unit testing. It lets you:
//
//   - start workflows synchronously or asynchronously
//   - send signals
//   - wait for completion
//
// LocalRunner is intentionally **not crash-durable**, but it provides the most
// convenient way to run and debug workflows during development.
//
// # Summary
//
// Fluxo’s goal is to give Go developers a workflow engine that feels like Go:
// easy to embed, easy to test, deterministic, and without operational overhead.
// Engines manage workflow state, Workers execute steps, FlowBuilder defines
// workflows, StepFuncs contain business logic, and LocalRunner provides a fast,
// developer-friendly runtime.
//
// For examples, see the /examples directory or the project README.
package fluxo
