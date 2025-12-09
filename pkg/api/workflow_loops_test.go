package api

import (
	"context"
	"errors"
	"testing"
)

// WhileStep: nil condition or body should be a no-op passthrough.
func TestWhileStep_DegenerateCasesPassthrough(t *testing.T) {
	ctx := context.Background()
	input := 42

	// cond=nil, body non-nil
	bodyCalled := false
	step1 := WhileStep(nil, func(ctx context.Context, in any) (any, error) {
		bodyCalled = true
		return in, nil
	})

	out, err := step1(ctx, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != input {
		t.Fatalf("expected passthrough output=%v, got %v", input, out)
	}
	if bodyCalled {
		t.Fatalf("expected body not to be called when cond is nil")
	}

	// cond non-nil, body=nil
	condCalled := false
	step2 := WhileStep(func(in any) bool {
		condCalled = true
		return true
	}, nil)

	out, err = step2(ctx, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != input {
		t.Fatalf("expected passthrough output=%v, got %v", input, out)
	}
	if condCalled {
		t.Fatalf("expected cond not to be called when body is nil")
	}
}

// WhileStep: loop until condition becomes false, threading current value.
func TestWhileStep_RunsUntilConditionFalse(t *testing.T) {
	ctx := context.Background()

	step := WhileStep(
		func(in any) bool {
			return in.(int) < 3
		},
		func(ctx context.Context, in any) (any, error) {
			return in.(int) + 1, nil
		},
	)

	out, err := step(ctx, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.(int) != 3 {
		t.Fatalf("expected final value 3, got %v", out)
	}
}

// WhileStep: if body returns error, loop should stop and propagate it.
func TestWhileStep_PropagatesBodyError(t *testing.T) {
	ctx := context.Background()
	wantErr := errors.New("boom")

	step := WhileStep(
		func(in any) bool { return true },
		func(ctx context.Context, in any) (any, error) {
			return nil, wantErr
		},
	)

	out, err := step(ctx, "ignored")
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected error %v, got %v", wantErr, err)
	}
	if out != nil {
		t.Fatalf("expected nil output on error, got %v", out)
	}
}

// LoopStep: non-positive times should be a no-op passthrough with no body calls.
func TestLoopStep_NonPositiveTimesPassthrough(t *testing.T) {
	ctx := context.Background()
	input := 10

	calls := 0
	body := func(ctx context.Context, in any) (any, error) {
		calls++
		return in.(int) + 1, nil
	}

	out, err := LoopStep(0, body)(ctx, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != input {
		t.Fatalf("expected passthrough output=%v, got %v", input, out)
	}
	if calls != 0 {
		t.Fatalf("expected body not to be called for times<=0, got %d calls", calls)
	}
}

// LoopStep: executes body exactly N times, threading the value.
func TestLoopStep_RunsFixedNumberOfTimes(t *testing.T) {
	ctx := context.Background()
	input := 1

	calls := 0
	body := func(ctx context.Context, in any) (any, error) {
		calls++
		return in.(int) * 2, nil
	}

	out, err := LoopStep(3, body)(ctx, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 3 {
		t.Fatalf("expected 3 body calls, got %d", calls)
	}
	// 1 * 2 * 2 * 2 = 8
	if out.(int) != 8 {
		t.Fatalf("expected output=8, got %v", out)
	}
}

// LoopStep: if body errors on some iteration, loop stops and propagates error.
func TestLoopStep_PropagatesBodyError(t *testing.T) {
	ctx := context.Background()
	input := 0
	wantErr := errors.New("loop error")

	calls := 0
	body := func(ctx context.Context, in any) (any, error) {
		calls++
		if calls == 2 {
			return nil, wantErr
		}
		return in.(int) + 1, nil
	}

	out, err := LoopStep(5, body)(ctx, input)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected error %v, got %v", wantErr, err)
	}
	if out != nil {
		t.Fatalf("expected nil output on error, got %v", out)
	}
	if calls != 2 {
		t.Fatalf("expected body to be called exactly twice, got %d", calls)
	}
}
