package postgres

import (
	"database/sql"

	"github.com/petrijr/fluxo/internal/engine"
	"github.com/petrijr/fluxo/internal/persistence"
	"github.com/petrijr/fluxo/pkg/api"

	pstore "github.com/petrijr/fluxo/postgres/internal/persistence"
)

// NewPostgresEngine returns an Engine that persists instances in PostgreSQL.
func NewPostgresEngine(db *sql.DB) (api.Engine, error) {
	return NewPostgresEngineWithObserver(db, nil)
}

// NewPostgresEngineWithObserver returns a Postgres-backed Engine with the given Observer.
func NewPostgresEngineWithObserver(db *sql.DB, obs api.Observer) (api.Engine, error) {
	inst, err := pstore.NewPostgresInstanceStore(db)
	if err != nil {
		return nil, err
	}
	memWF := persistence.NewInMemoryStore()

	return engine.NewEngineWithConfig(engine.Config{
		Persistence: persistence.Persistence{
			Workflows: memWF,
			Instances: inst,
		},
		Observer: obs,
	}), nil
}
