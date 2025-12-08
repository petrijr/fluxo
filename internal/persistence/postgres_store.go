package persistence

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

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
var _ InstanceStore = (*PostgresInstanceStore)(nil)

// NewPostgresInstanceStore initializes the required schema in the given
// database and returns a new PostgresInstanceStore.
func NewPostgresInstanceStore(db *sql.DB) (*PostgresInstanceStore, error) {
	s := &PostgresInstanceStore{db: db}
	if err := s.initSchema(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *PostgresInstanceStore) initSchema() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS instances (
			id TEXT PRIMARY KEY,
			workflow_name TEXT NOT NULL,
			status TEXT NOT NULL,
			current_step INTEGER NOT NULL,
			input BYTEA,
			output BYTEA,
			error TEXT
		);
	`)
	return err
}

func (s *PostgresInstanceStore) SaveInstance(inst *api.WorkflowInstance) error {
	input, err := encodeValue(inst.Input)
	if err != nil {
		return err
	}

	output, err := encodeValue(inst.Output)
	if err != nil {
		return err
	}

	errStr := ""
	if inst.Err != nil {
		errStr = inst.Err.Error()
	}

	_, err = s.db.Exec(`
		INSERT INTO instances (id, workflow_name, status, current_step, input, output, error)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`,
		inst.ID,
		inst.Name,
		string(inst.Status),
		inst.CurrentStep,
		input,
		output,
		errStr,
	)
	return err
}

func (s *PostgresInstanceStore) UpdateInstance(inst *api.WorkflowInstance) error {
	input, err := encodeValue(inst.Input)
	if err != nil {
		return err
	}

	output, err := encodeValue(inst.Output)
	if err != nil {
		return err
	}

	errStr := ""
	if inst.Err != nil {
		errStr = inst.Err.Error()
	}

	res, err := s.db.Exec(`
		UPDATE instances
		SET workflow_name = $1,
		    status        = $2,
		    current_step  = $3,
		    input         = $4,
		    output        = $5,
		    error         = $6
		WHERE id = $7
	`,
		inst.Name,
		string(inst.Status),
		inst.CurrentStep,
		input,
		output,
		errStr,
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

func (s *PostgresInstanceStore) GetInstance(id string) (*api.WorkflowInstance, error) {
	row := s.db.QueryRow(`
		SELECT id, workflow_name, status, current_step, input, output, error
		FROM instances
		WHERE id = $1
	`,
		id,
	)

	var inst api.WorkflowInstance
	var statusStr string
	var input, output []byte
	var errStr sql.NullString
	var currentStep int

	if err := row.Scan(&inst.ID, &inst.Name, &statusStr, &currentStep, &input, &output, &errStr); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrInstanceNotFound
		}
		return nil, err
	}

	inst.Status = api.Status(statusStr)
	inst.CurrentStep = currentStep

	inVal, err := decodeValue(input)
	if err != nil {
		return nil, err
	}
	inst.Input = inVal

	outVal, err := decodeValue(output)
	if err != nil {
		return nil, err
	}
	inst.Output = outVal

	if errStr.Valid && errStr.String != "" {
		inst.Err = errors.New(errStr.String)
	}

	return &inst, nil
}

func (s *PostgresInstanceStore) ListInstances(filter InstanceFilter) ([]*api.WorkflowInstance, error) {
	query := `
		SELECT id, workflow_name, status, current_step, input, output, error
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

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var instances []*api.WorkflowInstance

	for rows.Next() {
		var inst api.WorkflowInstance
		var statusStr string
		var input, output []byte
		var errStr sql.NullString
		var currentStep int

		if err := rows.Scan(&inst.ID, &inst.Name, &statusStr, &currentStep, &input, &output, &errStr); err != nil {
			return nil, err
		}

		inst.Status = api.Status(statusStr)
		inst.CurrentStep = currentStep

		inVal, err := decodeValue(input)
		if err != nil {
			return nil, err
		}
		inst.Input = inVal

		outVal, err := decodeValue(output)
		if err != nil {
			return nil, err
		}
		inst.Output = outVal

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
