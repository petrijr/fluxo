package fluxo

import (
	"context"
	"reflect"
	"testing"
	"time"
)

func TestSleepUntilStep_Past(t *testing.T) {
	step := SleepUntilStep(time.Now().Add(-1 * time.Second))
	out, err := step(context.Background(), 42)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if out.(int) != 42 {
		t.Fatalf("unexpected out: %#v", out)
	}
}

func TestParallelMapStep_Works(t *testing.T) {
	mapper := TypedStep(func(ctx context.Context, x int) (int, error) { return x + 1, nil })
	step := ParallelMapStep(mapper)
	in := []int{1, 2, 3}
	out, err := step(context.Background(), in)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	got := out.([]any)
	want := []any{2, 3, 4}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestTypedWhileAndTypedLoop(t *testing.T) {
	// TypedWhile increments until < 3 is false.
	w := TypedWhile(func(i int) bool { return i < 3 }, func(ctx context.Context, i int) (int, error) { return i + 1, nil })
	out, err := w(context.Background(), 0)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if out.(int) != 3 {
		t.Fatalf("typed while got %v want 3", out)
	}

	// TypedLoop increments twice.
	l := TypedLoop(2, func(ctx context.Context, i int) (int, error) { return i + 1, nil })
	out2, err := l(context.Background(), 10)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if out2.(int) != 12 {
		t.Fatalf("typed loop got %v want 12", out2)
	}
}
