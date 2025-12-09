package api

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

// --- SleepStep tests ---

func TestSleepStep_ImmediateWhenNonPositive(t *testing.T) {
	step := SleepStep(0)
	ctx := context.Background()
	start := time.Now()
	out, err := step(ctx, 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.(int) != 42 {
		t.Fatalf("expected passthrough 42, got %v", out)
	}
	if time.Since(start) > 10*time.Millisecond {
		t.Fatalf("expected near-immediate return")
	}
}

func TestSleepStep_WaitsAndPassesThrough(t *testing.T) {
	d := 30 * time.Millisecond
	step := SleepStep(d)
	ctx := context.Background()
	start := time.Now()
	out, err := step(ctx, "X")
	took := time.Since(start)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.(string) != "X" {
		t.Fatalf("expected passthrough 'X', got %v", out)
	}
	if took < d-5*time.Millisecond {
		t.Fatalf("sleep returned too early: %v < %v", took, d)
	}
}

func TestSleepStep_RespectsContextCancel(t *testing.T) {
	step := SleepStep(200 * time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	// cancel shortly after
	time.AfterFunc(30*time.Millisecond, cancel)
	out, err := step(ctx, 1)
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got err=%v, out=%v", err, out)
	}
}

// --- SleepUntilStep tests ---

func TestSleepUntilStep_PastDeadlineImmediate(t *testing.T) {
	step := SleepUntilStep(time.Now().Add(-1 * time.Second))
	out, err := step(context.Background(), 7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.(int) != 7 {
		t.Fatalf("expected passthrough 7, got %v", out)
	}
}

func TestSleepUntilStep_FutureDeadlineAndCancel(t *testing.T) {
	dl := time.Now().Add(150 * time.Millisecond)
	step := SleepUntilStep(dl)
	ctx, cancel := context.WithCancel(context.Background())
	// cancel before deadline
	time.AfterFunc(40*time.Millisecond, cancel)
	out, err := step(ctx, "in")
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got err=%v, out=%v", err, out)
	}
}

func TestSleepUntilStep_FutureDeadlineWaits(t *testing.T) {
	dl := time.Now().Add(40 * time.Millisecond)
	step := SleepUntilStep(dl)
	start := time.Now()
	out, err := step(context.Background(), "ok")
	took := time.Since(start)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.(string) != "ok" {
		t.Fatalf("expected passthrough 'ok', got %v", out)
	}
	if took < 35*time.Millisecond {
		t.Fatalf("returned too early: %v", took)
	}
}

// --- WaitForSignalStep tests ---

func TestWaitForSignalStep_FirstCallRequestsWait(t *testing.T) {
	step := WaitForSignalStep("sig")
	_, err := step(context.Background(), "anything")
	if name, ok := IsWaitForSignalError(err); !ok || name != "sig" {
		t.Fatalf("expected wait for 'sig', got ok=%v name=%q err=%v", ok, name, err)
	}
}

func TestWaitForSignalStep_ResumesWithMatchingSignal(t *testing.T) {
	step := WaitForSignalStep("go")
	out, err := step(context.Background(), SignalPayload{Name: "go", Data: 99})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.(int) != 99 {
		t.Fatalf("expected 99, got %v", out)
	}
}

func TestWaitForSignalStep_MismatchedSignalRequestsWait(t *testing.T) {
	step := WaitForSignalStep("need-this")
	_, err := step(context.Background(), SignalPayload{Name: "other", Data: nil})
	if name, ok := IsWaitForSignalError(err); !ok || name != "need-this" {
		t.Fatalf("expected wait for 'need-this', got ok=%v name=%q err=%v", ok, name, err)
	}
}

// --- WaitForAnySignalStep tests ---

func TestWaitForAnySignalStep_EmptyNamesBehavesSafely(t *testing.T) {
	step := WaitForAnySignalStep()
	_, err := step(context.Background(), nil)
	if name, ok := IsWaitForSignalError(err); !ok || name == "" {
		t.Fatalf("expected wait error with some name, got ok=%v name=%q err=%v", ok, name, err)
	}
}

func TestWaitForAnySignalStep_MatchingAndMismatching(t *testing.T) {
	step := WaitForAnySignalStep("a", "b")
	// matching
	out, err := step(context.Background(), SignalPayload{Name: "b", Data: "yes"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	sp, ok := out.(SignalPayload)
	if !ok || sp.Name != "b" || sp.Data.(string) != "yes" {
		t.Fatalf("unexpected payload: %#v", out)
	}
	// mismatching
	_, err = step(context.Background(), SignalPayload{Name: "c", Data: 1})
	if name, ok := IsWaitForSignalError(err); !ok || name != "a" { // primary is first
		t.Fatalf("expected wait for primary 'a', got ok=%v name=%q err=%v", ok, name, err)
	}
}

// --- ParallelStep tests ---

func TestParallelStep_EmptyAndNilChildren(t *testing.T) {
	ctx := context.Background()
	// empty -> passthrough
	empty := ParallelStep()
	out, err := empty(ctx, 5)
	if err != nil || out.(int) != 5 {
		t.Fatalf("expected passthrough 5, got out=%v err=%v", out, err)
	}

	// include nil child and a real child; order preserved
	s := ParallelStep(nil, func(ctx context.Context, in any) (any, error) { return fmt.Sprintf("%v!", in), nil })
	out, err = s(ctx, "X")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	results := out.([]ParallelResult)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Value != "X" || results[1].Value != "X!" {
		t.Fatalf("unexpected results: %#v", results)
	}
}

func TestParallelStep_ErrorPropagation(t *testing.T) {
	ctx := context.Background()
	errBoom := errors.New("boom")
	s := ParallelStep(
		func(ctx context.Context, in any) (any, error) { return nil, errBoom },
		func(ctx context.Context, in any) (any, error) { time.Sleep(50 * time.Millisecond); return "later", nil },
	)
	out, err := s(ctx, 0)
	if err == nil || !errors.Is(err, errBoom) {
		t.Fatalf("expected boom error, got out=%v err=%v", out, err)
	}
}

// --- ParallelMapStep tests ---

func TestParallelMapStep_InputValidationAndEmpty(t *testing.T) {
	fn := func(ctx context.Context, in any) (any, error) { return in, nil }
	step := ParallelMapStep(fn)
	if _, err := step(context.Background(), 123); err == nil {
		t.Fatalf("expected error for non-slice input")
	}
	out, err := step(context.Background(), []int{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := out.([]any); len(got) != 0 {
		t.Fatalf("expected empty result, got %v", got)
	}
}

func TestParallelMapStep_SuccessAndError(t *testing.T) {
	// Map ints to strings; return error on a sentinel value
	fn := func(ctx context.Context, in any) (any, error) {
		v := in.(int)
		if v == 3 {
			return nil, fmt.Errorf("bad: %d", v)
		}
		return fmt.Sprintf("%d", v), nil
	}
	step := ParallelMapStep(fn)

	// success path (no 3)
	out, err := step(context.Background(), []int{1, 2})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := out.([]any)
	if got[0].(string) != "1" || got[1].(string) != "2" {
		t.Fatalf("unexpected mapping: %v", got)
	}

	// error path (contains 3)
	_, err = step(context.Background(), []int{1, 3, 5})
	if err == nil {
		t.Fatalf("expected error from element mapping")
	}
}

// --- Error helper tests ---

func TestIsWaitForSignalError(t *testing.T) {
	err := NewWaitForSignalError("hello")
	name, ok := IsWaitForSignalError(err)
	if !ok || name != "hello" {
		t.Fatalf("expected (hello,true), got (%q,%v)", name, ok)
	}
	_, ok = IsWaitForSignalError(errors.New("nope"))
	if ok {
		t.Fatalf("expected false for non-wait error")
	}
}

// --- Context helper tests ---

type stubEngine struct{}

func (s *stubEngine) RegisterWorkflow(def WorkflowDefinition) error { return nil }
func (s *stubEngine) Run(ctx context.Context, name string, input any) (*WorkflowInstance, error) {
	return nil, nil
}
func (s *stubEngine) GetInstance(ctx context.Context, id string) (*WorkflowInstance, error) {
	return nil, nil
}
func (s *stubEngine) ListInstances(ctx context.Context, opts InstanceListOptions) ([]*WorkflowInstance, error) {
	return nil, nil
}
func (s *stubEngine) Resume(ctx context.Context, id string) (*WorkflowInstance, error) {
	return nil, nil
}
func (s *stubEngine) Signal(ctx context.Context, id string, name string, payload any) (*WorkflowInstance, error) {
	return nil, nil
}
func (s *stubEngine) RecoverStuckInstances(ctx context.Context) (int, error) { return 0, nil }

func TestContextHelpers(t *testing.T) {
	eng := &stubEngine{}
	base := context.Background()
	ctx := WithEngine(base, eng)
	if got := EngineFromContext(ctx); got == nil {
		t.Fatalf("expected engine in context")
	}

	inst := &WorkflowInstance{ID: "i-1"}
	ctx2 := ContextWithInstance(ctx, inst)
	if got := InstanceFromContext(ctx2); got == nil || got.ID != "i-1" {
		t.Fatalf("expected instance in context, got %#v", got)
	}
}
