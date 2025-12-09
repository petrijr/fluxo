package fluxo

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestInMemoryEngineWithObserverAndBasicMetrics verifies that:
//   - NewInMemoryEngineWithObserver is usable from public API
//   - BasicMetrics sees expected workflow/step counts
//   - The builder and Run helpers work end-to-end without any external infra.
func TestInMemoryEngineWithObserverAndBasicMetrics(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	metrics := &BasicMetrics{}

	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	observer := NewCompositeObserver(
		NewLoggingObserver(logger),
		metrics,
	)

	engine := NewInMemoryEngineWithObserver(observer)

	// Simple 2-step workflow.
	flow := New("inmemory-metrics-workflow").
		Step("first", func(ctx context.Context, input any) (any, error) {
			time.Sleep(1 * time.Millisecond)
			return "ok", nil
		}).
		Step("second", func(ctx context.Context, input any) (any, error) {
			time.Sleep(1 * time.Millisecond)
			return input, nil
		})

	require.NoError(t, flow.Register(engine), "Register should succeed")

	inst, err := Run(ctx, engine, flow.Name(), nil)
	require.NoError(t, err, "Run should succeed")
	require.NotNil(t, inst, "instance should not be nil")
	require.Equal(t, StatusCompleted, inst.Status, "workflow should complete successfully")

	snap := metrics.Snapshot()

	require.Equal(t, int64(1), snap.WorkflowsStarted, "expected exactly 1 workflow started")
	require.Equal(t, int64(1), snap.WorkflowsCompleted, "expected exactly 1 workflow completed")
	require.Equal(t, int64(0), snap.WorkflowsFailed, "expected 0 workflow failures")
	require.Equal(t, int64(0), snap.PendingWorkflows, "expected 0 pending workflows")
	require.Equal(t, int64(2), snap.StepsCompleted, "expected 2 steps completed")
	require.Greater(t, snap.AvgStepDuration, time.Duration(0), "expected AvgStepDuration > 0")
}

// TestInMemoryEngineWithNilLoggerObserver ensures that NewLoggingObserver(nil)
// is safe to use (it should fall back to slog.Default or similar behaviour)
// and that workflows still run successfully.
func TestInMemoryEngineWithNilLoggerObserver(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	metrics := &BasicMetrics{}

	observer := NewCompositeObserver(
		NewLoggingObserver(nil), // should not panic or misbehave
		metrics,
	)

	engine := NewInMemoryEngineWithObserver(observer)

	flow := New("nil-logger-workflow").
		Step("only-step", func(ctx context.Context, input any) (any, error) {
			return "done", nil
		})

	require.NoError(t, flow.Register(engine))

	inst, err := Run(ctx, engine, flow.Name(), nil)
	require.NoError(t, err)
	require.NotNil(t, inst)
	require.Equal(t, StatusCompleted, inst.Status)

	snap := metrics.Snapshot()
	require.Equal(t, int64(1), snap.WorkflowsCompleted)
	require.Equal(t, int64(1), snap.StepsCompleted)
}
