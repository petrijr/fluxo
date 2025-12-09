// Package worker provides the background worker implementation used to drive
// fluxo workflows forward.
//
// Workers consume tasks from a task queue, execute workflow steps using an
// engine, and apply retry and timeout policies. They are designed to be
// lightweight and easy to embed in existing services, and they can be scaled
// horizontally for higher throughput.
//
// Most applications construct workers via helper functions in the fluxo
// package, which wire engines, queues, and observers together with sensible
// defaults.
//
// # Worker Responsibilities
//
// A worker is responsible for:
//
//   - Polling a task queue for pending work
//   - Dispatching tasks to the appropriate workflow engine
//   - Executing step functions deterministically
//   - Handling timeouts and retry policies
//   - Reporting metrics and lifecycle events via observers
//
// Workers are long-lived components that typically run in dedicated
// goroutines or processes. Multiple workers can safely operate on the same
// queue to scale processing.
//
// # Configuration
//
// Workers are configurable through options and configuration structures,
// allowing callers to control:
//
//   - Concurrency (number of parallel task handlers)
//   - Queue polling behavior (intervals, batch sizes)
//   - Timeouts and retry/backoff strategies
//   - Graceful shutdown behavior
//
// The fluxo package exposes convenience constructors for creating workers
// with default settings, while the worker package provides the underlying
// types for more advanced scenarios.
//
// # Integration with Engine and Queues
//
// Workers are decoupled from any particular persistence backend. They rely on
// interfaces provided by the engine and task queue layers:
//
//   - The engine encapsulates workflow state and step execution.
//   - The task queue provides delivery of tasks to be performed.
//
// Different backends (e.g. in-memory, SQLite, Postgres, Redis, MongoDB) can
// be plugged in through matching queue implementations. This allows workers
// to be reused across different storage technologies.
//
// # Observability
//
// Worker lifecycle and activity are exposed through observers. A worker can
// report events such as:
//
//   - Task started / completed / failed
//   - Step execution durations
//   - Retry attempts and final failures
//
// These events can be logged, exported as metrics, or integrated with
// monitoring systems as needed.
//
// # Usage
//
// Most users should create workers via the fluxo package, which exposes a
// simplified API for common cases. The worker package is useful when
// implementing custom worker behavior, new queue backends, or extending
// observability.
//
// See the fluxo package documentation and examples for typical usage.
package worker
