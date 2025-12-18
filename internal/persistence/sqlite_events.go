package persistence

import (
	"context"
	"database/sql"
	"time"

	"github.com/petrijr/fluxo/pkg/api"
)

// SQLiteEventStore stores workflow events in SQLite.
type SQLiteEventStore struct {
	db *sql.DB
}

// Ensure SQLiteEventStore implements the interfaces.
var _ EventStore = (*SQLiteEventStore)(nil)

func NewSQLiteEventStore(db *sql.DB) (*SQLiteEventStore, error) {
	s := &SQLiteEventStore{db: db}
	if err := s.initSchema(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *SQLiteEventStore) initSchema() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS workflow_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			instance_id TEXT NOT NULL,
			at INTEGER NOT NULL,
			type TEXT NOT NULL,
			workflow_name TEXT NOT NULL DEFAULT '',
			workflow_version TEXT NOT NULL DEFAULT '',
			step INTEGER NOT NULL DEFAULT -1,
			detail TEXT NOT NULL DEFAULT ''
		);
		CREATE INDEX IF NOT EXISTS idx_workflow_events_instance_id ON workflow_events(instance_id, id);
	`)
	return err
}

func (s *SQLiteEventStore) AppendEvent(ctx context.Context, ev api.WorkflowEvent) error {
	at := ev.At
	if at.IsZero() {
		at = time.Now()
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO workflow_events (instance_id, at, type, workflow_name, workflow_version, step, detail)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		ev.InstanceID,
		at.UnixNano(),
		string(ev.Type),
		ev.WorkflowName,
		ev.WorkflowVersion,
		ev.Step,
		ev.Detail,
	)
	return err
}

func (s *SQLiteEventStore) ListEvents(ctx context.Context, instanceID string) ([]api.WorkflowEvent, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT instance_id, at, type, workflow_name, workflow_version, step, detail
		FROM workflow_events
		WHERE instance_id = ?
		ORDER BY id ASC`, instanceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []api.WorkflowEvent
	for rows.Next() {
		var (
			id     string
			atN    int64
			typ    string
			wname  string
			wver   string
			step   int
			detail string
		)
		if err := rows.Scan(&id, &atN, &typ, &wname, &wver, &step, &detail); err != nil {
			return nil, err
		}
		out = append(out, api.WorkflowEvent{
			InstanceID:      id,
			At:              time.Unix(0, atN),
			Type:            api.EventType(typ),
			WorkflowName:    wname,
			WorkflowVersion: wver,
			Step:            step,
			Detail:          detail,
		})
	}
	return out, rows.Err()
}
