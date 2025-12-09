package api

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// --- SleepStep additional tests ---

func TestSleepStep_FutureDurationWaits(t *testing.T) {
	step := SleepStep(30 * time.Millisecond)
	ctx := context.Background()

	start := time.Now()
	out, err := step(ctx, "payload")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.(string) != "payload" {
		t.Fatalf("expected passthrough payload, got %v", out)
	}
	if elapsed := time.Since(start); elapsed < 25*time.Millisecond {
		t.Fatalf("expected SleepStep to wait at least ~25ms, got %s", elapsed)
	}
}

func TestSleepStep_FutureDurationRespectsCancel(t *testing.T) {
	step := SleepStep(100 * time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(20*time.Millisecond, cancel)

	out, err := step(ctx, 123)
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got err=%v, out=%v", err, out)
	}
}

// --- IfStep degenerate cases ---

func TestIfStep_DegenerateCases(t *testing.T) {
	ctx := context.Background()

	// cond=nil -> passthrough
	step := IfStep(nil, func(ctx context.Context, in any) (any, error) {
		return "then", nil
	}, func(ctx context.Context, in any) (any, error) {
		return "else", nil
	})

	out, err := step(ctx, "x")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.(string) != "x" {
		t.Fatalf("expected passthrough when cond is nil, got %v", out)
	}

	// cond true, thenStep=nil -> passthrough
	step = IfStep(
		func(in any) bool { return true },
		nil,
		func(ctx context.Context, in any) (any, error) {
			return "else", nil
		},
	)

	out, err = step(ctx, "y")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.(string) != "y" {
		t.Fatalf("expected passthrough when thenStep is nil, got %v", out)
	}

	// cond false, elseStep=nil -> passthrough
	step = IfStep(
		func(in any) bool { return false },
		func(ctx context.Context, in any) (any, error) {
			return "then", nil
		},
		nil,
	)

	out, err = step(ctx, "z")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.(string) != "z" {
		t.Fatalf("expected passthrough when elseStep is nil, got %v", out)
	}
}

// --- SwitchStep degenerate cases ---

func TestSwitchStep_SelectorNilAndDefault(t *testing.T) {
	ctx := context.Background()

	// selector=nil, default=nil -> passthrough
	step := SwitchStep(nil, nil, nil)
	out, err := step(ctx, "in")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.(string) != "in" {
		t.Fatalf("expected passthrough with nil selector and default, got %v", out)
	}

	// selector=nil, default non-nil -> default branch runs
	defaultStep := func(ctx context.Context, in any) (any, error) {
		return "default", nil
	}
	step = SwitchStep(nil, map[string]StepFunc{
		"a": func(ctx context.Context, in any) (any, error) { return "A", nil },
	}, defaultStep)

	out, err = step(ctx, "ignored")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.(string) != "default" {
		t.Fatalf("expected default branch, got %v", out)
	}
}

// --- TypedStep / TypedWhile / TypedLoop tests ---

func TestTypedStep_Success(t *testing.T) {
	fn := func(ctx context.Context, in int) (string, error) {
		return fmt.Sprintf("n=%d", in), nil
	}
	step := TypedStep[int, string](fn)

	out, err := step(context.Background(), 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.(string) != "n=5" {
		t.Fatalf("expected n=5, got %v", out)
	}
}

func TestTypedStep_WrongTypeReturnsError(t *testing.T) {
	fn := func(ctx context.Context, in int) (string, error) {
		return fmt.Sprintf("n=%d", in), nil
	}
	step := TypedStep[int, string](fn)

	_, err := step(context.Background(), "not-int")
	if err == nil || !strings.Contains(err.Error(), "TypedStep: expected input of type") {
		t.Fatalf("expected type error, got %v", err)
	}
}

func TestTypedStep_NilInputForPointerType(t *testing.T) {
	fn := func(ctx context.Context, in *int) (string, error) {
		if in == nil {
			return "nil", nil
		}
		return fmt.Sprintf("%d", *in), nil
	}
	step := TypedStep[*int, string](fn)

	out, err := step(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.(string) != "nil" {
		t.Fatalf("expected nil handling, got %v", out)
	}
}

func TestTypedWhile_Success(t *testing.T) {
	step := TypedWhile[int](
		func(n int) bool { return n < 3 },
		func(ctx context.Context, n int) (int, error) {
			return n + 1, nil
		},
	)

	out, err := step(context.Background(), 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.(int) != 3 {
		t.Fatalf("expected 3, got %v", out)
	}
}

func TestTypedWhile_WrongInputTypeStopsLoop(t *testing.T) {
	step := TypedWhile[int](
		func(n int) bool { return true }, // would loop forever for ints
		func(ctx context.Context, n int) (int, error) {
			return n + 1, nil
		},
	)

	// When input is of wrong type, the inner cond returns false,
	// so the WhileStep should immediately return the original input.
	out, err := step(context.Background(), "bad-type")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.(string) != "bad-type" {
		t.Fatalf("expected passthrough of bad-type, got %v", out)
	}
}

func TestTypedLoop_Success(t *testing.T) {
	step := TypedLoop[int](3, func(ctx context.Context, n int) (int, error) {
		return n + 2, nil
	})

	out, err := step(context.Background(), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 1 + 2 + 2 + 2 = 7
	if out.(int) != 7 {
		t.Fatalf("expected 7, got %v", out)
	}
}

func TestTypedLoop_WrongInputTypeReturnsError(t *testing.T) {
	step := TypedLoop[int](2, func(ctx context.Context, n int) (int, error) {
		return n + 1, nil
	})

	_, err := step(context.Background(), "bad-type")
	if err == nil || !strings.Contains(err.Error(), "TypedLoop: expected input of type") {
		t.Fatalf("expected type error, got %v", err)
	}
}
