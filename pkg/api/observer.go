package api

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"
)

// Observer receives callbacks from the workflow engine for logging and metrics.
//
// Implementations should be fast and non-blocking; heavy work should be done
// asynchronously so as not to delay workflow execution.
type Observer interface {
	// OnWorkflowStart is called once when a workflow instance is first started
	// (Run), before the first step is executed.
	OnWorkflowStart(ctx context.Context, inst *WorkflowInstance)

	// OnWorkflowCompleted is called when a workflow instance successfully
	// reaches StatusCompleted.
	OnWorkflowCompleted(ctx context.Context, inst *WorkflowInstance)

	// OnWorkflowFailed is called when a workflow instance transitions to
	// StatusFailed.
	OnWorkflowFailed(ctx context.Context, inst *WorkflowInstance, err error)

	// OnStepStart is called before invoking a step function.
	// stepIndex is the 0-based index into WorkflowDefinition.Steps.
	OnStepStart(ctx context.Context, inst *WorkflowInstance, stepName string, stepIndex int)

	// OnStepCompleted is called after a step function returns, for both
	// successes and failures (err != nil).
	OnStepCompleted(ctx context.Context, inst *WorkflowInstance, stepName string, stepIndex int, err error, duration time.Duration)
}

// NoopObserver is an Observer that does nothing.
// It is used as the default when no observer is configured.
type NoopObserver struct{}

func (NoopObserver) OnWorkflowStart(ctx context.Context, inst *WorkflowInstance)             {}
func (NoopObserver) OnWorkflowCompleted(ctx context.Context, inst *WorkflowInstance)         {}
func (NoopObserver) OnWorkflowFailed(ctx context.Context, inst *WorkflowInstance, err error) {}
func (NoopObserver) OnStepStart(ctx context.Context, inst *WorkflowInstance, stepName string, idx int) {
}
func (NoopObserver) OnStepCompleted(ctx context.Context, inst *WorkflowInstance, stepName string, idx int, err error, d time.Duration) {
}

// CompositeObserver fans out events to multiple observers.
type CompositeObserver struct {
	observers []Observer
}

// NewCompositeObserver creates an Observer that forwards events to each
// non-nil observer in obs.
func NewCompositeObserver(obs ...Observer) Observer {
	filtered := make([]Observer, 0, len(obs))
	for _, o := range obs {
		if o != nil {
			filtered = append(filtered, o)
		}
	}
	if len(filtered) == 0 {
		return NoopObserver{}
	}
	if len(filtered) == 1 {
		return filtered[0]
	}
	return &CompositeObserver{observers: filtered}
}

func (c *CompositeObserver) OnWorkflowStart(ctx context.Context, inst *WorkflowInstance) {
	for _, o := range c.observers {
		o.OnWorkflowStart(ctx, inst)
	}
}

func (c *CompositeObserver) OnWorkflowCompleted(ctx context.Context, inst *WorkflowInstance) {
	for _, o := range c.observers {
		o.OnWorkflowCompleted(ctx, inst)
	}
}

func (c *CompositeObserver) OnWorkflowFailed(ctx context.Context, inst *WorkflowInstance, err error) {
	for _, o := range c.observers {
		o.OnWorkflowFailed(ctx, inst, err)
	}
}

func (c *CompositeObserver) OnStepStart(ctx context.Context, inst *WorkflowInstance, stepName string, idx int) {
	for _, o := range c.observers {
		o.OnStepStart(ctx, inst, stepName, idx)
	}
}

func (c *CompositeObserver) OnStepCompleted(ctx context.Context, inst *WorkflowInstance, stepName string, idx int, err error, d time.Duration) {
	for _, o := range c.observers {
		o.OnStepCompleted(ctx, inst, stepName, idx, err, d)
	}
}

// LoggingObserver writes structured logs using log/slog.
type LoggingObserver struct {
	Logger *slog.Logger
}

// NewLoggingObserver creates an Observer that logs workflow / step lifecycle
// events using the provided slog.Logger. If logger is nil, slog.Default()
// is used.
func NewLoggingObserver(logger *slog.Logger) Observer {
	if logger == nil {
		logger = slog.Default()
	}
	return &LoggingObserver{Logger: logger}
}

func (o *LoggingObserver) OnWorkflowStart(ctx context.Context, inst *WorkflowInstance) {
	o.Logger.InfoContext(ctx, "workflow_start",
		slog.String("workflow", inst.Name),
		slog.String("instance_id", inst.ID),
	)
}

func (o *LoggingObserver) OnWorkflowCompleted(ctx context.Context, inst *WorkflowInstance) {
	o.Logger.InfoContext(ctx, "workflow_completed",
		slog.String("workflow", inst.Name),
		slog.String("instance_id", inst.ID),
	)
}

func (o *LoggingObserver) OnWorkflowFailed(ctx context.Context, inst *WorkflowInstance, err error) {
	o.Logger.ErrorContext(ctx, "workflow_failed",
		slog.String("workflow", inst.Name),
		slog.String("instance_id", inst.ID),
		slog.Any("error", err),
	)
}

func (o *LoggingObserver) OnStepStart(ctx context.Context, inst *WorkflowInstance, stepName string, idx int) {
	o.Logger.DebugContext(ctx, "step_start",
		slog.String("workflow", inst.Name),
		slog.String("instance_id", inst.ID),
		slog.String("step", stepName),
		slog.Int("step_index", idx),
	)
}

func (o *LoggingObserver) OnStepCompleted(ctx context.Context, inst *WorkflowInstance, stepName string, idx int, err error, d time.Duration) {
	level := slog.LevelDebug
	if err != nil {
		level = slog.LevelError
	}
	o.Logger.Log(ctx, level, "step_completed",
		slog.String("workflow", inst.Name),
		slog.String("instance_id", inst.ID),
		slog.String("step", stepName),
		slog.Int("step_index", idx),
		slog.Duration("duration", d),
		slog.Any("error", err),
	)
}

// BasicMetrics collects simple counters and aggregate step durations.
// It implements Observer, and can be combined with LoggingObserver via
// NewCompositeObserver.
type BasicMetrics struct {
	NoopObserver

	workflowsStarted   atomic.Int64
	workflowsCompleted atomic.Int64
	workflowsFailed    atomic.Int64
	stepsCompleted     atomic.Int64
	totalStepDuration  atomic.Int64 // nanoseconds
}

// BasicMetricsSnapshot is an immutable snapshot of BasicMetrics.
type BasicMetricsSnapshot struct {
	WorkflowsStarted   int64
	WorkflowsCompleted int64
	WorkflowsFailed    int64
	PendingWorkflows   int64

	StepsCompleted  int64
	AvgStepDuration time.Duration
}

func (m *BasicMetrics) OnWorkflowStart(ctx context.Context, inst *WorkflowInstance) {
	m.workflowsStarted.Add(1)
}

func (m *BasicMetrics) OnWorkflowCompleted(ctx context.Context, inst *WorkflowInstance) {
	m.workflowsCompleted.Add(1)
}

func (m *BasicMetrics) OnWorkflowFailed(ctx context.Context, inst *WorkflowInstance, err error) {
	m.workflowsFailed.Add(1)
}

func (m *BasicMetrics) OnStepCompleted(ctx context.Context, inst *WorkflowInstance, stepName string, idx int, err error, d time.Duration) {
	// Only count successful steps for average duration.
	if err == nil {
		m.stepsCompleted.Add(1)
		m.totalStepDuration.Add(d.Nanoseconds())
	}
}

// Snapshot returns a snapshot of the current metrics.
func (m *BasicMetrics) Snapshot() BasicMetricsSnapshot {
	started := m.workflowsStarted.Load()
	completed := m.workflowsCompleted.Load()
	failed := m.workflowsFailed.Load()
	steps := m.stepsCompleted.Load()
	totalNs := m.totalStepDuration.Load()

	var avg time.Duration
	if steps > 0 {
		avg = time.Duration(totalNs / steps)
	}

	return BasicMetricsSnapshot{
		WorkflowsStarted:   started,
		WorkflowsCompleted: completed,
		WorkflowsFailed:    failed,
		PendingWorkflows:   started - completed - failed,
		StepsCompleted:     steps,
		AvgStepDuration:    avg,
	}
}
