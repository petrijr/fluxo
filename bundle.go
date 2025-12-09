package fluxo

import (
	"database/sql"

	"github.com/petrijr/fluxo/internal/taskqueue"
	workerpkg "github.com/petrijr/fluxo/pkg/worker"
)

// WorkerBundle wires together an Engine, a durable task queue, and a Worker
// that consumes tasks from that queue.
//
// For now, we only provide a SQLite-backed bundle.
type WorkerBundle struct {
	Engine Engine
	Worker *workerpkg.Worker

	// queue is kept unexported for now; it is primarily useful for internal
	// inspection and tests. The public API focuses on Engine and Worker.
	queue taskqueue.Queue
}

// NewSQLiteBundle constructs a durable Engine + Queue + Worker combo sharing
// the same SQLite database. Workflow instances and queued tasks are persisted
// in the provided *sql.DB.
//
// Typical usage:
//
//	db, _ := sql.Open("sqlite", "file:fluxo.db?_journal=WAL")
//	bundle, err := fluxo.NewSQLiteBundle(db, worker.Config{MaxAttempts: 3})
//	// register workflows on bundle.Engine
//	// enqueue work via bundle.Worker
func NewSQLiteBundle(db *sql.DB, cfg workerpkg.Config) (*WorkerBundle, error) {
	eng, err := NewSQLiteEngine(db)
	if err != nil {
		return nil, err
	}

	q, err := taskqueue.NewSQLiteQueue(db)
	if err != nil {
		return nil, err
	}

	w := workerpkg.NewWithConfig(eng, q, cfg)

	return &WorkerBundle{
		Engine: eng,
		Worker: w,
		queue:  q,
	}, nil
}
