# Fluxo Design Overview

This document describes the core design of Fluxo: the execution model, data
flow, persistence, workers, and key trade-offs. It is intended for
contributors and advanced users who want to understand how the engine works
internally.

## Goals

Fluxo is designed to:

- Provide a **deterministic workflow engine** that is easy to embed into Go
  services.
- Offer **durable state** when using persistent backends, without requiring
  heavy external infrastructure.
- Support **common workflow patterns** (steps, conditionals, loops, parallel
  branches, timers, signals) with a minimal and composable API.
- Allow **horizontal scaling** via workers and task queues.
- Remain **testable** and **developer-friendly**, with an in-process
  LocalRunner for development and unit tests.

Non-goals:

- Being a full-fledged orchestration platform with UI, multi-tenant
  management, or deployment tooling.
- Providing strong transactional guarantees across arbitrary external systems
  (those must be implemented in user code with idempotent side effects).

## High-Level Architecture

At a high level, Fluxo consists of:

- **Engine** – stores workflow definitions, manages instances and state,
  computes execution plans, and exposes APIs to start and inspect workflows.
- **Persistence Store** – backend-specific implementation for persisting
  workflow instances, history, and timers.
- **Task Queue** – backend-specific implementation for delivering work items
  (tasks) to workers.
- **Worker** – background component that polls the task queue, executes steps
  through the engine, and pushes new tasks.
- **FlowBuilder & API** – declarative library for defining workflows using
  steps and control-flow constructs.
- **LocalRunner** – in-process helper that wires an in-memory engine, queue,
  and worker for development and testing.

The engine is stateless with respect to process memory: all workflow progress
is derived from persisted state and definitions. This allows workers to crash
and restart without losing workflow history when using durable backends.

## Workflow Model

### Definitions

A **workflow definition** is a named, immutable description of a workflow’s
structure. It contains:

- Metadata (name, version, optional description).
- A graph of **steps**, connected by control-flow nodes (sequences, branches,
  loops, parallel forks/joins, etc.).
- Optional type metadata describing input and output shapes.

Definitions are registered with an engine at startup. Instances always refer
to a definition by name/version; the engine never persists “compiled” Go code.

### Instances

A **workflow instance** is a specific execution of a definition. It has:

- A unique identifier.
- A reference to a definition (name + version).
- A current status (e.g. pending, running, completed, failed).
- A persisted state payload (arbitrary JSON-like data).
- A history of completed steps and events (timers fired, signals delivered,
  retries, etc.).

Instances are always read and written via the persistence store; workers do
not keep long-lived in-memory instances between tasks.

## Step Execution & Determinism

### Step Functions

A **step function** is the unit of user-defined work. At the Fluxo level it is
typically expressed as a `StepFunc` with a context and a state handle.

Step functions are expected to be:

- **Deterministic** – given the same input state and external events, they
  should produce the same observable result and next actions.
- **Idempotent** – they may be re-executed if a worker crashes after committing
  step completion but before acknowledging the task.

Fluxo encourages **side-effect isolation**: external effects (e.g. HTTP calls,
database writes) should be designed so they can safely be retried or
de-duplicated.

### Execution Plan

The engine computes the “next step(s)” from a combination of:

- The workflow definition graph.
- The current persisted state.
- The set of completed steps for the instance.
- Pending timers and signals.

For each step to run, the engine enqueues a task into the task queue. Workers
pull tasks and execute the step function, then call back into the engine to
persist resultant state and schedule follow-up work.

This split between planning (engine) and execution (worker) keeps the engine
pure and deterministic and allows multiple workers to coordinate via the
queue.

## Control Flow

Fluxo provides composable control-flow constructs built on top of steps:

- **Sequential steps** – run in order, passing state forward.
- **Conditionals (If / Switch)** – select branches based on state.
- **Parallel branches** – fan-out multiple branches that run independently and
  join when all complete.
- **Parallel maps** – apply a body step over a collection with fan-out/fan-in.
- **Loops (While / counted)** – repeat a body while a condition holds.
- **Timers and sleeps** – delay progress until a time or interval.
- **Signals** – wait for external events from clients or other workflows.

These constructs are expressed in a higher-level FlowBuilder API and compiled
down to internal nodes in the api package. The engine itself only understands
a small set of primitive node types (e.g. “run step”, “branch”, “join”,
“wait for event”), which keeps the runtime simple.

## Persistence & Task Queues

Fluxo separates **persistence of workflow state** from **delivery of tasks**.

### Persistence Store

The persistence store is responsible for:

- Creating and updating workflow instances.
- Recording step completions and history events.
- Managing durable timers (for persistent backends).

Backends include:

- In-memory (non-durable, best suited for development and unit tests).
- SQLite.
- Postgres.
- Redis.
- MongoDB.

Each backend implements a common interface that the engine uses. Durability
and transactional semantics depend on the capabilities of the underlying
database, and are documented separately per backend.

### Task Queue

The task queue is responsible for delivering work items (tasks) to workers.
Each backend typically has a matching queue implementation that uses the same
underlying storage as the persistence store.

Queues support at-least-once delivery semantics:

- A task may be delivered to a worker more than once.
- Workers must therefore treat step execution as idempotent.
- Acknowledgement of tasks happens only after successful step completion and
  state persistence.

The queue does not enforce strict ordering across different workflow
instances; ordering guarantees are limited to what the specific backend can
provide.

## Worker Model

Workers are long-lived components that:

1. Poll the task queue for available tasks.
2. For each task:
    - Decode task metadata (instance ID, step ID, etc.).
    - Ask the engine to execute the corresponding step.
    - Persist results and schedule follow-up tasks via the engine.
3. Acknowledge the task to the queue if the step execution succeeded.

Workers are configurable in terms of:

- Concurrency (number of simultaneous handlers).
- Polling intervals and batch sizes.
- Retry policies and backoff.
- Graceful shutdown behavior.

Multiple workers can safely operate against the same queue to increase
throughput.

## LocalRunner

The **LocalRunner** is a convenience component that wires together:

- An in-memory engine and persistence store.
- An in-memory task queue.
- A worker running in the same process.

It provides methods to:

- Register workflow definitions.
- Start workflows synchronously or asynchronously.
- Send signals to running workflows.
- Await completion or inspect instance state.

LocalRunner is intentionally not crash-durable and is positioned as a tool for
development and testing rather than production use.

## Observability

Fluxo uses an **Observer** abstraction to report lifecycle events and metrics
from engines, workers, and runners. Observers can:

- Log workflow and step transitions.
- Track counts and latencies of steps, retries, and failures.
- Capture snapshots of system health.

The fluxo package includes:

- A logging observer for human-readable logging.
- A basic metrics implementation that stores counters and distributions
  in memory.
- A composite observer to fan-out events to multiple observers.

Applications can provide custom observers to integrate with logging and
monitoring systems such as Prometheus, OpenTelemetry, or proprietary stacks.

## Error Handling & Retries

Errors in step execution are handled through:

- **User-level errors** (returned by step functions) that may be retried
  based on configured policies.
- **Infrastructure errors** (e.g. store or queue failures) that are surfaced
  as transient failures and retried according to worker settings.

Retry policies typically include:

- Maximum number of attempts.
- Backoff strategy (e.g. exponential with jitter).
- Optional classification of retryable vs non-retryable errors.

If a step ultimately fails, the workflow instance is marked as failed, and
the final status is persisted for inspection.

## Backends & Trade-offs

Different backends offer different durability and performance characteristics:

- **In-memory**:
    - Fast, non-durable.
    - Ideal for unit tests and LocalRunner.
- **SQLite**:
    - Embedded durability.
    - Good default choice for single-node deployments.
- **Postgres / Redis / MongoDB**:
    - Networked durability.
    - Suitable for production deployments needing high availability or
      horizontal scaling.

Backend-specific details such as transaction scopes, isolation levels, and
failure modes are documented separately. Fluxo aims to expose a consistent
behavioral model while allowing each backend to use its strengths.

## Limitations & Future Work

Known limitations of the current design include:

- No built-in saga/compensation framework; compensating actions are implemented
  by user-defined steps.
- No global ordering guarantees across workflow instances.
- No built-in multi-tenant isolation beyond what the chosen backend provides.
- Operational aspects (deployment, UI, admin tools) are left to the host
  application.

Future extensions may include:

- More ergonomic saga helpers.
- Additional observability integrations.
- Higher-level DSLs for workflow definitions.
- Optional CLI tools for inspection and debugging.

## Summary

Fluxo’s design focuses on a clear, composable core:

- Engines manage definitions and workflow state.
- Persistence stores and queues provide durable storage and work delivery.
- Workers execute step functions and advance workflows.
- FlowBuilder and the api package define workflows in regular Go code.
- LocalRunner makes development and testing easy.

This separation of concerns keeps the implementation small and testable while
providing enough power to express complex, long-running business workflows.
