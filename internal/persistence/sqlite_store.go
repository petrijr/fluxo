package persistence

import (
	"database/sql"
	"errors"
	"strings"

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
			status TEXT NOT NULL,
			current_step INTEGER NOT NULL,
			input BLOB,
			output BLOB,
			step_results BLOB,
			error TEXT
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
		INSERT INTO instances (id, workflow_name, status, current_step, input, output, step_results, error)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		inst.ID,
		inst.Name,
		string(inst.Status),
		inst.CurrentStep,
		input,
		output,
		stepResults,
		errStr,
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
		SET workflow_name = ?, status = ?, current_step = ?, input = ?, output = ?, step_results = ?, error = ?
		WHERE id = ?`,
		inst.Name,
		string(inst.Status),
		inst.CurrentStep,
		input,
		output,
		stepResults,
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

func (s *SQLiteInstanceStore) GetInstance(id string) (*api.WorkflowInstance, error) {
	row := s.db.QueryRow(`
		SELECT id, workflow_name, status, current_step, input, output, step_results, error
		FROM instances
		WHERE id = ?`,
		id,
	)

	var inst api.WorkflowInstance
	var statusStr string
	var input, output, stepResults []byte
	var errStr sql.NullString
	var currentStep int

	if err := row.Scan(&inst.ID, &inst.Name, &statusStr, &currentStep, &input, &output, &stepResults, &errStr); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrInstanceNotFound
		}
		return nil, err
	}

	inst.Status = api.Status(statusStr)
	inst.CurrentStep = currentStep

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
		SELECT id, workflow_name, status, input, output, step_results, error
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
		var errStr sql.NullString

		if err := rows.Scan(&inst.ID, &inst.Name, &statusStr, &input, &output, &stepResults, &errStr); err != nil {
			return nil, err
		}

		inst.Status = api.Status(statusStr)

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
