package persistence

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/petrijr/fluxo/pkg/api"
)

// SQLiteInstanceStore is an InstanceStore backed by SQLite.
//
// It expects an *sql.DB that uses a SQLite driver (for example,
// "modernc.org/sqlite"). The caller is responsible for importing
// the driver, e.g.:
//
//	import _ "modernc.org/sqlite"
type SQLiteInstanceStore struct {
	db *sql.DB
}

// Ensure SQLiteInstanceStore implements InstanceStore.
var _ InstanceStore = (*SQLiteInstanceStore)(nil)

// NewSQLiteInstanceStore initializes the required schema in the given
// database and returns a new SQLiteInstanceStore.
func NewSQLiteInstanceStore(db *sql.DB) (*SQLiteInstanceStore, error) {
	s := &SQLiteInstanceStore{db: db}
	if err := s.initSchema(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *SQLiteInstanceStore) initSchema() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS instances (
			id TEXT PRIMARY KEY,
			workflow_name TEXT NOT NULL,
			workflow_version TEXT NOT NULL DEFAULT 'v1',
			workflow_fingerprint TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL,
			current_step INTEGER NOT NULL,
			input BLOB,
			output BLOB,
			step_results BLOB,
			error TEXT,
			lease_owner TEXT NOT NULL DEFAULT '',
			lease_expires_at INTEGER NOT NULL DEFAULT 0
		);`,
	)
	return err
}

func (s *SQLiteInstanceStore) SaveInstance(inst *api.WorkflowInstance) error {
	input, err := EncodeValue(inst.Input)
	if err != nil {
		return err
	}

	output, err := EncodeValue(inst.Output)
	if err != nil {
		return err
	}

	stepResults, err := EncodeValue(inst.StepResults)
	if err != nil {
		return err
	}

	errStr := ""
	if inst.Err != nil {
		errStr = inst.Err.Error()
	}

	_, err = s.db.Exec(`
		INSERT INTO instances (id, workflow_name, workflow_version, workflow_fingerprint, status, current_step, input, output, step_results, error, lease_owner, lease_expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		inst.ID,
		inst.Name,
		inst.Version,
		inst.Fingerprint,
		string(inst.Status),
		inst.CurrentStep,
		input,
		output,
		stepResults,
		errStr,
		inst.LeaseOwner,
		inst.LeaseExpiresAt.UnixNano(),
	)
	return err
}

func (s *SQLiteInstanceStore) UpdateInstance(inst *api.WorkflowInstance) error {
	input, err := EncodeValue(inst.Input)
	if err != nil {
		return err
	}

	output, err := EncodeValue(inst.Output)
	if err != nil {
		return err
	}

	stepResults, err := EncodeValue(inst.StepResults)
	if err != nil {
		return err
	}

	errStr := ""
	if inst.Err != nil {
		errStr = inst.Err.Error()
	}

	res, err := s.db.Exec(`
		UPDATE instances
		SET workflow_name = ?, workflow_version = ?, workflow_fingerprint = ?, status = ?, current_step = ?, input = ?, output = ?, step_results = ?, error = ?, lease_owner = ?, lease_expires_at = ?
		WHERE id = ?`,
		inst.Name,
		inst.Version,
		inst.Fingerprint,
		string(inst.Status),
		inst.CurrentStep,
		input,
		output,
		stepResults,
		errStr,
		inst.LeaseOwner,
		inst.LeaseExpiresAt.UnixNano(),
		inst.ID,
	)
	if err != nil {
		return err
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrInstanceNotFound
	}

	return nil
}

func (s *SQLiteInstanceStore) GetInstance(id string) (*api.WorkflowInstance, error) {
	row := s.db.QueryRow(`
		SELECT id, workflow_name, workflow_version, workflow_fingerprint, status, current_step, input, output, step_results, error, lease_owner, lease_expires_at
		FROM instances
		WHERE id = ?`,
		id,
	)

	var inst api.WorkflowInstance
	var statusStr string
	var input, output, stepResults []byte
	var errStr sql.NullString
	var leaseOwner sql.NullString
	var leaseExpiresAtInt sql.NullInt64
	var currentStep int

	if err := row.Scan(&inst.ID, &inst.Name, &inst.Version, &inst.Fingerprint, &statusStr, &currentStep, &input, &output, &stepResults, &errStr, &leaseOwner, &leaseExpiresAtInt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrInstanceNotFound
		}
		return nil, err
	}

	inst.Status = api.Status(statusStr)
	inst.CurrentStep = currentStep
	if leaseOwner.Valid {
		inst.LeaseOwner = leaseOwner.String
	}
	if leaseExpiresAtInt.Valid && leaseExpiresAtInt.Int64 > 0 {
		inst.LeaseExpiresAt = time.Unix(0, leaseExpiresAtInt.Int64)
	}

	inVal, err := DecodeValue[any](input)
	if err != nil {
		return nil, err
	}
	inst.Input = inVal

	outVal, err := DecodeValue[any](output)
	if err != nil {
		return nil, err
	}
	inst.Output = outVal

	stepResultsVal, err := DecodeValue[map[int]any](stepResults)
	if err != nil {
		return nil, err
	}
	inst.StepResults = stepResultsVal

	if errStr.Valid && errStr.String != "" {
		inst.Err = errors.New(errStr.String)
	}

	return &inst, nil
}

func (s *SQLiteInstanceStore) ListInstances(filter InstanceFilter) ([]*api.WorkflowInstance, error) {
	query := `
		SELECT id, workflow_name, workflow_version, workflow_fingerprint, status, current_step, input, output, step_results, error, lease_owner, lease_expires_at
		FROM instances`
	var args []any
	var clauses []string

	if filter.WorkflowName != "" {
		clauses = append(clauses, "workflow_name = ?")
		args = append(args, filter.WorkflowName)
	}
	if filter.Status != "" {
		clauses = append(clauses, "status = ?")
		args = append(args, string(filter.Status))
	}

	if len(clauses) > 0 {
		query = query + " WHERE " + strings.Join(clauses, " AND ")
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var instances []*api.WorkflowInstance

	for rows.Next() {
		var inst api.WorkflowInstance
		var statusStr string
		var input, output, stepResults []byte
		var leaseOwner sql.NullString
		var leaseExpiresAtInt sql.NullInt64
		var errStr sql.NullString
		var currentStep int

		if err := rows.Scan(&inst.ID, &inst.Name, &inst.Version, &inst.Fingerprint, &statusStr, &currentStep, &input, &output, &stepResults, &errStr, &leaseOwner, &leaseExpiresAtInt); err != nil {
			return nil, err
		}

		inst.Status = api.Status(statusStr)
		inst.CurrentStep = currentStep
		if leaseOwner.Valid {
			inst.LeaseOwner = leaseOwner.String
		}
		if leaseExpiresAtInt.Valid && leaseExpiresAtInt.Int64 > 0 {
			inst.LeaseExpiresAt = time.Unix(0, leaseExpiresAtInt.Int64)
		}

		inVal, err := DecodeValue[any](input)
		if err != nil {
			return nil, err
		}
		inst.Input = inVal

		outVal, err := DecodeValue[any](output)
		if err != nil {
			return nil, err
		}
		inst.Output = outVal

		stepResultsVal, err := DecodeValue[map[int]any](stepResults)
		if err != nil {
			return nil, err
		}
		inst.StepResults = stepResultsVal

		if errStr.Valid && errStr.String != "" {
			inst.Err = errors.New(errStr.String)
		}

		// Note: inst is re-used each loop, so take a copy.
		copied := inst
		instances = append(instances, &copied)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return instances, nil
}

func (s *SQLiteInstanceStore) TryAcquireLease(ctx context.Context, instanceID, owner string, ttl time.Duration) (bool, error) {
	now := time.Now()
	expires := now.Add(ttl).UnixNano()
	nowInt := now.UnixNano()

	res, err := s.db.ExecContext(ctx, `
		UPDATE instances
		SET lease_owner = ?, lease_expires_at = ?
		WHERE id = ?
		AND (
			lease_owner = ''
			OR lease_expires_at <= ?
			OR lease_owner = ?
		)`,
		owner, expires, instanceID, nowInt, owner,
	)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if n == 0 {
		return false, nil
	}
	return true, nil
}

func (s *SQLiteInstanceStore) RenewLease(ctx context.Context, instanceID, owner string, ttl time.Duration) error {
	expires := time.Now().Add(ttl).UnixNano()
	res, err := s.db.ExecContext(ctx, `
		UPDATE instances
		SET lease_expires_at = ?
		WHERE id = ? AND lease_owner = ?`,
		expires, instanceID, owner,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return api.ErrWorkflowInstanceLocked
	}
	return nil
}

func (s *SQLiteInstanceStore) ReleaseLease(ctx context.Context, instanceID, owner string) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE instances
		SET lease_owner = '', lease_expires_at = 0
		WHERE id = ? AND (lease_owner = '' OR lease_owner = ?)`,
		instanceID, owner,
	)
	if err != nil {
		return err
	}
	_, _ = res.RowsAffected()
	return nil
}
