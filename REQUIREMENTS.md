# Lightweight Go Workflow Engine - Requirements Document

## 1. Introduction

This document defines the functional and non-functional requirements for a high-performance, embeddable workflow engine
for the Go ecosystem. The goal is to provide a deterministic, lightweight, developer-friendly orchestration and workflow
system that can be embedded directly into Go services without external dependencies.

---

# 2. MVP Requirements

## 2.1 Core Functional Requirements

### 2.1.1 Workflow Definition

- Developers can define workflows using Go code (state machine style).
- Support for:
    - Sequential steps
    - Parallel steps (fan-out, fan-in)
    - Conditional branches
    - Loops / retries with backoff
- Steps must accept context and typed input/output.
- Workflow definitions must be deterministic.

### 2.1.2 Workflow Execution Engine

- Executes workflows in a deterministic manner.
- Pluggable persistence layer:
    - In-memory (default)
    - SQLite
    - PostgreSQL
    - Redis
    - MongoDB
- Supports durable execution:
    - Resume after crash
    - Resume after server restart
- Workflow instance lifecycle:
    - Start
    - Running
    - Suspended (if waiting on external events)
    - Completed
    - Failed

### 2.1.3 Task Runtime

- Steps run inside the engine (synchronous `Engine.Run`) or via external workers pulling tasks from a queue (asynchronous mode).
- Automatic retry policies:
    - Retry count
    - Exponential backoff
- Idempotency guarantees:
    - Provided automatically via step-level checkpoints.

### 2.1.4 Signals / External Events

- Workflows can pause until receiving a signal.
- Signals are persisted so they are durable.

### 2.1.5 Logging & Instrumentation

- Built-in structured logging via `log/slog` (customizable via the Observer interface).
- Metrics:
    - Workflow completions, failures
    - Step duration
    - Pending workflows

## 2.2 Developer Experience Requirements

- Simple, intuitive API.
- Workflows can be tested with or without persistence.
- Local runner (debug mode).

## 2.3 Non-Functional Requirements

### Performance

- < 1ms overhead per step (excluding user logic).
- < 5MB memory footprint in minimal config.

### Reliability

- Crash-safe state transitions.

### Security

- No remote code execution.
- No external dependencies by default.

---

# 3. Post-MVP Roadmap

## 3.1 Advanced Workflow Features

- Multi-workflow transactions
- Timeout policies for workflows and steps
- Human-in-the-loop steps
- Scheduled workflows (cron-like triggers)
- Versioning of workflow definitions

## 3.2 Observability Enhancements

- Built-in tracing with OpenTelemetry
- Visual workflow debugger
- UI for monitoring workflow state

## 3.3 Cloud / Distributed Mode

- Distributed worker model
- Horizontal scaling
- Sharding of workflows
- Built-in leader election (RAFT-based)

## 3.4 Developer Ecosystem

- Web-based workflow editor (visual DSL)
- Code generation from DSL
- TypeScript/CLI client SDK

## 3.5 Persistence Plugins

- FoundationDB
- BadgerDB
- DynamoDB

## 3.6 Additional Integrations

- Kafka, NATS, Redis Streams, RabbitMQ step triggers
- gRPC / HTTP step invocation
- Terraform provider for infrastructure

## 3.7 Improved reliability
- Event sourcing + snapshotting for persistence.

---

# 4. Target Users

- Backend developers building microservices
- SaaS platforms needing durable orchestration
- ETL/automation developers needing simple workflows
- Teams migrating from heavy systems like Temporal or Camunda

---

# 5. Constraints

- Single binary
- Pure Go implementation
- No heavy dependencies
