package fluxo

import (
	"context"
	"database/sql"

	"github.com/petrijr/fluxo/internal/engine"
	"github.com/petrijr/fluxo/pkg/api"
	"github.com/redis/go-redis/v9"
)

// Re-export key types so users don't need to dig into pkg/api.

type (
	Engine              = api.Engine
	WorkflowDefinition  = api.WorkflowDefinition
	WorkflowInstance    = api.WorkflowInstance
	InstanceListOptions = api.InstanceListOptions
	Status              = api.Status
	StepFunc            = api.StepFunc
	ConditionFunc       = api.ConditionFunc
	SelectorFunc        = api.SelectorFunc
	RetryPolicy         = api.RetryPolicy
)

// Re-export status values for convenience.

const (
	StatusPending   = api.StatusPending
	StatusRunning   = api.StatusRunning
	StatusWaiting   = api.StatusWaiting
	StatusFailed    = api.StatusFailed
	StatusCompleted = api.StatusCompleted
)

// Engine constructors
// These wrap the internal/engine package so external callers
// never need to import internal packages.

// NewInMemoryEngine returns an Engine backed entirely by in-memory stores.
func NewInMemoryEngine() Engine {
	return engine.NewInMemoryEngine()
}

// NewSQLiteEngine returns an Engine that persists workflow instances
// in a SQLite database. Workflow definitions are kept in-memory.
func NewSQLiteEngine(db *sql.DB) (Engine, error) {
	return engine.NewSQLiteEngine(db)
}

// NewPostgresEngine returns an Engine that persists instances in PostgreSQL.
func NewPostgresEngine(db *sql.DB) (Engine, error) {
	return engine.NewPostgresEngine(db)
}

// NewRedisEngine returns an Engine that persists instances in Redis.
func NewRedisEngine(client *redis.Client) Engine {
	return engine.NewRedisEngine(client)
}

// Convenience helpers that just forward to the underlying Engine.

// Run runs a registered workflow synchronously.
func Run(ctx context.Context, eng Engine, name string, input any) (*WorkflowInstance, error) {
	return eng.Run(ctx, name, input)
}

// GetInstance fetches an instance by ID.
func GetInstance(ctx context.Context, eng Engine, id string) (*WorkflowInstance, error) {
	return eng.GetInstance(ctx, id)
}

// ListInstances lists workflow instances according to the given options.
func ListInstances(ctx context.Context, eng Engine, opts InstanceListOptions) ([]*WorkflowInstance, error) {
	return eng.ListInstances(ctx, opts)
}

// Resume resumes a previously failed instance.
func Resume(ctx context.Context, eng Engine, id string) (*WorkflowInstance, error) {
	return eng.Resume(ctx, id)
}

// Signal delivers a signal to a waiting instance.
func Signal(ctx context.Context, eng Engine, id string, name string, payload any) (*WorkflowInstance, error) {
	return eng.Signal(ctx, id, name, payload)
}

// RecoverStuckInstances delegates to eng.RecoverStuckInstances.
//
// It is typically called on process startup before starting any workers:
//
//	count, err := fluxo.RecoverStuckInstances(ctx, engine)
func RecoverStuckInstances(ctx context.Context, eng Engine) (int, error) {
	return eng.RecoverStuckInstances(ctx)
}
