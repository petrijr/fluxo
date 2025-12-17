package taskqueue

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"

	coreq "github.com/petrijr/fluxo/internal/taskqueue"
)

// PostgresQueue implements Queue using a PostgreSQL table.
//
// Schema (created automatically if missing):
//
//	CREATE TABLE IF NOT EXISTS queue_tasks (
//	    id          TEXT PRIMARY KEY,
//	    payload     BYTEA NOT NULL,
//	    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
//	);
//
// The queue is FIFO by created_at.
type PostgresQueue struct {
	db *sql.DB
}

// NewPostgresQueue creates the required schema if needed and returns a Queue.
func NewPostgresQueue(db *sql.DB) (*PostgresQueue, error) {
	q := &PostgresQueue{db: db}
	if err := q.initSchema(); err != nil {
		return nil, err
	}
	return q, nil
}

// Ensure PostgresQueue implements Queue.
var _ coreq.Queue = (*PostgresQueue)(nil)

func (q *PostgresQueue) initSchema() error {
	_, err := q.db.Exec(`
		CREATE TABLE IF NOT EXISTS queue_tasks (
			id         TEXT PRIMARY KEY,
			payload    BYTEA NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		);
	`)
	return err
}

// Enqueue inserts a task into the queue.
func (q *PostgresQueue) Enqueue(ctx context.Context, t coreq.Task) error {
	data, err := coreq.EncodeTask(t)
	if err != nil {
		return err
	}

	// We rely on Task having a stable ID if needed, but the queue itself
	// doesn’t care about the semantics of ID beyond uniqueness.
	_, err = q.db.ExecContext(ctx, `
		INSERT INTO queue_tasks (id, payload)
		VALUES ($1, $2)
	`, t.ID, data)
	return err
}

// Dequeue blocks (with polling) until a task is available or ctx is cancelled.
//
// Implementation notes:
//   - Uses SELECT ... FOR UPDATE SKIP LOCKED in a transaction to safely claim
//     a single row, then DELETEs it in the same transaction.
//   - If no rows are available, sleeps briefly and retries, checking ctx.
func (q *PostgresQueue) Dequeue(ctx context.Context, owner string, leaseTTL time.Duration) (*coreq.Task, error) {
	// Use a reusable timer to avoid allocating a new timer on every idle poll.
	tmr := time.NewTimer(0)
	if !tmr.Stop() {
		select {
		case <-tmr.C:
		default:
		}
	}
	defer tmr.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		tx, err := q.db.BeginTx(ctx, nil)
		if err != nil {
			return nil, err
		}

		var (
			id      string
			payload []byte
		)

		// Lock a single oldest row, if any.
		err = tx.QueryRowContext(ctx, `
			SELECT id, payload
			FROM queue_tasks
			ORDER BY created_at
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		`).Scan(&id, &payload)

		if err != nil {
			_ = tx.Rollback()
			if err == sql.ErrNoRows {
				// Nothing available yet – wait a bit and retry using reusable timer.
				tmr.Reset(100 * time.Millisecond)
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-tmr.C:
				}
				continue
			}
			return nil, err
		}

		// Delete the claimed row within the same transaction.
		if _, err := tx.ExecContext(ctx, `
			DELETE FROM queue_tasks
			WHERE id = $1
		`, id); err != nil {
			_ = tx.Rollback()
			return nil, err
		}

		if err := tx.Commit(); err != nil {
			return nil, err
		}

		task, err := coreq.DecodeTask(payload)
		if err != nil {
			return nil, fmt.Errorf("decode task %q failed: %w", id, err)
		}
		// If Task has an ID field, it should already be set as part of the gob payload.
		return task, nil
	}
}

// Len returns an approximate number of queued tasks.
func (q *PostgresQueue) Len() int {
	var n int
	if err := q.db.QueryRow(`SELECT COUNT(*) FROM queue_tasks`).Scan(&n); err != nil {
		log.Printf("PostgresQueue: Len failed: %v", err)
		return 0
	}
	return n
}

func (q *PostgresQueue) Ack(ctx context.Context, taskID string, owner string) error {
	// Postgres implementation should delete only when acking. If Dequeue already deletes, this is a no-op.
	return nil
}

func (q *PostgresQueue) Nack(ctx context.Context, taskID string, owner string, notBefore time.Time, attempts int) error {
	// Best-effort fallback: re-enqueue is handled by worker if needed.
	return nil
}
