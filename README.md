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

This project aims to be the **SQLite of workflow orchestration**:  
simple, fast, reliable, and easy to embed.

---

## ✨ Key Features (MVP)

- Deterministic workflows defined in pure Go
- Embeddable into any Go program
- Durable execution with pluggable persistence
- Automatic retries with backoff
- Parallelism, branching, loops
- Step-level idempotency
- Simple developer-friendly API
- In-memory or SQLite persistence
- Signals and external event support
- Structured logging and basic metrics

Example:

```go
flow := wf.New("OnboardUser").
Step("createAccount", createAccount).
Step("sendEmail", sendWelcomeEmail).
Step("activate", waitForActivation)

engine.Run(flow)
```

---

## 🌱 Roadmap (Post-MVP)

- Cron triggers
- Workflow versioning
- Visual debugger
- Distributed worker mode
- OpenTelemetry tracing
- UI dashboard
- Kafka/NATS integrations

---

## 🧩 Philosophy

- **Simple:** learn it in minutes, not weeks
- **Embedded:** no external services required
- **Deterministic:** replay-friendly and safe
- **Fast:** minimal overhead
- **Portable:** single binary

---

## 📦 Installation

```bash
go get github.com/example/go-workflow
```

---

## 📄 License

MIT (to be confirmed)

---

## 🧭 Project Status

Currently in **MVP implementation**.  
Community feedback is welcome—open an issue or proposal!

