package api

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestParallelStep_Success(t *testing.T) {
	step := ParallelStep(
		func(ctx context.Context, input any) (any, error) {
			// e.g. slow path
			time.Sleep(5 * time.Millisecond)
			return input.(int) + 1, nil
		},
		func(ctx context.Context, input any) (any, error) {
			return input.(int) + 2, nil
		},
		func(ctx context.Context, input any) (any, error) {
			return input.(int) + 3, nil
		},
	)

	out, err := step(context.Background(), 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	values, ok := out.([]any)
	if !ok {
		t.Fatalf("expected []any output, got %T", out)
	}
	if len(values) != 3 {
		t.Fatalf("expected 3 results, got %d", len(values))
	}

	want := []int{11, 12, 13}
	for i, v := range values {
		got := v.(int)
		if got != want[i] {
			t.Fatalf("index %d: expected %d, got %d", i, want[i], got)
		}
	}
}

func TestParallelStep_Error(t *testing.T) {
	sentinel := errors.New("boom")

	step := ParallelStep(
		func(ctx context.Context, input any) (any, error) {
			return input, nil
		},
		func(ctx context.Context, input any) (any, error) {
			return nil, sentinel
		},
	)

	out, err := step(context.Background(), 42)
	if err == nil {
		t.Fatalf("expected error, got nil (out=%v)", out)
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error, got %v", err)
	}
}

func TestParallelMapStep_Success(t *testing.T) {
	mapper := func(ctx context.Context, input any) (any, error) {
		return input.(int) * 2, nil
	}

	step := ParallelMapStep(mapper)

	out, err := step(context.Background(), []int{1, 2, 3})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	values, ok := out.([]any)
	if !ok {
		t.Fatalf("expected []any, got %T", out)
	}
	if len(values) != 3 {
		t.Fatalf("expected 3 results, got %d", len(values))
	}

	want := []int{2, 4, 6}
	for i, v := range values {
		got := v.(int)
		if got != want[i] {
			t.Fatalf("index %d: expected %d, got %d", i, want[i], got)
		}
	}
}

func TestParallelMapStep_ContextCancelled(t *testing.T) {
	mapper := func(ctx context.Context, input any) (any, error) {
		// Block until ctx is cancelled.
		<-ctx.Done()
		return nil, ctx.Err()
	}

	step := ParallelMapStep(mapper)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	out, err := step(ctx, []int{1, 2, 3})
	if err == nil {
		t.Fatalf("expected error from cancelled context, got nil (out=%v)", out)
	}
}
