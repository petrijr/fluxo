package api

import (
	"context"
	"testing"
)

func TestIfStepBranchesCorrectly(t *testing.T) {
	thenStep := func(ctx context.Context, input any) (any, error) {
		return "then", nil
	}
	elseStep := func(ctx context.Context, input any) (any, error) {
		return "else", nil
	}

	step := IfStep(
		func(input any) bool { return input.(int) > 0 },
		thenStep,
		elseStep,
	)

	ctx := context.Background()

	out, err := step(ctx, 1)
	if err != nil || out.(string) != "then" {
		t.Fatalf("expected then branch, got %v, err=%v", out, err)
	}

	out, err = step(ctx, -1)
	if err != nil || out.(string) != "else" {
		t.Fatalf("expected else branch, got %v, err=%v", out, err)
	}
}

func TestSwitchStepSelectsBranches(t *testing.T) {
	step := SwitchStep(
		func(input any) string { return input.(string) },
		map[string]StepFunc{
			"a": func(ctx context.Context, input any) (any, error) { return "A", nil },
			"b": func(ctx context.Context, input any) (any, error) { return "B", nil },
		},
		func(ctx context.Context, input any) (any, error) { return "default", nil },
	)

	ctx := context.Background()

	cases := map[string]string{
		"a": "A",
		"b": "B",
		"z": "default",
	}

	for in, want := range cases {
		out, err := step(ctx, in)
		if err != nil {
			t.Fatalf("unexpected error for input %q: %v", in, err)
		}
		if out.(string) != want {
			t.Fatalf("for input %q, expected %q, got %q", in, want, out.(string))
		}
	}
}
