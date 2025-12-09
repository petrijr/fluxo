package fluxo

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type loopState struct {
	Count int
}

// TestWhileDeterminism verifies that the While combinator produces deterministic
// results when given deterministic condition and body functions.
func TestWhileDeterminism(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	eng := NewInMemoryEngine()

	cond := func(input any) bool {
		st, ok := input.(loopState)
		if !ok {
			return false
		}
		return st.Count < 3
	}

	body := func(ctx context.Context, input any) (any, error) {
		st, ok := input.(loopState)
		if !ok {
			st = loopState{}
		}
		st.Count++
		return st, nil
	}

	flow := New("while-determinism").
		While("loop", cond, body)

	require.NoError(t, flow.Register(eng))

	// Run multiple times to assert deterministic behavior.
	for i := 0; i < 5; i++ {
		inst, err := Run(ctx, eng, flow.Name(), loopState{})
		require.NoError(t, err)
		require.NotNil(t, inst)
		require.Equal(t, StatusCompleted, inst.Status)

		out, ok := inst.Output.(loopState)
		require.True(t, ok, "unexpected output type %T", inst.Output)
		require.Equal(t, 3, out.Count, "expected final count to be 3 on run %d", i)
	}
}

// TestLoopStepIdempotencyWithResume verifies that a Loop step executes only once
// even when a FAILED workflow instance is resumed, thanks to step-level
// idempotency in the engine.
func TestLoopStepIdempotencyWithResume(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	eng := NewInMemoryEngine()

	var bodyCalls int

	body := func(ctx context.Context, input any) (any, error) {
		st, ok := input.(loopState)
		if !ok {
			st = loopState{}
		}
		st.Count++
		bodyCalls++
		return st, nil
	}

	flow := New("loop-idempotency").
		Loop("loop", 3, body).
		Step("fail", func(ctx context.Context, input any) (any, error) {
			return nil, errors.New("boom")
		})

	require.NoError(t, flow.Register(eng))

	inst, err := Run(ctx, eng, flow.Name(), loopState{})
	require.Error(t, err, "expected failure from fail step")
	require.NotNil(t, inst)
	require.Equal(t, StatusFailed, inst.Status)
	require.Equal(t, 3, bodyCalls, "expected loop body to run exactly 3 times on first execution")

	// Resuming should not re-run the loop step; idempotency should reuse the
	// cached result for the loop step and only re-execute the failing step.
	inst2, err := Resume(ctx, eng, inst.ID)
	require.Error(t, err, "expected failure again after resume")
	require.NotNil(t, inst2)
	require.Equal(t, StatusFailed, inst2.Status)
	require.Equal(t, 3, bodyCalls, "expected loop body to still have run exactly 3 times after resume")
}
