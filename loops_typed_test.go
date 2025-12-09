package fluxo

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type typedCounter struct {
	Value int
}

func TestTypedLoopAndTypedWhile(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	eng := NewInMemoryEngine()

	flow := New("typed-loop-while").
		Step("loop-3-times", TypedLoop(3, func(ctx context.Context, s typedCounter) (typedCounter, error) {
			s.Value++
			return s, nil
		})).
		Step("while-less-than-5", TypedWhile(
			func(s typedCounter) bool { return s.Value < 5 },
			func(ctx context.Context, s typedCounter) (typedCounter, error) {
				s.Value++
				return s, nil
			},
		))

	require.NoError(t, flow.Register(eng))

	inst, err := Run(ctx, eng, flow.Name(), typedCounter{})
	require.NoError(t, err)
	require.NotNil(t, inst)
	require.Equal(t, StatusCompleted, inst.Status)

	out, ok := inst.Output.(typedCounter)
	require.True(t, ok, "expected typedCounter output, got %T", inst.Output)
	require.Equal(t, 5, out.Value, "expected final Value 5 (3 from loop + 2 from while)")

	// Run again to assert determinism.
	inst2, err := Run(ctx, eng, flow.Name(), typedCounter{})
	require.NoError(t, err)
	out2, ok := inst2.Output.(typedCounter)
	require.True(t, ok)
	require.Equal(t, 5, out2.Value)
}
