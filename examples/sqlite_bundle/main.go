package main

import (
	"context"
	"database/sql"
	"encoding/gob"
	"fmt"
	"log"
	"time"

	"github.com/petrijr/fluxo"
	_ "modernc.org/sqlite"
)

type Counter struct {
	Value int
}

func main() {
	gob.Register(Counter{})
	ctx := context.Background()

	db, err := sql.Open("sqlite", "file:fluxo_bundle.db?_journal=WAL")
	if err != nil {
		log.Fatalf("sql.Open failed: %v", err)
	}
	defer db.Close()

	// Build a durable engine + queue + worker combo.
	bundle, err := fluxo.NewSQLiteBundle(db, fluxo.Config{
		MaxAttempts: 3,
	})
	if err != nil {
		log.Fatalf("NewSQLiteBundle failed: %v", err)
	}

	// Define a small workflow: increment a counter a few times.
	flow := fluxo.New("bundle-loop").
		Loop("loop-3-times", 3, func(ctx context.Context, input any) (any, error) {
			c, _ := input.(Counter)
			c.Value++
			fmt.Printf("[worker] loop body, value=%d\n", c.Value)
			return c, nil
		})

	if err := flow.Register(bundle.Engine); err != nil {
		log.Fatalf("Register failed: %v", err)
	}

	// Enqueue a start-workflow task. In a real service this might happen in
	// an HTTP handler or some other goroutine.
	if err := bundle.Worker.EnqueueStartWorkflow(ctx, flow.Name(), Counter{}); err != nil {
		log.Fatalf("EnqueueStartWorkflow failed: %v", err)
	}

	// In a real process you would typically run ProcessOne in a loop or in
	// a background goroutine. Here we just process a single task.
	processed, err := bundle.Worker.ProcessOne(ctx)
	if err != nil {
		log.Fatalf("ProcessOne failed: %v", err)
	}
	if !processed {
		log.Println("no task processed")
		return
	}

	// Look up the resulting instance.
	instances, err := fluxo.ListInstances(ctx, bundle.Engine, fluxo.InstanceListOptions{
		WorkflowName: flow.Name(),
	})
	if err != nil {
		log.Fatalf("ListInstances failed: %v", err)
	}
	if len(instances) == 0 {
		log.Println("no instances found")
		return
	}

	inst := instances[0]
	fmt.Printf("workflow completed: id=%s status=%s output=%+v\n", inst.ID, inst.Status, inst.Output)

	// Example of using RecoverStuckInstances on startup:
	//
	//   ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	//   defer cancel()
	//   n, err := fluxo.RecoverStuckInstances(ctx, bundle.Engine)
	//   log.Printf("recovered %d stuck instances (err=%v)", n, err)
	//
	_ = time.Second // keep 'time' import used in this example commentary.
}
