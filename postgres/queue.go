package postgres

import (
	"database/sql"

	"github.com/petrijr/fluxo"
	pqueue "github.com/petrijr/fluxo/postgres/internal/taskqueue"
)

func NewPostgresQueue(db *sql.DB) (fluxo.Queue, error) {
	return pqueue.NewPostgresQueue(db)
}
