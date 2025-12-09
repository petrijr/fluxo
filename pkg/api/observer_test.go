package api

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"
)

//
// Helpers
//

// testObserver is a simple Observer implementation used to verify fan-out behavior.
type testObserver struct {
	mu sync.Mutex

	starts    int
	completes int
	fails     int

	stepStarts    int
	stepCompletes int

	lastWorkflowStart    *WorkflowInstance
	lastWorkflowComplete *WorkflowInstance
	lastWorkflowFail     struct {
		Inst *WorkflowInstance
		Err  error
	}
	lastStepStart struct {
		Inst      *WorkflowInstance
		StepName  string
		StepIndex int
	}
	lastStepComplete struct {
		Inst      *WorkflowInstance
		StepName  string
		StepIndex int
		Err       error
		Duration  time.Duration
	}
}

func (o *testObserver) OnWorkflowStart(ctx context.Context, inst *WorkflowInstance) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.starts++
	o.lastWorkflowStart = inst
}

func (o *testObserver) OnWorkflowCompleted(ctx context.Context, inst *WorkflowInstance) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.completes++
	o.lastWorkflowComplete = inst
}

func (o *testObserver) OnWorkflowFailed(ctx context.Context, inst *WorkflowInstance, err error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.fails++
	o.lastWorkflowFail.Inst = inst
	o.lastWorkflowFail.Err = err
}

func (o *testObserver) OnStepStart(ctx context.Context, inst *WorkflowInstance, stepName string, stepIndex int) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.stepStarts++
	o.lastStepStart = struct {
		Inst      *WorkflowInstance
		StepName  string
		StepIndex int
	}{inst, stepName, stepIndex}
}

func (o *testObserver) OnStepCompleted(ctx context.Context, inst *WorkflowInstance, stepName string, stepIndex int, err error, d time.Duration) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.stepCompletes++
	o.lastStepComplete = struct {
		Inst      *WorkflowInstance
		StepName  string
		StepIndex int
		Err       error
		Duration  time.Duration
	}{inst, stepName, stepIndex, err, d}
}

// recordingHandler is a minimal slog.Handler that just records log records.
type recordingHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *recordingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return true
}

func (h *recordingHandler) Handle(ctx context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	// Copy to avoid reuse issues.
	cpy := slog.Record{
		Time:    r.Time,
		Level:   r.Level,
		Message: r.Message,
	}
	r.Attrs(func(a slog.Attr) bool {
		cpy.AddAttrs(a)
		return true
	})
	h.records = append(h.records, cpy)
	return nil
}

func (h *recordingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	// Not needed for tests; just return itself.
	return h
}

func (h *recordingHandler) WithGroup(name string) slog.Handler {
	// Not needed for tests.
	return h
}

func attrsToMap(r slog.Record) map[string]any {
	m := make(map[string]any)
	r.Attrs(func(a slog.Attr) bool {
		m[a.Key] = a.Value.Any()
		return true
	})
	return m
}

func newTestInstance() *WorkflowInstance {
	return &WorkflowInstance{
		ID:   "inst-123",
		Name: "wf-test",
	}
}

//
// NoopObserver
//

func TestNoopObserver_DoesNotPanic(t *testing.T) {
	ctx := context.Background()
	inst := newTestInstance()
	var o Observer = NoopObserver{}

	// These calls should simply not panic.
	o.OnWorkflowStart(ctx, inst)
	o.OnWorkflowCompleted(ctx, inst)
	o.OnWorkflowFailed(ctx, inst, errors.New("boom"))
	o.OnStepStart(ctx, inst, "step-1", 0)
	o.OnStepCompleted(ctx, inst, "step-1", 0, nil, time.Second)
}

//
// CompositeObserver
//

func TestNewCompositeObserver_EmptyReturnsNoop(t *testing.T) {
	o := NewCompositeObserver()
	if _, ok := o.(NoopObserver); !ok {
		t.Fatalf("expected NewCompositeObserver() to return NoopObserver, got %T", o)
	}
}

func TestNewCompositeObserver_SingleReturnsThatObserver(t *testing.T) {
	single := &testObserver{}
	o := NewCompositeObserver(single, nil) // include a nil to ensure it is filtered

	if got, ok := o.(*testObserver); !ok || got != single {
		t.Fatalf("expected the single non-nil observer to be returned, got %T (%p)", o, o)
	}
}

func TestNewCompositeObserver_MultipleReturnsComposite(t *testing.T) {
	o1 := &testObserver{}
	o2 := &testObserver{}
	o := NewCompositeObserver(o1, o2)

	if _, ok := o.(*CompositeObserver); !ok {
		t.Fatalf("expected *CompositeObserver, got %T", o)
	}
}

func TestCompositeObserver_ForwardsAllEvents(t *testing.T) {
	ctx := context.Background()
	inst := newTestInstance()

	o1 := &testObserver{}
	o2 := &testObserver{}
	co, ok := NewCompositeObserver(o1, o2).(*CompositeObserver)
	if !ok {
		t.Fatalf("expected *CompositeObserver")
	}

	err := errors.New("step failed")
	co.OnWorkflowStart(ctx, inst)
	co.OnWorkflowCompleted(ctx, inst)
	co.OnWorkflowFailed(ctx, inst, err)
	co.OnStepStart(ctx, inst, "step-1", 1)
	co.OnStepCompleted(ctx, inst, "step-1", 1, err, 2*time.Second)

	for i, o := range []*testObserver{o1, o2} {
		if o.starts != 1 || o.completes != 1 || o.fails != 1 || o.stepStarts != 1 || o.stepCompletes != 1 {
			t.Fatalf("observer %d did not receive all calls: %+v", i+1, o)
		}
		if o.lastWorkflowStart != inst || o.lastWorkflowComplete != inst || o.lastWorkflowFail.Inst != inst {
			t.Fatalf("observer %d instance mismatch", i+1)
		}
		if o.lastWorkflowFail.Err != err {
			t.Fatalf("observer %d fail error mismatch", i+1)
		}
		if o.lastStepStart.StepName != "step-1" || o.lastStepStart.StepIndex != 1 {
			t.Fatalf("observer %d stepStart mismatch: %+v", i+1, o.lastStepStart)
		}
		if o.lastStepComplete.StepName != "step-1" || o.lastStepComplete.StepIndex != 1 ||
			o.lastStepComplete.Err != err || o.lastStepComplete.Duration != 2*time.Second {
			t.Fatalf("observer %d stepComplete mismatch: %+v", i+1, o.lastStepComplete)
		}
	}
}

//
// LoggingObserver
//

func TestNewLoggingObserver_NilLoggerUsesDefault(t *testing.T) {
	o := NewLoggingObserver(nil)
	lo, ok := o.(*LoggingObserver)
	if !ok {
		t.Fatalf("expected *LoggingObserver, got %T", o)
	}
	if lo.Logger == nil {
		t.Fatalf("expected non-nil Logger when created with nil")
	}
}

func TestLoggingObserver_OnWorkflowStart_EmitsInfoLog(t *testing.T) {
	ctx := context.Background()
	inst := newTestInstance()

	h := &recordingHandler{}
	logger := slog.New(h)
	o := NewLoggingObserver(logger)

	o.OnWorkflowStart(ctx, inst)

	if len(h.records) != 1 {
		t.Fatalf("expected 1 log record, got %d", len(h.records))
	}

	rec := h.records[0]
	if rec.Level != slog.LevelInfo {
		t.Fatalf("expected LevelInfo, got %v", rec.Level)
	}
	if rec.Message != "workflow_start" {
		t.Fatalf("expected message workflow_start, got %q", rec.Message)
	}

	attrs := attrsToMap(rec)
	if attrs["workflow"] != inst.Name {
		t.Fatalf("expected workflow=%q, got %v", inst.Name, attrs["workflow"])
	}
	if attrs["instance_id"] != inst.ID {
		t.Fatalf("expected instance_id=%q, got %v", inst.ID, attrs["instance_id"])
	}
}

func TestLoggingObserver_OnStepCompleted_LevelDependsOnError(t *testing.T) {
	ctx := context.Background()
	inst := newTestInstance()

	h := &recordingHandler{}
	logger := slog.New(h)
	o := NewLoggingObserver(logger)

	// success
	o.OnStepCompleted(ctx, inst, "step-ok", 0, nil, time.Second)
	// failure
	err := errors.New("boom")
	o.OnStepCompleted(ctx, inst, "step-fail", 1, err, 2*time.Second)

	if len(h.records) != 2 {
		t.Fatalf("expected 2 log records, got %d", len(h.records))
	}

	successRec := h.records[0]
	failRec := h.records[1]

	if successRec.Level != slog.LevelDebug {
		t.Fatalf("expected success record LevelDebug, got %v", successRec.Level)
	}
	if failRec.Level != slog.LevelError {
		t.Fatalf("expected failure record LevelError, got %v", failRec.Level)
	}
	if successRec.Message != "step_completed" || failRec.Message != "step_completed" {
		t.Fatalf("expected step_completed messages, got %q and %q", successRec.Message, failRec.Message)
	}

	attrs := attrsToMap(failRec)
	if attrs["step"] != "step-fail" {
		t.Fatalf("expected step=step-fail, got %v", attrs["step"])
	}
	if attrs["error"] == nil {
		t.Fatalf("expected error attribute on failure record, got nil")
	}
}

//
// BasicMetrics
//

func TestBasicMetrics_WorkflowCountersAndSnapshot(t *testing.T) {
	var m BasicMetrics

	ctx := context.Background()
	inst := newTestInstance()

	// 3 started, 1 completed, 1 failed -> pending = 1
	m.OnWorkflowStart(ctx, inst)
	m.OnWorkflowStart(ctx, inst)
	m.OnWorkflowStart(ctx, inst)

	m.OnWorkflowCompleted(ctx, inst)
	m.OnWorkflowFailed(ctx, inst, errors.New("fail"))

	snap := m.Snapshot()

	if snap.WorkflowsStarted != 3 {
		t.Fatalf("WorkflowsStarted=%d, want 3", snap.WorkflowsStarted)
	}
	if snap.WorkflowsCompleted != 1 {
		t.Fatalf("WorkflowsCompleted=%d, want 1", snap.WorkflowsCompleted)
	}
	if snap.WorkflowsFailed != 1 {
		t.Fatalf("WorkflowsFailed=%d, want 1", snap.WorkflowsFailed)
	}
	if snap.PendingWorkflows != 1 {
		t.Fatalf("PendingWorkflows=%d, want 1", snap.PendingWorkflows)
	}
	// No step metrics yet.
	if snap.StepsCompleted != 0 {
		t.Fatalf("StepsCompleted=%d, want 0", snap.StepsCompleted)
	}
	if snap.AvgStepDuration != 0 {
		t.Fatalf("AvgStepDuration=%v, want 0", snap.AvgStepDuration)
	}
}

func TestBasicMetrics_OnStepCompleted_SuccessOnlyCountsDuration(t *testing.T) {
	var m BasicMetrics
	ctx := context.Background()
	inst := newTestInstance()

	// two successful steps: 1s and 3s
	m.OnStepCompleted(ctx, inst, "step-1", 0, nil, 1*time.Second)
	m.OnStepCompleted(ctx, inst, "step-2", 1, nil, 3*time.Second)

	// one failing step, should NOT affect step metrics
	err := errors.New("fail")
	m.OnStepCompleted(ctx, inst, "step-3", 2, err, 10*time.Second)

	snap := m.Snapshot()

	if snap.StepsCompleted != 2 {
		t.Fatalf("StepsCompleted=%d, want 2", snap.StepsCompleted)
	}

	wantAvg := 2 * time.Second // (1s + 3s) / 2
	if snap.AvgStepDuration != wantAvg {
		t.Fatalf("AvgStepDuration=%v, want %v", snap.AvgStepDuration, wantAvg)
	}
}

func TestBasicMetrics_SnapshotZeroStepsHasZeroAverage(t *testing.T) {
	var m BasicMetrics
	snap := m.Snapshot()
	if snap.StepsCompleted != 0 {
		t.Fatalf("StepsCompleted=%d, want 0", snap.StepsCompleted)
	}
	if snap.AvgStepDuration != 0 {
		t.Fatalf("AvgStepDuration=%v, want 0", snap.AvgStepDuration)
	}
}
