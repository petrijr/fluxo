package taskqueue

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/gob"
	"errors"
	"time"
)

// SQLiteQueue is a persistent task queue implementation backed by SQLite.
// It is safe for concurrent use for our purposes, using simple FIFO semantics
// based on an auto-incrementing id.
type SQLiteQueue struct {
	db           *sql.DB
	pollInterval time.Duration
}

// NewSQLiteQueue initializes the tasks table in the given DB and returns a new queue.
func NewSQLiteQueue(db *sql.DB) (*SQLiteQueue, error) {
	q := &SQLiteQueue{
		db:           db,
		pollInterval: 20 * time.Millisecond,
	}
	if err := q.initSchema(); err != nil {
		return nil, err
	}
	return q, nil
}

func (q *SQLiteQueue) initSchema() error {
	_, err := q.db.Exec(`
		CREATE TABLE IF NOT EXISTS tasks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			type TEXT NOT NULL,
			workflow_name TEXT,
			instance_id TEXT,
			signal_name TEXT,
			payload BLOB,
			enqueued_at INTEGER NOT NULL,
			not_before INTEGER NOT NULL,
			attempts INTEGER NOT NULL
		);
	`)
	return err
}

// Ensure SQLiteQueue implements Queue.
var _ Queue = (*SQLiteQueue)(nil)

func (q *SQLiteQueue) Enqueue(ctx context.Context, t Task) error {
	payloadBytes, err := encodePayload(t.Payload)
	if err != nil {
		return err
	}

	now := time.Now()
	enqueuedAt := now.UnixNano()

	var notBefore int64
	if t.NotBefore.IsZero() {
		notBefore = enqueuedAt
	} else {
		notBefore = t.NotBefore.UnixNano()
	}

	_, err = q.db.ExecContext(ctx, `
		INSERT INTO tasks (type, workflow_name, instance_id, signal_name, payload, enqueued_at, not_before, attempts)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		string(t.Type),
		t.WorkflowName,
		t.InstanceID,
		t.SignalName,
		payloadBytes,
		enqueuedAt,
		notBefore,
		t.Attempts,
	)
	return err
}

func (q *SQLiteQueue) Dequeue(ctx context.Context) (*Task, error) {
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		now := time.Now().UnixNano()

		tx, err := q.db.BeginTx(ctx, nil)
		if err != nil {
			return nil, err
		}

		var (
			id          int64
			typeStr     string
			wfName      sql.NullString
			instanceID  sql.NullString
			signalName  sql.NullString
			payload     []byte
			enqueuedInt int64
			notBefore   int64
			attempts    int
		)

		row := tx.QueryRowContext(ctx, `
			SELECT id, type, workflow_name, instance_id, signal_name, payload, enqueued_at, not_before, attempts
			FROM tasks
			WHERE not_before <= ?
			ORDER BY not_before, id
			LIMIT 1`, now)
		err = row.Scan(&id, &typeStr, &wfName, &instanceID, &signalName, &payload, &enqueuedInt, &notBefore, &attempts)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				_ = tx.Rollback()
				// Nothing available: sleep a bit and retry.
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(q.pollInterval):
					continue
				}
			}
			_ = tx.Rollback()
			return nil, err
		}

		// Delete the row we just claimed.
		if _, err := tx.ExecContext(ctx, `DELETE FROM tasks WHERE id = ?`, id); err != nil {
			_ = tx.Rollback()
			return nil, err
		}

		if err := tx.Commit(); err != nil {
			return nil, err
		}

		decoded, err := decodePayload(payload)
		if err != nil {
			return nil, err
		}

		task := &Task{
			ID:   "", // numeric id is internal; expose empty string for now
			Type: TaskType(typeStr),

			WorkflowName: func() string {
				if wfName.Valid {
					return wfName.String
				}
				return ""
			}(),
			InstanceID: func() string {
				if instanceID.Valid {
					return instanceID.String
				}
				return ""
			}(),
			SignalName: func() string {
				if signalName.Valid {
					return signalName.String
				}
				return ""
			}(),
			Payload:    decoded,
			EnqueuedAt: time.Unix(0, enqueuedInt),
			NotBefore:  time.Unix(0, notBefore),
			Attempts:   attempts,
		}

		return task, nil
	}
}

func (q *SQLiteQueue) Len() int {
	var n int
	err := q.db.QueryRow(`SELECT COUNT(*) FROM tasks`).Scan(&n)
	if err != nil {
		return 0
	}
	return n
}

// encodePayload serializes arbitrary Go values using encoding/gob.
// Callers must ensure that values are gob-encodable and that their
// concrete types have been registered with gob.Register where needed.
func encodePayload(v any) ([]byte, error) {
	if v == nil {
		return nil, nil
	}
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	var iv = v
	if err := enc.Encode(&iv); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// decodePayload deserializes gob-encoded data back into an `any`.
func decodePayload(data []byte) (any, error) {
	if len(data) == 0 {
		return nil, nil
	}
	buf := bytes.NewBuffer(data)
	dec := gob.NewDecoder(buf)
	var iv any
	if err := dec.Decode(&iv); err != nil {
		return nil, err
	}
	return iv, nil
}
