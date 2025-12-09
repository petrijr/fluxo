package fluxo

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/petrijr/fluxo/pkg/api"
	workerpkg "github.com/petrijr/fluxo/pkg/worker"
	"github.com/stretchr/testify/require"
)

// TestSQLiteBundle_DurableAcrossRestart demonstrates that a workflow started
// via the worker/queue combination remains durable across a simulated process
// restart, assuming workflows are re-registered on startup.
func TestSQLiteBundle_DurableAcrossRestart(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	dbPath := filepath.Join(t.TempDir(), "fluxo_bundle.db")
	dsn := "file:" + dbPath + "?_journal=WAL"

	// --- Phase 1: enqueue start-workflow task, no processing yet.

	db1, err := sql.Open("sqlite", dsn)
	require.NoError(t, err)

	bundle1, err := NewSQLiteBundle(db1, workerpkg.Config{
		MaxAttempts: 3,
	})
	require.NoError(t, err)

	// Simple workflow: increment input by 1.
	flow := New("async-add-one").
		Step("add-one", func(ctx context.Context, input any) (any, error) {
			n, _ := input.(int)
			return n + 1, nil
		})

	require.NoError(t, flow.Register(bundle1.Engine), "Register should succeed on first engine")

	// Sanity: no instances yet.
	before, err := bundle1.Engine.ListInstances(ctx, api.InstanceListOptions{
		WorkflowName: flow.Name(),
	})
	require.NoError(t, err)
	require.Len(t, before, 0)

	// Enqueue a start-workflow task; this should NOT create an instance yet.
	err = bundle1.Worker.EnqueueStartWorkflow(ctx, flow.Name(), 41)
	require.NoError(t, err)

	mid, err := bundle1.Engine.ListInstances(ctx, api.InstanceListOptions{
		WorkflowName: flow.Name(),
	})
	require.NoError(t, err)
	require.Len(t, mid, 0, "no instances should exist before worker processes the queue")

	// Simulate process crash by closing the DB and discarding bundle1.
	require.NoError(t, db1.Close())

	// --- Phase 2: "restart" with new DB handle and bundle.

	db2, err := sql.Open("sqlite", dsn)
	require.NoError(t, err)
	defer db2.Close()

	bundle2, err := NewSQLiteBundle(db2, workerpkg.Config{
		MaxAttempts: 3,
	})
	require.NoError(t, err)

	// IMPORTANT: workflow definitions are in-memory only.
	// We must re-register workflows on each process start.
	require.NoError(t, flow.Register(bundle2.Engine), "Register should succeed on second engine")

	// On startup, it's safe to recover any stuck instances.
	_, err = RecoverStuckInstances(ctx, bundle2.Engine)
	require.NoError(t, err)

	// Process one task from the durable queue; this should start and complete
	// the workflow using the re-registered definition.
	processed, err := bundle2.Worker.ProcessOne(ctx)
	require.NoError(t, err)
	require.True(t, processed, "expected one task to be processed")

	after, err := bundle2.Engine.ListInstances(ctx, api.InstanceListOptions{
		WorkflowName: flow.Name(),
	})
	require.NoError(t, err)
	require.Len(t, after, 1, "expected a single workflow instance after processing")

	inst := after[0]
	require.Equal(t, StatusCompleted, inst.Status)
	require.Equal(t, 42, inst.Output, "expected async-add-one(41) == 42")
}
