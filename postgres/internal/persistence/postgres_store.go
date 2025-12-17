package persistence

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	corep "github.com/petrijr/fluxo/internal/persistence"
	"github.com/petrijr/fluxo/pkg/api"
)

// PostgresInstanceStore is an InstanceStore backed by PostgreSQL.
//
// It expects an *sql.DB that uses a PostgreSQL driver (for example,
// "github.com/jackc/pgx/v5/stdlib" or "github.com/lib/pq").
//
// The caller is responsible for:
//   - importing the driver for its side effects, e.g.:
//     _ "github.com/jackc/pgx/v5/stdlib"
//   - providing a DSN via sql.Open.
type PostgresInstanceStore struct {
	db *sql.DB
}

// Ensure PostgresInstanceStore implements InstanceStore.
var _ corep.InstanceStore = (*PostgresInstanceStore)(nil)

// NewPostgresInstanceStore initializes the required schema in the given
// database and returns a new PostgresInstanceStore.
func NewPostgresInstanceStore(db *sql.DB) (*PostgresInstanceStore, error) {
	s := &PostgresInstanceStore{db: db}
	if err := s.initSchema(); err != nil {
		return nil, err
	}
	return s, nil
}

func (p *PostgresInstanceStore) initSchema() error {
	_, err := p.db.Exec(`
		CREATE TABLE IF NOT EXISTS instances (
			id TEXT PRIMARY KEY,
			workflow_name TEXT NOT NULL,
			workflow_version TEXT NOT NULL DEFAULT 'v1',
			workflow_fingerprint TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL,
			current_step INTEGER NOT NULL,
			input BYTEA,
			output BYTEA,
			step_results BYTEA,
			error TEXT,
			lease_owner TEXT NOT NULL DEFAULT '',
			lease_expires_at BIGINT NOT NULL DEFAULT 0
		);
	`)
	return err
}

func (p *PostgresInstanceStore) SaveInstance(inst *api.WorkflowInstance) error {
	input, err := corep.EncodeValue(inst.Input)
	if err != nil {
		return err
	}

	output, err := corep.EncodeValue(inst.Output)
	if err != nil {
		return err
	}

	stepResults, err := corep.EncodeValue(inst.StepResults)
	if err != nil {
		return err
	}

	errStr := ""
	if inst.Err != nil {
		errStr = inst.Err.Error()
	}

	_, err = p.db.Exec(`
		INSERT INTO instances (id, workflow_name, workflow_version, workflow_fingerprint, status, current_step, input, output, step_results, error, lease_owner, lease_expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`,
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

func (p *PostgresInstanceStore) UpdateInstance(inst *api.WorkflowInstance) error {
	input, err := corep.EncodeValue(inst.Input)
	if err != nil {
		return err
	}

	output, err := corep.EncodeValue(inst.Output)
	if err != nil {
		return err
	}

	stepResults, err := corep.EncodeValue(inst.StepResults)
	if err != nil {
		return err
	}

	errStr := ""
	if inst.Err != nil {
		errStr = inst.Err.Error()
	}

	res, err := p.db.Exec(`
		UPDATE instances
		SET workflow_name        = $1,
		    workflow_version     = $2,
		    workflow_fingerprint = $3,
		    status               = $4,
		    current_step         = $5,
		    input                = $6,
		    output               = $7,
		    step_results         = $8,
		    error                = $9,
		    lease_owner          = $10, 
		    lease_expires_at     = $11
		WHERE id = $12
	`,
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
		return corep.ErrInstanceNotFound
	}

	return nil
}

func (p *PostgresInstanceStore) GetInstance(id string) (*api.WorkflowInstance, error) {
	row := p.db.QueryRow(`
		SELECT id, workflow_name, workflow_version, workflow_fingerprint, status, current_step, input, output, step_results, error, lease_owner, lease_expires_at
		FROM instances
		WHERE id = $1
	`,
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
			return nil, corep.ErrInstanceNotFound
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

	inVal, err := corep.DecodeValue[any](input)
	if err != nil {
		return nil, err
	}
	inst.Input = inVal

	outVal, err := corep.DecodeValue[any](output)
	if err != nil {
		return nil, err
	}
	inst.Output = outVal

	stepResultsVal, err := corep.DecodeValue[map[int]any](stepResults)
	if err != nil {
		return nil, err
	}
	inst.StepResults = stepResultsVal

	if errStr.Valid && errStr.String != "" {
		inst.Err = errors.New(errStr.String)
	}

	return &inst, nil
}

func (p *PostgresInstanceStore) ListInstances(filter corep.InstanceFilter) ([]*api.WorkflowInstance, error) {
	query := `
		SELECT id, workflow_name, workflow_version, workflow_fingerprint, status, current_step, input, output, step_results, error, lease_owner, lease_expires_at
		FROM instances`
	var args []any
	var clauses []string

	if filter.WorkflowName != "" {
		clauses = append(clauses, fmt.Sprintf("workflow_name = $%d", len(args)+1))
		args = append(args, filter.WorkflowName)
	}
	if filter.Status != "" {
		clauses = append(clauses, fmt.Sprintf("status = $%d", len(args)+1))
		args = append(args, string(filter.Status))
	}

	if len(clauses) > 0 {
		query = query + " WHERE " + strings.Join(clauses, " AND ")
	}

	rows, err := p.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var instances []*api.WorkflowInstance

	for rows.Next() {
		var inst api.WorkflowInstance
		var statusStr string
		var input, output, stepResults []byte
		var errStr sql.NullString
		var leaseOwner sql.NullString
		var leaseExpiresAtInt sql.NullInt64
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

		inVal, err := corep.DecodeValue[any](input)
		if err != nil {
			return nil, err
		}
		inst.Input = inVal

		outVal, err := corep.DecodeValue[any](output)
		if err != nil {
			return nil, err
		}
		inst.Output = outVal

		stepResultsVal, err := corep.DecodeValue[map[int]any](stepResults)
		if err != nil {
			return nil, err
		}
		inst.StepResults = stepResultsVal

		if errStr.Valid && errStr.String != "" {
			inst.Err = errors.New(errStr.String)
		}

		// Copy to avoid pointer aliasing
		copied := inst
		instances = append(instances, &copied)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return instances, nil
}

func (p *PostgresInstanceStore) TryAcquireLease(ctx context.Context, instanceID, owner string, ttl time.Duration) (bool, error) {
	now := time.Now()
	expires := now.Add(ttl).UnixNano()
	nowInt := now.UnixNano()

	res, err := p.db.ExecContext(ctx, `
		UPDATE instances
		SET lease_owner = $1, lease_expires_at = $2
		WHERE id = $3
		AND (
			lease_owner = ''
			OR lease_expires_at <= $4
			OR lease_owner = $5
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

func (p *PostgresInstanceStore) RenewLease(ctx context.Context, instanceID, owner string, ttl time.Duration) error {
	expires := time.Now().Add(ttl).UnixNano()
	res, err := p.db.ExecContext(ctx, `
		UPDATE instances
		SET lease_expires_at = $1
		WHERE id = $2 AND lease_owner = $3`,
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

func (p *PostgresInstanceStore) ReleaseLease(ctx context.Context, instanceID, owner string) error {
	res, err := p.db.ExecContext(ctx, `
		UPDATE instances
		SET lease_owner = '', lease_expires_at = 0
		WHERE id = $1 AND (lease_owner = '' OR lease_owner = $2)`,
		instanceID, owner,
	)
	if err != nil {
		return err
	}
	_, _ = res.RowsAffected()
	return nil
}
