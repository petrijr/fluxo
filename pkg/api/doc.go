// Package api contains the core building blocks used by the fluxo workflow
// engine. It provides the low-level primitives for defining workflows,
// building execution graphs, and observing engine behavior.
//
// Most users interact with the higher-level fluxo package, which re-exports
// selected types and helpers from this package. The api package is intended
// for advanced use cases, custom integrations, or contributors extending the
// engine itself.
//
// # Concepts
//
// The api package centers around a small set of concepts:
//
//   - Workflow definitions
//   - Steps and step functions
//   - Control-flow composition
//   - Observability
//
// These primitives are assembled by the higher-level FlowBuilder API in the
// fluxo package, but can also be used directly where fine-grained control is
// needed.
//
// # Workflow Definitions
//
// A workflow definition describes the structure of a workflow: its name,
// input/output types, and the graph of steps that will be executed.
//
// Definitions are immutable once constructed and are registered with an
// engine before they can be started. The engine uses these definitions to
// compute execution plans and to reconstruct the next step to run when a
// workflow resumes.
//
// # Steps and Step Functions
//
// A step represents a single unit of work in a workflow. Steps are backed by
// step functions, which encapsulate user code. The engine invokes step
// functions deterministically according to the workflow definition.
//
// Step functions are expected to:
//
//   - Be deterministic: same inputs yield the same observable behavior.
//   - Be idempotent: they may be retried if a worker crashes or a task is
//     rescheduled.
//   - Modify workflow state through well-defined mechanisms provided by the
//     engine (e.g. updating variables, scheduling timers, sending signals).
//
// # Control Flow
//
// The api package defines the core control-flow nodes used to build workflows,
// including:
//
//   - Sequential steps
//   - Conditionals (if / switch style branching)
//   - Parallel branches and parallel maps
//   - Loops (while-style and counted loops)
//   - Signal waits and timers
//
// These constructs are exposed in a more ergonomic form from the fluxo package,
// but they are all built from common definitions and step primitives found
// here.
//
// Typed helpers are available to work with strongly typed inputs and outputs
// without forcing the caller to manage serialization manually.
//
// # Observability
//
// The api package defines the Observer interface, which is used by engines,
// workers, and runners to report lifecycle events and metrics.
//
// Observers can be used to:
//
//   - Log workflow and step transitions
//   - Collect metrics (e.g. counts, latencies, error rates)
//   - Integrate with external monitoring systems
//
// The fluxo package exposes ready-made implementations such as logging and
// basic in-memory metrics, along with helpers to combine multiple observers.
//
// # Usage
//
// Most applications should start from the fluxo package, using the FlowBuilder
// and Engine constructors provided there. The api package is useful when you
// need lower-level access, custom composition, or when contributing changes
// to the core engine.
//
// See the fluxo package documentation and the examples directory for
// end-to-end usage.
package api
