package engine

import (
	"context"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // PostgreSQL driver
	"github.com/petrijr/fluxo/internal/testutil"
	"github.com/petrijr/fluxo/pkg/api"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func TestMongoDBEngine_SequentialWorkflow(t *testing.T) {
	endpoint := testutil.StartMongoDBContainer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(endpoint))
	if err != nil {
		t.Fatalf("mongo.Connect failed: %v", err)
	}
	t.Cleanup(func() {
		_ = client.Disconnect(context.Background())
	})

	eng := NewMongoEngine(client)

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
		t.Fatalf("unexpected output from MongoDB: %v", inst2.Output)
	}
}
