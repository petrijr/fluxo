package mongo

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/petrijr/fluxo"
	"github.com/petrijr/fluxo/mongo/internal/testutil"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// TestMongoEngineWithObserverAndBasicMetrics wires together:
//   - a real MongoDB instance (via testcontainers)
//   - the public NewMongoEngineWithObserver constructor
//   - the public builder API (New / FlowBuilder)
//   - the public BasicMetrics implementation and Snapshot
//
// The goal is to verify that, from the perspective of an external user,
// logging/metrics and the Mongo-backed engine can be used end-to-end
// using only the public fluxo package.
func TestMongoEngineWithObserverAndBasicMetrics(t *testing.T) {
	t.Parallel()

	// Spin up a throwaway MongoDB instance for the duration of the test.
	uri := testutil.GetMongoURI(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	require.NoError(t, err, "mongo.Connect failed")
	t.Cleanup(func() {
		_ = client.Disconnect(context.Background())
	})

	// 🔑 Ensure we start with a clean collection so IDs like "wf-1" don't collide
	coll := client.Database("fluxo").Collection("instances")
	_ = coll.Drop(ctx)

	metrics := &fluxo.BasicMetrics{}

	// Use a real slog.Logger, but discard output so tests stay quiet.
	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	observer := fluxo.NewCompositeObserver(
		fluxo.NewLoggingObserver(logger),
		metrics,
	)

	// This is the constructor we want to validate: public, Mongo-backed,
	// and accepting an Observer for logging/metrics.
	engine := NewMongoEngineWithObserver(client, observer)

	// Define a simple 2-step workflow.
	flow := fluxo.New("mongo-metrics-workflow").
		Step("first", func(ctx context.Context, input any) (any, error) {
			time.Sleep(1 * time.Millisecond)
			return "ok", nil
		}).
		Step("second", func(ctx context.Context, input any) (any, error) {
			time.Sleep(1 * time.Millisecond)
			// Just pass through; we don't depend on the value.
			return input, nil
		})

	require.NoError(t, flow.Register(engine), "Register should succeed")

	inst, err := fluxo.Run(ctx, engine, flow.Name(), nil)
	require.NoError(t, err, "Run should succeed")
	require.NotNil(t, inst, "instance should not be nil")
	require.Equal(t, fluxo.StatusCompleted, inst.Status, "workflow should complete successfully")

	snap := metrics.Snapshot()

	require.Equal(t, int64(1), snap.WorkflowsStarted, "expected exactly 1 workflow started")
	require.Equal(t, int64(1), snap.WorkflowsCompleted, "expected exactly 1 workflow completed")
	require.Equal(t, int64(0), snap.WorkflowsFailed, "expected 0 workflow failures")
	require.Equal(t, int64(0), snap.PendingWorkflows, "expected 0 pending workflows")
	require.Equal(t, int64(2), snap.StepsCompleted, "expected 2 steps completed")
	require.Greater(t, snap.AvgStepDuration, time.Duration(0), "expected AvgStepDuration > 0")
}
