package engine

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib" // PostgreSQL driver
	"github.com/petrijr/fluxo/internal/testutil"
	"github.com/petrijr/fluxo/pkg/api"
)

func TestPostgresEngine_SequentialWorkflow(t *testing.T) {
	endpoint := testutil.StartPostgreSQLContainer(t)
	db, err := sql.Open("pgx", endpoint)
	if err != nil {
		t.Fatalf("sql.Open failed: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	eng, err := NewPostgresEngine(db)
	if err != nil {
		t.Fatalf("NewPostgresEngine failed: %v", err)
	}

	wf := api.WorkflowDefinition{
		Name: "alpha",
		Steps: []api.StepDefinition{
			{
				Name: "step",
				Fn: func(ctx context.Context, input any) (any, error) {
					return "done", nil
				},
			},
		},
	}

	if err := eng.RegisterWorkflow(wf); err != nil {
		t.Fatalf("RegisterWorkflow failed: %v", err)
	}

	inst, err := eng.Run(context.Background(), "alpha", nil)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if inst.Status != api.StatusCompleted {
		t.Fatalf("expected COMPLETED, got %q", inst.Status)
	}

	// Query from persistent storage
	inst2, err := eng.GetInstance(context.Background(), inst.ID)
	if err != nil {
		t.Fatalf("GetInstance failed: %v", err)
	}

	if inst2.Output != "done" {
		t.Fatalf("unexpected output from Postgres: %v", inst2.Output)
	}
}
