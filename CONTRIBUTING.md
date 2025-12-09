# Contributing to Fluxo

Thanks for your interest in contributing! Fluxo is early but production-minded, and contributions are welcome.

---

## 🧱 Philosophy

Fluxo aims to be:

* **Deterministic** – no hidden randomness or non-reproducible behavior
* **Simple** – small API surface, understandable internals
* **Composable** – easy to extend with new backends or worker behaviors
* **Testable** – minimal global state, deterministic tests, clear invariants

Please keep these principles in mind when contributing.

---

## 🛠 Development Setup

Clone the repo:

```sh
git clone https://github.com/petrijr/fluxo
cd fluxo
```

Run tests:

```sh
go test ./...
```

Some backend tests use **testcontainers** (Postgres, Redis, Mongo).
Docker must be running for those tests to execute.

To skip them:

```sh
go test ./... -short
```

---

## 📐 Code Style

Fluxo follows standard Go conventions:

* `go fmt ./...`
* `go vet ./...`
* Names are descriptive but not verbose
* Avoid unnecessary interfaces
* Prefer composition over complexity
* Keep exported API surface small & deliberate

### Steps and Workflows

* All step functions **must be deterministic and idempotent**
* No step should read external clocks except through engine-provided timers
* No goroutines inside step logic

---

## 📦 Project Structure

```
/fluxo            – public API
/pkg/api          – core workflow definitions, step graphs, typed helpers
/pkg/worker       – worker implementation
/internal/engine  – deterministic workflow engine
/internal/store   – persistence backends (in-memory, sqlite, pg, redis, mongo)
/internal/taskqueue – queue backends
/examples         – runnable sample apps
```

`internal/` packages are intentionally not part of the public API contract.

---

## 🧪 Tests

Please include tests for any new behavior:

* Unit tests for pure functions and helpers
* Engine tests for determinism-related changes
* Backend tests for store/queue behavior
* Performance tests where relevant (step overhead goal < 1ms)

Every control-flow primitive must have **at least one integration test**.

---

## 🧩 Adding a New Backend

Backends require two components:

1. **Persistence store** (implements instance storage)
2. **Task queue** (implements at-least-once delivery)

Follow the patterns in:

```
internal/store/sqlite
internal/taskqueue/sqlite
```

Each backend must include:

* Tests
* Container-based integration test (unless unsuitable, e.g. SQLite)
* Documentation updates (`docs/backends.md` when added)

---

## 🔧 Branching Model

* `main` is stable
* PRs should branch from `main`
* Commit messages should reference issues when applicable

Small PRs are always preferred.

---

## 🧰 Before Submitting a PR

Please ensure:

* `go test ./...` passes
* `golangci-lint` (if configured) passes
* Public API changes are documented in README
* Examples compile

---

## 🗣 Reporting Issues

When filing issues, include:

* Reproduction steps
* Workflow definition (minimal case is ideal)
* Backend used
* Engine/worker logs if relevant

Bugs related to determinism, idempotency, or persistence correctness are highest priority.

---

## 🤝 Thank You

Your contributions help make Fluxo a reliable, minimal, practical workflow engine for Go developers everywhere.
