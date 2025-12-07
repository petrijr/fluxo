# `examples/approval` — Purchase Approval Workflow Example

This example demonstrates a realistic asynchronous **purchase approval workflow** built on top of **Fluxo** using:

* **SQLite-backed workflow instances**
* **SQLite-backed persistent task queue**
* **Workers** that execute asynchronous workflow tasks
* **Signals** for mid-workflow interactions
* **Auto-timeouts** via scheduled delayed signal tasks
* **Worker-level retries**
* **Sleep-free backoff** using scheduled tasks (`NotBefore`)
* Gob-encoded payloads for durability

---

## Overview

The workflow simulates a typical business process:

1. **prepare** — log and validate the purchase request
2. **wait-approval** — pause the workflow, waiting for an external approval signal
3. **finalize** — continue either because a manager approved the request *or* because a timeout signal occurred

This example shows two scenarios:

### **Scenario 1 — Automatic Timeout**

If nobody approves the request, the worker schedules a *timeout signal* at `now + DefaultSignalTimeout`
→ When delivered, the workflow resumes and ends in a **timeout path**.

### **Scenario 2 — Manual Approval Before Timeout**

A manager "approves" the request (via an enqueued signal) before timeout.
→ The workflow resumes immediately and ends in an **approval path**.
→ The eventual timeout signal becomes a no-op.

Both scenarios run in a single example program using:

* **SQLiteEngine** for workflow instance state
* **SQLiteQueue** for persistent task scheduling
* **Worker** for executing start/signal tasks asynchronously

---

## Workflow Definition

The example registers a `purchase-approval` workflow:

```go
prepare → wait-approval → finalize
```

Where:

* `wait-approval` is implemented using:

```go
Fn: api.WaitForSignalStep("approve")
```

This pauses the workflow after step 1 and returns a **wait-for-signal** error that Fluxo interprets as a WAITING state.

* `finalize` inspects its input:

```go
switch v := input.(type) {
case api.TimeoutPayload:
return "timeout:" + v.Reason, nil
case string:
return "approved:" + v, nil
}
```

---

## How Auto-Timeout Works

The worker is constructed with:

```go
worker.Config{
DefaultSignalTimeout: 1 * time.Second,
}
```

When the workflow stops at a `WaitForSignalStep("approve")`, the worker automatically schedules:

```
TaskTypeSignal{
    InstanceID: <workflow ID>,
    SignalName: "approve",
    Payload: TimeoutPayload{Reason: "signal timeout"},
    NotBefore: now + 1 second
}
```

The worker later processes this scheduled timeout signal, resuming the workflow.

No `sleep` is used: it is fully event-driven and queue-based.

---

## Running the Example

From the project root:

```bash
go run ./examples/approval
```

You will see output similar to:

```
=== Scenario 1: auto-timeout approval ===
[prepare] request REQ-001 by Alice for 123.45€
[main] instance <id> is WAITING for approval
[worker] processing timeout signal...
[finalize] request timed out: signal timeout
[main] after timeout: instance <id> status=COMPLETED output=timeout:signal timeout

=== Scenario 2: manual approval before timeout ===
[prepare] request REQ-002 by Bob for 777€
[main] instance <id> is WAITING for approval
[main] enqueuing manual approval signal...
[worker] processing manual approval signal...
[finalize] request approved: APPROVED_BY_MANAGER
[main] after manual approval: instance <id> status=COMPLETED output=approved:APPROVED_BY_MANAGER
```

---

## What This Example Demonstrates

✓ How to start workflows asynchronously with a worker
✓ How workflows can *park* on a signal and later *resume*
✓ How automatic timeout logic can be implemented with scheduled signals
✓ How to send manual (human-triggered) signals
✓ How worker-level retries and backoff work
✓ How to use SQLite for durability (instances + tasks)
✓ How gob-encoded payload types must be registered

This example forms a foundation for more realistic systems:

* Approval flows with multiple approvers
* Time-limited offers
* Orchestration of external systems
* SLA-based escalations
* Background processing that must survive restarts
